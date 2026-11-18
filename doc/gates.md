# Gates: who may call an operation

Agreed with Greg in discussion on 17 Nov 2026, as the design to work
from. `doc/work.md` has the rest of that day: workers, outside
services and time limits; `doc/measured.md` has the counts both rest
on. It is not built and not in `spec.md`: until it is, the spec
stands as written. No example here has been through `kogo check`, the
names in them are invented, and every size is a guess.

## The problem

Every request comes from someone, names a thing, and asks to read or
change it. Before any work, one question must be settled: may this
caller do this action to this thing, in the state the thing is in now?
If not, the request is refused in words that reveal nothing. If so, the
work is done on the thing that was checked, and on nothing else.

A request is therefore a pipeline, and each stage consumes what the
stage before produced:

```
bytes ──read──► input ──gate──► proof ──work──► answer ──write──► JSON
        400 if it       refused if
        does not fit    not allowed
```

Kogo already runs the first stage: an operation never sees the body,
only its typed input. This design has it run the second as well: the
work never sees the request, only the gate's proof.

## What was measured

Counted in cusoon2 at `691aa93cf` on 17 Nov 2026, by regex over the
source, so approximate.

- 73 GraphQL fetchers and 20 RPC operations. 48 fetchers call a gate
  and 25 do not, on purpose; a 26th by mistake would compile.
- The gates are 8 functions. A fetcher's policy is its first two
  lines: `requireVisibleBooking(id)`, then
  `access.require(roles, states) { action }`.
- The access matrix test's 44 rows by 6 callers collapse to 7 patterns
  of allowed and refused, and three of them cover 37 rows: admin only;
  owner and admin; payer, owner and admin.
- 23 of the 48 gated fetchers are for an admin alone. The 14 booking
  writes that call `require` use three sets of roles and eight sets of
  states.
- 11 of the 48 read the caller's role again inside the work.
- 28 of the 48 gates read nothing of the input (16 admin, 9 booker,
  3 viewer); 20 name a thing in it.
- 74 functions of `Model` take a bare id unmarked, and 26 of the public
  ones write. `deletePassenger(passengerId)` deletes any passenger of
  the tenant.

## The design

### The list states each operation's gate

A row of an `api` list is an operation and its gate, or an operation
marked `ungated`:

```kotlin
api booker checkedBy fromOurPage {
    setBookingNotes     gatedBy setBookingNotesGate
    drivers             gatedBy admin
    areas               gatedBy anyone
    completeEmailLogin  ungated
}
```

`checkedBy` names the list's check, which the server runs before
every row, ungated ones included: the Origin's check, then the CSRF
token's (`doc/origin.md`; `decisions.md`, 18 Nov 2026). A list that
names none is served to no browser.

### A gate and its work are two functions

The gate takes what an operation takes today and gives a proof or a
refusal. The work takes the proof where the request was.

```kotlin
// Gate file: the only file that makes a HeldBooking.
@file:OptIn(UnscopedRead::class)
package cusoon.gate

val ADMIN_OR_OWNER_ROLES: Set<BookingRole> = setOf(BookingRole.Admin, BookingRole.Owner)
val EDITABLE_STATES: Set<BookingState> = setOf(
    BookingState.Unpriced, BookingState.Priced, BookingState.Assigned,
    BookingState.Confirmed, BookingState.Complete)

data class HeldBooking @UnscopedRead constructor(
    val id: ID, val state: BookingState, val isAdmin: Boolean,
    val tenant: AtTenant, @UnscopedRead val tx: SerializableTx)

fun booking(tx: SerializableTx, env: Env, request: Request, id: ID, roles: Set<BookingRole>,
            states: Set<BookingState>, action: String): Either<String, HeldBooking> = either { ... }
```

```kotlin
// Operation file: does not opt in.
package cusoon.booker

data class NotesInput(val id: ID, val notes: String)

fun setBookingNotesGate(tx: SerializableTx, env: Env, request: Request, input: NotesInput): Either<String, HeldBooking> =
    booking(tx, env, request, input.id, ADMIN_OR_OWNER_ROLES, EDITABLE_STATES, "set the notes on")

fun setBookingNotes(env: Env, booking: HeldBooking, input: NotesInput): Either<String, BookingView> = either {
    setNotes(booking, input.notes)
    bookingView(booking)
}
```

```kotlin
// Model file: opts in, and holds the SQL.
fun setNotes(booking: HeldBooking, notes: String) {
    booking.tx.execute(sql"update booking set notes = $notes where id = ${booking.id}")
}
```

For one request the server:

0. runs the list's check first, in a read-only transaction of its own
   that it rolls back, and on a refusal answers 403 with its words
   (`doc/origin.md`);
1. reads the body into the work's input type, or answers 400;
2. opens the transaction and calls the gate with it, the environment,
   the request and the input;
3. on a refusal, rolls back and answers with the gate's words;
4. otherwise calls the work with the environment, the proof and the
   input, and commits and answers as it does today.

A gate is ordinary code and may run any number of steps before it
answers, each with `bind`: who is asking, then what they are to the
thing named, then whether its state allows the action. There is one
gate for an operation and no stack of layers: what ran before the
work, and in what order, is read from one function.

### A gate reads and decides; it does not write

Every gate of cusoon2 obeys this already: none writes a row, and three
of the ops site's steps bind a tenant, which is a setting of the
transaction. Its routes do write, in front of the gates: each makes a
session for a request that comes without a live one ("The first call"
below). It makes a gate safe to run twice, which the outside step
below needs. A count of attempts writes, so it is no gate's: it
belongs to an ungated operation or an outside step. Both checks were
run on Postgres 18.7 on 17 Nov 2026:

- **When it runs.** The server runs the gate in a savepoint with
  `transaction_read_only` on and releases it before the work. Postgres
  then refuses a write in the gate, one inside a SQL function
  included, and `nextval`, and a row lock (`for update`, `for share`,
  `for key share`); it allows `set_config`. After the release the
  transaction writes again, and a setting made in the gate is still
  set, until the transaction ends.
- **When it compiles.** A statement a gate reaches is refused unless
  every table that Postgres's tree lists for it needs select alone:
  the tree the printed table is read from ("The table is printed"
  below). Over twelve statements it refused what the run refused: an
  `update`, an `insert ... returning`, an `update` through a view, a
  `with` that deletes and the three locks; and it passed a `select`, a
  `select` from a view and the tenant's binding. A `select` of a
  function that writes, or of `nextval`, is not caught; the run finds
  it. The message is Kogo's and names the table:

