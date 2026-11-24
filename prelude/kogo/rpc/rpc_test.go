package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Cloud-Limo/kogo/internal/testdb"
	"github.com/Cloud-Limo/kogo/prelude/kogo"
	khttp "github.com/Cloud-Limo/kogo/prelude/kogo/http"
	"github.com/Cloud-Limo/kogo/rt"
)

// Operations as the emitter writes them, over the test database.

type env struct {
	ready    *sync.WaitGroup // the two first attempts of race, each having read
	attempts *atomic.Int32
	gates    *atomic.Int32 // the runs of gate
	xids     *sync.Map     // the transaction of each gate run, by name, as pg_current_xact_id gives it
}

type Person struct {
	_    struct{} `kogo:"Person"`
	Name string   `kogo:"name"`
}

type Who struct {
	_       struct{} `kogo:"Who"`
	Header  *string  `kogo:"header"`
	Cookie  *string  `kogo:"cookie"`
	Missing *string  `kogo:"missing"`
	Host    *string  `kogo:"host"`
	Address string   `kogo:"address"`
}

func insert(tx kogo.Tx, name string) {
	kogo.Tx_Execute(tx, "insert into person (id, name, email) values ($1, $2, $3)", kogo.NewId(), name, name)
}

func count(tx kogo.Tx) int32 {
	return kogo.Tx_QuerySingle(tx, "select count(*)::int from person where name like 'race %'", func(s kogo.Scan) (n int32) { s(&n); return })
}

// add answers, refuses or panics after it inserts, as its input says.
func add(tx kogo.Tx, _ env, _ Request, p Person) kogo.Either[string, string] {
	insert(tx, p.Name)
	switch {
	case strings.HasPrefix(p.Name, "refused"):
		return kogo.Either_Left[string]{Value: "secret refusal"}
	case strings.HasPrefix(p.Name, "panics"):
		panic("add panics")
	}
	return kogo.Either_Right[string]{Value: "secret answer"}
}

// race counts its rows and inserts one, its first two attempts each
// waiting until both have counted, so that they conflict. A rerun may
// conflict again with the other's attempt.
func race(tx kogo.Tx, e env, _ Request, _ rt.Unit) kogo.Either[string, int32] {
	n := count(tx)
	if e.attempts.Add(1) <= 2 {
		e.ready.Done()
		e.ready.Wait()
	}
	insert(tx, "race "+kogo.NewId().String())
	return kogo.Either_Right[int32]{Value: n}
}

func deadlocked(kogo.Tx, env, Request, rt.Unit) kogo.Either[string, int32] {
	panic(&pgconn.PgError{Code: "40P01"})
}

// login keeps what its own db.tx inserts, and refuses.
func login(db kogo.Db, _ env, _ Request, p Person) kogo.Either[string, rt.Unit] {
	kogo.Db_Tx_Boolean_TxToA(db, nil, func(tx kogo.Tx) rt.Unit { insert(tx, p.Name); return rt.Unit{} })
	return kogo.Either_Left[string]{Value: "secret refusal"}
}

// signIn inserts, then sets the cookie its input names: secure, at
// the path "/", unless the name says otherwise.
func signIn(tx kogo.Tx, _ env, _ Request, p Person) kogo.Either[string, Reply[string]] {
	insert(tx, p.Name)
	c := Cookie{Name: p.Name, Value: "v", Path: "/", MaxAgeSeconds: 60, SameSite: SameSite_Lax, Secure: true}
	switch {
	case strings.HasSuffix(p.Name, "insecure"):
		c.Secure = false
	case strings.HasSuffix(p.Name, "none"):
		c.SameSite, c.Secure = SameSite_None, false
	case strings.HasSuffix(p.Name, "path"):
		c.Path = "/x"
	}
	return kogo.Either_Right[Reply[string]]{Value: Reply[string]{Answer: "in", Cookies: kogo.List[Cookie]{c}}}
}

func who(_ kogo.Db, _ env, r Request, _ rt.Unit) kogo.Either[string, Who] {
	return kogo.Either_Right[Who]{Value: Who{Header: Request_Header(r, "x-who"), Cookie: Request_Cookie(r, "c"),
		Missing: Request_Header(r, "x-none"), Host: Request_Header(r, "host"), Address: Request_ClientAddress(r)}}
}

// ours is the list's check: it refuses or writes as a header says,
// and Postgres refuses the write.
func ours(tx kogo.Tx, _ env, r Request) kogo.Either[string, rt.Unit] {
	switch h := Request_Header(r, "x-check"); {
	case h != nil && *h == "check forged":
		return kogo.Either_Left[string]{Value: "secret check"}
	case h != nil:
		insert(tx, "checked")
	}
	return kogo.Either_Right[rt.Unit]{}
}

