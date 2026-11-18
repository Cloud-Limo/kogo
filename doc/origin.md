# Origin and CSRF, on paper

DEV-909, 18 Nov 2026. The check Greg agreed that day in three rules
(`decisions.md`, "Origin and CSRF"; `doc/main.md`, Q4), written out as
cusoon2 would have it and read against the Kotlin at `691aa93cf`
(`CusoonApp.kt`, `CusoonAppState.kt`, `model/database.kt`,
`OriginCheckTest.kt`, cusoon2 PR 1473). It is not built, and it changes
no other document: the last section lists what would change. Invented
here: `rpc.Unserved`, `Table.Check`, `kogo.ReadOnlyTx`, the code
`check-shape`, `toUuidOrNull`, and the name of every Kogo function
but `sameSite` and `liveSession`.

## What the Kotlin does

1. Three routes call `verifyOrigin` as their first line (`:107`,
   `:146`, `:167`): before the tenant's header, the body, or the ops
   operation's name is read. The login callback, logout and the webhook
   do not call it (`:86`, `:94`, `:185`).
2. `verifyOrigin` (`:406`) refuses a `Sec-Fetch-Site` that is there and
   is neither `same-origin` nor `none`. It then passes a request with
   no `Origin`; trims one trailing `/`; for `null`, refuses when
   `Sec-Fetch-Site` is absent and passes otherwise; and refuses any
   other Origin that is not ours, `same-origin` or not.
3. Ours (`:424`) is one of `extraAllowedOrigins`, whatever
   `trustDevTenantHeader` says, or `opsOrigin`, or equal to some
   `tenant.domain`, active or not (`database.kt:301`). The table is read
   only when the first two miss, on a connection of its own at READ
   COMMITTED that is rolled back; nothing marks it read-only.
4. The token is checked in `execute` (`CusoonAppState.kt:385`), as the
   first statement of the request's SERIALIZABLE transaction and again
   on each rerun. `checkCsrfToken` (`CusoonApp.kt:366`) runs when the
   request has the session's cookie (`:490`), reads the row by id and
   not through `liveSession`, and compares its `csrf_token` with
   `X-CSRF-Token`, a missing header reading as `""`. No cookie, or no
   row, passes. So it covers every request of the three routes that
   carries a session, public reads and logins included, an expired
   session and another tenant's included, before `ensureSession`
   (`:548`) or `ensureOpsSession` (`:565`) keeps or replaces it.
5. Both refusals are 400, `text/plain`, "Invalid origin" and "Invalid
   CSRF token" (`:41`, `:61`). The client throws on any status but 2xx
   (`frontend/src/rpc.ts:33` for ops, Apollo's `HttpLink` at
   `frontend/src/App.tsx:72` for the booker): neither is shown as an
   operation's words.
6. Two reads of the cookie check no token: the login callback locks the
   row (`liveSession(..., forUpdate = true)`, `:243`), and logout
   deletes by the cookie's id and reads no row (`:95`). A cookie that
   is no UUID is a 400 with Java's message for it (`:429`).

## The lists

```kotlin
api booker checkedBy sameSite {
    whoAmI           ungated
    areas            gatedBy anyone
    setBookingNotes  gatedBy setBookingNotesGate
}
api ops        checkedBy sameSite { ... }
api storefront checkedBy sameSite { ... }
api routes { logout ungated, recordDeliveryFailure ungated }   // no check: never mounted
```

- `checkedBy f` names one top-level fun in scope, of the shape `(Tx,
  Env, Request): Either<String, Unit>`, with the list's own `Env`;
  anything else is `check-shape`. One fun serves the three lists, as
  one `verifyOrigin` serves the three routes (1).
- The server runs it before every row of the list, `ungated` and
  `gatedBy anyone` included. A list with no `checkedBy` is legal Kogo.

## The check

```kotlin
// cusoon.gate, beside the gates
fun sameSite(tx: Tx, env: Env, request: Request): Either<String, Unit> = either {
    val site = request.header("Sec-Fetch-Site")
    ensure(site == null || site == "same-origin" || site == "none") { "Invalid origin" }
    val origin = request.header("Origin")?.let { if (it.endsWith("/")) it.substring(0, it.length - 1) else it }
    if (origin == "null") ensure(site != null) { "Invalid origin" }
    else if (origin != null) ensure(isOurOrigin(tx, env.settings, origin)) { "Invalid origin" }
}

fun isOurOrigin(tx: Tx, settings: Settings, origin: String): Boolean =
    origin in settings.extraAllowedOrigins || origin == settings.opsOrigin ||
        tx.exists(sql"select 1 from tenant where domain = $origin")
```

Line by line it is (2) and (3). It differs once: the Kotlin opens its
connection only for the third test, and this is handed a transaction
whatever the request (Q1). An ops page's request, matched by
`opsOrigin`, and a request with no Origin then cost a transaction they
do not use; a tenant's page reaches the table in both.

