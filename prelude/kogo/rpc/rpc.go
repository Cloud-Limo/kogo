// Package rpc is the Go behind Kogo's package kogo.rpc, in
// prelude/rpc.kg, and the server of a table of operations (spec.md
// section 15; doc/design.md, "The server").
package rpc

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	"github.com/Cloud-Limo/kogo/rt"
)

// A Request is what the server read of one HTTP request.
type Request struct{ r *http.Request }

// Request_Header matches the header's name whatever its case, and
// gives its first value. Go keeps Host out of the headers.
func Request_Header(this Request, name string) *string {
	if strings.EqualFold(name, "Host") {
		return &this.r.Host
	}
	if v := this.r.Header.Values(name); len(v) > 0 {
		return &v[0]
	}
	return nil
}

func Request_Cookie(this Request, name string) *string {
	if c, err := this.r.Cookie(name); err == nil {
		return &c.Value
	}
	return nil
}

// Request_ClientAddress is the address of what connected to the
// server, without its port. Behind a proxy, main sets the request's
// RemoteAddr from the proxy's header before the Handler sees it.
func Request_ClientAddress(this Request) string {
	if host, _, err := net.SplitHostPort(this.r.RemoteAddr); err == nil {
		return host
	}
	return this.r.RemoteAddr
}

// SameSite, Cookie and Reply are prelude/rpc.kg's, written here as
// the emitter would write them.
type SameSite string

const (
	SameSite_Strict SameSite = "Strict"
	SameSite_Lax    SameSite = "Lax"
	SameSite_None   SameSite = "None"
)

var SameSite_entries = []SameSite{SameSite_Strict, SameSite_Lax, SameSite_None}

type Cookie struct {
	_             struct{} `kogo:"Cookie"`
	Name          string   `kogo:"name"`
	Value         string   `kogo:"value"`
	Path          string   `kogo:"path"`
	MaxAgeSeconds int64    `kogo:"maxAgeSeconds"`
	SameSite      SameSite `kogo:"sameSite"`
	Secure        bool     `kogo:"secure"`
	HttpOnly      bool     `kogo:"httpOnly"`
}

type Reply[A any] struct {
	_       struct{}          `kogo:"Reply"`
	Answer  A                 `kogo:"answer"`
	Cookies kogo.List[Cookie] `kogo:"cookies"`
}

func (r Reply[A]) reply() (reflect.Value, kogo.List[Cookie]) {
	return reflect.ValueOf(&r.Answer).Elem(), r.Cookies
}

// browserRefuses says why a browser drops c, or "" when it keeps it.
// Browsers read the prefixes in any case.
func browserRefuses(c Cookie) string {
	name := strings.ToLower(c.Name)
	switch {
	case c.SameSite == SameSite_None && !c.Secure:
		return "SameSite None needs secure"
	case strings.HasPrefix(name, "__secure-") && !c.Secure:
		return "the prefix __Secure- needs secure"
	case strings.HasPrefix(name, "__host-") && (!c.Secure || c.Path != "/"):
		return `the prefix __Host- needs secure and the path "/"`
	}
	return ""
}

// An answered is an answer's JSON and the cookies it sets.
type answered struct {
	body    []byte
	cookies []*http.Cookie
}

// An Operation is one row of a table: its name on the wire and its
// fun, which Tx and Db take with its Go type, so that Go checks it.
type Operation[Env any] struct {
	Name  string
	input reflect.Type
	run   func(db kogo.Db, again func(*pgconn.PgError) bool, env Env, r Request, input any) kogo.Either[string, answered]
}

// Tx is the row of an operation that takes a Tx: it runs in one
// SERIALIZABLE transaction, run again on a conflict for as long as
// again says, which an answer commits and a refusal or a panic rolls
// back. The answer is written inside it, so that one that has no JSON
// rolls back too.
func Tx[Env, I, A any](name string, f func(kogo.Tx, Env, Request, I) kogo.Either[string, A]) Operation[Env] {
	return Operation[Env]{name, reflect.TypeFor[I](), func(db kogo.Db, again func(*pgconn.PgError) bool, env Env, r Request, input any) kogo.Either[string, answered] {
		return kogo.Either_RaiseEToA(func(raise kogo.Raise[string]) answered {
			return kogo.SerializableTx(db, again, func(tx kogo.Tx) answered {
				return answer(name, kogo.Either_Bind[string, A](f(tx, env, r, input.(I)), raise, func(s string) string { return s }))
			})
		})
	}}
}

// Db is the row of an operation that takes a Db and runs in no
// transaction of the server's.
func Db[Env, I, A any](name string, f func(kogo.Db, Env, Request, I) kogo.Either[string, A]) Operation[Env] {
	return Operation[Env]{name, reflect.TypeFor[I](), func(db kogo.Db, _ func(*pgconn.PgError) bool, env Env, r Request, input any) kogo.Either[string, answered] {
		return kogo.Either_Map[string](f(db, env, r, input.(I)), func(a A) answered { return answer(name, a) })
	}}
}