```
gate.kg:41:9 gate-writes: this statement updates person, and a gate only reads: move it to the work
gate.kg:52:9 gate-writes: this statement locks session, and a gate only reads: lock it in the work
```

### The gate binds the tenant

Which tenant's rows a statement sees is a setting of the transaction,
and a gate may make it. The work does not change it, so a proof's
`AtTenant` means one tenant for the whole of the work. In cusoon2 the
model switches tenant for one row in three places on the ops site:
queuing an invite's mail (`Outbox.kt`), linking a driver who accepts
an in-house invite (`HeldInvite.acceptInHouse`), and reading a
storefront's in-house drivers (`inHouseDrivers`). Each operation's gate
binds the storefront instead, as `requireStorefront` does, and the
model loses its `withTenant` and its opt-ins to `@BindsTenant`:

```kotlin
// gate.kt: beside requireBusinessToAct
fun Raise<String>.requireStorefrontToAct(call: OpsCall, person: Driver, slug: String): ActingBusiness {
    val business = requireBusinessToAct(call, person, slug)
    val storefront = business.storefront ?: raise("Only a business with a storefront can invite.")
    call.model.tenantId = storefront.id
    return business
}
```

Kogo holds nothing here: it knows no tenant. cusoon2's marker
`@BindsTenant` ports as it is (spec 11), opted into by the gates'
file alone, and in a port a binding in the work would run on a
proof's transaction and bind nothing of it, which the rule on
statements refuses. It is a habit and not yet a rule while
`driverJobs` loops over tenants (Open). Agreed with Greg on 17 Nov
2026; made in cusoon2's Kotlin first.

### A gate that reads no input is shared

A gate that takes no input is named by many rows, and their operations
have no gate function of their own:

```kotlin
fun admin(tx: SerializableTx, env: Env, request: Request): Either<String, Admin> = either { ... }

fun drivers(env: Env, admin: Admin, input: DriversInput): Either<String, List<DriverView>> = either { ... }
```

### The proof is the only handle

A proof (a *held* value) is a data class whose constructor is marked,
so only a file that opts in makes one: the gate's, or the model's for a
row it has just inserted. It carries the facts the gate found and the
transaction, in a marked property that only such a file reads.

So the work has no `Request` and no `Tx`. It cannot write SQL, and it
cannot call a function that takes a `Tx` and a bare id. A model
function says in its signature whose authority it acts under.

- **Shared reads take a wider proof.** Places, areas and prices are
  read under many proofs, so every proof carries a tenant's, `AtTenant`:
  the caller is inside this tenant and may read what any of its users
  may. Row-level security in Postgres scopes its statements on a table
  that has it, and nothing scopes them on one that has not (cusoon2's
  `person`, `session`, `business`). It is not held to the rule on
  statements, so it may also write: a journey, a booking's number,
  Google's answers, rows that belong to no caller. A function that
  takes it acts with the whole tenant's authority, so each model
  function demands the narrowest proof that does its job. Every proof
  of a tenant's site carries one; the ops site's proofs (a driver, a
  business, an invite) carry none, since its pages run with no tenant
  bound and bind a storefront only where the gate names one.
- **Making a thing gives its proof.** `insertBooking(held, ...)` takes
  the proof that the caller may book and gives the `HeldBooking` of the
  new row, which the writes that follow take.
- **An admin's proof is its own type.** `AdminBooking` is not a
  `HeldBooking`, and the model's admin-only functions take the first,
  so work for an admin paired with a gate that admits an owner does not
  compile. Owner against payer, and the states, are arguments of the
  gate.
- **A proof carries facts.** A rule that needs data the gate has not
  read stays in the work and reads the proof: `held.isAdmin`.

### A statement binds something from its proof

A statement run on a proof's transaction binds at least one other
property of that proof, written directly.

```kotlin
// Refused: runs on booking's transaction and binds nothing of booking.
fun deletePassenger(booking: HeldBooking, passengerId: ID): Boolean =
    booking.tx.execute(sql"delete from passenger where id = $passengerId") == 1

// Accepted.
fun deletePassenger(booking: HeldBooking, passengerId: ID): Boolean =
    booking.tx.execute(sql"delete from passenger where id = $passengerId and booking_id = ${booking.id}") == 1
```

The checker reads the receiver of the call and the values the literal
binds, both of which it has; it does not read the SQL. The rule holds
whichever way SQL's parameters come to be spelt (DEV-894). A proof
with nothing but the transaction, as `AtTenant`, is not held to it.

**A proof's transaction is used only before the dot.** It is written
as the receiver of one of the five calls, `booking.tx.execute(...)`,
or as the argument of a proof's constructor in the position of that
proof's own marked `tx`, `HeldBooking(id, booker.tenant, booker.tx)`,
so that making a thing can give its proof; and nowhere else: not kept
in a `val`, passed to a function or to any other constructor, or
returned. In a constructor it goes from one proof into another, where
both rules hold it again, and only a file that opts in can call the
constructor; that the facts put into the new proof are the new row's
is not held, as for any proof (agreed with Greg on 18 Nov 2026). Without
this the rule is passed by a helper, the shape cusoon2's model is in
already (`scoped.kt`: a public function takes the held row and calls a
private one by id):

```kotlin
// Refused: the proof's transaction leaves it, and the helper's statement is on a bare Tx.
fun removePassenger(booking: HeldBooking, passengerId: ID): Boolean =
    deletePassenger(booking.tx, passengerId)
```

```
model.kg:2:5 proof-tx-escapes: booking.tx is passed to deletePassenger; a proof's transaction is used only as booking.tx.query/execute/...: pass booking instead
```

So a helper takes the proof, and its statements are under the rule.
Work done under several proofs takes an interface they implement
(spec 3.4), `interface OnBooking { val id: ID; @UnscopedRead val tx:
Tx }`. Code that holds a bare transaction (a gate, an ungated
operation, the daemon) opts in and may make a proof, so it reaches a
function written for proofs by wrapping its transaction: a login
answers `bookerView(AtTenant(tx), booker)`, which gated work calls as
`bookerView(held.tenant, held.booker)`. The cost is that wrapping,
not a second form of each function. Agreed with Greg on 17 Nov 2026.

The rule is odd for a general language. It fires only where a
transaction is reached through a proof, so a program with no proofs
never meets it, and it is no flag of `kogo check`: a flag would make
two dialects and put the safety outside the source.

