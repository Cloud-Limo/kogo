package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Cloud-Limo/kogo/internal/testdb"
	"github.com/Cloud-Limo/kogo/prelude/kogo"
	"github.com/Cloud-Limo/kogo/rt"
)

// Operations as the emitter writes them, over the test database.

type env struct {
	ready    *sync.WaitGroup // the two first attempts of race, each having read
	attempts *atomic.Int32
	gates    *atomic.Int32 // the runs of gate
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

// gate, step and work each refuse a name of their own. The gate and
// the work insert first, which a gate would not: a refusal of either,
// or of the step after the gate, leaves no row.
func gate(tx kogo.Tx, e env, _ Request, p Person) kogo.Either[string, Held] {
	e.gates.Add(1)
	insert(tx, "gate "+p.Name)
	if strings.HasPrefix(p.Name, "nobody") {
		return kogo.Either_Left[string]{Value: "secret gate"}
	}
	return kogo.Either_Right[Held]{Value: Held{p.Name, tx}}
}

func step(_ env, h Held, p Person) kogo.Either[string, rt.Unit] {
	if h.name != p.Name || strings.HasPrefix(p.Name, "unsent") {
		return kogo.Either_Left[string]{Value: "secret step"}
	}
	return kogo.Either_Right[rt.Unit]{}
}

func work(e env, h Held, p Person) kogo.Either[string, string] {
	return add(h.tx, e, Request{}, p)
}

var table = Table[env]{Check: ours, Operations: []Operation[env]{
	Tx("add", add), Tx("race", race), Tx("deadlocked", deadlocked), Db("login", login), Db("who", who),
	Gated("held", gate, nil, work), Gated("sent", gate, step, work),
}}

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
	h := NewHandler(table, pool, env{&sync.WaitGroup{}, &atomic.Int32{}, &atomic.Int32{}}, slog.New(slog.NewJSONHandler(&log, nil)))
	post := func(method, name, body string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/"+name, strings.NewReader(body))
		r.Header.Set("X-Who", "me")
		if strings.HasPrefix(body, "check ") {
			r.Header.Set("X-Check", body)
		}
		r.AddCookie(&http.Cookie{Name: "c", Value: "crumb"})
		h.ServeHTTP(w, r)
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
		{"POST", "login", `{"name":"kept"}`, `200 {"type":"refused","message":"secret refusal"}`},
		{"POST", "who", `not read`, `200 {"type":"ok","value":{"header":"me","cookie":"crumb","missing":null,"host":"example.com","address":"192.0.2.1"}}`},
		{"POST", "nothing", `{}`, `404 `},
		{"GET", "add", ``, `405 `},
		// The check runs first, before the name is looked up, and
		// only reads; then the gate, the step and the work.
		{"POST", "add", `check forged`, `403 secret check`},
		{"POST", "nothing", `check forged`, `403 secret check`},
		{"POST", "add", `check writes`, `500 request `},
		{"POST", "held", `{"name":"held"}`, `200 {"type":"ok","value":"secret answer"}`},
		{"POST", "held", `{"name":"nobody held"}`, `200 {"type":"refused","message":"secret gate"}`},
		{"POST", "held", `{"name":"refused held"}`, `200 {"type":"refused","message":"secret refusal"}`},
		{"POST", "sent", `{"name":"nobody sent"}`, `200 {"type":"refused","message":"secret gate"}`},
		{"POST", "sent", `{"name":"unsent"}`, `200 {"type":"refused","message":"secret step"}`},
		{"POST", "sent", `{"name":"refused sent"}`, `200 {"type":"refused","message":"secret refusal"}`},
		{"POST", "sent", `{"name":"sent"}`, `200 {"type":"ok","value":"secret answer"}`},
	} {
		if status, body := post(c.method, c.name, c.body); !strings.HasPrefix(fmt.Sprint(status, " ", body), c.want) {
			t.Errorf("%s %s %s answers %d %s, want %s", c.method, c.name, c.body, status, body, c.want)
		}
	}
	if rows("answers") != 1 || rows("refused") != 0 || rows("panics") != 0 || rows("kept") != 1 {
		t.Error("an answer does not commit, or a refusal or a panic does not roll back, or a Db operation's db.tx does not stay")
	}
	// A gated row: the gate and the work are one transaction, and a
	// step's gate ran before it in one that was rolled back, so the
	// gate runs twice where the step passes and once where it or the
	// gate refuses: 3 runs for held, and 1, 1, 2 and 2 for sent.
	for name, want := range map[string]int{"held": 1, "gate held": 1, "gate nobody held": 0, "refused held": 0, "gate refused held": 0,
		"gate nobody sent": 0, "gate unsent": 0, "refused sent": 0, "gate refused sent": 0, "sent": 1, "gate sent": 1, "checked": 0} {
		if rows(name) != want {
			t.Errorf("%d rows of %q, want %d: a gated row keeps what it wrote, or a refusal does not roll back", rows(name), name, want)
		}
	}
	if n := h.env.gates.Load(); n != 9 {
		t.Errorf("the gate ran %d times, want 9", n)
	}
	h.Limit = 50 * time.Millisecond
	if status, body := post("POST", "deadlocked", ""); status != 503 || !strings.HasPrefix(body, "request ") {
		t.Errorf("reruns past the limit answer %d %s", status, body)
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

	// The log: a line for each request, with its id; reruns and a
	// panic under the request's id; no input, answer or refusal.
	ids, reruns, checkWrote := map[any]string{}, map[any]string{}, false
	for _, l := range lines() {
		switch l["msg"] {
		case "served":
			if _, ok := l["duration"]; !ok {
				t.Errorf("a served line has no duration: %v", l)
			}
			ids[l["request"]] = l["operation"].(string) + " " + l["outcome"].(string)
		case "rerun":
			reruns[l["request"]] = l["cause"].(string)
		case "panic":
			// The check that writes panics with Postgres's refusal.
			if strings.Contains(l["message"].(string), "cannot execute INSERT in a read-only transaction") {
				checkWrote = true
			} else if l["message"] != "add panics" || !strings.Contains(l["trace"].(string), "rpc.add(") {
				t.Errorf("a panic is logged as %v", l)
			}
		}
	}
	if !checkWrote {
		t.Error("a check that writes is not refused by Postgres")
	}
	if len(ids) != 21 || strings.Contains(log.String(), "secret") {
		t.Errorf("the log has %d requests, or holds an input, an answer or a refusal:\n%s", len(ids), log.String())
	}
	causes := map[string]bool{}
	for id, cause := range reruns {
		causes[ids[id]+" "+cause] = true
	}
	if !causes["race ok serialization failure"] || !causes["deadlocked 503 deadlock"] {
		t.Errorf("reruns are logged as %v", reruns)
	}
}
