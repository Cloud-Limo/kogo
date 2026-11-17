# cusoon2's main, on paper

DEV-903, 17 Nov 2026. cusoon2's Go `main` written out whole, as the
designs of 17 Nov would have it: `doc/gates.md`, `doc/work.md`, and
`decisions.md`'s seam ("Where Kogo ends and Go begins"). Read from
cusoon2 at `691aa93cf` (`Main.kt`, `CusoonApp.kt`, `CusoonAppState.kt`,
`GracefulShutdown.kt`, `config/CusoonConfig.kt`, `Bounces.kt`). It is
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
	"net"
	"net/http"
	"os"
	"os/signal"
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
// this does not are named and ignored, as the Kotlin's are.
type Config struct {
	Deployment struct {
		BackendHost, OpsOrigin string
		BackendPort            int
		Operators              []mail.Address
		ExtraAllowedOrigins    []string
		GoogleMapsBrowserKey   *string

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
		ClientSecret, BackendServer  json.RawMessage // Google's login, gone
	}
	GooglePlaceAPIKey string
	StripeConfig      struct{ PublicKey, SecretKey string }
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
	for key, present := range map[string]bool{ // Q7: Go reads a missing key as zero
		"dbConfig.uri": c.DbConfig.URI != "", "dbConfig.maximumPoolSize": c.DbConfig.MaximumPoolSize > 0,
		"deployment.backendPort": c.Deployment.BackendPort > 0, "deployment.opsOrigin": c.Deployment.OpsOrigin != "",
		"smtpConfig.server": c.SmtpConfig.Server != "", "smtpConfig.port": c.SmtpConfig.Port > 0,
		"authConfig.faUrl": c.AuthConfig.FaURL != "", "authConfig.apiKey": c.AuthConfig.APIKey != "",
		"googlePlaceApiKey": c.GooglePlaceAPIKey != "", "stripeConfig.secretKey": c.StripeConfig.SecretKey != "",
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

// SMTP2GO's bounces. A body that lacks a field is a 200 and a log line,
// as the Kotlin's, so that SMTP2GO does not send it again.
func webhook(calls *rpc.Handler[app.Env], secret *string, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if secret == nil || *secret == "" ||
			subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+*secret)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var ev struct {
			Event     string  `json:"event"`
			MessageID string  `json:"message-id"`
			Rcpt      string  `json:"rcpt"`
			Bounce    *string `json:"bounce"`
			Message   string  `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		} else if ev.Event == "" || ev.MessageID == "" || ev.Rcpt == "" {
			log.Warn("smtp2go webhook lacks message-id, rcpt or event")
			return
		} else if ev.Event != "bounce" && ev.Event != "reject" {
			return
		}
		_, err := rpc.Call(calls, r, routes.RecordDeliveryFailure_op, routes.DeliveryFailure{
			MessageId: ev.MessageID, Rcpt: ev.Rcpt, Event: ev.Event, Kind: ev.Bounce, Reason: ev.Message})
		if err != nil {
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
days after a deploy).

| Part | Lines | Grows with | Written wrong | Shows |
| --- | --- | --- | --- | --- |
| `run`: modes, flag, signals | 50 | nothing | a mode misspelt: it is taken for a command | start: "unknown command", exit 1 |
| [1] the configuration | 55 | a key: 1 to 2 | a key unread or misspelt in the struct | start: unknown key |
| | | | a required key left off the list in `load` | use: a zero value, e.g. Stripe answers 401 at the first charge |
| [2] the handles | 13 | a service: 3 | a wrong address or encoding | use: the first call fails, as a `Left` |
| | | | a credential in the wrong header | use: 401 from the service |
| | | | no limit on an answer's length | start, if the constructor requires one (Q2) |
| [2] the environment | 6 | a value Kogo reads: ⅓ | a field left out | use: Go's zero; build, if the emitter's `Env` can make a missing field a compile error (Q1) |
| [3] the pool | 15 | nothing | a URL pgx cannot read; a database that is down | start (`Ping`) |
| [4] the lists at their paths | 10 | a list: ⅓ | a path that does not match nginx's or the TypeScript client's | use: 404 |
| | | | a list left out | use: 404; nothing else, since a list's TypeScript still compiles |
| [4] the client's address | 12 | nothing | X-Forwarded-For, or the server not on loopback | never, in a test: a client picks its own address, and per-address limits on logins count nothing |
| [4] the limit, the body's limit, the drain | 16 | nothing | no `ReadHeaderTimeout`, `MaxBytesHandler` or drain | use, under load or at a deploy: a stop cuts requests mid-way |
| [4] logout | 14 | a route: 10 to 25 | cookies not written before the redirect | use: the browser stays logged in on its side, its session gone |
| [4] the webhook | 31 | | the secret compared with `==`, or an unset secret let through | never, in a test: anyone can mark a message bounced |
| | | | the wrong key read from SMTP2GO's body | use: every bounce logged as lacking a field and dropped |
| [5] the runner | 1 | nothing: a queue is Kogo's | | |
| [6] commands, `check-config` | 10 | nothing: a command is Kogo's | `check-config` opening the pool | start: the deploy's check needs the database |

About 280 lines in the file, 215 of them code, not imports, blanks
or comments: about twice the issue's guess of 80 to 120. Written once:
about 115 (`run`, `load`'s frame, the pool, `web`'s frame, the
address, the drain, `or`). The other 100 grow: a service adds 3 lines
and its keys; a key that Kogo reads adds about 2; a list adds a third
of a line; a route adds 10 to 25 and a line of `web`. A queue or a
command adds nothing. The configuration is the largest single part,
and the one part the Kotlin also writes by hand (`CusoonConfig.kt`).

**Not in `main`, as the designs have it.** `migrate` is the migration
tool's (`doc/gates.md`, "Commands"), so cusoon2's deploy calls that
tool where it calls `--op migrate`. The ten commands, `check-pricing`
and `ensure-fusionauth-tenants` among them, are rows of `commands
admin`, read and run by `command.Run`. Which cookies a login, `whoAmI`
or logout sets is Kogo's (`Reply`), and the handler writes them; a
route's own Go writes them only because it answers with a redirect.
The tenant from the host or `X-Dev-Tenant`, `isSecure` from
`X-Forwarded-Proto`, the session from its cookie: each is a gate's
reading of the `Request`. The login callback goes with Google's login
(`doc/gates.md`, "Routes that are not operations").

## Two ways a route calls an operation

Both go through a handler of the `routes` list that is not mounted, so
a route's call has a request id, a `served` line, the request's limit,
reruns and a 503, as a browser's does. The question is what Go hands
it.

**a. By wire name, JSON in and out.** `rpc.Handler` gains `Call(r,
name, body) (answer []byte, cookies []*http.Cookie, err error)`, which
runs `serve`'s middle with no `ResponseWriter`.

```go
body, _ := json.Marshal(map[string]any{"messageId": ev.MessageID, "rcpt": ev.Rcpt,
	"event": ev.Event, "kind": ev.Bounce, "reason": ev.Message})
_, _, err := calls.Call(r, "recordDeliveryFailure", body)

_, cookies, err := calls.Call(r, "logout", nil)
```

- It reuses the strict reader, and names no emitted Go type.
- A renamed property, or a misspelt key or operation, is a 400 or a
  404 at use: the first bounce in production, unless cusoon2 has a
  test that posts to the route against a database.
- About 15 lines of `rpc` and 30 of tests; nothing in the emitter.

**b. By the emitted types, as written above.** The emitter writes,
for each operation of a list, a typed value beside the table
(`routes.Logout_op`, of `rpc.Op[Env, I, A]`), from which the table is
built; `rpc.Call(h, r, op, input I) (A, error)` runs it.

- A renamed property, a changed input or answer, or a removed
  operation is a compile error at build.
- Hand-written Go names emitted Go: the types, their fields and the
  operation's value, so the emitter's naming rule (`export`, and the
  `_api` and full names a clash gives) reaches `main`. `main` already
  names `app.Env` and its fields, and each table, so this is more of a
  tie it has, not a new kind.
- About 10 lines of emitter, 20 of `rpc` and 40 of tests.

Lean b: a mistake shows when Go compiles, where under a it shows at
the first bounce, which no test of Kogo's sees and cusoon2's may not.
Left open, as the issue asks.

## Checked against the designs

- **Routes** (`doc/gates.md`, "Routes that are not operations"): Go
  holds the method, the path and the translation; the secret is
  configuration, compared in Go; logout writes the `Reply`'s cookies
  and a 303 to `/`; each route touches the database only through its
  operation. The sketch there answers 400 to a body missing a field;
  the Kotlin answers 200 with a log line, which is kept here. A body
  that is not JSON is a 400 here, where the Kotlin's parse throws and
  answers 500, which no line of it chose.
- **Commands** ("Commands"): `web` and `batch` are modes, `migrate`
  is the tool's, `check-config` is Go's; a command's outcome is its
  exit code, which `run` returns.
- **Handles** (`doc/work.md`, "An endpoint is declared once"): one
  per service, made in `main` with its address, credentials and
  encoding; no secret in Kogo; the per-tenant headers
  (`Stripe-Account`, `X-FusionAuth-TenantId`) are added in Kogo, and
  `main` sets none of them.
- **What Go would decide** ("What Go would decide, Kogo states"): a
  limit on an answer's length is set in `main`, per handle; no proxy,
  since `main` states none; redirects and the second send are the
  binding's, with no line here.
- **Time limits** ("One deadline at the outer edge"): one number for
  a request, `requestLimit`; nothing for Stripe, Google or mail; a
  queue's lease is the runner's (Q6); the database's idle limit is
  read at start (Q9).
- **Workers** ("The list of work", "Each queue is served on its
  own"): one call starts every queue; the `work` list names the
  queues and the alert, so `main` names neither. Each busy queue
  holds a connection: the dev file's pool of 20 has room for
  cusoon2's 16, and the batch process shares no pool with the web's.
- **The handler** (`decisions.md`, DEV-850): `NewHandler` and
  `Limit`; `main` mounts each list with `http.StripPrefix`, sets
  `RemoteAddr` from the proxy's header, and limits the body with
  `http.MaxBytesHandler`. `NewHandler` gains the clock, as "How it is
  tested" has it.
- **The seam** (`decisions.md`, "Where Kogo ends and Go begins"): no
  line here reads the pool or calls a service, other than through
  `Call`, `command.Run` and `work.Run`. The pool is open in `web`, so
  nothing but this reading holds it.

## Open

Each is left open; the lean is the writer's.

**Q1. How Go calls an operation of a list that is not mounted.** Above,
"Two ways". Lean b. With b, whether the emitter's `Env` and inputs
are built through a constructor that takes every field, so that a
field left out is a compile error, as a Kotlin constructor makes it.

**Q2. The handle's Go form, and Google's key.** `khttp.New` and its
`Service` are not designed. Google's legacy APIs take the key in the
query string, which a handle of headers alone cannot carry.
- a. A handle states query fields beside its headers, as written.
- b. Google's client adds `field("key", ...)`: a secret in Kogo,
  against "Kogo code holds no secret".
- Lean a. And whether `MaxAnswer` is required, so that a handle made
  without one fails at start: lean yes.

**Q3. Where `Stripe-Version` is stated.** It decides which keys
Stripe's answers carry, which `stripe.kg` reads by name.
- a. In `main`, beside the key, as written.
- b. In `stripe.kg`, a `val` that `main` reads from the emitted Go, so
  the version and the keys read under it change in one file.
- Lean b.

**Q4. Origin and CSRF.** They were to be options of the handler
(`doc/design.md`). cusoon2's Origin check reads the tenant table on every
request (`isAllowedOrigin`), and its CSRF check reads the session row,
so under the seam's rule neither can be Go in `main`, and an option of
the handler would call Kogo. `Sec-Fetch-Site` alone is pure. No line
is written for them here. Lean: an issue of its own, before any
mutation is served.

**Q5. The storefront's admin from the ops site.** cusoon2 serves it at
`/backend/ops/s/{storefront}/graphql`. A handler serves `POST /name`
and reads no path, so either the storefront moves into each input,
where its gate reads it, as `requireStorefront` takes it now, or
`Request` gains the path. Written above as a list `storefront`, its
rows the booker's admin work under gates that read the storefront.
Lean: the input.

**Q6. Where a queue's lease is stated.** `doc/work.md` gives it to
"the runner, for each queue"; cusoon2 has one, 2 minutes, for every
effect. a. A constant of the runner. b. A part of the effect's form.
c. A map `main` passes to `work.Run`. Lean a, until two queues differ
(`decisions.md`: the runner holds facts, a queue states choices).

**Q7. Reading the configuration strictly.** Go's `encoding/json`
refuses an unknown key when told to, but reads a missing one as zero,
and matches a key whatever its case, so `URI` and `uri` both read.
- a. A list of required keys in `main`, as written: it grows with each
  key, and a key left off it is the Kotlin's old `Int` read as `0`.
- b. Kogo's strict reader, exported for a Go struct: it already refuses
  a missing key. Nothing is optional there (spec 15.4), and cusoon2's
  file leaves keys out for their defaults.
- Lean a, for one application.

**Q8. The clock goes in twice**, in `Env` and to `NewHandler` and
`work.Run`, since Kogo's Go cannot see into an application's `Env`. A
test that moves one moves only that one. Lean: leave it.

**Q9. Who reads Postgres's idle limit at start** (`doc/work.md`,
"Open"): the handler and the runner from the pool, with no line here,
or `main`, passing it to both. Lean: the handler and the runner.

**Q10. Google's answers in CI.** cusoon2's CI replays Google from a
file (`GoogleMapsFileCache`, the `googleAnswers` key). With Google's
client in Kogo, either `main` points the handle at a local server
that replays, its address from the configuration, or the binding
gains a replay. Lean: the local server; nothing in Kogo.

**Q11. Which configuration file.** As written, the Go reads the
Kotlin's file as it is, for a side-by-side run: `jdbc:` cut from the
URL, JDBC's keys and Google's login's ignored by name. A JDBC
parameter in the URL that Postgres does not know fails at start. Or
cusoon2 writes a file for the Go. Lean: the Kotlin's, until the port
ends.

**Q12. Waiting for a connection.** Hikari gives up after 8 seconds
when the pool has none free; pgx waits as long as the request's
context, here 90 seconds. Lean: leave it, the one deadline covering
it.