### Gates that admit anyone

What cusoon2 leaves without a gate is three things, and two of them
have one.

- **A tenant's public reads** (13 gated by `anyone`: areas, branding, place
  suggestions). Nobody logs in, but the read is of one tenant:
  `gatedBy anyone`, whose proof is `AtTenant`.
- **Bearer links** (4: an invoice, a charge, a payment link, a new
  booker's secret). The secret in the input is the credential, and its
  gate gives the proof of that one thing.

Some public reads use the caller when there is one: place suggestions
put a booker's own places first, and past passengers answer a
stranger with none. Their gate never refuses, and its proof says who,
if anyone, is asking:

```kotlin
data class MaybeBooker @UnscopedRead constructor(val tenant: AtTenant, val booker: HeldBooker?)

fun maybeBooker(tx: SerializableTx, env: Env, request: Request): Either<String, MaybeBooker> = either {
    MaybeBooker(atTenant(tx, request).bind(), booker(tx, env, request).getOrNull())
}
```

So `anyone` is not the only public door: any gate that never refuses
is one, and the printed table names it on each row it serves. A gate
that checks several things gives a proof that holds a list, as hiding
several bookings at once does
(`data class AdminBookings(val bookings: List<AdminBooking>)`); each
statement of the work binds one of them. Neither shape changes the
language.

### Ungated: logins and logout

Five fetchers on the booker side, and the ops site's logins. They read
the request's own session and address, must keep the count of a failed
attempt after a refusal, call the identity service, and make the
session that every gate reads. An `ungated` operation takes today's
four parameters, a `Db` among them where it runs its own transactions
(spec 15.3). It is trusted code and sits beside the gates.

A login ends the caller's session and starts another, and the browser
must be given the new one's cookies. cusoon2's fetcher leaves the
session in a slot of the request's context (`renewedSession`), which
the route reads after, and guards against the slot being filled
twice. Here the answer carries them, in two types of `kogo.rpc`:

```kotlin
// kogo.rpc
data class Cookie(val name: String, val value: String, val path: String, val maxAgeSeconds: Long,
                  val sameSite: SameSite, val secure: Boolean, val httpOnly: Boolean)
data class Reply<A>(val answer: A, val cookies: List<Cookie>)
```

```kotlin
@Acts fun completeEmailLogin(db: Db, env: Env, request: Request, input: CodeInput): Either<String, Reply<BookerView>> = either {
    val user = redeemEmailCode(db, env, request, input.oneTimeCode).bind()
    val session = db.tx(serializable = true) { tx -> completeLogin(tx, env, request, user) }.bind()
    Reply(bookerView(session), sessionCookies(session, env.clock.now(), isSecure(request)))
}

// The application's own: which cookies, and their names, paths and flags.
fun sessionCookies(session: Session, now: Instant, secure: Boolean): List<Cookie> = ...
```

The server sends `answer` as the JSON and each cookie as a
`Set-Cookie` header, when the operation answers; a refusal sets none.
Only an ungated operation answers with a `Reply`, so a search for it
lists every place a cookie is set. The browser's TypeScript has the
answer's type alone.

### The first call makes the session

A session's row is there before anyone logs in: it holds the CSRF
token, the emailed code and the count of attempts. cusoon2's routes
make it for any request that comes without a live one, before a
fetcher runs. A first load's many requests then each made a session,
so its page already makes one request and waits for it before it
draws anything (`InitializeSession.tsx`): the `Booker` query.

A gate does not write and nothing runs before one, so in Kogo that
first call is an ungated operation that says what it does, and no
other request makes a session. Kogo gains nothing for it:

```kotlin
api booker {
    whoAmI  ungated        // the page's first call: makes the session when there is none
}

fun whoAmI(tx: SerializableTx, env: Env, request: Request, input: Unit): Either<String, Reply<BookerView?>> = either {
    val live = liveSession(tx, env, request)
    val session = live ?: createSession(tx, tenantOf(tx, request).bind())
    val cookies = if (live == null) sessionCookies(session, env.clock.now(), isSecure(request)) else emptyList()
    Reply(bookerOf(tx, session), cookies)
}
```

A request with no live session is refused by a gate that needs one
and served by `anyone`, which needs the tenant alone. Cookies are set
by the first call, a login and a logout, where cusoon2 sets them on
every response: the same cookies, since a session's deadline does not
move. It changes how cusoon2 behaves, so it is made in the Kotlin
first, under its tests, and ported after.

### Routes that are not operations

Some requests are not a browser's JSON call: a page the identity
service sends the browser back to, a webhook from a server. cusoon2
has two once its Google login goes: logout, a GET that ends the
session, clears the cookies and redirects, and SMTP2GO's webhook,
which sends a Bearer secret and JSON with keys like `message-id` and
is answered with a status alone. They are Go, in the application's
`main`: each route calls an ungated operation of an `api` list that
`main` does not serve to the browser.

```go
mux.HandleFunc("POST /backend/smtp2go-webhook", func(w http.ResponseWriter, r *http.Request) {
    if !hook.Matches(r.Header.Get("Authorization")) { w.WriteHeader(401); return }
    event, ok := readEvent(r.Body)                  // a hooks.DeliveryFailure, built by the emitter's chain
    if !ok { w.WriteHeader(400); return }
    // an ungated operation, by its emitted value; calls is rpc.NewCalls(hooks.Hooks, ...),
    // the handler of a list that is not mounted
    if _, err := rpc.Call(calls, r, hooks.RecordDeliveryFailure_op, event); err != nil {
        w.WriteHeader(500); return
    }
    w.WriteHeader(200)
})
```

- **Go holds the method, the path and the translation**: it reads the
  URL, the headers and the body into the operation's typed input, and
  turns the answer into a status or a redirect. The secret is
  configuration, compared in Go.
- **Kogo holds what the route does**: the operation's shape and wire
  types, and its lines in the printed file. `recordDeliveryFailure`
  knows nothing of HTTP.
- **Logout** calls an ungated operation that answers a `Reply`
  ("Ungated: logins and logout"); Go writes its cookies as
  `Set-Cookie` headers and then a 303 to `/` in place of the JSON. The
  cookies stay Kogo's; only the redirect is Go's.
- **A route touches the database only through its operation**, as
  `decisions.md` has the seam (17 Nov 2026). `main` opens the pool,
  so nothing enforces it: a reviewer holds `main` to it.
- **Go calls the operation by its emitted value**, through a handler
  of the list that `main` does not mount, and builds the input by the
  emitter's chain (`decisions.md`, 18 Nov 2026; `doc/main.md`).

So the language has no `routes` list, no `Response` or `Secret`, no
`status` or `redirect`, and `Request` gains no `query` or `body`. The
printed table lists no route; what each route writes is its
operation's. Reopen at five routes. Commands are not affected and stay
a list. Agreed with Greg on 17 Nov 2026 (`doc/lists-assessment.md`,
choice B), reversing a `routes` list agreed earlier the same day.

### Commands

cusoon2's binary takes `--op <name>` and flags (`Main.kt`). Ten of
its operations are the application's work, run by Greg or the deploy
script: granting and removing an admin, the support mark, recording
an ABN, a PayID, invoicing and platform fees by hand, a test email,
the pricing check at deploy, and setting up the identity service's
tenants. Each reads its flags into values, runs a transaction and
sometimes an outside call, and refuses on stderr with exit 1 or says
what it did. They are declared and written in Kogo:

```kotlin
commands admin {
    "record-abn"  recordAbn
    "set-payid"   setPayid
    "mail-test"   mailTest
}

data class AbnInput(val business: String, val abn: String, val gstRegistered: Boolean)

fun recordAbn(db: Db, env: Env, input: AbnInput): Either<String, String> = either {
    db.tx(serializable = true) { tx -> recordAbnCheck(tx, input.business, input.abn, input.gstRegistered) }.bind()
    "ABN checked: ${input.business}"
}
```

```
$ cusoon2 record-abn --business acme --abn "51 824 753 556" --gstRegistered true
ABN checked: acme
```

- **A command has one shape**, `(db: Db, env: Env, input: I):
  Either<String, String>`: no `Request` and no gate, since whoever
  runs the binary on the server is trusted, as now. It holds a `Db`,
  so it may act between its transactions.
- **Its name is a literal string**. **A flag is a property's name
  as written**, as an operation's JSON key is (spec 15.4), and its
  value is read as JSON's are: `true`, an ISO date, a number, text.
  The reader is strict: an unknown, missing or
  unreadable flag is refused before the command runs, with its flags
  listed, so a renamed property breaks a deploy script at its next
  run and never in silence.
- **The outcome is the exit code**: a `Right` printed with 0, a `Left`
  on stderr with 1, a panic with 2 and its trace. The printed table
  lists each command, with what it reads and writes.
- **Not commands**: `web` and `batch` are modes of the Go `main`,
  `migrate` is the migration tool's, and `check-config` checks the
  configuration, which is Go's.

Perhaps 80 lines of Go and 20 of checker. Weighed and not taken:
flags in kebab-case made from property names, a name turned into text
out of sight; input as JSON on the command line, one reader but
painful by hand; commands in Go, which for ten commands would grow
Go with each and lose the reading by property name. Agreed with Greg
on 17 Nov 2026, and kept when routes went to Go the same day.

### The table is printed

A command of its own prints, from the list, the gate functions and
Postgres's answers, each operation with its gate, the arguments it
passes and the tables it touches, into a file that is checked in. A
change of who may do what, or of what an operation reads and writes,
is then a diff, made on purpose or by accident:

```
setBookingNotes     booking      input.id, ADMIN_OR_OWNER_ROLES, EDITABLE_STATES
                    runs         serializable
                    gate reads   booking, booking_role, session
                    reads        booking
                    writes       booking
submitBooking       booker
                    runs         serializable
                    gate reads   booker, session
                    reads        area, booker, place, price_regime, stripe_card
                    writes       booking, passenger, journey (tenant-wide)
                    asks         google.distance (in transaction)
drivers             admin
                    runs         serializable
                    gate reads   booker, session
                    reads        person, tenant_driver
areas               anyone
                    runs         serializable
                    gate reads   tenant
                    reads        area
completeEmailLogin  ungated
                    reads        booker, session
                    writes       login_attempt, session
                    acts         idp.redeemEmailCode
```

Each list's own line heads its rows: `api booker   checked by
fromOurPage   check reads session, tenant`, and for a list with none
`api routes   no check: not served to a browser`, since the absence
is what a reviewer looks for (`doc/origin.md`, Q4).

The file enforces nothing: it is what a reviewer reads.

- **It is kept fresh as the TypeScript file is** (`doc/design.md`,
  "The server"): the application's checks run the command and fail
  when `git diff` shows a change, so Kogo has no rule for a stale
  file. The command needs a database, as `kogo check` does.
- **The outside step is on the first line, beside the gate**:
  `updateBooker  updateBookerGate  outside renameLogins`, and
  `outside` alone for an operation that runs wholly outside. A row's
  order of calls is then read where its gate is: a line with no
  `outside` is gate, then work. That the gate runs a second time
  after an outside step is left to the spec. Perhaps 5 / 5. Agreed
  with Greg on 17 Nov 2026 (`doc/lists-assessment.md`, choice C), over
  a line of order on every entry, which prints `gate, work` on most.
- **It lists no route**: routes are Go ("Routes that are not
  operations"), and what each writes is on its operation's lines.
- **The tables are those a statement names**, over the statements the
  gate or the work reaches. Postgres gives them: the tree the SQL pass
  already reads for what can be null lists every table of a statement,
  however deep, with the permissions it needs there. A table can be on
  two lines: an `update` with a `where` reads the table it writes.
  Tables are in alphabetical order, so the order statements are
  written in is no diff.
- **`gate reads` is the gate's; `reads` and `writes` are the work's
  alone.** Work that reads the session for itself then shows, where
  one list for the request would hide it behind the gate. A gate has
  no `writes`, since Postgres refuses one there; an ungated operation
  has no `gate reads`; an outside step has no lines of tables, since
  it holds no transaction. A shared gate that reads a new table
  changes a line of every operation behind it, which is noisy and
  true.
- **`locks` is a row lock, of any strength**, printed where there is
  one. Postgres reports a `select ... for update` as needing update on
  the table, as an `update` does, and marks the lock apart, which is
  how the two are told. cusoon2's requests take no locks; its logins
  and its daemon do.
- **`asks` and `acts` are the calls to outside services**, each by
  the name of the application's function that makes it: one that
  calls `ask` itself, or a binding marked `@Acts` itself. It is
  printed as its package's last name and its own, `stripe.charge`. A
  gate's lookups are on a `gate asks` line. So a change to what an
  operation reaches outside is a diff, a lookup added to a helper
  three calls down included.
- **A lookup made while a transaction is open is marked `(in
  transaction)`**: it holds the transaction open while the service
  answers, and a rerun asks again. An action needs no mark, since
  none runs there. A call made with a key is marked `(keyed)`.
- **These lines are as sharp as the code.** A function that makes
  two calls is printed once, and a wrapper that takes its endpoint
  as a parameter is printed as itself on every operation that uses
  it. Nothing is refused for the table's sake.
- **A write made under `AtTenant` is marked `(tenant-wide)`**, since
  no rule holds it: `journey` above is meant, and a `passenger` so
  marked is a diff someone has to explain. The printer knows which
  proof's transaction a statement runs on, as the rule on statements
  does. Perhaps 10 lines. Agreed with Greg on 17 Nov 2026.
- **A view is listed by its own name**, read or written.
- **It does not see** inside a function, under a view, or a row that a
  foreign key's cascade or a trigger changes. It lists what the
  statement's text names.

Run on Postgres 18.7 on 17 Nov 2026, in a scratch database made from
`testdata/schema.sql` with a view and a function added: a join, an
`update`, an `insert ... select ... returning`, a `with` that deletes,
a table named only in an `exists`, a `select ... for update`, a
`select` from a view, a `select` of a function that writes (nothing is
listed), and a `delete`; and, for the gate's check, `for share` and
`for key share`, which Postgres marks as it does `for update`. Not
run: a cascade, a trigger and `for no key update`.

## Two kinds of transaction

cusoon2 runs its requests at SERIALIZABLE with no locks, and reruns
one on a conflict; its daemon, logins and logout run at READ
COMMITTED and lock what they read. Most programs work the second way
throughout. In the Kotlin a flag on `Model`, read as a statement is
built (`withLocking`), decides whether a read locks, so one call
sends three statements:

```kotlin
unscopedInvoice(id)                                     // a request:           select ... where id = ?
model.unscopedInvoice(row.invoiceId)                    // the charge's prepare: select ... for share
model.unscopedInvoice(row.invoiceId, forUpdate = true)  // the charge's record:  select ... for update
```

Kogo's statements are constants and a transaction has no flag, so the
kind is the handle's type, and every function that takes a
transaction writes one of two:

- **`Tx` is any transaction.** A function that takes it assumes
  nothing, and locks what it needs.
- **`SerializableTx` is one that is rerun on a conflict.** A function
  that takes it may read with no lock. It fits wherever a `Tx` is
  wanted.

```kotlin
fun invoice(tx: SerializableTx, id: ID): Invoice? =            // relies on serializable
    tx.queryOne<Invoice>(sql"select * from invoice where id = $id")

fun sharedInvoice(tx: Tx, id: ID): Invoice? =                  // holds the row against a change
    tx.queryOne<Invoice>(sql"select * from invoice where id = $id for share")

fun lockedInvoice(tx: Tx, id: ID): Invoice? =                  // about to write it
    tx.queryOne<Invoice>(sql"select * from invoice where id = $id for update")
```

```
invoices.kg:40:19 needs-serializable: invoice takes a SerializableTx and sendInvoice holds a Tx: call a function that locks
```

- **Who opens which is read from the type.** `db.tx { }` gives a `Tx`
  at READ COMMITTED, set by the handle and not left to the database's
  default; `db.tx(serializable = true) { }` gives a `SerializableTx`.
  For a transaction the server or the runner opens, the first
  parameter of the gate, the ungated operation or the queue's step
  says which. There is no default.
- **A gate on a `Tx` may lock, and still may not write.** Where
  nothing is rerun on a conflict the gate is where the lock belongs:
  without it, what the gate found can change before the work writes.
  A gate on a `SerializableTx` is as above: a lock is refused.
- **A rerun is safe at either level**, since nothing marked `@Acts`
  runs in a transaction. A `Tx` the server or the runner opened is
  rerun on a deadlock.
- **The printed table says which**, on a line of its own: `runs
  serializable` or `runs plain`. An operation that takes a `Db` has
  no such line: it opens its own.

**A proof made both ways shares an interface**, as work done under
several proofs does. A tenant's proof is made by a request and by the
daemon (the names here are invented):

```kotlin
interface InTenant { @UnscopedRead val tx: Tx }                // the tenant is bound; assume no more

data class AtTenant @UnscopedRead constructor(@UnscopedRead override val tx: SerializableTx) : InTenant   // a request's
data class WorkerTenant @UnscopedRead constructor(@UnscopedRead override val tx: Tx) : InTenant           // the daemon's, or a login's

fun nextInvoiceNumber(at: AtTenant): Long = ...                // relies on serializable: a request alone calls it
fun lockedInvoice(at: InTenant, id: ID): Invoice? = ...        // locks for itself: either calls it
```

Run on Kotlin 2.4.0 on 17 Nov 2026: it compiles and runs, a marked
`val` in an interface included, and a `WorkerTenant` passed where an
`AtTenant` is wanted is refused. Kogo gains one thing for it: a data
class may implement an interface's `val` with a type that fits it.

**For a port of cusoon2.** The gates and proofs of its requests say
`SerializableTx`, as the examples above do, and the daemon's
functions say `Tx`. A session has one reader, `liveSession`, on a
`SerializableTx`: a login reads its session in
`db.tx(serializable = true) { }`, as each login of the Kotlin does,
and none locks it (`doc/origin.md`, Q8). Each read of the daemon's writes its lock in its
statement, where the Kotlin's flag added it out of sight. A read used
both ways becomes two functions, so that a request sends the
statement the Kotlin sends. About 30 reads of `database.kt` go
through the flag; how many are used both ways was not counted.

**What a gate on a `Tx` loses.** The server cannot put it in
Postgres's read-only mode, which refuses a lock as it refuses a
write, and Postgres has no mode that allows the one and refuses the
other. So such a gate is held by the check made when it compiles,
which does not see a `select` of a function that writes, or of
`nextval`. Postgres knows which functions may write, so the checker
could refuse those in a gate; that was not tried, and binding the
tenant is itself such a function.

Perhaps 40 lines of checker, 30 of Go and 60 of tests. Weighed and
not taken:

- **The level stated on the list, or in `main`.** A function would
  then rely on what its signature does not say, and one called from
  both kinds of transaction would be wrong in one of them in silence.
- **A flag on the handle, read when the program runs**, as the Kotlin
  has: one call that sends different SQL by who opened the
  connection.
- **One function that locks, for a read used both ways.** It is
  allowed, since a `SerializableTx` fits a `Tx`, and costs a lock the
  Kotlin does not take; a port writes two.

Agreed with Greg on 17 Nov 2026.

## Outside calls

A transaction is all or nothing and may be run again from the start.
A call to another service is neither: it cannot be rolled back, and
run twice it happens twice. Made inside a transaction it can be done
twice, be done and then undone on our side alone, or hold the
transaction open while the service answers.

cusoon2's daemon already has the rule, agreed 25 Sep 2026
(`doc/plans/effect-runner.md` there): "A remote write happens in no
transaction, ever." Its requests do not. Of the 73 fetchers, 7 queue a
row for the daemon, about 9 look something up (Google's places and
distances, the identity service's "has a passkey?"), 4 are logins, and
4 change something outside in the middle of their transaction (three
passkey operations and a booker's change of address). The ops site
has the same three passkey operations, so seven in all. The logins
act too, on both sites, inside their transaction today: they are
`ungated` with a `Db` here, and act between their transactions. Six
comments argue, call by call, what a rerun would do.

- **A binding says what kind of call it is**: a `lookup` asks and
  changes nothing; an `action` changes something outside. The one
  outbound binding is a general HTTP call, and each endpoint of a
  service is declared once with its kind (`doc/work.md`).
- **An action is refused where a transaction is open**: in a function
  that holds a `Tx` or a proof, and in a `db.tx { }` block. A function
  that calls an action says so ("A function that acts says so"
  below), so a helper hides none. The outside step is the one
  function that holds a proof and may act.
- **An action has three homes**: a worker, fed by a row; an ungated
  operation, between its own transactions; and the outside step.
- **A lookup is allowed anywhere.** A slow one still holds a
  transaction open, as today.

### A function that acts says so

A function that calls an action carries the prelude's marker `Acts`,
as the binding it calls does:

```kotlin
// kogo.http
@RequiresOptIn
annotation class Acts

@Acts external fun Http.perform(endpoint: Action, fields: List<Field>): Either<SendFailure, Json>

// The application.
@Acts fun placeDetails(google: Http, placeId: String): Json { ... }     // calls perform
@Acts fun pickupOf(google: Http, placeId: String): Place = ...          // calls placeDetails
```

It is Kotlin's own rule for a marker, which Kogo's markers otherwise
leave out (spec 11): a function that uses a marked declaration
carries the mark, and so demands it of its own callers. Run on Kotlin
2.4.0 on 17 Nov 2026: a marked function that calls a marked one
compiles, and an unmarked caller is refused, as are a `::` reference,
a lambda and an unmarked `override` whose body acts. Kogo is stricter
in four ways:

- **No file opts in to it.** `@file:OptIn(Acts::class)` is refused,
  since a whole file could then act and say nothing.
- **The mark stays true.** A function that carries it and calls no
  action is an error.
- **It keeps out of a transaction.** A function that carries it takes
  no `Tx`, takes a proof only as an outside step or an operation that
  runs wholly outside, and is not called in a `db.tx { }` block.
- **It is not kept in a value.** There is no `::` reference to one,
  an interface's `fun` is not one, and a lambda that acts is passed
  straight to a prelude function that runs it, as a lambda that fails
  is (spec 9).

```
places.kg:3:5 unmarked-action: placeDetails calls perform, an action: mark placeDetails with @Acts, or move the call
```

As first written, whether a function acts was worked out through what
it calls. A line added to a helper then changed what its callers
were, with no word of theirs changed, and the error came up where a
transaction was held, files away; where no caller held one, nothing
was reported. A lambda kept in a value, a `::` reference and an
interface's `fun` each carried an action past that walk. Only an
action is marked: no rule rests on whether a function looks something
up.

`Acts` is the one marker held by the function and never by the file,
and spec 11 says so as the exception it is. Perhaps 40 lines of
checker and 45 of tests; in a port of cusoon2 perhaps 40 to 50
functions carry it, by a guess. Weighed and not taken:

- **The kind left worked out**, with the hole closed and the error
  naming the chain of calls. Cheaper, and a reader still opens what a
  function calls to learn what it is.
- **A word of Kogo's own, `action fun`.** Plainer, and spec 11 keeps
  one rule; but it adds syntax, and is respelt if Kogo source is to
  be valid Kotlin (DEV-894). If that choice keeps Kogo's additions
  unlike Kotlin, `@Acts` is respelt this way, with the binding's
  words.
- **Two environments**, one that can act and one that cannot. Spec 7
  at its purest, and two environments in every list with a handle
  that only asks.

Agreed with Greg on 17 Nov 2026.

### The outside step

An operation may name a step that runs between its gate and its work
with no transaction open:

```kotlin
api booker {
    updateBooker gatedBy updateBookerGate outside renameLogins
}

// In a transaction that then ends. Our own refusals belong here.
fun updateBookerGate(tx: SerializableTx, env: Env, request: Request, input: BookerInput): Either<String, HeldBooker> = either {
    val booker = booker(tx, env, request, input.id).bind()
    if (input.email != booker.email) {
        ensure(booker.isAdmin) { EMAIL_CHANGE_BY_US_MESSAGE }
        ensure(emailIsFree(booker, input.email)) { "A booker with this email already exists." }
    }
    booker
}

// No transaction is open. The service may refuse, and nothing is written.
@Acts fun renameLogins(env: Env, booker: HeldBooker, input: BookerInput): Either<String, Unit> = either {
    if (input.email != booker.email) {
        booker.idpUserId?.let { ensure(setUserEmail(env.idp, it, input.email)) { EMAIL_HAS_LOGIN_MESSAGE } }
        booker.stripeCustomerId?.let { updateCustomerEmail(env.stripe, it, input.email) }
    }
}

// A new transaction, in which the server runs the gate again first.
fun updateBooker(env: Env, booker: HeldBooker, input: BookerInput): Either<String, BookerView> = either {
    saveBooker(booker, input)
    bookerView(booker)
}
```

The step is handed the gate's proof after that proof's transaction
has ended, for the facts in it: an address, an id at the identity
service. cusoon2's four read nothing else there. So an outside step
passes no proof to a call, and SQL in one is an error of the checker
and not a failure of the runs that reach it, after the service has
been changed:

```
booker.kg:31:5 outside-passes-proof: renameLogins passes booker to saveBooker, and an outside step has no transaction: move the call to the work
```

The refusal comes before any write, so nothing is rolled back for it,
and the order is in the shape of the code. It is the daemon's own
shape for a remote write: prepare in a transaction, act with none,
record in another.

The two services can still disagree: when the step succeeds and the
last transaction fails, the outside is ahead, as it is today, and
saving again finishes the change. Only a row and a worker close that.

### An operation that runs wholly outside

Six of cusoon2's seven write nothing of ours after the gate: the
three passkey operations, on both sites, change the identity service
and are done, and starting a registration answers with what the
service said. So a gated operation may have no work in a transaction:

```kotlin
api booker {
    setBookingNotes           gatedBy setBookingNotesGate                      // gate, then work, in one transaction
    updateBooker              gatedBy updateBookerGate   outside renameLogins  // gate; outside step; gate again and work
    startPasskeyRegistration  gatedBy booker             outside               // gate; then the operation itself, outside
}

// No transaction is open.
@Acts fun startPasskeyRegistration(env: Env, booker: HeldBooker, input: Unit): Either<String, PasskeyCeremony> = either {
    if (booker.isAdmin && !booker.isPasskeySession) {
        ensure(!hasPasskey(env.idp, booker.idpUserId).bind()) { ADD_PASSKEY_MESSAGE }      // a lookup
    }
    PasskeyCeremony(startRegistration(env.idp, booker.idpUserId, booker.deviceName).bind())   // an action
}
```

The server runs the gate in a transaction, ends it, and calls the
operation with none open; its answer is the request's. It is held as
an outside step is: it may act, and it passes its proof to nothing.
The gate runs once. The printed table gives it a `gate reads` line
and no other. What it needs of the request is a fact of the proof:
the Kotlin names a new passkey from the User-Agent (`deviceName`).

An outside step's answer is not handed to the work: `updateBooker`
alone has both, and its step answers nothing. Nothing gated in
cusoon2 needs a `Db`: by a scan of its mutations every gated one is
one transaction but these seven.

A third kind of call, `repeatable` (an action that is safe to make
twice, allowed in a transaction), was weighed for these four and not
taken: it rests on the binding's word, and fits one of them. Nor does
it decide a worker's retry: cusoon2's mail is not safe to repeat and
is retried without limit, and its charge is safe to repeat under its
key and is never retried. Retry is a judgment for each queue, so a
binding says `lookup` or `action` and nothing more.

## What the checker holds

- A gate reaches no statement that writes, by Postgres's answer, nor
  one that locks where its transaction is a `SerializableTx`.
- A function that takes a `SerializableTx` is not handed a `Tx`.
- A function that calls an action carries `@Acts`, and one that
  carries it calls one.
- A function that carries `@Acts` takes no `Tx`, and a proof only as
  an outside step or an operation that runs wholly outside; none is
  called in a `db.tx { }` block.
- An action is not kept in a value: no `::` reference, no interface's
  `fun`, and a lambda that acts goes straight to a prelude function
  that runs it.
- An outside step, or an operation that runs wholly outside, passes
  no proof to a call.
- Every row is `gatedBy` a function or `ungated`.
- The gate's result is `Either<String, P>`, and `P` is the type of the
  work's second parameter.
- A gate named with an input takes the work's input type.
- Gated work takes no `Request`, `Tx` or `Db`.
- A statement on a proof's transaction binds something of the proof.
- A proof's transaction is written only as the receiver of one of the
  five calls, or as a proof constructor's argument for that proof's
  marked `tx`.

What a held value's marked constructor and marked `tx` already give
(spec 11) does the rest: no operation makes a proof, or takes the
transaction out of one.

## What it does not hold

- **Whether the policy is right.** Notes for an admin or an owner
  where an admin alone was meant compiles. The gate function holds
  nothing but the policy, and the printed table shows a change to it.
- **Whether a gate's own code is right.** There are few gates, in one
  file.
- **Whether a gate on a `Tx` locks enough**, or calls a function
  that writes ("Two kinds of transaction").
- **Whether a bound value scopes its statement.**
  `where id = $passengerId or booking_id = ${booking.id}` compiles.
- **Whether a function takes the narrowest proof.**
  `deletePassenger(at: AtTenant, passengerId: ID)` compiles, and work
  that calls it with `booking.tenant` deletes any passenger of the
  tenant. The signature and the call say so where they are written,
  and the printed table marks the write `(tenant-wide)`.
- **Anything in a file that opts in, past the rules above.** The model
  and the gates are the trusted part; the two rules on a proof's
  transaction hold in the model, the one place it is read.

So the access matrix test stays: it sees policy, which no rule here
does.

## What stays the application's

The gates, the roles and states, the refusals' words, the proofs and
their facts. The language gains two words in the `api` list, the shape
of gated work, and one rule on statements. Constants are named in
upper case, with `_ROLES` and `_STATES`, as Kotlin's conventions have
it: a habit of the application's code, not a rule.

## What it would change in the spec

- **1 and 15:** lists are stated once, by one rule for a role's
  shape and one table of roles (`doc/lists.md`).
- **15.1:** a row names a gate or says `ungated`.
- **15.2:** "Nothing runs before an operation" is reversed for a gated
  one, whose shape becomes environment, proof, input.
- **7:** a function that touches the database has a `Tx`, a `Db` or a
  proof as a parameter.
- **15, a new part:** proofs, the rule on statements and on a proof's
  transaction, the read-only gate and the outside step.
- **3.4 and 11:** an interface may declare a marked `val`, so proofs
  share one (`OnBooking`), and a data class may implement a `val`
  with a type that fits it. Kotlin 2.4.0 accepts both (run on 17
  Nov 2026).
- **7, 10.7 and 15.3:** `SerializableTx` beside `Tx`; `db.tx` states
  READ COMMITTED; the first parameter of a gate or an ungated
  operation says which the server opens, where 15.3 has it always
  SERIALIZABLE.
- **13:** a binding with an effect says `lookup` or `action`.
- **11 and 13:** a binding that acts carries the prelude's marker
  `Acts`, and so does every function that calls one: the one marker
  held by the function and never by the file.
- **12 and 15.2:** `Cookie` and `Reply` in `kogo.rpc`; an ungated
  operation may answer `Either<String, Reply<A>>`.
- `doc/design.md`, "The server": the handler calls the gate, in a
  read-only savepoint, and the outside step; a command writes the
  printed table; "Not designed, on purpose" loses the cookie. Its
  sentence that paths and other routes stay out of the language
  stands. "The SQL pass": a statement's signature carries its tables.
- **12 and 15, a new part:** the `commands` list, a command's shape,
  and its flags read by property name.

## Weighed and not taken

- **A `routes` list in Kogo**, each route `(db: Db, env: Env,
  request: Request): Response`, declared with its method and path as
  literal strings; the prelude gaining `Response` with `status` and
  `redirect`, `Secret`, and `Request.query` and `Request.body`.
  Agreed on 17 Nov 2026 and reversed the same day (choice B of
  `doc/lists-assessment.md`): two routes do not pay for a list, two
  prelude types, two members of `Request` and a section of the spec,
  perhaps 140 lines of Kogo's Go, and the reason it was taken, that
  Greg does not read Go, is no hard rule. What it gave, the route's
  path in the printed file and the checker's sight of each route's
  translation, is lost. Reopen at five routes.
- **Following a proof's transaction through `val`s and arguments**, so
  that a helper it reaches is held to the rule on statements. A walk of
  where each value goes, perhaps 100 lines, and a rule that holds
  somewhere other than where it is written. The rule as first written,
  on the receiver alone, was passed by any helper.
- **`AtTenant` narrowed**: to reads, to reads and inserts, or to
  tables under row-level security, with a policy that admits everyone
  on cusoon2's platform tables. Each refused writes cusoon2's requests
  make to rows no caller owns (journeys, booking and invoice numbers,
  Google's answers, about 13 statements), and each patch for those
  needed a list of tables of its own. `AtTenant` exists for those
  writes; the mark in the printed table shows them instead.

- **The gate as a lambda in the list**
  (`gatedBy { tx, env, request, input -> ... }`). It put the policy in
  another file from the work that relies on it.
- **The gate as the work's first statements**, held there by the
  checker. The work would hold the request and the transaction, and
  the checker would need rules about the order of statements.
- **The row's arguments written with borrowed names** (`input.id` in
  the list, `tx` and the rest left out). Too much was undeclared where
  it was used.
- **Row-level security for each caller**, as tenants have. It decides
  by row, not by action, so a payer who may choose a card and not edit
  the notes has no place in it; it does nothing for an admin; and it
  needs a policy on about 15 tables. The rule on statements took its
  place.
- **A proof type for each role and state.** Eight sets of states for
  14 operations is a type for nearly each one. The admin's type alone
  was taken.
- **Another form for small operations.** Shared gates removed the need.
- **No change to the language**, and a wider matrix test. It sees
  policy, but only for the cases someone put in the table.
- **The gate's statements prepared a second time**, each wrapped as
  `select 1 from (<statement>) _`, for Postgres to refuse one that
  writes. It accepts a lock, which every run then refuses, and its
  refusal of an `update` is a syntax error in text the author did not
  write.
- **The outside step handed a plain copy of the proof's facts.** The
  rules as they stood would hold it, but each such operation gains a
  data class, a function that fills it and a fourth name in its row.
  And the step left as first sketched: its own example broke the rule
  on actions, and SQL in it failed only when run.
- **An empty work for an operation that writes nothing of ours**,
  with the outside step's answer handed to it. A transaction for
  nothing, and a second run of the gate that can refuse after the
  service has been changed. And such an operation as `ungated` with a
  `Db`: the printed table would lose its gate.
- **A slot of the request that a login fills with its cookies**, as
  the Kotlin has: it needs a guard against a second filling, and Kogo
  code changes nothing else it is handed. And a handler that knows
  what a session is: sessions are the application's.
- **A function the server runs before every gate, which may write**,
  to make the session. A layer in front of the gate again, and a
  write that must outlast a refusal, so a transaction of its own on
  every request. And a gate that writes: not safe to run twice.
- **`kogo check` failing on a stale printed table.** It would read a
  generated file, and the two generated files would be kept fresh in
  two ways.
- **A row lock printed as a read or as a write.** The first hides it;
  the second has a login that only reads the session say it writes it.
- **The gate's tables in one list with the work's**, or each gate's
  once in a section of its own. The first hides what the work reads;
  the second puts one operation in two places.
- **A view printed as the tables under it.** A walk of the catalog,
  about 25 lines, for cusoon2's three views, and the one place the
  table would look past the statement's text.
- **The endpoint's name on the `asks` and `acts` lines**, held by
  two rules: the first argument of `ask`, `perform` and `performOnce`
  written as a top-level `val` or a call of a top-level function,
  and an endpoint made only at the top level. It shows both calls of
  a function that makes two. But `perform` looks like any function,
  and a rule on how one of its arguments is written is a
  straitjacket on it: Greg, 17 Nov, took the slightly less useful
  table. And the method and path as literal strings at the call,
  which undoes "An endpoint is declared once" (`doc/work.md`).

## Open

- Whether `gatedBy`, `ungated`, `outside` and `commands`
  are spelt as valid Kotlin (DEV-894), and how a binding spells
  `lookup`; an action is `@Acts`.
- The printed table's command has no name.
- **`driverJobs` has no shape here.** It binds each of a driver's
  operators in turn and reads its jobs, gate and work in one loop
  (`eachOperator`). Putting the loop in the gate makes the gate the
  whole operation, which a gate is not for; the work may not bind. It
  waits on replacing cusoon2's loops over tenants, this one and the
  daemon's ("Discover, then enter" in `doc/work.md`), which Greg has
  wanted on other grounds.
- Checked on paper against three fetchers only: setting a booking's
  notes, setting its driver, and submitting a booking. The work of
  adopting it is in the model: each of some 150 functions says which
  proof it demands.
- Not sized as a whole. The rule on statements is perhaps 30 to 50
  lines of checker, and the rule on a proof's transaction 20 to 30. The tables of the printed table are perhaps 35
  lines of the SQL pass to read them, 8 to tell a lock, 30 to print
  and 15 for the command, with the walk of the functions an operation
  reaches shared with the gate's read-only check, which is perhaps 10
  lines more, and the `asks` and `acts` lines perhaps 25 on that
  walk. The rule on an outside step's proof is perhaps 15,
  and an operation wholly outside 20 of checker and 15 of handler.
  A `Reply` is perhaps 50 lines over the prelude, the checker, the
  handler and the TypeScript.