## The session reader

```kotlin
// cusoon.gate, session.kg: the only file that reads the cookie or selects a session
private fun sessionId(request: Request): Uuid? =
    request.cookie(if (isSecure(request)) "__Host-SID" else "SID")?.let { toUuidOrNull(it) }

private fun session(tx: SerializableTx, id: Uuid): Session? =
    tx.queryOne<Session>(sql"select * from session where id = $id")

fun liveSession(tx: SerializableTx, env: Env, request: Request): Either<String, Session?> = either {
    val row = sessionId(request)?.let { session(tx, it) }
    ensure(row == null || row.csrfToken == (request.header("X-CSRF-Token") ?: "")) { "Invalid CSRF token" }
    row?.takeIf { env.clock.now() < expires(it) }
}

fun endSession(tx: Tx, request: Request) {             // logout's: no row read, no token (6)
    sessionId(request)?.let { tx.execute(sql"delete from session where id = $it") }
}
```

- The compare comes before the expiry, on the row whatever its tenant,
  as (4) has it; which tenant the session must be of stays the
  caller's, as `ensureSession`'s `takeIf` is.
- **Who calls it.** Every gate that needs a person: `booker`, `admin`,
  `booking` and the gates built on them, `maybeBooker`, and the ops
  site's `requireDriver` with the gates built on it, the shared gate
  of `api storefront` among them. The ungated `whoAmI` and each
  login, on both sites, where it first reads its session.
- **Who does not.** `anyone` and the four bearer links' gates, which
  read no session: their operations lose the token's check and keep
  the Origin's (Q6). `logout`, which calls `endSession`; and
  `recordDeliveryFailure`, which has no cookie.
- `doc/gates.md` wrote `liveSession` as giving a `Session?`; it gives
  an `Either`, and `whoAmI` binds it (Q7).

## The emitted Go

```go
// kogo/prelude/kogo/rpc
type Table[Env any] struct { // a list that names its check: what NewHandler takes
	Check      func(kogo.Tx, Env, Request) kogo.Either[string, rt.Unit]
	Operations []Operation[Env]
	Enums      []any
}
type Unserved[Env any] struct { // a list that names none: only NewCalls takes it
	Operations []Operation[Env]
	Enums      []any
}

// cusoon2/out/booker and cusoon2/out/routes, emitted
var Booker = rpc.Table[app.Env]{Check: gate.SameSite, Operations: ..., Enums: ...}
var Routes = rpc.Unserved[app.Env]{Operations: ..., Enums: ...}
```

`NewHandler` takes a `Table` and panics on a nil `Check`; `NewCalls`
takes an `Unserved`, and a `Calls` has no `ServeHTTP` and runs no
check. In `serve`, after the name's 404 and the method's 405 and before
the body is read (Q9):

```go
if refusal := h.check(r); refusal != nil {
	return text(http.StatusBadRequest, *refusal) // Q3
}

// One transaction of its own at READ COMMITTED, read only, rolled back (Q2).
func (h *Handler[Env]) check(r *http.Request) *string {
	result := kogo.ReadOnlyTx(kogo.NewDb(r.Context(), h.pool), func(tx kogo.Tx) kogo.Either[string, rt.Unit] {
		return h.table.Check(tx, h.env, Request{r})
	})
	if left, refused := result.(kogo.Either_Left[string]); refused {
		return &left.Value
	}
	return nil
}
```

The check runs once for a request and not again on a rerun; its time
counts toward the limit; a panic in it is the handler's 500. The token
has no line in the handler: `liveSession` runs in the gate's or the
operation's own transaction and is rerun with it, as (4) has it, and
its `Left` leaves as any refusal does (Q3).

**`main`.** No line of `doc/main.md`'s file changes: `handler` takes
an `rpc.Table[app.Env]` and `NewCalls` takes `routes.Routes` as
written there, and `opsOrigin` and `extraAllowedOrigins` are already
properties of `Settings`.

## Part by part

