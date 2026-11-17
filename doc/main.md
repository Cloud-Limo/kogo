# cusoon2's main, on paper

DEV-903, 17 Nov 2026. cusoon2's Go `main` written out whole, as the
designs of 17 Nov would have it: `doc/gates.md`, `doc/work.md`, and
`decisions.md`'s seam ("Where Kogo ends and Go begins"). Read from
cusoon2 at `691aa93cf` (`Main.kt`, `CusoonApp.kt`, `CusoonAppState.kt`,
`GracefulShutdown.kt`, `config/CusoonConfig.kt`, `AuthConfig.kt`,
`Bounces.kt`). It is
not built. Every Go name below that is not `net/http`'s, `slog`'s or
pgx's is invented: the emitted packages, and the prelude's handle,
command and runner APIs, which no design has given a Go form yet. The
Stripe version is the one stripe-java 31.4.1, cusoon2's, pins.
Qn points into "Open".

## The file

```go
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"kogo/prelude/kogo"
	"kogo/prelude/kogo/command"
	khttp "kogo/prelude/kogo/http"
	"kogo/prelude/kogo/mail"
	"kogo/prelude/kogo/rpc"
	ktime "kogo/prelude/kogo/time"
	"kogo/prelude/kogo/work"
	"kogo/rt"

	"cusoon2/out/admin"  // commands admin
	"cusoon2/out/app"    // data class Env
	"cusoon2/out/booker" // api booker
	"cusoon2/out/daemon" // work daemon
	"cusoon2/out/ops"    // api ops, api storefront
	"cusoon2/out/routes" // api routes { logout, recordDeliveryFailure }, not mounted
)

// How long a person waits for a request: cusoon2's figure.
const requestLimit = 90 * time.Second

func main() { os.Exit(run()) }

func run() int {
	path := flag.String("config", os.Getenv("CUSOON_CONFIG_FILE"), "the configuration file")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: cusoon2 [--config file] web | batch | check-config | <command> --flag value ...")
		return 1
	}
	mode := flag.Arg(0)
	if flag.NArg() > 1 && (mode == "web" || mode == "batch" || mode == "check-config") {
		fmt.Fprintln(os.Stderr, "--config goes before", mode) // else it is read from the environment
		return 1
	}
	clock := ktime.SystemClock()
	cfg, err := load(*path) // [1]
	var env app.Env
	if err == nil {
		env, err = environment(cfg, clock) // [2]
	}
	if mode == "check-config" { // [6] reads and builds; opens nothing
		if err != nil {
			fmt.Fprintln(os.Stderr, "Config INVALID:", err)
			return 1
		}
		fmt.Println("Config OK")
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	pool, err := open(ctx, cfg) // [3]
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pool.Close() // after the drain: nothing is still holding a connection
	switch mode {
	case "web": // [4]
		err = web(ctx, cfg, env, pool, clock, log)
	case "batch": // [5] returns once each queue's row in hand is done
		err = work.Run(ctx, daemon.Daemon, pool, env, clock, log)
	default: // [6] a command, or "unknown command", with exit 1
		return command.Run(kogo.NewDb(ctx, pool), env, admin.Admin, mode, flag.Args()[1:], os.Stdout, os.Stderr)
	}
	if err != nil {
		log.Error("stopped", "error", err)
		return 1
	}
	return 0
}

// [1] cusoon2's file as it is, read strictly. Keys the Kotlin reads and
// this does not, and the keys the Kotlin itself ignores, are named and
// ignored as json.RawMessage.
type Config struct {
	Deployment struct {
		BackendHost, OpsOrigin string
		BackendPort            int
		Operators, Dev         []mail.Address // "dev": the old name, still read
		ExtraAllowedOrigins    []string
		GoogleMapsBrowserKey   *string
		PlatformAdminIds       json.RawMessage // retired; old files carry it

		ShowTestModeBand, RefreshGooglePlaceIds, ExposeOneTimeCode, TrustDevTenantHeader bool
	}
	DbConfig struct {
		URI, User, Password                string
		MaximumPoolSize                    int32
		JDBCOnly                           json.RawMessage `json:"preparedStatementCacheSizeMiB"`
		MigrationsUser, MigrationsPassword json.RawMessage // the migration tool's
	}
	SmtpConfig struct {
		Server, Transport                 string
		Port                              int
		Username, Password, WebhookSecret *string
	}
	AuthConfig struct {
		FaURL, FaInternalURL, APIKey string
		ClientSecret, BackendServer  json.RawMessage // the login callback's (Not in main)
	}
	GooglePlaceAPIKey string
	StripeConfig      struct{ PublicKey, SecretKey string }

	UpstreamTimeouts, GoogleAnswers     json.RawMessage // CI's and agents' files; Q10
	GoogleResponsesCacheDir, JooqConfig json.RawMessage // retired; old files carry them
}

func load(path string) (c Config, err error) {
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	d := &c.Deployment
	hasOperators := d.Operators != nil || d.Dev != nil // [] is present; a missing key silences every alert
	d.Operators = append(d.Operators, d.Dev...)
	c.AuthConfig.FaURL = strings.TrimRight(c.AuthConfig.FaURL, "/")
	c.AuthConfig.FaInternalURL = strings.TrimRight(c.AuthConfig.FaInternalURL, "/")
	for key, present := range map[string]bool{ // Q7: Go reads a missing key as zero
		"dbConfig.uri": c.DbConfig.URI != "", "dbConfig.maximumPoolSize": c.DbConfig.MaximumPoolSize > 0,
		"dbConfig.user": c.DbConfig.User != "", "dbConfig.password": c.DbConfig.Password != "",
		"deployment.backendPort": d.BackendPort > 0, "deployment.opsOrigin": d.OpsOrigin != "",
		"deployment.operators": hasOperators,
		"smtpConfig.server":    c.SmtpConfig.Server != "", "smtpConfig.port": c.SmtpConfig.Port > 0,
		"smtpConfig.transport": c.SmtpConfig.Transport != "",
		"authConfig.faUrl":     c.AuthConfig.FaURL != "", "authConfig.apiKey": c.AuthConfig.APIKey != "",
		"googlePlaceApiKey": c.GooglePlaceAPIKey != "", "stripeConfig.secretKey": c.StripeConfig.SecretKey != "",
		"stripeConfig.publicKey": c.StripeConfig.PublicKey != "",
	} {
		if !present {
			return c, fmt.Errorf("%s is missing", key)
		}
	}
	return c, nil
}

// [2] A handle for each service, and the environment that carries them.
// Each constructor checks its values and touches no network (Q2).
func environment(c Config, clock ktime.Clock) (app.Env, error) {
	stripe, e1 := khttp.New(khttp.Service{Address: "https://api.stripe.com", Encoding: khttp.Form, MaxAnswer: 1 << 20,
		Headers: map[string]string{"Authorization": "Bearer " + c.StripeConfig.SecretKey, "Stripe-Version": "2026-02-25.clover"}}) // Q3
	google, e2 := khttp.New(khttp.Service{Address: "https://maps.googleapis.com", Encoding: khttp.Form, MaxAnswer: 1 << 20,
		Query: map[string]string{"key": c.GooglePlaceAPIKey}}) // Q2
	idp, e3 := khttp.New(khttp.Service{Address: or(c.AuthConfig.FaInternalURL, c.AuthConfig.FaURL), Encoding: khttp.JSON,
		MaxAnswer: 1 << 20, Headers: map[string]string{"Authorization": c.AuthConfig.APIKey}})
	smtp, e4 := mail.NewSmtp(mail.Server{Host: c.SmtpConfig.Server, Port: c.SmtpConfig.Port,
		Security: c.SmtpConfig.Transport, Username: c.SmtpConfig.Username, Password: c.SmtpConfig.Password})
	d := c.Deployment
	return app.Env{Clock: clock, Stripe: stripe, Google: google, Idp: idp, Smtp: smtp,
		Operators: d.Operators, OpsOrigin: d.OpsOrigin, ExtraAllowedOrigins: d.ExtraAllowedOrigins,
		IdpAddress: c.AuthConfig.FaURL, StripePublicKey: c.StripeConfig.PublicKey, GoogleMapsBrowserKey: d.GoogleMapsBrowserKey,
		ShowTestModeBand: d.ShowTestModeBand, RefreshGooglePlaceIds: d.RefreshGooglePlaceIds,
		ExposeOneTimeCode: d.ExposeOneTimeCode, TrustDevTenantHeader: d.TrustDevTenantHeader,
	}, errors.Join(e1, e2, e3, e4)
}

// [3] The pool, from the JDBC URL the Kotlin reads (Q11).
func open(ctx context.Context, c Config) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(strings.TrimPrefix(c.DbConfig.URI, "jdbc:"))
	if err != nil {
		return nil, err
	}
	pc.ConnConfig.User, pc.ConnConfig.Password = c.DbConfig.User, c.DbConfig.Password
	pc.MaxConns = c.DbConfig.MaximumPoolSize
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err == nil {
		err = pool.Ping(ctx) // a database that is down fails the start, not the first request
	}
	return pool, err
}

// [4] The api lists at their paths, the two routes, and the drain.
func web(ctx context.Context, c Config, env app.Env, pool *pgxpool.Pool, clock ktime.Clock, log *slog.Logger) error {
	handler := func(t rpc.Table[app.Env]) *rpc.Handler[app.Env] {
		h := rpc.NewHandler(t, pool, env, clock, log)
		h.Limit = requestLimit // Origin and CSRF: not designed (Q4)
		return h
	}
	mux := http.NewServeMux()
	for path, t := range map[string]rpc.Table[app.Env]{
		"/backend/rpc": booker.Booker, "/backend/ops/rpc": ops.Ops, "/backend/ops/admin": ops.Storefront, // Q5
	} {
		mux.Handle(path+"/", http.StripPrefix(path, handler(t)))
	}
	calls := handler(routes.Routes) // served to nobody: Go calls it (Q1)
	mux.HandleFunc("GET /backend/logout", logout(calls))
	mux.HandleFunc("POST /backend/smtp2go-webhook", webhook(calls, c.SmtpConfig.WebhookSecret, log))
	srv := &http.Server{
		Addr:              net.JoinHostPort(or(c.Deployment.BackendHost, "127.0.0.1"), strconv.Itoa(c.Deployment.BackendPort)),
		Handler:           http.MaxBytesHandler(behindProxy(mux), 1<<20),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	drained := make(chan error, 1)
	go func() {
		<-ctx.Done()
		wait, cancel := context.WithTimeout(context.Background(), 75*time.Second) // inside systemd's 90
		defer cancel()
		drained <- srv.Shutdown(wait)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return <-drained
}

// The client's address is nginx's X-Real-IP, which nginx overwrites, so
// no client chooses it; never X-Forwarded-For, to which nginx appends.
// It holds while the server listens on loopback alone.
func behindProxy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip := r.Header.Get("X-Real-IP"); ip != "" {
			r.RemoteAddr = net.JoinHostPort(ip, "0")
		}
		next.ServeHTTP(w, r)
	})
}

// The identity service sends the browser here after its own logout.
func logout(calls *rpc.Handler[app.Env]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reply, err := rpc.Call(calls, r, routes.Logout_op, rt.Unit{})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		rpc.SetCookies(w, reply.Cookies)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// SMTP2GO's bounces, as Bounces.kt answers them: 200 for any JSON object,
// so only our own failure makes SMTP2GO send it again. A body that is not
// a JSON object is a 500 on purpose: it most likely means SMTP2GO's output
// is set to form encoding, and its retries keep those events for 48 hours.
func webhook(calls *rpc.Handler[app.Env], secret *string, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if secret == nil || strings.TrimSpace(*secret) == "" ||
			subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+*secret)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		field := func(k string) (string, bool) { s, ok := body[k].(string); return s, ok } // as? String
		event, ok := field("event")
		if ok && event != "bounce" && event != "reject" {
			return
		}
		id, okID := field("message-id")
		rcpt, okRcpt := field("rcpt")
		if !ok || !okID || !okRcpt { // the 200 counts as delivered: the log is the only trace
			log.Warn("smtp2go webhook lacks message-id, rcpt or event", "keys", slices.Collect(maps.Keys(body)))
			return
		}
		kind, okKind := field("bounce")
		reason, _ := field("message")
		in := routes.DeliveryFailure{MessageId: id, Rcpt: rcpt, Event: event, Reason: reason}
		if okKind {
			in.Kind = &kind
		}
		if _, err := rpc.Call(calls, r, routes.RecordDeliveryFailure_op, in); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func or(s, otherwise string) string {
	if s == "" {
		return otherwise
	}
	return s
}
```