// answer writes a, or the answer of a Reply with its cookies. A
// cookie that HTTP cannot carry, or that a browser drops without a
// word, is a bug, and panics. Max-Age is the
// cookie's seconds, and 0 or less ends it.
func answer[A any](op string, a A) answered {
	var b bytes.Buffer
	var cookies []*http.Cookie
	v := reflect.ValueOf(&a).Elem()
	if r, ok := any(a).(interface {
		reply() (reflect.Value, kogo.List[Cookie])
	}); ok {
		var cs kogo.List[Cookie]
		v, cs = r.reply()
		for _, c := range cs {
			hc := &http.Cookie{Name: c.Name, Value: c.Value, Path: c.Path, MaxAge: int(c.MaxAgeSeconds), Secure: c.Secure, HttpOnly: c.HttpOnly,
				SameSite: map[SameSite]http.SameSite{SameSite_Strict: http.SameSiteStrictMode, SameSite_Lax: http.SameSiteLaxMode, SameSite_None: http.SameSiteNoneMode}[c.SameSite]}
			if hc.MaxAge <= 0 {
				hc.MaxAge = -1
			}
			if err := hc.Valid(); err != nil {
				panic(fmt.Sprintf("the answer of %s sets a cookie HTTP cannot carry: %v", op, err))
			}
			if refused := browserRefuses(c); refused != "" {
				panic(fmt.Sprintf("the answer of %s sets a cookie %s that a browser refuses: %s", op, c.Name, refused))
			}
			cookies = append(cookies, hc)
		}
	}
	b.WriteString(`{"type":"ok","value":`)
	write(&b, v, op, "value")
	b.WriteString("}")
	return answered{b.Bytes(), cookies}
}

// A Table is what the emitter writes for an api list: its operations
// and the entries (Name_entries) of each enum they reach. NewHandler
// needs those of each enum an input reaches, and panics without them.
type Table[Env any] struct {
	Operations []Operation[Env]
	Enums      []any
}

// A Handler serves a table at the names of its operations, POST
// /name, under the path where main mounts it with http.StripPrefix.
type Handler[Env any] struct {
	// Limit is how long the reruns of a Tx operation go on before
	// the answer is 503.
	Limit time.Duration
	ops   map[string]Operation[Env]
	read  reader
	pool  *pgxpool.Pool
	env   Env
	log   *slog.Logger
}

// NewHandler serves t over pool with env, and logs to log. Its Limit
// is 90 seconds. It panics if t lacks an enum's entries that an input
// reaches.
func NewHandler[Env any](t Table[Env], pool *pgxpool.Pool, env Env, log *slog.Logger) *Handler[Env] {
	h := &Handler[Env]{90 * time.Second, map[string]Operation[Env]{}, newReader(t.Enums), pool, env, log}
	for _, op := range t.Operations {
		h.read.check(op.input, map[reflect.Type]bool{})
		h.ops[op.Name] = op
	}
	return h
}

// ServeHTTP gives each request an id, before any rerun, and logs one
// line for it with its outcome and time, one for each rerun with its
// cause and one for a panic with its message and trace. It logs no
// input, answer or refusal.
func (h *Handler[Env]) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	name := strings.TrimPrefix(r.URL.Path, "/")
	id := kogo.NewId().String()
	log := h.log.With("request", id, "operation", name)
	outcome := h.serve(w, r, name, id, log, start)
	log.Info("served", "outcome", outcome, "duration", time.Since(start))
}

func (h *Handler[Env]) serve(w http.ResponseWriter, r *http.Request, name, id string, log *slog.Logger, start time.Time) (outcome string) {
	text := func(status int, body string) string {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		w.Write([]byte(body))
		return fmt.Sprint(status)
	}
	ranOut := false
	defer func() {
		if p := recover(); p != nil && ranOut {
			outcome = text(http.StatusServiceUnavailable, "request "+id)
		} else if p != nil {
			log.Error("panic", "message", fmt.Sprint(p), "trace", string(debug.Stack()))
			outcome = text(http.StatusInternalServerError, "request "+id)
		}
	}()
	op, found := h.ops[name]
	if !found {
		return text(http.StatusNotFound, "")
	} else if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		return text(http.StatusMethodNotAllowed, "")
	}
	input := reflect.ValueOf(rt.Unit{})
	if op.input != input.Type() {
		var err error
		if input, err = h.read.readBody(op.input, r.Body); err != nil {
			return text(http.StatusBadRequest, name+": "+err.Error())
		}
	}
	again := func(conflict *pgconn.PgError) bool {
		if ranOut = time.Since(start) >= h.Limit; !ranOut {
			log.Info("rerun", "cause", map[string]string{"40001": "serialization failure", "40P01": "deadlock"}[conflict.Code])
		}
		return !ranOut
	}
	w.Header().Set("Content-Type", "application/json")
	switch result := op.run(kogo.NewDb(r.Context(), h.pool), again, h.env, Request{r}, input.Interface()).(type) {
	case kogo.Either_Left[string]:
		w.Write([]byte(`{"type":"refused","message":` + quote(result.Value) + `}`))
		return "refused"
	case kogo.Either_Right[answered]:
		for _, c := range result.Value.cookies {
			http.SetCookie(w, c)
		}
		w.Write(result.Value.body)
	}
	return "ok"
}
