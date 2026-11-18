# Origin and CSRF, on paper

DEV-909, 18 Nov 2026. The check Greg agreed that day in three rules
(`decisions.md`, "Origin and CSRF"; `doc/main.md`, Q4), written out as
cusoon2 would have it and read against the Kotlin at `691aa93cf`
(`CusoonApp.kt`, `CusoonAppState.kt`, `model/database.kt`,
`OriginCheckTest.kt`, cusoon2 PR 1473). It is not built. Its thirteen questions were answered on 18 Nov
2026, Q1, Q3 and Q5 to Q8 by Greg and the rest by the lead, and
DEV-915 wrote the answers into this document, the designs and
`decisions.md`: "What changes" lists what is left, which is code,
`spec.md` and cusoon2's Kotlin. Invented here: `rpc.Unserved`,
`rpc.NewUnserved`, `Table.Check`, `kogo.ReadOnlyTx`, the code
`check-shape`, `toUuidOrNull`, and the name of every Kogo function
but `sameSite` and `liveSession`. `fromOurPage` and `tokenMatches` are
the names of the session that answered Q6, not decided: cusoon2's to
name.

## What the Kotlin does

1. Three routes call `verifyOrigin` as their first line (`:107`,
   `:146`, `:167`): before the tenant's header, the body, or the ops
   operation's name is read. On `renew`, cusoon2 PR 1503 (DEV-910)
   adds a fourth, `answerStorefrontRpc` (the lead's word; not in the
   checkout DEV-915 read). The login callback, logout and the webhook
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
   Both cookies are `SameSite=Lax` (`CusoonApp.kt:522`, `:537`), so a
   cross-site POST carries no session and meets no token check: there
   it is the Origin check alone.
6. Two reads of the cookie check no token: the login callback locks the
   row (`liveSession(..., forUpdate = true)`, `:243`), and logout
   deletes by the cookie's id and reads no row (`:95`). A cookie that
   is no UUID is a 400 with Java's message for it (`:429`).
7. Every login of the Kotlin reads its session with no lock, in the
   request's one SERIALIZABLE transaction (`ensureSession` and
   `ensureOpsSession`, called from `execute`, `CusoonAppState.kt:252`,
   `:277`, `:299`). The one locked read is the Google callback's (6),
   a route that goes with the Google login.
8. A row's token is set when the row is made and never updated
   (`database.kt:1848` is the only write of `csrf_token`), and a login
   deletes the row and makes another.

## The lists

```kotlin
api booker checkedBy fromOurPage {
    whoAmI           ungated
    areas            gatedBy anyone
    setBookingNotes  gatedBy setBookingNotesGate
}
api ops        checkedBy fromOurPage { ... }
api storefront checkedBy fromOurPage { ... }
api routes { logout ungated, recordDeliveryFailure ungated }   // no check: never mounted
```

- `checkedBy f` names one top-level fun in scope, of the shape `(Tx,
  Env, Request): Either<String, Unit>`, with the list's own `Env`;
  anything else, a `Db` in place of the `Tx` included, is
  `check-shape` (Q1). One fun serves the three lists, as one
  `verifyOrigin` serves the three routes (1).
- The server runs it before every row of the list, `ungated` and
  `gatedBy anyone` included. A list with no `checkedBy` is legal Kogo.

## The check

The function a list names makes both checks: the Origin's, then the
token's (Q6).

```kotlin
// cusoon.gate, beside the gates: the list's check
fun fromOurPage(tx: Tx, env: Env, request: Request): Either<String, Unit> = either {
    sameSite(tx, env, request).bind()
    tokenMatches(tx, request).bind()
}

fun sameSite(tx: Tx, env: Env, request: Request): Either<String, Unit> = either {
    val site = request.header("Sec-Fetch-Site")
    ensure(site == null || site == "same-origin" || site == "none") { "Invalid origin" }
    val origin = request.header("Origin")?.let { if (it.endsWith("/")) it.substring(0, it.length - 1) else it }
    ensure(origin == null || isOurOrigin(tx, env.settings, origin)) { "Invalid origin" }
}

fun isOurOrigin(tx: Tx, settings: Settings, origin: String): Boolean =
    origin in settings.extraAllowedOrigins || origin == settings.opsOrigin ||
        tx.exists(sql"select 1 from tenant where domain = $origin")
```