type Held struct {
	name string
	tx   kogo.Tx
}

// gate and work each refuse a name of their own. The work
// inserts first: its refusal leaves no row.
func gate(tx kogo.Tx, e env, _ Request, p Person) kogo.Either[string, Held] {
	e.gates.Add(1)
	e.xids.Store(p.Name, kogo.Tx_QuerySingle(tx, "select pg_current_xact_id()::text", func(s kogo.Scan) (x string) { s(&x); return }))
	if strings.HasPrefix(p.Name, "nobody") {
		return kogo.Either_Left[string]{Value: "secret gate"}
	}
	return kogo.Either_Right[Held]{Value: Held{p.Name, tx}}
}

// ended reports whether tx has ended: a statement on it panics.
func ended(tx kogo.Tx) (ended bool) {
	defer func() { ended = recover() != nil }()
	count(tx)
	return
}

func work(e env, h Held, p Person) kogo.Either[string, string] {
	return add(h.tx, e, Request{}, p)
}

// act is work that is an action fun: no transaction is open.
func act(_ env, h Held, p Person) kogo.Either[string, string] {
	if !ended(h.tx) || strings.HasPrefix(p.Name, "refused") {
		return kogo.Either_Left[string]{Value: "secret refusal"}
	}
	return kogo.Either_Right[string]{Value: "secret answer"}
}

// strays is a gate that does what its input names: three things a
// gate must not, and binding a setting, which it may.
func strays(tx kogo.Tx, _ env, _ Request, p Person) kogo.Either[string, Held] {
	kogo.Tx_Exists(tx, map[string]string{"updates": "update person set name = name returning 1", "locks": "select 1 from person for share",
		"counts": "select nextval('ticket')", "binds": "select set_config('kogo.who', 'me', true)"}[p.Name])
	return kogo.Either_Right[Held]{Value: Held{p.Name, tx}}
}

// bound is work that writes, and answers the setting its gate bound.
func bound(_ env, h Held, _ Person) kogo.Either[string, string] {
	insert(h.tx, "bound "+h.name)
	return kogo.Either_Right[string]{Value: kogo.Tx_QuerySingle(h.tx, "select coalesce(current_setting('kogo.who', true), 'unset')", func(s kogo.Scan) (v string) { s(&v); return })}
}

// raceGate counts its runs, reads a row of the table the work races
// on, as a real gate reads the session, and gives its Tx.
func raceGate(tx kogo.Tx, e env, _ Request, _ rt.Unit) kogo.Either[string, kogo.Tx] {
	e.gates.Add(1)
	kogo.Tx_Exists(tx, "select 1 from person limit 1")
	return kogo.Either_Right[kogo.Tx]{Value: tx}
}

func raceWork(e env, tx kogo.Tx, u rt.Unit) kogo.Either[string, int32] {
	return race(tx, e, Request{}, u)
}

// level gives the isolation of the transaction it runs in, as a gate,
// an ungated fun and work do.
func level(tx kogo.Tx, _ env, _ Request, _ rt.Unit) kogo.Either[string, string] {
	return kogo.Either_Right[string]{Value: kogo.Tx_QuerySingle(tx, "select current_setting('transaction_isolation')", func(s kogo.Scan) (l string) { s(&l); return })}
}

func levelWork(_ env, l string, _ rt.Unit) kogo.Either[string, string] {
	return kogo.Either_Right[string]{Value: l}
}

var table = Table[env]{Check: ours, Operations: []Operation[env]{
	Tx("add", true, add), Tx("race", true, race), Tx("deadlocked", true, deadlocked), Tx("deadlockedPlain", false, deadlocked),
	Db("login", login), Db("who", who), Tx("signIn", true, signIn),
	Gated("held", true, gate, work), Gated("heldRace", true, raceGate, raceWork), Outside("rung", true, gate, act),
	Tx("plain", false, level), Tx("serializable", true, level), Gated("plainGated", false, level, levelWork),
	Gated("serializableGated", true, level, levelWork), Outside("plainOutside", false, level, levelWork),
	Outside("serializableOutside", true, level, levelWork),
	Gated("strays", true, strays, bound), Outside("straysOutside", true, strays, act),
	Gated("straysPlain", false, strays, bound),
	Tx("waits", false, waits), Tx("cut", false, cut), Tx("crashed", false, crashed),
	Db("unanswered", unanswered(true)), Db("unansweredThenBug", unanswered(false)),
}}

