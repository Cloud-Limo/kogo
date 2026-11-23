// Package rpc is the Go behind Kogo's package kogo.rpc, in
// prelude/rpc.kg, and the server of a table of operations (spec.md
// section 15; doc/design.md, "The server").
package rpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	"github.com/Cloud-Limo/kogo/rt"
)

// A Request is what the server read of one HTTP request. Its field is
// unexported so that no Go outside rpc makes one: an operation's
// request is always one a Handler or Call was handed.
type Request struct{ r *http.Request }

// Request_Header matches the header's name whatever its case, and
// gives its first value. Go keeps Host out of the headers. A value
// that is not UTF-8 ends the request: it panics with a notText, which
// run answers 400 and does not log.
func Request_Header(this Request, name string) *string {
	v := this.r.Header.Values(name)
	if strings.EqualFold(name, "Host") {
		v = []string{this.r.Host}
	}
	if len(v) == 0 {
		return nil
	} else if !utf8.ValidString(v[0]) {
		panic(notText(http.CanonicalHeaderKey(name)))
	}
	return &v[0]
}

// A notText is the panic of a header read whose value is not UTF-8,
// with the header's name, never its value.
type notText string

func (n notText) String() string { return "the header " + string(n) + " is not UTF-8" }

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

// An answered is an answer's JSON and the cookies it sets, with the
// value itself, which Call gives.
type answered struct {
	body    []byte
	cookies []*http.Cookie
	value   any
}

// An Operation is one row of a table: its name on the wire and its
// fun, which Tx and Db take with its Go type, so that Go checks it.
type Operation[Env any] struct {
	Name  string
	input reflect.Type
	run   func(db kogo.Db, again func(*pgconn.PgError) bool, env Env, r Request, input any) kogo.Either[string, answered]
}