`sameSite` is (2) and (3) line by line but for `null`, which it
refuses whatever `Sec-Fetch-Site` says (Q5): it has no line for it,
and `null` falls to the last test, where it is no setting's origin and
no tenant's domain. It also differs once in its handle: the Kotlin
opens its connection only for the third test, and this is handed a
transaction whatever the request (Q1). An ops page's request, matched
by `opsOrigin`, and a request with no Origin and no session then cost
a transaction they do not use; a tenant's page reaches the table in
both.

`tokenMatches` is (4): it reads the session's row by the cookie's id,
whatever its tenant and whether or not it has expired, and compares
its `csrf_token` with `X-CSRF-Token`, a missing header reading as
`""`; no cookie, or no row, passes. So every operation of a mounted
list has it, one that reads no session included: the public reads
gated by `anyone`, the four bearer links, `whoAmI` and each login, as
the Kotlin has it. It runs once for a request, in the check's own
transaction (Q2), where the Kotlin compares on each rerun; by (8) a
compare made before the request's transaction cannot go stale, since
the row read later has that token or is gone.

## The session reader

```kotlin
// cusoon.gate, session.kg: the only file that reads the cookie or selects a session (Q10)
private fun sessionId(request: Request): Uuid? =
    request.cookie(if (isSecure(request)) "__Host-SID" else "SID")?.let { toUuidOrNull(it) }

fun tokenMatches(tx: Tx, request: Request): Either<String, Unit> = either {
    val row = sessionId(request)?.let { tx.queryOne<Session>(sql"select * from session where id = $it") }
    ensure(row == null || row.csrfToken == (request.header("X-CSRF-Token") ?: "")) { "Invalid CSRF token" }
}

private fun session(tx: SerializableTx, id: Uuid): Session? =
    tx.queryOne<Session>(sql"select * from session where id = $id")

fun liveSession(tx: SerializableTx, env: Env, request: Request): Session? =
    sessionId(request)?.let { session(tx, it) }?.takeIf { env.clock.now() < expires(it) }

fun endSession(tx: Tx, request: Request) {             // logout's: no row read, no token (6)
    sessionId(request)?.let { tx.execute(sql"delete from session where id = $it") }
}
```

- `liveSession` compares nothing and gives `Session?`, as
  `doc/gates.md` wrote it: the token is the check's (Q6). A wrong token
  is a refusal wherever a session is read, `whoAmI` and `maybeBooker`
  included (Q7), since the check refused it first; neither binds
  anything for it.
- It is the only reader of a session, and takes a `SerializableTx`, as
  gates do. A login, which opens its own transactions, reads its
  session in `db.tx(serializable = true) { }`; one that holds a plain
  `Tx` there is refused by `needs-serializable` (Q8). That is what
  the Kotlin does for every login that stays (7).
- Which tenant the session must be of stays the caller's, as
  `ensureSession`'s `takeIf` is.
- A cookie that is no UUID is no session (Q11): it is met first in the
  check, where `tokenMatches` passes it, and `liveSession` gives null,
  so `whoAmI` gives the browser a new session.
- `sessionId` and the cookie's name are private to `session.kg`, which
  exports `tokenMatches`, `liveSession` and `endSession` (Q10). A second
  `select ... from session` elsewhere is a reviewer's.

## The emitted Go

```go
// kogo/prelude/kogo/rpc
type Table[Env any] struct { // a list that names its check: what NewHandler takes
	Check      func(kogo.Tx, Env, Request) kogo.Either[string, rt.Unit]
	Operations []Operation[Env]
	Enums      []any
}
type Unserved[Env any] struct { // a list that names none: only NewCalls takes it (Q13)
	operations []Operation[Env]
	enums      []any
}

func NewUnserved[Env any](operations []Operation[Env], enums []any) Unserved[Env] {
	return Unserved[Env]{operations, enums}
}

// cusoon2/out/booker and cusoon2/out/routes, emitted
var Booker = rpc.Table[app.Env]{Check: gate.FromOurPage, Operations: ..., Enums: ...}
var Routes = rpc.NewUnserved[app.Env](..., ...)
```