// unanswered calls a service that is not there and panics with the
// failure, as a program that asked for no Either does; or, not named,
// with a bug of its own.
func unanswered(named bool) func(kogo.Db, env, Request, rt.Unit) kogo.Either[string, bool] {
	return func(kogo.Db, env, Request, rt.Unit) kogo.Either[string, bool] {
		idp, _ := khttp.New(khttp.Service{Address: "http://127.0.0.1:1", Encoding: khttp.JSON, MaxAnswer: 1 << 10,
			Timeout: time.Second, Log: slog.New(slog.DiscardHandler)})
		l := khttp.Http_Ask(idp, khttp.Lookup_String_String("GET", "/me"), nil).(kogo.Either_Left[khttp.HttpFailure])
		if named {
			kogo.Error("idp: " + rt.ToString(l.Value))
		}
		panic("a bug")
	}
}

// waits locks the row "locked", which a test holds from a connection
// of its own, and so waits until that ends or the request does.
func waits(tx kogo.Tx, _ env, _ Request, _ rt.Unit) kogo.Either[string, bool] {
	return kogo.Either_Right[bool]{Value: kogo.Tx_Exists(tx, "select 1 from person where name = 'locked' for update")}
}

// crashed fails as every session does while Postgres restarts after
// a backend of its own crashed.
func crashed(kogo.Tx, env, Request, rt.Unit) kogo.Either[string, bool] {
	panic(&pgconn.PgError{Code: "57P02"})
}

// cut ends its own connection, as Postgres ends one when it stops.
func cut(tx kogo.Tx, _ env, _ Request, _ rt.Unit) kogo.Either[string, bool] {
	return kogo.Either_Right[bool]{Value: kogo.Tx_Exists(tx, "select pg_terminate_backend(pg_backend_pid())")}
}