## Part by part

What a mistake in each part does, and when it shows: at **build**
(Go's compiler), at **start** (the process exits, or `check-config`
fails the deploy before anything stops), or at **use** (the first
request, row or call that reaches it, which for the webhook may be
days after a deploy). **Never** means no test sees it.

| Part | Lines | Grows with | Written wrong | Shows |
| --- | --- | --- | --- | --- |
| `run`: modes, flag, signals | 55 | nothing | a mode misspelt: it is taken for a command | start: "unknown command", exit 1 |
| | | | words after the mode not refused | never: `web --config f` reads `CUSOON_CONFIG_FILE`, the ambient file `-D` exists to beat |
| [1] the configuration | 63 | a key: 1 to 2 | a key unread or misspelt in the struct | start: unknown key |
| | | | a required key left off the list in `load` | use: a zero value, e.g. Stripe answers 401 at the first charge |
| | | | `operators` not required | never: every alert goes to nobody |
| [2] the handles | 11 | a service: 3 | a wrong address or encoding | use: the first call fails, as a `Left` |
| | | | a credential in the wrong header | use: 401 from the service |
| | | | no limit on an answer's length | start, if the constructor requires one (Q2) |
| [2] the environment | 6 | a value Kogo reads: ⅓ | a field left out | use: Go's zero; build, under Q13's a |
| [3] the pool | 13 | nothing | a URL pgx cannot read; a database that is down | start (`Ping`) |
| [4] the lists at their paths | 10 | a list: ⅓ | a path that does not match nginx's or the TypeScript client's | use: 404 |
| | | | a list left out | use: 404; nothing else, since a list's TypeScript still compiles |
| [4] Origin and CSRF | 0 | | absent, as written | never: any site's page can post as a logged-in user. This `main` serves no mutation until Q4 is settled |
| [4] the client's address | 8 | nothing | X-Forwarded-For, or the server not on loopback | never: a client picks its own address |
| | | | `behindProxy` left out | use: every request is 127.0.0.1, and everyone shares one per-address login limit |
| [4] the limit, the body's limit, the drain | 17 | nothing | no `ReadHeaderTimeout`, `MaxBytesHandler` or drain | use, under load or at a deploy: a stop cuts requests mid-way |
| [4] logout | 11 | a route: 10 to 35 | cookies not written before the redirect | use: the browser stays logged in on its side, its session gone |
| [4] the webhook | 34 | | the secret unset or blank let through | never: anyone can mark a message bounced. `==` instead of `subtle` leaks timing |
| | | | a 400 for a body that is not JSON | never: form-encoded events are dropped, not kept for 48 hours |
| | | | the wrong key read from SMTP2GO's body | use: every bounce logged as lacking a field and dropped |
| [5] the runner | 1 | nothing: a queue is Kogo's | `ctx`, the signal's, reaching a row's transaction or `act` | at a deploy: SIGTERM cancels a Stripe call in flight. `work.Run` stops claiming rows when `ctx` is done and finishes each row on a context of its own, as the Kotlin's `Drain` does (`GracefulShutdown.kt:31`) |
| [6] commands, `check-config` | 10 | nothing: a command is Kogo's | `check-config` opening the pool | start: the deploy's check needs the database |