`NewHandler` takes a `Table` and panics on a nil `Check`; `NewCalls`
takes an `Unserved` alone (Q12), and a `Calls` has no `ServeHTTP` and
runs no check. `Unserved`'s fields are unexported (Q13), so `main`
cannot lift `routes`' operations into a `Table` it mounts. In `serve`,
first, before the name's 404, the method's 405 and the body, as the
Kotlin (1) (Q9):

```go
if refusal := h.check(r); refusal != nil {
	return text(http.StatusForbidden, *refusal) // Q3: 403, the check's words as text/plain
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
counts toward the limit; a panic in it is the handler's 500. Both of
its refusals, "Invalid origin" and "Invalid CSRF token", are the
check's `Left`, answered 403 and logged by their status (Q3): no
operation's refusal carries either, and the page gets a failure, not
words to show. The handler logs no refusal's words, so which of the
two it was is in the response alone.

**`main`.** No line of `doc/main.md`'s file changes: `handler` takes
an `rpc.Table[app.Env]` and `NewCalls` takes `routes.Routes` as
written there, `routes.Routes` now made by `rpc.NewUnserved`, and `opsOrigin` and `extraAllowedOrigins` are already
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
| | a line added that passes `null` | never: PR 1473's hole, on a Safari from 16.4 to about 17.x, which sends `none` with no referrer (Q5) |
| | a request with no Origin refused | use: curl and every test that is no browser get the refusal |
| | Origin not looked at when the site is `same-origin` | never |
| | the table left out, or `opsOrigin` | use: every tenant's page, or the ops site, is refused |
| | a statement that writes | use: Postgres refuses it, a 500 on every request that the extras and `opsOrigin` miss (Q2, a) |
| The check | `tokenMatches` left out of it | never: every operation of the list has the Origin check alone |
| | a list that names `sameSite` alone, or another check | never: a reviewer's, on the list's first line (Q6) |
| `tokenMatches` | a missing header taken as a match | never: a forged request sends none, so the token checks nothing |
| | the cookie's token compared, not the header's | never: a forged request carries the cookie |
| | an expired or another tenant's row passed | never: a forged request with such a cookie is served (4) |
| `liveSession` | a lock added, or a second reader that locks | never: a reviewer's; it sends a statement the Kotlin does not (Q8) |
| | `session` or `sessionId` called from another file | build: they are `private` to `session.kg` (Q10) |
| | a gate or login that selects a session or reads the cookie itself | never: a reviewer's (Q10) |
| The Go | a `Table` built from `Routes`' operations | build: `Unserved`'s fields are unexported (Q13) |
| | a `Table` of operations `main` writes itself, with a nil `Check` | start: `NewHandler` panics |
| | such a `Table` with a check that always passes | never: a reviewer's (Q13) |
| | the check run for gated rows only | never: a login and a public read have no Origin check |
| | the check run after the name, the method or the body | use, harmlessly: a stranger's unknown name gets a 404, or a bad body the body's 400, before "Invalid origin" (Q9) |
| | the check's `Left` answered with another status | use, in the log: a 400 reads as a body that could not be read (Q3) |

## Answered

Each was settled on 18 Nov 2026: Q1, Q3 and Q5 to Q8 by Greg, the rest
by the lead, Q2 and Q13 told to Greg. `decisions.md` has each rule,
why, what was not taken and what reopens it. Greg's were put in the
order Q7, Q8, Q6, Q1, Q3, Q5, and Q6 c, an option of the session's
that this document did not offer, reshaped Q7 and Q8.

- **Q1 (Greg). The check's handle: a, a `Tx`.** The server opens that
  transaction for every request of the list, holds it to reading and
  rolls it back; a check that takes a `Db` is `check-shape`.
- **Q2 (lead; Greg is told). Which transaction: a, its own.** Once for
  a request, before the request's, read only and rolled back. Q6 c
  makes it safe for the token too (8).
- **Q3 (Greg). The status of a refusal: b, 403.** `text/plain`, the
  check's words, logged by its status. Asked after Q6 c, as one
  question: both refusals are the check's, so the token's half (d, e)
  is gone. cusoon2's Kotlin states 403 first.
- **Q4 (lead). The printed table: c.** One line at the head of each
  list, and a list with none says so:
  `api booker   checked by fromOurPage   check reads session, tenant`;
  `api routes   no check: not served to a browser`.
- **Q5 (Greg). Origin `null`: b, refused always.** Passing it on
  `same-origin` or `none` has a hole on Safari 16.4 to about 17.x,
  which sent `none` for any request with no referrer (WebKit bug
  256472). cusoon2's Kotlin drops its `null` branch first.
- **Q6 (Greg). The token's cover: c, the check compares it.** Neither
  of this document's options: the list's check makes the Origin's
  check, then the token's, so the token covers what the Kotlin's does
  (4), and `liveSession` compares nothing. It replaces rule 3 of the Q4
  entry, "the token is the session reader's". The count this document
  gave, "13 of the Query's 34", is 13 of 36 top-level fetchers at
  `691aa93cf` by the session's count; the other 19 read the caller,
  the session's nonce or a `require*`.
- **Q7 (Greg). A mismatch where no session is needed: a, a refusal.**
  Changed in form by Q6 c: the refusal is the check's, `liveSession`
  gives `Session?`, and `whoAmI` and `maybeBooker` bind nothing for
  the token.
- **Q8 (Greg). One reader: b.** Every login reads its session in
  `db.tx(serializable = true) { }`, as the Kotlin's do (7). Two readers
  (a) at the first login that cannot; none is known.
- **Q9 (lead). Where the check runs in `serve`: b, first,** as the
  Kotlin (1). Reopen: a check whose cost on a 404 shows in the
  `served` lines.
- **Q10 (lead). Who holds "every read goes through the one
  function": b, `private` in `session.kg`.** After Q6 c the token no
  longer rests on it.
- **Q11 (lead). A cookie that is no UUID: a, no session.** cusoon2's
  Kotlin changes first.
- **Q12 (lead). A route that calls an operation of a mounted list: a.**
  `NewCalls` takes an `Unserved` alone; such an operation is listed in
  `routes` too. Reopen: the first route that must call an operation
  of a mounted list and cannot be listed in `routes`.
- **Q13 (lead; Greg is told). `Unserved`'s fields: b, unexported,**
  built by an emitted `rpc.NewUnserved(ops, enums)`.

## Open

- **The request's `Content-Type`**, which `decisions.md` left "with
  the CSRF checks". Not asked, and still not: the Kotlin reads none,
  and the sketch reads none.

## What changes

The designs and `decisions.md` hold the answers (DEV-915). What is
left:

- **`spec.md`, with the check's code** (DEV-914): **15.1**, `checkedBy
  f` after a list's name, the check's shape with a `Tx` first and
  `check-shape`, and that a list without one is served to no browser;
  **15.2**, "Nothing runs before an operation" becomes "nothing but
  its list's check"; **15.3**, the check's own transaction, read only
  and rolled back (Q1, Q2); **15.4**, the refusal's 403 (Q3).
- **Kogo's code** (DEV-914): the check in the checker, the emitter and
  `rpc`, `kogo.ReadOnlyTx` (a guess, 10 / 10, inside the 18 Nov
  estimate), `rpc.NewUnserved` (about 4 lines), the 403, and the
  printed table's line (Q4).
- **cusoon2's Kotlin**, first and under its tests, in one PR of its
  own: Q3 b, the two throws of `HttpClientException` (`CusoonApp.kt:368`,
  `:407`) state 403 (a guess, 2 / 2); Q5 b, `verifyOrigin` loses its
  `null` branch (`:411` to `:416`) and `OriginCheckTest`'s
  `verify("null", "same-origin")` becomes a refusal (about 4 lines
  fewer, 1 test changed); Q11 a, a cookie that is no UUID reads as no
  session (a guess, 2 / 1). Q5's fix closes a hole in production
  today, if the research is right, and need not wait for Kogo: when it
  is made is not decided.