func TestHandler(t *testing.T) {
	url := os.Getenv("KOGO_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
	}
	t.Setenv("DATABASE_URL", url)
	ctx := context.Background()
	pool, drop := testdb.Open(ctx, "../../../testdata/schema.sql")
	defer drop()
	var log bytes.Buffer
	h := NewHandler(table, pool, env{&sync.WaitGroup{}, &atomic.Int32{}, &atomic.Int32{}, &sync.Map{}}, slog.New(slog.NewJSONHandler(&log, nil)))
	var cookies []string
	var header http.Header
	var mu sync.Mutex
	post := func(method, name, body string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/"+name, strings.NewReader(body))
		r.Header.Set("X-Who", "me")
		if strings.HasPrefix(body, "check ") {
			r.Header.Set("X-Check", body)
		}
		r.AddCookie(&http.Cookie{Name: "c", Value: "crumb"})
		h.ServeHTTP(w, r)
		mu.Lock()
		defer mu.Unlock()
		cookies, header = w.Header().Values("Set-Cookie"), w.Header()
		return w.Code, w.Body.String()
	}
	rows := func(name string) (n int) {
		pool.QueryRow(ctx, "select count(*) from person where name = $1", name).Scan(&n)
		return
	}
	lines := func() (out []map[string]any) {
		for _, l := range strings.Split(strings.TrimSpace(log.String()), "\n") {
			var m map[string]any
			json.Unmarshal([]byte(l), &m)
			out = append(out, m)
		}
		return
	}
	for _, c := range []struct{ method, name, body, want string }{
		{"POST", "add", `{"name":"answers"}`, `200 {"type":"ok","value":"secret answer"}`},
		{"POST", "add", `{"name":"refused"}`, `200 {"type":"refused","message":"secret refusal"}`},
		{"POST", "add", `{"name":"panics"}`, `500 request `},
		{"POST", "add", `{"name":"secret","more":1}`, `400 add: input.more is not a key of Person`},
		// Postgres's text holds no U+0000, so the reader refuses it.
		{"POST", "add", `{"name":"a\u0000secret"}`, `400 add: input.name holds U+0000, which no input or setting may: leave it out`},
		{"POST", "add", `{"name":"asecret"}`, `200 {"type":"ok","value":"secret answer"}`},
		{"POST", "login", `{"name":"kept"}`, `200 {"type":"refused","message":"secret refusal"}`},
		{"POST", "who", `not read`, `200 {"type":"ok","value":{"header":"me","cookie":"crumb","missing":null,"host":"example.com","address":"192.0.2.1"}}`},
		{"POST", "nothing", `{}`, `404 `},
		{"GET", "add", ``, `405 `},
		// The check runs first, before the name is looked up or the
		// method read, and only reads; then the gate and the work.
		{"POST", "add", `check forged`, `403 secret check`},
		{"GET", "add", `check forged`, `403 secret check`},
		{"POST", "nothing", `check forged`, `403 secret check`},
		{"POST", "add", `check writes`, `500 request `},
		{"POST", "held", `{"name":"held"}`, `200 {"type":"ok","value":"secret answer"}`},
		{"POST", "held", `{"name":"nobody held"}`, `200 {"type":"refused","message":"secret gate"}`},
		{"POST", "held", `{"name":"refused held"}`, `200 {"type":"refused","message":"secret refusal"}`},
		{"POST", "rung", `{"name":"rung"}`, `200 {"type":"ok","value":"secret answer"}`},
		{"POST", "rung", `{"name":"nobody rung"}`, `200 {"type":"refused","message":"secret gate"}`},
		{"POST", "rung", `{"name":"refused rung"}`, `200 {"type":"refused","message":"secret refusal"}`},
		// Each row opens the kind of transaction it says.
		{"POST", "plain", ``, `200 {"type":"ok","value":"read committed"}`},
		{"POST", "serializable", ``, `200 {"type":"ok","value":"serializable"}`},
		{"POST", "plainGated", ``, `200 {"type":"ok","value":"read committed"}`},
		{"POST", "serializableGated", ``, `200 {"type":"ok","value":"serializable"}`},
		{"POST", "plainOutside", ``, `200 {"type":"ok","value":"read committed"}`},
		{"POST", "serializableOutside", ``, `200 {"type":"ok","value":"serializable"}`},
		// A gate on a SerializableTx is held to reading by Postgres: a
		// write, a row lock and nextval are each a bug of ours, before
		// an action fun too. A setting it binds is
		// still set in the work, which writes. A gate on a Tx may lock,
		// and its work writes: the one row of "bound locks".
		{"POST", "strays", `{"name":"updates"}`, `500 request `},
		{"POST", "strays", `{"name":"locks"}`, `500 request `},
		{"POST", "strays", `{"name":"counts"}`, `500 request `},
		{"POST", "straysOutside", `{"name":"updates"}`, `500 request `},
		{"POST", "strays", `{"name":"binds"}`, `200 {"type":"ok","value":"me"}`},
		{"POST", "straysPlain", `{"name":"locks"}`, `200 {"type":"ok","value":"`},
	} {
		if status, body := post(c.method, c.name, c.body); !strings.HasPrefix(fmt.Sprint(status, " ", body), c.want) {
			t.Errorf("%s %s %s answers %d %s, want %s", c.method, c.name, c.body, status, body, c.want)
		}
	}
	// A 405 says what is allowed, and every answer its type: a
	// refusal's is JSON, a status's text.
	for _, c := range []struct{ method, name, body, key, want string }{
		{"GET", "add", ``, "Allow", "POST"},
		{"POST", "add", `{"name":"refused"}`, "Content-Type", "application/json"},
		{"POST", "who", ``, "Content-Type", "application/json"},
		{"POST", "add", `check forged`, "Content-Type", "text/plain; charset=utf-8"},
	} {
		if post(c.method, c.name, c.body); header.Get(c.key) != c.want {
			t.Errorf("%s %s %s has %s %q, want %q", c.method, c.name, c.body, c.key, header.Get(c.key), c.want)
		}
	}
	// A cookie HTTP cannot carry or a browser refuses panics, and
	// rolls its Tx back; a cookie both keep is set.
	for _, name := range []string{"a b", "samesite-none", "__Secure-insecure", "__host-insecure", "__Host-path", "__Host-ok"} {
		status, _ := post("POST", "signIn", `{"name":"`+name+`"}`)
		if kept := map[bool]int{true: 1}[name == "__Host-ok"]; status != 500-300*kept || rows(name) != kept || len(cookies) != kept {
			t.Errorf("signIn %q answers %d, keeps %d rows and sets %v", name, status, rows(name), cookies)
		}
	}
	if rows("answers") != 1 || rows("refused") != 0 || rows("panics") != 0 || rows("kept") != 1 {
		t.Error("an answer does not commit, or a refusal or a panic does not roll back, or a Db operation's db.tx does not stay")
	}
	// A gated row: the gate and the work are one transaction, and the
	// gate runs once: 3 runs for held. The gate of work that is an
	// action fun runs once, in a transaction that commits unless the
	// gate refuses: 3 runs for rung, and their transactions, by id,
	// commit for rung and refused rung.
	for name, want := range map[string]int{"held": 1, "refused held": 0, "checked": 0,
		"bound updates": 0, "bound counts": 0, "bound binds": 1, "bound locks": 1} {
		if rows(name) != want {
			t.Errorf("%d rows of %q, want %d: a gated row keeps what it wrote, or a refusal does not roll back", rows(name), name, want)
		}
	}
	for name, want := range map[string]string{"rung": "committed", "refused rung": "committed", "nobody rung": "aborted"} {
		x, _ := h.env.xids.Load(name)
		var got string
		pool.QueryRow(ctx, "select pg_xact_status($1::text::xid8)", x).Scan(&got)
		if got != want {
			t.Errorf("the gate's transaction of %q is %q, want %q: the gate of an action fun commits before the work", name, got, want)
		}
	}
	if n := h.env.gates.Swap(0); n != 6 {
		t.Errorf("the gate ran %d times, want 6", n)
	}
	h.Limit = 50 * time.Millisecond
	for _, name := range []string{"deadlocked", "deadlockedPlain"} {
		if status, body := post("POST", name, ""); status != 503 || !strings.HasPrefix(body, "request ") {
			t.Errorf("%s: reruns past the limit answer %d %s", name, status, body)
		}
	}
	h.Limit = time.Minute
	h.env.ready.Add(2)
	var wg sync.WaitGroup
	answers := make([]string, 2)
	for i := range answers {
		wg.Go(func() { _, answers[i] = post("POST", "race", "") })
	}
	wg.Wait()
	if h.env.attempts.Load() < 3 || !strings.Contains(answers[0]+answers[1], `{"type":"ok","value":0}`) ||
		!strings.Contains(answers[0]+answers[1], `{"type":"ok","value":1}`) {
		t.Errorf("two requests in conflict answer %v after %d attempts", answers, h.env.attempts.Load())
	}
	// A gated row races as a Tx operation does: its transaction is
	// SERIALIZABLE, and each rerun runs the gate again.
	h.env.attempts.Store(0)
	h.env.ready.Add(2)
	for i := range answers {
		wg.Go(func() { _, answers[i] = post("POST", "heldRace", "") })
	}
	wg.Wait()
	if n := h.env.attempts.Load(); n < 3 || h.env.gates.Load() != n || !strings.Contains(answers[0]+answers[1], `{"type":"ok","value":2}`) ||
		!strings.Contains(answers[0]+answers[1], `{"type":"ok","value":3}`) {
		t.Errorf("two gated requests in conflict answer %v after %d attempts and %d runs of the gate", answers, n, h.env.gates.Load())
	}

	// The log: a line for each request, with its id and its count of
	// reruns, and no line for a rerun; a panic under the request's id;
	// no input, answer or refusal.
	ids, reruns, readOnly := map[any]string{}, map[string]float64{}, map[string]bool{}
	for _, l := range lines() {
		switch l["msg"] {
		case "served":
			_, timed := l["duration"]
			n, counted := l["reruns"].(float64)
			if !timed || !counted {
				t.Errorf("a served line has no duration or reruns: %v", l)
			}
			ids[l["request"]] = l["operation"].(string) + " " + l["outcome"].(string)
			reruns[ids[l["request"]]] += n
		case "rerun":
			t.Errorf("a rerun has a line of its own: %v", l)
		case "panic":
			// The check that writes panics with Postgres's refusal,
			// and so does a gate that strays.
			if m := l["message"].(string); strings.Contains(m, "in a read-only transaction") {
				readOnly[strings.Fields(m[strings.Index(m, "cannot execute"):])[2]] = true
			} else if m != "add panics" && !strings.Contains(m, "cookie") ||
				m == "add panics" && !strings.Contains(l["trace"].(string), "rpc.add(") {
				t.Errorf("a panic is logged as %v", l)
			}
		}
	}
	if len(readOnly) != 4 || !readOnly["INSERT"] || !readOnly["UPDATE"] || !readOnly["SELECT"] || !readOnly["nextval()"] {
		t.Errorf("a check that writes, or a gate that writes, locks or counts, is not refused by Postgres: %v", readOnly)
	}
	if len(ids) != 48 || strings.Contains(log.String(), "secret") {
		t.Errorf("the log has %d requests, or holds an input, an answer or a refusal:\n%s", len(ids), log.String())
	}
	// Each rerun pauses first, so a request that conflicts every time
	// reruns a few times in 50 ms, where with no pause it reruns
	// hundreds.
	if reruns["race ok"] < 1 || reruns["heldRace ok"] < 1 || reruns["deadlocked 503"] < 1 || reruns["deadlockedPlain 503"] < 1 || reruns["add ok"] != 0 ||
		reruns["deadlocked 503"] > 20 || reruns["deadlockedPlain 503"] > 20 {
		t.Errorf("reruns are counted as %v", reruns)
	}
}