About 310 lines in the file, 240 of them code, not imports, blanks
or comments: twice the issue's guess of 80 to 120. Written once:
about 135 (`run`, `load`'s frame, the pool, `web`'s frame, the
address, the drain, `or`). The other 105 grow: a service adds 3 lines
and its keys; a key adds 1 or 2; a list a third of a line; a route
10 to 35 and a line of `web`. A queue or a command adds nothing. The
configuration is the largest part, and the one the Kotlin also writes
by hand (`CusoonConfig.kt`).

**Not in `main`, as the designs have it.** `migrate` is the migration
tool's (`doc/gates.md`, "Commands"). The ten commands are rows of
`commands admin`, run by `command.Run`. Which cookies a login, `whoAmI`
or logout sets is Kogo's (`Reply`); a route's Go writes them only
because it answers with a redirect. The tenant from the host or
`X-Dev-Tenant`, `isSecure`, the session from its cookie: each is a
gate's reading of the `Request`. The login callback goes with Google's
login (`doc/gates.md`, "Routes that are not operations"), so
`clientSecret` and `backendServer`, which it reads, are ignored here.

**Where it differs from the Kotlin.**
- The booker's path moves from `/backend/graphql` to `/backend/rpc`;
  nginx and the TypeScript client move with it.
- Logout redirects with 303, from `doc/gates.md`; Javalin's `redirect`
  answers 302.
