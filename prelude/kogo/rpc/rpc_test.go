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

var table = Table[env]{Operations: []Operation[env]{
	Tx("add", add), Tx("race", race), Tx("deadlocked", deadlocked), Db("login", login), Db("who", who),
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
	h := NewHandler(table, pool, env{&sync.WaitGroup{}, &atomic.Int32{}}, slog.New(slog.NewJSONHandler(&log, nil)))
	post := func(method, name, body string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/"+name, strings.NewReader(body))
		r.Header.Set("X-Who", "me")
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
	} {
		if status, body := post(c.method, c.name, c.body); !strings.HasPrefix(fmt.Sprint(status, " ", body), c.want) {
			t.Errorf("%s %s %s answers %d %s, want %s", c.method, c.name, c.body, status, body, c.want)
		}
	}
	if rows("answers") != 1 || rows("refused") != 0 || rows("panics") != 0 || rows("kept") != 1 {
		t.Error("an answer does not commit, or a refusal or a panic does not roll back, or a Db operation's db.tx does not stay")
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
	ids, reruns := map[any]string{}, map[any]string{}
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
			if l["message"] != "add panics" || !strings.Contains(l["trace"].(string), "rpc.add(") {
				t.Errorf("a panic is logged as %v", l)
			}
		}
	}
	if len(ids) != 11 || strings.Contains(log.String(), "secret") {
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