type Mood string

var Mood_entries = []Mood{"Calm", "Cross"}

type Felt struct {
	_     struct{} `kogo:"Felt"`
	Moods []*Mood  `kogo:"moods"`
}

func feel(_ kogo.Db, _ env, _ Request, f Felt) kogo.Either[string, int32] {
	return kogo.Either_Right[int32]{Value: int32(len(f.Moods))}
}

// Call runs an operation of an unchecked list by its typed value, as
// a browser's call runs: an id, a served line, the reruns and the
// rollback of a refusal. It gives the answer as its Go type, a
// refusal as a Refused, and refuses an enum that is not an entry.
func TestCall(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://kogo:kogo@127.0.0.1/kogo_test")
	ctx := context.Background()
	pool, drop := testdb.Open(ctx, "../../../testdata/schema.sql")
	defer drop()
	var log bytes.Buffer
	e := env{&sync.WaitGroup{}, &atomic.Int32{}, &atomic.Int32{}, &sync.Map{}}
	unserved := NewUnserved([]Operation[env]{Tx("add", true, add), Tx("signIn", true, signIn), Db("who", who), Db("feel", feel), Tx("deadlocked", true, deadlocked)}, []any{Mood_entries})
	calls := NewCalls(unserved, pool, e, slog.New(slog.NewJSONHandler(&log, nil)))
	if _, mounts := any(calls).(http.Handler); mounts {
		t.Error("a Calls is an http.Handler")
	}
	r := httptest.NewRequest("GET", "/any/route", nil)
	r.Header.Set("X-Who", "me")
	addOp := NewOp[env, Person, string]("add")
	show := func(a any, err error) string {
		if _, refused := err.(Refused); refused {
			return fmt.Sprint("refused: ", err)
		}
		return strings.TrimSuffix(fmt.Sprint(a, " ", err), " <nil>")
	}
	cross, wild := Mood("Cross"), Mood("Wild")
	who, err := Call(calls, r, NewOp[env, rt.Unit, Who]("who"), rt.Unit{})
	reply, replyErr := Call(calls, r, NewOp[env, Person, Reply[string]]("signIn"), Person{Name: "called-in"})
	calls.Limit = 100 * time.Millisecond
	for i, c := range [][2]string{
		{show(Call(calls, r, addOp, Person{Name: "called"})), "secret answer"},
		{show(Call(calls, r, addOp, Person{Name: "refused call"})), "refused: secret refusal"},
		{show(Call(calls, r, addOp, Person{Name: "panics call"}))[:12], " 500 request"},
		{show(Call(calls, r, addOp, Person{Name: "a\x00called"})), " 400 add: input.name holds U+0000, which no input or setting may: leave it out"},
		{show(Call(calls, r, NewOp[env, rt.Unit, int32]("nothing"), rt.Unit{})), "0 404 "},
		{show(Call(calls, r, NewOp[env, Felt, int32]("feel"), Felt{Moods: []*Mood{&cross, nil}})), "2"},
		{show(Call(calls, r, NewOp[env, Felt, int32]("feel"), Felt{Moods: []*Mood{&cross, &wild}})), "0 400 feel: input.moods[1] is not one of Calm, Cross"},
		{show(Call(calls, r, NewOp[env, rt.Unit, int32]("deadlocked"), rt.Unit{}))[:12], "0 503 reques"},
		{fmt.Sprint(*who.Header, who.Address, err), "me192.0.2.1<nil>"},
		{fmt.Sprint(reply.Answer, len(reply.Cookies), replyErr), "in1 <nil>"},
	} {
		if c[0] != c[1] {
			t.Errorf("call %d gives %q, want %q", i, c[0], c[1])
		}
	}
	var n int
	pool.QueryRow(ctx, "select count(*) from person where name in ('called', 'refused call', 'panics call')").Scan(&n)
	if n != 1 {
		t.Errorf("%d rows, want 1: a refusal and a panic roll back", n)
	}
	// Each call logs one served line with an id; the panic one more.
	var served []string
	for _, l := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		var m map[string]any
		json.Unmarshal([]byte(l), &m)
		if m["msg"] == "served" && m["request"] != "" {
			served = append(served, fmt.Sprint(m["operation"], " ", m["outcome"]))
		}
	}
	// A failure is a Failed, whose Status a route answers.
	_, err = Call(calls, r, NewOp[env, rt.Unit, int32]("deadlocked"), rt.Unit{})
	if f, _ := err.(Failed); f.Status != 503 {
		t.Errorf("deadlocked fails with %#v, want a Failed of 503", err)
	}
	if got, want := strings.Join(served, ","), "who ok,signIn ok,add ok,add refused,add 500,add 400,nothing 404,feel ok,feel 400,deadlocked 503"; got != want {
		t.Errorf("served %s, want %s", got, want)
	}
	// SetCookies writes what a browser's call would have set.
	w := httptest.NewRecorder()
	SetCookies(w, reply.Cookies)
	if got := w.Header().Values("Set-Cookie"); len(got) != 1 || !strings.HasPrefix(got[0], "called-in=v; Path=/; Max-Age=60") {
		t.Errorf("SetCookies set %q", got)
	}
}