- `upstreamTimeouts` is read and dropped: `doc/work.md` sets nothing
  for Stripe, Google or mail, one deadline at the outer edge.
- `--config f` before the mode replaces `-Dcusoon.configurationFile`,
  and the mode or command replaces `--op X`.

## Two ways a route calls an operation

What Go hands the operation (Q1's second half).

**a. By wire name, JSON in and out.** `rpc.Handler` gains `Call(r,
name, body) (answer []byte, cookies []*http.Cookie, err error)`, which
runs `serve`'s middle with no `ResponseWriter`.

```go
body, _ := json.Marshal(map[string]any{"messageId": id, "rcpt": rcpt,
	"event": event, "kind": in.Kind, "reason": reason})
_, _, err := calls.Call(r, "recordDeliveryFailure", body)
```

It reuses the strict reader and names no emitted Go type. A renamed
property, or a misspelt key or operation, is a 400 or 404 at use: the
first bounce in production. About 15 lines of `rpc` and 30 of tests.

**b. By the emitted types, as written above.** The emitter writes,
for each operation, a typed value beside the table (`routes.Logout_op`,
of `rpc.Op[Env, I, A]`); `rpc.Call(h, r, op, input I) (A, error)` runs
it. A renamed property or removed operation is a compile error.
Hand-written Go then names emitted Go, so the emitter's naming rule
reaches `main`; it already names `app.Env` and each table. About 10
lines of emitter, 20 of `rpc` and 40 of tests.

## Checked against the designs

Each line of `main` was read against `doc/gates.md` ("Commands",
"Routes that are not operations"), `doc/work.md` ("An endpoint is
declared once", "What Go would decide, Kogo states", "Time limits",
"The list of work") and `decisions.md` (DEV-850, the seam). What
is not a plain reading of them:

- **Departures**: the webhook's answers follow `Bounces.kt`, not the
  sketch in `doc/gates.md` (200 for a missing field, 500 for a body
  that is not a JSON object, where the sketch has 400 for both).
- **Rules the Go holds**: the secret's check decides who may call
  `recordDeliveryFailure`, which is access, and the seam puts access
  in Kogo; `doc/gates.md` agreed to compare the secret in Go, so it is
  that agreed exception. `pool.Ping` reads the pool outside `Call`,
  `command.Run` and `work.Run`; it reads nothing of the application's
  and stays. Origin and CSRF would be a third: Q4.
- **Added to the prelude**: `NewHandler` gains the clock ("How it is
  tested"); `rpc.Call` or `Handler.Call` (Q1); `khttp.New` (Q2).

## Open

Each is left open; the lean is the writer's. **Greg's** are Q1, Q4,
Q5 and Q13: what the seam, `Request` and the emitter's `Env` are. The
**lead** can settle the rest; Q2 amends an agreed handle, so Greg is
told when it is.

**Q1 (Greg). How a route reaches its operation, and what Go hands it.**
- First, the way in: (a) through a handler of the list that is not
  mounted, so the call has a request id, a `served` line, the limit
  and reruns, as a browser's does, as written; it adds an entry point
  to `rpc`. (b) The route runs the emitted function in its own
  transaction, which copies the handler's rules into each `main`.
- Then the input: (a) the wire name with JSON; (b) the emitted types,
  as written ("Two ways", above).
- Lean a, then the emitted types: a mistake shows when Go compiles.

**Q2 (lead; tell Greg). The handle's Go form, and Google's key.**
`khttp.New` is not designed, and Google's legacy APIs take the key in
the query string. (a) A handle states query fields beside its
headers, as written. (b) Google's client adds the key: a secret in
Kogo, against "Kogo code holds no secret". Lean a, with `MaxAnswer`
required so a handle made without one fails at start.

**Q3 (lead). Where `Stripe-Version` is stated.** It decides which keys
Stripe's answers carry, which `stripe.kg` reads. (a) In `main`, as
written. (b) A `val` in `stripe.kg` that `main` reads from the emitted
Go, so the version and the keys read under it change in one file.
Lean b.

**Q4 (Greg). Origin and CSRF.** cusoon2's Origin check reads the
tenant table and its CSRF check the session row, so under the seam
neither can be Go in `main`. (a) `Sec-Fetch-Site` in Go in `main`,
with Origin and the CSRF token checked by the session gates in Kogo,
since a gate already reads and decides (`doc/gates.md`). (b) A
handler option that calls a Kogo function. (c) An issue of its own
before choosing. Lean c, with a as its start. Either way it lands
before this `main` serves a mutation.

**Q5 (Greg). The storefront in the ops admin's path**
(`/backend/ops/s/{storefront}/graphql`). A handler reads no path.
(a) The storefront moves into each input, where its gate reads it, as
written. (b) `Request` gains the path. Lean a: `Request` stays as
agreed.

**Q6 (lead). A queue's lease**: (a) a constant of the runner, (b) the
effect's form, (c) a map passed to `work.Run`. Lean a.

**Q7 (lead). Reading the configuration strictly.** `encoding/json`
reads a missing key as zero and matches keys whatever their case.
(a) A list of required keys in `main`, as written, which grows with
each key. (b) Kogo's strict reader, exported for a Go struct, which
refuses a missing key, but nothing is optional there (spec 15.4) and
cusoon2's file leaves keys out for their defaults. Lean a.

**Q8 (lead). The clock goes in twice**, in `Env` and to the handler
and runner: (a) leave it, (b) the runner reads it from `Env` through
an interface. Lean a.

**Q9 (lead). Postgres's idle limit** is `doc/work.md`'s Open: (a) the
handler and runner read it from the pool, (b) `main` passes it. Lean a.

**Q10 (lead). Google's answers in CI** (`googleAnswers`). (a) `main`
points the handle at a local server that replays. (b) The binding
gains a replay. Lean a: nothing in Kogo.

**Q11 (lead). Which configuration file.** (a) The Kotlin's, as
written: `jdbc:` cut, its other keys ignored by name, and a JDBC
parameter Postgres does not know fails at start. (b) cusoon2 writes a
file for the Go. Lean a, until the port ends.

**Q12 (lead). Waiting for a connection**: Hikari gives up at 8
seconds, pgx at the request's 90. (a) Leave it. (b) A pool limit.
Lean a.

**Q13 (Greg). An `Env` that cannot be built short.** (a) The emitter
writes a constructor taking every field, so a field left out of `Env`
or of an input is a compile error, as a Kotlin constructor makes it.
(b) A struct literal, as written, where a field left out is Go's zero.
Lean a.