// Tx is the row of an operation that takes a SerializableTx or a Tx,
// as serializable says: it runs in one transaction at SERIALIZABLE or
// at READ COMMITTED, run again on a conflict for as long as again
// says, which an answer commits and a refusal or a panic rolls
// back. The answer is written inside it, so that one that has no JSON
// rolls back too.
func Tx[Env, I, A any](name string, serializable bool, f func(kogo.Tx, Env, Request, I) kogo.Either[string, A]) Operation[Env] {
	return Operation[Env]{name, reflect.TypeFor[I](), func(db kogo.Db, again func(*pgconn.PgError) bool, env Env, r Request, input any) kogo.Either[string, answered] {
		return kogo.Either_RaiseEToA(func(raise kogo.Raise[string]) answered {
			return kogo.InTx(db, serializable, again, func(tx kogo.Tx) answered {
				return answer(name, bind[A](f(tx, env, r, input.(I)), raise))
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

// Gated is a gated row: the gate runs in the operation's transaction
// and answers a call, or facts, or a refusal, and the work takes that
// answer where an ungated operation takes the request (doc/gates.md). A refusal of
// either is the operation's, and rolls back. The transaction is of
// the kind serializable says, as the gate's first parameter names.
func Gated[Env, I, P, A any](name string, serializable bool, gate func(kogo.Tx, Env, Request, I) kogo.Either[string, P],
	work func(Env, P, I) kogo.Either[string, A]) Operation[Env] {
	return Operation[Env]{name, reflect.TypeFor[I](), func(db kogo.Db, again func(*pgconn.PgError) bool, env Env, r Request, input any) kogo.Either[string, answered] {
		in := input.(I)
		return kogo.Either_RaiseEToA(func(raise kogo.Raise[string]) answered {
			return kogo.InTx(db, serializable, again, func(tx kogo.Tx) answered {
				given := bind[P](reading(tx, serializable, func() kogo.Either[string, P] { return gate(tx, env, r, in) }), raise)
				return answer(name, bind[A](work(env, given, in), raise))
			})
		})
	}}
}

// Outside is a gated row whose work is an action fun, so that it runs
// with no transaction open (doc/gates.md, "Work that changes
// something outside"): the gate runs once, in a transaction that commits
// before the work is called, of the kind serializable says. A refusal of either is the operation's.
func Outside[Env, I, P, A any](name string, serializable bool, gate func(kogo.Tx, Env, Request, I) kogo.Either[string, P], work func(Env, P, I) kogo.Either[string, A]) Operation[Env] {
	return Operation[Env]{name, reflect.TypeFor[I](), func(db kogo.Db, again func(*pgconn.PgError) bool, env Env, r Request, input any) kogo.Either[string, answered] {
		in := input.(I)
		return kogo.Either_RaiseEToA(func(raise kogo.Raise[string]) answered {
			given := kogo.InTx(db, serializable, again, func(tx kogo.Tx) P {
				return bind[P](reading(tx, serializable, func() kogo.Either[string, P] { return gate(tx, env, r, in) }), raise)
			})
			return answer(name, bind[A](work(env, given, in), raise))
		})
	}}
}

// reading runs a gate held to reading (doc/gates.md, "A gate reads
// and decides; it does not write"): in a savepoint that Postgres
// holds read-only, where it refuses a write, nextval and a row lock,
// and a panic is the gate's bug. The savepoint is released before the
// work, which writes again, and a setting the gate made stays. A gate
// on a Tx may lock, which Postgres's read-only mode refuses with a
// write, so it runs as it is, held by kogo check alone.
func reading[P any](tx kogo.Tx, serializable bool, gate func() kogo.Either[string, P]) kogo.Either[string, P] {
	if !serializable {
		return gate()
	}
	return kogo.Tx_Atomically(tx, func() kogo.Either[string, P] {
		kogo.Tx_Execute(tx, "set local transaction_read_only = on")
		return gate()
	})
}

// bind gives what e holds, or raises its refusal.
func bind[A any](e kogo.Either[string, A], raise kogo.Raise[string]) A {
	return kogo.Either_Bind[string, A](e, raise, func(s string) string { return s })
}

// Shared makes a gate that reads no input the gate of a row whose
// input is I.
func Shared[I, Env, P any](gate func(kogo.Tx, Env, Request) kogo.Either[string, P]) func(kogo.Tx, Env, Request, I) kogo.Either[string, P] {
	return func(tx kogo.Tx, env Env, r Request, _ I) kogo.Either[string, P] { return gate(tx, env, r) }
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
			cookies = append(cookies, httpCookie("the answer of "+op, c))
		}
	}
	b.WriteString(`{"type":"ok","value":`)
	write(&b, v, op, "value")
	b.WriteString("}")
	return answered{b.Bytes(), cookies, a}
}

// httpCookie gives c as net/http writes it, or panics, naming who set it.
func httpCookie(who string, c Cookie) *http.Cookie {
	hc := &http.Cookie{Name: c.Name, Value: c.Value, Path: c.Path, MaxAge: int(c.MaxAgeSeconds), Secure: c.Secure, HttpOnly: c.HttpOnly,
		SameSite: map[SameSite]http.SameSite{SameSite_Strict: http.SameSiteStrictMode, SameSite_Lax: http.SameSiteLaxMode, SameSite_None: http.SameSiteNoneMode}[c.SameSite]}
	if hc.MaxAge <= 0 {
		hc.MaxAge = -1
	}
	if err := hc.Valid(); err != nil {
		panic(fmt.Sprintf("%s sets a cookie HTTP cannot carry: %v", who, err))
	}
	if refused := browserRefuses(c); refused != "" {
		panic(fmt.Sprintf("%s sets a cookie %s that a browser refuses: %s", who, c.Name, refused))
	}
	return hc
}

// SetCookies writes a Reply's cookies to w, for a route that answers
// in Go what Call gave it. It panics on a cookie the server refuses.
func SetCookies(w http.ResponseWriter, cookies kogo.List[Cookie]) {
	for _, c := range cookies {
		http.SetCookie(w, httpCookie("rpc.SetCookies", c))
	}
}

// A Table is what the emitter writes for an api list that names its
// check: the check, its operations and the entries (Name_entries) of
// each enum they reach. NewHandler needs those of each enum an input
// reaches, and panics without them.
type Table[Env any] struct {
	Check      func(kogo.Tx, Env, Request) kogo.Either[string, rt.Unit]
	Operations []Operation[Env]
	Enums      []any
}

// An Unserved is what the emitter writes for an unchecked list. No
// handler takes one and its fields are unexported, so it cannot be
// mounted for a browser (doc/origin.md).
type Unserved[Env any] struct {
	operations []Operation[Env]
	enums      []any
}

func NewUnserved[Env any](operations []Operation[Env], enums []any) Unserved[Env] {
	return Unserved[Env]{operations, enums}
}

// A Handler serves a table at the names of its operations, POST
// /name, under the path where main mounts it with http.StripPrefix.
type Handler[Env any] struct {
	*server[Env]
	// MaxBody is the most bytes of a body the Handler reads: 64 KiB
	// unless main sets it. Past it the answer is 413. A body is built
	// whole into a tree before its type is checked, so until it is
	// answered one request can take up to 35 times MaxBody in memory:
	// 2 MB at the default, 35 MB at 1 MiB.
	MaxBody int64
}

// A server is what a Handler and Calls share: a list, its check if
// it has one, and what its operations run with.
type server[Env any] struct {
	// Limit is how long a request may take, from its start to its
	// answer: past it, what it waits on ends and the answer is 503.
	Limit time.Duration
	check func(kogo.Tx, Env, Request) kogo.Either[string, rt.Unit]
	ops   map[string]Operation[Env]
	read  reader
	pool  *pgxpool.Pool
	env   Env
	log   *slog.Logger
}

// NewHandler serves t over pool with env, and logs to log. Its Limit
// is 90 seconds and its MaxBody 64 KiB. It panics if t has no check,
// or lacks an enum's entries that an input reaches.
func NewHandler[Env any](t Table[Env], pool *pgxpool.Pool, env Env, log *slog.Logger) *Handler[Env] {
	if t.Check == nil {
		panic("the table has no check: a list that a browser calls names one, api name checkedBy check")
	}
	return &Handler[Env]{newServer(t.Check, t.Operations, t.Enums, pool, env, log), 64 << 10}
}

func newServer[Env any](check func(kogo.Tx, Env, Request) kogo.Either[string, rt.Unit], ops []Operation[Env], enums []any, pool *pgxpool.Pool, env Env, log *slog.Logger) *server[Env] {
	h := &server[Env]{90 * time.Second, check, map[string]Operation[Env]{}, newReader(enums), pool, env, log}
	for _, op := range ops {
		h.read.check(op.input, map[reflect.Type]bool{})
		h.ops[op.Name] = op
	}
	return h
}

// ServeHTTP gives each request an id, before any rerun, and logs one
// line for it with its outcome, its reruns and its time, one more
// for a database that is down or cut off, with the driver's error,
// and one for a panic, with its message and trace. It logs no input,
// answer or refusal.
func (h *Handler[Env]) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	res := h.run(r, name, func(op Operation[Env]) (any, result) {
		input := reflect.ValueOf(rt.Unit{})
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			return nil, result{status: http.StatusMethodNotAllowed}
		} else if op.input != input.Type() {
			var err error
			var tooLarge *http.MaxBytesError
			if input, err = h.read.readBody(op.input, http.MaxBytesReader(w, r.Body, h.MaxBody)); errors.As(err, &tooLarge) {
				return nil, result{status: http.StatusRequestEntityTooLarge, text: fmt.Sprintf("%s: the input is past %d bytes", name, tooLarge.Limit)}
			} else if err != nil {
				return nil, result{status: http.StatusBadRequest, text: name + ": " + err.Error()}
			}
		}
		return input.Interface(), result{}
	})
	switch {
	case res.status != http.StatusOK:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(res.status)
		w.Write([]byte(res.text))
	case res.refused:
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"type":"refused","message":` + quote(res.text) + `}`))
	default:
		w.Header().Set("Content-Type", "application/json")
		for _, c := range res.answer.cookies {
			http.SetCookie(w, c)
		}
		w.Write(res.answer.body)
	}
}

// statusLeft is the status of a request whose client left before it
// was answered, which nobody reads: nginx's, so that its log and the
// served line agree.
const statusLeft = 499

// A result is how one call ended: at 200 an answer, or a refusal
// whose words are text; at any other status, text is what a browser
// is answered.
type result struct {
	status  int
	text    string
	refused bool
	answer  answered
}

// run is what a browser's call and Call share: the id, the log's
// lines, the list's check, the limit and the reruns. input gives the
// operation's input, or how the call ends without one. One deadline,
// Limit from the start, is on the context of everything inside: the
// wait for a connection, each statement, the pause before a rerun.
func (h *server[Env]) run(r *http.Request, name string, input func(Operation[Env]) (any, result)) (res result) {
	start := time.Now()
	id := kogo.NewId().String()
	log := h.log.With("request", id, "operation", name)
	reruns := 0
	defer func() {
		outcome := fmt.Sprint(res.status)
		if res.status == http.StatusOK {
			outcome = map[bool]string{false: "ok", true: "refused"}[res.refused]
		}
		log.Info("served", "outcome", outcome, "reruns", reruns, "duration", time.Since(start))
	}()
	ctx, cancel := context.WithTimeout(r.Context(), h.Limit)
	defer cancel()
	r = r.WithContext(ctx)
	ranOut := false
	defer func() {
		p := recover()
		err, _ := p.(error)
		var pg *pgconn.PgError
		code := ""
		if errors.As(err, &pg) {
			code = pg.Code
		}
		ended := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || code == "57014"
		switch header, ok := p.(notText); {
		case p == nil:
		case ok:
			res = result{status: http.StatusBadRequest, text: name + ": " + header.String()}
		case (ranOut || ended) && ctx.Err() == context.Canceled:
			res = result{status: statusLeft}
		case (ranOut || ended) && ctx.Err() != nil:
			res = result{status: http.StatusServiceUnavailable, text: "request " + id}
		case code == "57014" || code == "57P01" || strings.HasPrefix(code, "08") || errors.Is(err, pgconn.ErrConnClosed) ||
			errors.As(err, new(*pgconn.ConnectError)) || errors.As(err, new(net.Error)) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
			log.Error("database", "error", err.Error())
			res = result{status: http.StatusServiceUnavailable, text: "request " + id}
		default:
			log.Error("panic", "message", fmt.Sprint(p), "trace", string(debug.Stack()))
			res = result{status: http.StatusInternalServerError, text: "request " + id}
		}
	}()
	// The list's check runs first, once, in a read-only transaction
	// of its own: its refusal is a 403 with its words. An unchecked
	// list has none, and only Call reaches it.
	db := kogo.NewDb(ctx, h.pool)
	if h.check != nil {
		checked := kogo.ReadOnlyTx(db, func(tx kogo.Tx) kogo.Either[string, rt.Unit] { return h.check(tx, h.env, Request{r}) })
		if refusal, refused := checked.(kogo.Either_Left[string]); refused {
			return result{status: http.StatusForbidden, text: refusal.Value}
		}
	}
	op, found := h.ops[name]
	if !found {
		return result{status: http.StatusNotFound}
	}
	in, stop := input(op)
	if stop.status != 0 {
		return stop
	}
	again := func(*pgconn.PgError) bool {
		if ranOut = ctx.Err() != nil; !ranOut {
			reruns++
		}
		return !ranOut
	}
	switch got := op.run(db, again, h.env, Request{r}, in).(type) {
	case kogo.Either_Left[string]:
		return result{status: http.StatusOK, text: got.Value, refused: true}
	case kogo.Either_Right[answered]:
		res = result{status: http.StatusOK, answer: got.Value}
	}
	return res
}

// An Op is one operation as hand-written Go calls it: its name on the
// wire with its input's and its answer's types, which the emitter
// writes beside the operation's table as Name_op.
type Op[Env, I, A any] struct{ name string }

func NewOp[Env, I, A any](name string) Op[Env, I, A] { return Op[Env, I, A]{name} }

// Calls runs the operations of an unchecked list for main's routes.
// It shares a Handler's server but has no ServeHTTP, and so is no
// http.Handler: no browser reaches the list, and only Call does. Its
// Limit is a Handler's. Its blank field keeps it from converting to a
// Handler outside this package.
type Calls[Env any] struct {
	*server[Env]
	_ struct{}
}

// NewCalls is NewHandler for an unchecked list.
func NewCalls[Env any](u Unserved[Env], pool *pgxpool.Pool, env Env, log *slog.Logger) *Calls[Env] {
	return &Calls[Env]{server: newServer(nil, u.operations, u.enums, pool, env, log)}
}

// Refused is the error Call gives for an operation's refusal, with
// its words.
type Refused string

func (r Refused) Error() string { return string(r) }

// Call runs op of c's list with input for the request r, as a Handler
// runs a browser's call: with a request id, the served line, the
// limit and the reruns. It reads no body: the input is Go's, and only
// its enums can be wrong, which Call checks as the strict reader
// does. Its error is a Refused, or the status and text a browser
// would be answered: 400 for an enum or a header that is not UTF-8,
// 404 for an operation not of the list, 499 where r's client left,
// 500 with the request's id for a panic, 503 with it past the limit
// or where the database is down.
func Call[Env, I, A any](c *Calls[Env], r *http.Request, op Op[Env, I, A], input I) (A, error) {
	res := c.run(r, op.name, func(Operation[Env]) (any, result) {
		if err := c.read.valid(reflect.ValueOf(&input).Elem(), "input"); err != nil {
			return nil, result{status: http.StatusBadRequest, text: op.name + ": " + err.Error()}
		}
		return input, result{}
	})
	var none A
	switch {
	case res.status != http.StatusOK:
		return none, fmt.Errorf("%d %s", res.status, res.text)
	case res.refused:
		return none, Refused(res.text)
	}
	return res.answer.value.(A), nil
}