// A body past the Handler's MaxBody is a 413, at the default and
// where main sets it. A header read that is not UTF-8 is a 400 that
// names it, from the check as from an operation, and logs nothing
// but the served line; one no program reads is left alone.
func TestLimits(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://kogo:kogo@127.0.0.1/kogo_test")
	pool, drop := testdb.Open(context.Background(), "../../../testdata/schema.sql")
	defer drop()
	var log bytes.Buffer
	h := NewHandler(table, pool, env{&sync.WaitGroup{}, &atomic.Int32{}, &atomic.Int32{}, &sync.Map{}}, slog.New(slog.NewJSONHandler(&log, nil)))
	post := func(name, body string, header ...string) string {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/"+name, strings.NewReader(body))
		for i := 0; i < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
		}
		h.ServeHTTP(w, r)
		return fmt.Sprint(w.Code, " ", w.Body.String())
	}
	sized := func(n int) string {
		j := fmt.Sprintf(`{"name":"sized %d"}`, n)
		return j + strings.Repeat(" ", n-len(j))
	}
	ok, unset := `200 {"type":"ok","value":"secret answer"}`, h.MaxBody
	for _, c := range []struct {
		max            int64
		body, x, check string
		want           string
	}{
		{0, sized(64 << 10), "", "", ok},
		{0, sized(64<<10 + 1), "", "", "413 add: the input is past 65536 bytes"},
		{100, sized(100), "", "", ok},
		{100, sized(101), "", "", "413 add: the input is past 100 bytes"},
		{100, `{"name":"` + strings.Repeat("x", 100) + `"}`, "", "", "413 add: the input is past 100 bytes"},
		{0, `{"name":"x"}`, "", "\xff", "400 add: the header X-Check is not UTF-8"},
		{0, `{"name":"x"}`, "\xffsecret", "", ok},
		{0, ``, "\xffsecret", "", "400 who: the header X-Who is not UTF-8"},
		{0, ``, "café", "", `200 {"type":"ok","value":{"header":"café",`},
	} {
		if h.MaxBody = unset; c.max != 0 {
			h.MaxBody = c.max
		}
		name, header := "add", []string{"X-Unread", c.x}
		if c.body == `` {
			name, header = "who", []string{"X-Who", c.x}
		}
		if c.check != "" {
			header = append(header, "X-Check", c.check)
		}
		if got := post(name, c.body, header...); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s of %d bytes with %q, %q and MaxBody %d answers %.80s, want %s", name, len(c.body), c.x, c.check, c.max, got, c.want)
		}
	}
	var served []string
	for _, l := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		var m map[string]any
		json.Unmarshal([]byte(l), &m)
		served = append(served, fmt.Sprint(m["msg"], " ", m["operation"], " ", m["outcome"]))
	}
	if got, want := strings.Join(served, ","), "served add ok,served add 413,served add ok,served add 413,served add 413,served add 400,served add ok,served who 400,served who ok"; got != want || strings.Contains(log.String(), "secret") {
		t.Errorf("the log is %s, want %s, with no value:\n%s", got, want, log.String())
	}
}

