# cusoon2's main, on paper

DEV-903, 17 Nov 2026. cusoon2's Go `main` written out whole, as the
designs of 17 Nov would have it: `doc/gates.md`, `doc/work.md`, and
`decisions.md`'s seam ("Where Kogo ends and Go begins"). Read from
cusoon2 at `691aa93cf` (`Main.kt`, `CusoonApp.kt`, `CusoonAppState.kt`,
`GracefulShutdown.kt`, `config/CusoonConfig.kt`, `AuthConfig.kt`,
`Bounces.kt`). Rewritten on 18 Nov (DEV-908) to the answers Greg and
the lead gave that day to its thirteen questions: Qn points into
"Open", where each is settled in a line, and `decisions.md` holds each
rule with its reasons. It is not built. Every Go name below that is
not `net/http`'s, `slog`'s or pgx's is invented: the emitted packages,
and the prelude's handle, command and runner APIs, which no design has
given a Go form yet. Four are the names of the session that settled
the questions and are provisional: `X-Storefront`, `rpc.NewCalls`
(and `rpc.Calls`, what it returns), `rpc.ReadSettings`, and a step's
type such as `DeliveryFailure_needsRcpt`.

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

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	"github.com/Cloud-Limo/kogo/prelude/kogo/command"
	khttp "github.com/Cloud-Limo/kogo/prelude/kogo/http"
	"github.com/Cloud-Limo/kogo/prelude/kogo/mail"
	"github.com/Cloud-Limo/kogo/prelude/kogo/rpc"
	ktime "github.com/Cloud-Limo/kogo/prelude/kogo/time"
	"github.com/Cloud-Limo/kogo/prelude/kogo/work"
	"github.com/Cloud-Limo/kogo/rt"

	"cusoon2/out/admin"  // commands admin
	"cusoon2/out/app"    // data class Env, data class Settings
	"cusoon2/out/booker" // api booker
	"cusoon2/out/daemon" // work daemon
	"cusoon2/out/ops"    // api ops, api storefront
	"cusoon2/out/routes" // api routes { logout, recordDeliveryFailure }: no check, so never mounted
	stripekg "cusoon2/out/stripe" // val apiVersion, beside the keys read under it (Q3)
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

// [1] Go's own keys: what makes a handle or opens the server. Read
// strictly: a key this struct lacks stops the start (Q7). "settings" is
// Kogo's object, handed on unread. Keys the Kotlin reads and this does
// not, and the keys the Kotlin itself ignores, are named and ignored as
// json.RawMessage.
type Config struct {
	Deployment struct {
		BackendHost      string
		BackendPort      int
		PlatformAdminIds json.RawMessage // retired; old files carry it
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
		FaInternalURL, APIKey       string
		ClientSecret, BackendServer json.RawMessage // the login callback's (Not in main)
	}
	GooglePlaceAPIKey string
	GoogleAddress     string          // CI's files alone: a local server that replays (Q10)
	StripeConfig      struct{ SecretKey string }
	Settings          json.RawMessage // Kogo's: app.Settings declares its keys

	UpstreamTimeouts, GoogleAnswers     json.RawMessage // CI's and agents' files, for the Kotlin
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
	for key, present := range map[string]bool{ // Q7: Go reads a missing key as zero
		"dbConfig.uri": c.DbConfig.URI != "", "dbConfig.maximumPoolSize": c.DbConfig.MaximumPoolSize > 0,
		"dbConfig.user": c.DbConfig.User != "", "dbConfig.password": c.DbConfig.Password != "",
		"deployment.backendPort": c.Deployment.BackendPort > 0,
		"smtpConfig.server":      c.SmtpConfig.Server != "", "smtpConfig.port": c.SmtpConfig.Port > 0,
		"smtpConfig.transport":   c.SmtpConfig.Transport != "",
		"authConfig.apiKey":      c.AuthConfig.APIKey != "", "googlePlaceApiKey": c.GooglePlaceAPIKey != "",
		"stripeConfig.secretKey": c.StripeConfig.SecretKey != "",
	} {
		if !present {
			return c, fmt.Errorf("%s is missing", key)
		}
	}
	return c, nil
}