When a mistake shows, as `doc/main.md` has it: at **build** (`kogo
check` or Go's compiler), at **start**, at **use**, or **never**.

| Part | Written wrong | Shows |
| --- | --- | --- |
| The lists | `checkedBy` left off a list `main` mounts | build: `NewHandler` takes no `Unserved` |
| | a check of another shape or `Env` | build: `check-shape` |
| | a login listed in `api routes` and called by a route | never: no Origin check, the hole of PR 1473; the printed table is the only trace (Q4) |
| `sameSite` | `same-site` let through | never: a page on another of our own hosts in the site, another tenant's or the ops site's, posts as the visitor; Origin still refuses a host not ours |
| | `null` passed with no `Sec-Fetch-Site` | never: PR 1473's hole, on a Safari before 16.4 |
| | a request with no Origin refused | use: curl and every test that is no browser get the refusal |
| | Origin not looked at when the site is `same-origin` | never |
| | the table left out, or `opsOrigin` | use: every tenant's page, or the ops site, is refused |
| | a statement that writes | use: Postgres refuses it, a 500 on every request that the extras and `opsOrigin` miss (Q2, a) |
| `liveSession` | a missing header taken as a match | never: a forged request sends none, so the token checks nothing |
| | the cookie's token compared, not the header's | never: a forged request carries the cookie |
| | a mismatch given as no session | use, quietly: `whoAmI` makes a new session and the person is logged out |
| | the refusal dropped by a caller's `getOrNull()` | never: `maybeBooker` answers a forged request as a stranger (Q7) |
| | `session` or `sessionId` called from another file | build, if they stay `private` to one file (Q10); else never |
| | a gate or login that selects a session or reads the cookie itself | never: a reviewer's (Q10) |
| The Go | a `Table` built by hand from `Routes`' operations, no `Check` | start: `NewHandler` panics, in (a) of Q13; build in (b) |
| | one built from `Routes`' operations with a check that always passes | never in (a) of Q13, a reviewer holds `main`; build in (b) |
| | a `Table` of operations `main` writes itself, with such a check | never in either: a reviewer's (Q13) |
| | the check run for gated rows only | never: a login and a public read have no Origin check |
| | the check run after the body | use, harmlessly: a stranger's bad body gets the body's 400 before "Invalid origin" |
| | the check's `Left` answered as the JSON of a refusal | use: the page shows "Invalid origin" as an operation's words (Q3) |

## Open

Q1 to Q5 are what Greg's answer left to this issue.

- **Q1 (Greg). The check's handle.** (a) A `Tx`, as sketched: the
  server opens a transaction for every request. (b) A `Db`, and the
  check opens one when it needs the table, as the Kotlin does:
  `|| db.tx { tx -> tx.exists(sql"...") }`; it may then write, and act
  between transactions, and nothing makes it read only. Lean a: a
  tenant's page reaches the table in either, the extras being dev's
  and CI's, and a `Tx` can be held to reading.
- **Q2 (lead; Greg is told). Which transaction.** (a) Its own, before
  the request's, read only and rolled back, as sketched and as the
  Kotlin's is but for read only: it serves a `Db` operation and one
  that runs wholly outside, which have no transaction of the server's.
  (b) The request's, first, in the gate's read-only savepoint: one
  connection for a request, but the check reruns with the operation,
  its read joins a SERIALIZABLE read set, and a `Db` operation needs
  (a) anyway. Lean a.
- **Q3 (Greg). The status of a refusal.** For the Origin: (a) 400 and
  the check's words as text, as the Kotlin answers; (b) 403, so the
  `served` line tells it from a body that could not be read, also a
  400; (c) 200 and `{"type":"refused","message":"Invalid origin"}`,
  which a page shows to its user. Lean b: no client reads the number.
  For the token there is no such choice in Kogo as it stands: a
  `Left` is words alone, so "Invalid CSRF token" is (c), where the
  Kotlin's client throws. (d) Accept that. (e) A refusal that states
  its status, a new type of `kogo.rpc` (`raise(Refused(400, "..."))`).
  Lean d.
- **Q4 (lead). The printed table.** (a) One line at the head of each
  list: `api booker   checked by sameSite   check reads tenant`. (b) A
  `check reads tenant` line on every operation, as `gate reads` is.
  (c) As a, and a list with none says so: `api routes   no check: not
  served to a browser`. Lean c: the absence is what a reviewer looks
  for.
- **Q5 (Greg). Origin `null`.** (a) As the Kotlin and the sketch: it
  passes when `Sec-Fetch-Site` is `same-origin` or `none`, which a
  page that withholds its referrer sends, and is refused when the
  header is absent. (b) Refused always: one line fewer, and such a
  page of our own stops working. Lean a.
- **Q6 (Greg). The token covers less than in the Kotlin.** There every
  request with a session is compared (4); here only one that reads its
  session, so the public reads gated by `anyone` (13 of the Query's
  34 fetchers in `fetchers.kt`) and the four bearer links (an invoice,
  a charge, a payment link, a new booker's secret) have the Origin
  check alone. (a) Accept: it is what rule 3 says, and the bearer's
  secret is the credential. (b) `anyone` and the bearer gates call `liveSession` and
  drop the session: `liveSession(tx, env, request).bind()`. Lean a.
- **Q7 (Greg). A mismatch where no session is needed.** (a) A refusal,
  as sketched: `liveSession` gives `Either<String, Session?>`,
  `whoAmI` binds it, and `maybeBooker` binds it before it makes
  "nobody is logged in" a null, in place of `booker(...).getOrNull()`.
  (b) No session: `liveSession` gives `Session?`, as `doc/gates.md`
  wrote it, and a request with a wrong token is served as a stranger
  and by `whoAmI` logged out. Lean a: it is the Kotlin's.
- **Q8 (Greg). One reader, two kinds of transaction.** The sketch
  takes a `SerializableTx`, as gates do. A login that reads its
  session in `db.tx { }` holds a `Tx`, may not call it
  (`needs-serializable`) and must lock, and `doc/gates.md` makes a
  read used both ways two functions. (a) Two readers,
  `liveSession` and `lockedSession(tx: Tx, ...)`, over one private
  function that compares: rule 3 then reads "the one function that
  compares". (b) Every login reads its session in
  `db.tx(serializable = true)`, and there is one reader. Lean b, and a
  at the first login that cannot.
- **Q9 (lead). Where the check runs in `serve`.** (a) After the name
  and the method, before the body, as sketched: no transaction for a
  path that is not there. (b) First, as the Kotlin (1): an unknown
  name from another site is then refused and not a 404. Lean a.
- **Q10 (lead). Who holds "every read goes through the one
  function".** (a) A reviewer, as the decision accepted. (b) Kotlin's
  `private`, as sketched: the cookie's name and the `select` are
  private to `session.kg`, which exports `liveSession` and
  `endSession` alone; a second `select ... from session` elsewhere is
  still a reviewer's. Lean b: it costs nothing.
- **Q11 (lead). A cookie that is no UUID.** (a) No session, as
  sketched. (b) A refusal, as the Kotlin's 400 (6):
  `sessionId` gives an `Either`. Lean a.
- **Q12 (lead). A route that calls an operation of a mounted list.**
  (a) `NewCalls` takes an `Unserved` alone, as sketched: such an
  operation is listed in `routes` too (spec 15.1 lets a fun stand in
  two lists). (b) `NewCalls` takes a `Table` as well and skips its
  check. Lean a: cusoon2 has no such route.
- **Q13 (lead; Greg is told). Can `main` lift a list's operations out
  of an `Unserved`.** The Q4 entry's rule 2 asks for a compile error,
  "by construction, and no reviewer holds it"; with `Unserved`'s
  fields exported, `main` can write `rpc.Table[app.Env]{Check: ...,
  Operations: routes.Routes.Operations, Enums: routes.Routes.Enums}`
  and mount `routes`. Go has no non-nil func type, so neither form
  makes a nil `Check` a compile error. (a) Exported fields, as
  sketched, and `NewHandler` panics on a nil `Check`. (b) `Unserved`'s
  fields unexported, built by an emitted `rpc.NewUnserved(ops, enums)`:
  `main` cannot take `routes`' operations out of it; a `Table` of
  operations it writes itself, or a check that always passes, stays a
  reviewer's. Lean b: about 4 lines of `rpc`.

Not asked, since it is wider than this issue: the request's
`Content-Type`, which `decisions.md` left "with the CSRF checks". The
Kotlin reads none, and the sketch reads none.

## What would change

- **`spec.md` 15.1**: `checkedBy f` after a list's name; the check's
  shape and `check-shape`; that a list without one is served to no
  browser. **15.2**: "Nothing runs before an operation" becomes
  "nothing but its list's check". **15.3**: the check's transaction
  (Q1, Q2). **15.4**: the refusal's status (Q3).
- **`doc/lists.md`**, the roles: a row `check | api | a Tx, reading
  only | the request | nothing, or a refusal`, fifteen rows for
  fourteen.
- **`doc/gates.md`**: "The list states each operation's gate" gains
  `checkedBy`; "For one request the server" gains a step before 1;
  "The first call makes the session" has `liveSession(...).bind()`,
  and "Gates that admit anyone" a `maybeBooker` that binds (Q7); "Two
  kinds of transaction" names the session's reader (Q8); "The table is
  printed" gains the list's line (Q4); "What the checker holds" and
  "What it would change in the spec" gain the check.
- **`doc/design.md`, "The server"**: the two table types, the order of
  `serve`, and that a refused check is logged by its status.
- **`doc/main.md`**: the row "Origin and CSRF" loses "the check not
  designed yet" and gains the hand-built `Table`; "Added to the
  prelude" names `rpc.Unserved`; Q4's line under "Open" points here.
- **`decisions.md`**: an entry for each answer; the Q4 entry's "Left
  to the issue" closes, and its accepted cost changes with Q10.
- **cusoon2's Kotlin**, first and under its tests, if any of Q3 b or
  c, Q5 b, Q6 a, Q7 b, Q9 a or Q11 a is taken: Q6 a drops the token
  from reads that read no session, Q9 a answers a stranger's unknown
  ops name 404, and Q11 a reads a cookie that is no UUID as none.