// A request ends at its Limit, whatever it waits on: a row lock, or a
// connection of a pool that every one of them holds. Each answers 503
// and gives its connection back. A request whose client leaves is
// served as 499, a database that ends the connection or cannot be
// reached is a 503 and one line, and neither is logged as a panic.
func TestDeadline(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://kogo:kogo@127.0.0.1/kogo_test")
	ctx := context.Background()
	pool, drop := testdb.Open(ctx, "../../../testdata/schema.sql")
	defer drop()
	var log bytes.Buffer
	h := NewHandler(table, pool, env{&sync.WaitGroup{}, &atomic.Int32{}, &atomic.Int32{}, &sync.Map{}}, slog.New(slog.NewJSONHandler(&log, nil)))
	h.Limit = 500 * time.Millisecond
	post := func(rctx context.Context, name string) (int, time.Duration) {
		start, w := time.Now(), httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(rctx, "POST", "/"+name, nil))
		return w.Code, time.Since(start)
	}
	// The row is locked by a connection outside the pool, until the
	// test ends or for 3 seconds, after which a request that has not
	// ended answers. A request waits on it; then the test holds every
	// connection of the pool itself, for 2.
	pool.Exec(ctx, "insert into person (id, name, email) values ($1, 'locked', 'locked')", kogo.NewId())
	holder, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close(ctx)
	lock, _ := holder.Begin(ctx)
	lock.Exec(ctx, "select 1 from person where name = 'locked' for update")
	unlock := sync.OnceFunc(func() { lock.Rollback(ctx) })
	time.AfterFunc(3*time.Second, unlock)
	defer unlock()
	// The client leaves while its request waits on the row.
	left, leave := context.WithCancel(ctx)
	time.AfterFunc(100*time.Millisecond, leave)
	if status, _ := post(left, "waits"); status != 499 {
		t.Errorf("a request whose client left answers %d, want 499", status)
	}
	// A client that sent its body and left, over a socket, while a
	// Unit operation waits on the row: Go sees it go only once the
	// body is read.
	served := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(served)
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	fmt.Fprint(conn, "POST /waits HTTP/1.1\r\nHost: x\r\nContent-Length: 2\r\n\r\n{}")
	time.AfterFunc(100*time.Millisecond, func() { conn.Close() })
	<-served
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Errorf("a client that left during a Unit operation is seen after %v, want at once", took)
	}
	// A connection a request ended is closed, and the pool counts it
	// given back once it is.
	held := func() (busy int32) {
		for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if busy = pool.Stat().AcquiredConns(); busy == 0 {
				break
			}
		}
		return
	}
	if status, _ := post(ctx, "waits"); status != 503 {
		t.Errorf("a request waiting on a row lock answers %d, want 503", status)
	}
	if busy := held(); busy != 0 {
		t.Errorf("%d connections are still held a second after every request ended", busy)
	}
	var taken []*pgxpool.Conn
	for range pool.Config().MaxConns {
		c, _ := pool.Acquire(ctx)
		taken = append(taken, c)
	}
	give := sync.OnceFunc(func() {
		for _, c := range taken {
			c.Release()
		}
	})
	time.AfterFunc(2*time.Second, give)
	if status, took := post(ctx, "who"); status != 503 || took > time.Second {
		t.Errorf("a request waiting on a full pool answers %d after %v, want 503 at %v", status, took, h.Limit)
	}
	give()
	// A service that does not answer is a 503 too, with one line.
	if status, _ := post(ctx, "unanswered"); status != 503 {
		t.Errorf("a service that does not answer answers %d, want 503", status)
	}
	if status, _ := post(ctx, "unansweredThenBug"); status != 500 {
		t.Errorf("a bug after a service did not answer answers %d, want 500", status)
	}
	// The database ends the connection, or there is none to reach.
	if status, _ := post(ctx, "cut"); status != 503 {
		t.Errorf("a connection the database ended answers %d, want 503", status)
	}
	if status, _ := post(ctx, "crashed"); status != 503 {
		t.Errorf("a database restarting after a crash answers %d, want 503", status)
	}
	down, _ := pgxpool.New(ctx, "postgres://kogo:kogo@127.0.0.1:1/kogo_test")
	defer down.Close()
	h.pool = down
	if status, _ := post(ctx, "who"); status != 503 {
		t.Errorf("a database that cannot be reached answers %d, want 503", status)
	}
	if busy := held(); busy != 0 {
		t.Errorf("%d connections are still held a second after every request ended", busy)
	}
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		var m map[string]any
		json.Unmarshal([]byte(l), &m)
		if m["level"] != "INFO" {
			_, traced := m["trace"]
			got = append(got, fmt.Sprint(m["level"], " ", m["msg"], " ", m["operation"], " ", traced))
		} else if m["outcome"] != "503" {
			got = append(got, fmt.Sprint(m["msg"], " ", m["operation"], " ", m["outcome"]))
		}
	}
	if got, want := strings.Join(got, ","), "served waits 499,served waits 499,ERROR service unanswered false,ERROR panic unansweredThenBug true,served unansweredThenBug 500,ERROR database cut false,ERROR database crashed false,ERROR database who false"; got != want {
		t.Errorf("the log is %s, want %s:\n%s", got, want, log.String())
	}
}

// The handler refuses at start a pool that is not as Kogo requires,
// naming the setting (kogo.CheckPool), before anything connects.
func TestPool(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://kogo:kogo@127.0.0.1/kogo_test")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg) // it connects when first asked
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	defer func() {
		if p := fmt.Sprint(recover()); !strings.Contains(p, "timezone") {
			t.Errorf("a pool not in UTC is mounted, or refused with %s", p)
		}
	}()
	NewHandler(table, pool, env{}, slog.New(slog.DiscardHandler))
}