// [2] The settings, a handle for each service, and the environment that
// carries them. The reader and each constructor check their values and
// touch no network.
func environment(c Config, clock ktime.Clock) (app.Env, error) {
	// A setting is a property of Settings in Kogo and a key of "settings"
	// in the file: this line never changes. It refuses a setting that is
	// missing and reports, on stderr, a key Settings does not have.
	settings, e0 := rpc.ReadSettings[app.Settings](c.Settings, os.Stderr)
	// Each handle's Timeout is a placeholder for the application to set.
	stripe, e1 := khttp.New(khttp.Service{Address: "https://api.stripe.com", Encoding: khttp.Form, MaxAnswer: 1 << 20,
		Timeout: 30 * time.Second,
		Headers: map[string]string{"Authorization": "Bearer " + c.StripeConfig.SecretKey, "Stripe-Version": stripekg.ApiVersion}})
	google, e2 := khttp.New(khttp.Service{Address: or(c.GoogleAddress, "https://maps.googleapis.com"), Encoding: khttp.Form,
		MaxAnswer: 1 << 20, Timeout: 10 * time.Second, Query: map[string]string{"key": c.GooglePlaceAPIKey}}) // Q2
	// The identity service's public address is a setting, and the handle's
	// when the file gives no internal one: which owns it is the settings' issue's.
	idp, e3 := khttp.New(khttp.Service{Address: strings.TrimRight(or(c.AuthConfig.FaInternalURL, settings.IdpAddress), "/"),
		Encoding: khttp.JSON, MaxAnswer: 1 << 20, Timeout: 10 * time.Second,
		Headers: map[string]string{"Authorization": c.AuthConfig.APIKey}})
	smtp, e4 := mail.NewSmtp(mail.Server{Host: c.SmtpConfig.Server, Port: c.SmtpConfig.Port,
		Security: c.SmtpConfig.Transport, Username: c.SmtpConfig.Username, Password: c.SmtpConfig.Password})
	// The emitter's chain: a step for each property of Env, in its order,
	// so one left out or misplaced does not compile (Q13). Never app.Env{}.
	return app.Env_().Clock(clock).Stripe(stripe).Google(google).Idp(idp).Smtp(smtp).Settings(settings),
		errors.Join(e0, e1, e2, e3, e4)
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
// Origin and CSRF have no line here: each mounted list names its check,
// which the handler runs before every operation (Q4).
func web(ctx context.Context, c Config, env app.Env, pool *pgxpool.Pool, clock ktime.Clock, log *slog.Logger) error {
	handler := func(t rpc.Table[app.Env]) *rpc.Handler[app.Env] {
		h := rpc.NewHandler(t, pool, env, clock, log)
		h.Limit = requestLimit
		return h
	}
	mux := http.NewServeMux()
	for path, t := range map[string]rpc.Table[app.Env]{
		"/backend/rpc": booker.Booker, "/backend/ops/rpc": ops.Ops, "/backend/ops/admin": ops.Storefront,
	} {
		mux.Handle(path+"/", http.StripPrefix(path, handler(t)))
	}
	// routes.Routes names no check, so it is a type NewHandler does not
	// take and no http.Handler: only rpc.Call runs its operations (Q1, Q4).
	calls := rpc.NewCalls(routes.Routes, pool, env, clock, log)
	calls.Limit = requestLimit
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

// A route fills its operation's input by the emitter's chain, a
// step for each property. Fine for two routes; if routes multiply,
// revisit the chain and routes in Go (decisions.md).

// The identity service sends the browser here after its own logout.
func logout(calls *rpc.Calls[app.Env]) http.HandlerFunc {
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
func webhook(calls *rpc.Calls[app.Env], secret *string, log *slog.Logger) http.HandlerFunc {
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
		var kind *string
		if k, ok := field("bounce"); ok {
			kind = &k
		}
		reason, _ := field("message")
		in := routes.DeliveryFailure_().MessageId(id).Rcpt(rcpt).Event(event).Kind(kind).Reason(reason)
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
| [1] Go's keys | 54 | a key of Go's: 1 to 2 | a key unread or misspelt in the struct | start: unknown key |
| | | | a required key left off the list in `load` | use: a zero value, e.g. Stripe answers 401 at the first charge |
| [2] the settings | 1 | nothing: a setting is Kogo's and the file's | a setting missing, misspelt or of the wrong type in the file | start: `settings.opsOrigin is missing` |
| | | | a key `Settings` does not have | start: reported, and ignored |
| | | | a secret declared as a setting | never: a reviewer's rule |
| [2] the handles | 9 | a service: 2 | a wrong address or encoding | use: the first call fails, as a `Left` |
| | | | a credential in the wrong header | use: 401 from the service |
| | | | no limit on an answer's length, or on a call's time | start: `khttp.New` requires `MaxAnswer` (Q2) and `Timeout` |
| [2] the environment | 3 | a handle: a step | a step left out, misplaced or misnamed | build (Q13) |
| | | | `app.Env{}` in place of the chain | never: it compiles. A reviewer holds `main` to the chain; nothing checks it |
| [3] the pool | 13 | nothing | a URL pgx cannot read; a database that is down | start (`Ping`) |
| [4] the lists at their paths | 10 | a list: ⅓ | a path that does not match nginx's or the TypeScript client's | use: 404 |
| | | | a list left out | use: 404; nothing else, since a list's TypeScript still compiles |
| [4] Origin and CSRF | 0 | nothing: the check is the list's, in Kogo | a list that names no check mounted | build (Q4) |
| | | | a `Table` built from `routes`' operations | build: `Unserved`'s fields are unexported (`doc/origin.md`, Q13) |
| | | | the check not built yet | this `main` serves no browser until its code lands (DEV-914); designed in `doc/origin.md` |
| [4] the client's address | 8 | nothing | X-Forwarded-For, or the server not on loopback | never: a client picks its own address |
| | | | `behindProxy` left out | use: every request is 127.0.0.1, and everyone shares one per-address login limit |
| [4] the limit, the body's limit, the drain | 17 | nothing | no `ReadHeaderTimeout`, `MaxBytesHandler` or drain | use, under load or at a deploy: a stop cuts requests mid-way |
| [4] logout | 11 | a route: 10 to 35 | cookies not written before the redirect | use: the browser stays logged in on its side, its session gone |
| [4] the webhook | 34 | | the secret unset or blank let through | never: anyone can mark a message bounced. `==` instead of `subtle` leaks timing |
| | | | a 400 for a body that is not JSON | never: form-encoded events are dropped, not kept for 48 hours |
| | | | the wrong key read from SMTP2GO's body | use: every bounce logged as lacking a field and dropped |
| | | | a property of the input left out or exchanged, or renamed in Kogo | build (Q1, Q13) |
| [5] the runner | 1 | nothing: a queue is Kogo's | `ctx`, the signal's, reaching a row's transaction or `act` | at a deploy: SIGTERM cancels a Stripe call in flight. `work.Run` stops claiming rows when `ctx` is done and finishes each row on a context of its own, as the Kotlin's `Drain` does (`GracefulShutdown.kt:31`) |
| [6] commands, `check-config` | 10 | nothing: a command is Kogo's | `check-config` opening the pool | start: the deploy's check needs the database |

About 315 lines in the file, 230 of them code, not imports, blanks or
comments; before the answers of 18 Nov it was 310 and 240. Written
once: about 135 (`run`, `load`'s frame, the pool, `web`'s frame, the
address, the drain, `or`). The other 95 grow, and with less than they
did: a service adds 2 lines, a step and its keys; a key of Go's adds 1
or 2; a list a third of a line; a route 10 to 35 and a line of `web`.
A setting, a queue or a command adds nothing: on paper before, a
setting was a field of `Config`, an entry of `load`'s list and a third
of a line of `environment`. Go's keys are still the largest part.

**Not in `main`, as the designs have it.** `migrate` is the migration
tool's (`doc/gates.md`, "Commands"). The ten commands are rows of
`commands admin`, run by `command.Run`. Which cookies a login, `whoAmI`
or logout sets is Kogo's (`Reply`); a route's Go writes them only
because it answers with a redirect. The tenant from the host or
`X-Dev-Tenant`, `isSecure`, the session from its cookie, the
storefront an ops admin's call is for (`X-Storefront`, Q5): each is a
gate's reading of the `Request`. Origin and CSRF are both the list's
check (Q4; `doc/origin.md`, Q6). The ten settings are properties of
`Settings`, which Kogo declares. The login callback goes with Google's
login (`doc/gates.md`, "Routes that are not operations"), so
`clientSecret` and `backendServer`, which it reads, are ignored here.

**Where it differs from the Kotlin.**
- The booker's path moves from `/backend/graphql` to `/backend/rpc`;
  nginx and the TypeScript client move with it.
- Logout redirects with 303, from `doc/gates.md`; Javalin's `redirect`
  answers 302.
- The ops admin's path loses its storefront,
  `/backend/ops/s/{storefront}/graphql` becoming `/backend/ops/admin`:
  the ops site names the storefront in `X-Storefront` (Q5).
- The file gains a `settings` object, and ten keys move into it:
  from `deployment`: `operators`, `opsOrigin`, `extraAllowedOrigins`,
  `googleMapsBrowserKey` and the four flags; `stripeConfig.publicKey`;
  `authConfig.faUrl`. None has a default, `dev`, the old name of
  `operators`, is no longer read, and a key `Settings` does not have
  is reported where the Kotlin refuses it. cusoon2 moves its reader,
  the deploy script's `jq` and each file first, in the issue that
  writes this `main` (Q11). `googleAddress`, CI's key for the replay
  server (Q10), is one the Kotlin's reader refuses, so CI's file
  carries it only once cusoon2's reader knows it, in that same move.
- The identity service's public address reaches Kogo as the file has
  it: only the handle's copy is trimmed of its trailing `/`, where the
  paper of 17 Nov trimmed it in `load` for `Env` too. The trim's owner
  is the settings issue.
- `upstreamTimeouts` is read and dropped: `doc/work.md` sets nothing
  for Stripe, Google or mail, one deadline at the outer edge. Until
  that deadline is built `khttp.New` requires a `Timeout` of each
  handle, and the three figures above are placeholders.
- `--config f` before the mode replaces `-Dcusoon.configurationFile`,
  and the mode or command replaces `--op X`.

## Two ways a route calls an operation

**b is the way** (Q1): by the emitted types, as written above. The
emitter writes, for each operation, a typed value beside the table
(`routes.Logout_op`, of `rpc.Op[Env, I, A]`), and `rpc.Call(h, r, op,
input I) (A, error)` runs it through a handler `main` does not mount,
checking the input's enums against their entries as the strict reader
does. A renamed property or a removed operation is a compile error,
and the chain (Q13) makes one for a property added. Hand-written Go
then names emitted Go, so the emitter's naming rule reaches `main`; it
already names `app.Env` and each table. About 10 lines of emitter, 25
of `rpc` and 50 of tests.

Not taken was a, by wire name with JSON in and out
(`calls.Call(r, "recordDeliveryFailure", body)`): a renamed property,
or a misspelt key or operation, is a 400 or 404 at the first bounce in
production (`decisions.md`).

## Checked against the designs

Each line of `main` was read against `doc/gates.md` ("Commands",
"Routes that are not operations"), `doc/work.md` ("An endpoint is
declared once", "What Go would decide, Kogo states", "Time limits",
"The list of work") and `decisions.md` (DEV-850, the seam, and the
six entries of 18 Nov that settle "Open"). What is not a plain reading
of them:

- **Departures**: the webhook's answers follow `Bounces.kt`, not the
  sketch in `doc/gates.md` (200 for a missing field, 500 for a body
  that is not a JSON object, where the sketch has 400 for both).
- **Rules the Go holds**: the secret's check decides who may call
  `recordDeliveryFailure`, which is access, and the seam puts access
  in Kogo; `doc/gates.md` agreed to compare the secret in Go, so it is
  that agreed exception. `pool.Ping` reads the pool outside `Call`,
  `command.Run` and `work.Run`; it reads nothing of the application's
  and stays. Origin and CSRF are no third: both are Kogo's (Q4).
- **Rules a reviewer holds**, since nothing checks them: `main` builds
  `Env` by the chain and never as `app.Env{}`; no secret is a setting.
- **Added to the prelude**: `NewHandler` gains the clock ("How it is
  tested"); `rpc.Call`, `rpc.NewCalls`, `rpc.Unserved` with
  `rpc.NewUnserved`, and `rpc.SetCookies` (Q1, Q4);
  `rpc.ReadSettings` (settings); `khttp.New`, with a handle's query
  fields (Q2). `rpc.Request` stays impossible to make outside `rpc`.
- **Added to the emitter**: an `_op` value for each operation (Q1),
  the chain for each list's environment and each data class an input
  reaches (Q13), and a list that names no check as a type of its own
  (Q4).

## Open

Nothing of the thirteen: each was settled on 18 Nov 2026, Q1, Q4, Q5
and Q13 by Greg and the rest by the lead, with Greg's rule for
settings reaching Q7 and Q11. `decisions.md` has each rule, what was
not taken and what reopens it. What Q4 and the settings leave to
their own issues is in their entries.

- **Q1 (Greg). How a route reaches its operation: a, then the emitted
  types.** Through a handler of its list that `main` does not mount,
  with `rpc.Call` and the operation's `_op` value.
- **Q2 (lead; Greg is told). The handle's Go form, and Google's key:
  a.** A handle states its query fields beside its headers, and
  `khttp.New` requires `MaxAnswer`.
- **Q3 (lead). Where `Stripe-Version` is stated: b.** A `val` in
  `stripe.kg`, read by `main` from the emitted Go.
- **Q4 (Greg). Origin and CSRF: c, an issue of its own.** It started
  from three rules: the list names its check; a list that names none
  cannot be mounted; the token is the session reader's, which Q6 c of
  `doc/origin.md` replaced: the list's check compares the token too.
  The design is `doc/origin.md`, its thirteen questions answered on 18
  Nov 2026; this `main` serves no browser until its code lands.
- **Q5 (Greg). The storefront of an ops admin's call: a header.**
  Neither of the two options: one shared gate of `api storefront`
  reads `X-Storefront`, and the list is mounted at one fixed path.
- **Q6 (lead). A queue's lease: a.** A constant of the runner.
- **Q7 (lead, after Greg's rule). Reading the configuration
  strictly.** Settings by Kogo's strict reader; for Go's own keys a,
  a struct that refuses a key it does not know and a list of those
  that must be present.
- **Q8 (lead). The clock: a.** It goes in twice, in `Env` and to the
  handler and runner.
- **Q9 (lead). Postgres's idle limit: a.** The handler and the runner
  read it from the pool.
- **Q10 (lead). Google's answers in CI: a.** `main` points the handle
  at a local server that replays; nothing in Kogo.
- **Q11 (lead, after Greg's rule). Which configuration file.** The
  Kotlin's with a `settings` object added, cusoon2 moving its keys
  first, in the issue that writes its `main`.
- **Q12 (lead). Waiting for a connection: a.** Left at the request's
  limit.
- **Q13 (Greg). A data class that cannot be built short: c, a chain
  of named steps.** Neither of the two options: the emitter writes a
  step for each property, for `Env` and for each input.
- **Settings (Greg).** Kogo declares them as a data class, the file
  holds them in one object, Kogo's strict reader fills them in one
  line of `main`, and `Env` holds them as one property.
