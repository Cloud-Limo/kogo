# Gates: who may call an operation

How an operation finds out who is asking, checks what they may do and
reaches the database. Greg ruled on this design on 21 Nov 2026
(DEV-1035), from cusoon2's `doc/plans/gates.md`, which is the same
design written for cusoon2's port and has its counts. This document
is the design alone, in Kogo's terms. What was weighed and not taken
keeps a sentence each, with its reason, at the end; `decisions.md`
has the rest.

What is built: the list, its check, the gate and the work (spec 15.1
to 15.3, 15.5), a gate held to reading (15.2, 15.3), `action fun`
(7.1), markers (11), and the two refusals of a transaction used after
it ended (15.7; DEV-1040). The printed table and commands are
designed and not built. The listings of the desk are
`examples/24_gates.kg`, which the tests check and serve. A listing
marked "a sketch" was not checked.

## The shape

- The list says who may call each operation, in a word.
- The word names a gate, which answers a call: the transaction and
  who is asking. One file makes a call.
- The work is a plain function handed the call. Its first lines are
  steps. A step turns an id from the request into a held value: a
  permission that no other file can make.
- A model function that changes a thing takes the held value, so its
  step cannot be left out.
- Postgres holds the rows, where the application gives it policies.

Every rule the checker holds here refuses a mistake itself and not a
stand-in for one (`doc/design.md`, principle 1). A transaction is a
plain value: it is kept in a property, in a name, and passed on. What
is refused is a transaction used after it ended, in the two places
that can happen (7 and 8).

## 1. The list

```kotlin
// api.kg
api desk checkedBy fromOurPage {
    statusOf    gatedBy atDesk
    unpaid      gatedBy atDesk
    markPaid    gatedBy atDesk
    sendInvoice gatedBy atDesk
    testPrinter gatedBy mayPrint
    looks       ungated
}

api hooks unchecked {
    forgetLooks ungated
}
```

- `checkedBy` names the list's check, which the server runs once for
  a request, before any row, ungated ones included: the Origin's
  check, then the CSRF token's (`doc/origin.md`; `decisions.md`, 18
  Nov 2026). It runs in a read-only transaction of its own that the
  server rolls back, and its refusal is a 403 with its words.
- A list with no check says so, `api hooks unchecked { ... }`, as a
  row says `ungated`, and its Go type cannot be mounted for a
  browser.
- A row says `gatedBy <gate>` or `ungated`. A row with neither is a
  compile error.
- A gate says who is asking, so many rows share one. The desk has
  one, `atDesk`. cusoon2's ops site has one, `loggedIn`, and a
  storefront a handful: an admin, a customer, anyone.
- `ungated` is for a row that finds out who is asking itself (8). The
  word is rare, and marks the rows a reviewer reads whole.

For one request the server runs the list's check; reads the body into
the work's input type, or answers 400; opens the transaction and
calls the gate; on a refusal rolls back and answers with the gate's
words; and otherwise calls the work with the environment, the gate's
answer and the input, and commits (spec 15.3, 15.5).

## 2. The call

```kotlin
// call.kg: the one file that makes a call
data class Clerk private constructor(val address: String)

data class DeskCall private constructor(val tx: SerializableTx, val clerk: Clerk)

fun atDesk(tx: SerializableTx, env: DeskEnv, request: Request): Either<String, DeskCall> = either {
    val misses = tx.queryOne<Int>(sql"select count from look where address = ${request.clientAddress}") ?: 0
    ensure(misses == 0) { "This address may not use the desk." }
    DeskCall(tx, Clerk(request.clientAddress))
}
```

- A gate answers a call: the transaction, in a plain property, and
  what the gate found. It is an ordinary data class, and no rule
  knows it as a call.
- `private` is the file, and covers `copy` (spec 2, 3.1). Only
  `call.kg` makes a `DeskCall` or a `Clerk`, so a call means that
  `atDesk` ran in this transaction and found a clerk.
- Where Postgres is told who is asking (6), the gate binds the person
  as it makes the call, so that the person bound and the person
  handed to the work are one value, set on adjacent lines of the only
  function that makes a call. Binding is a setting, not a write.

How far it reaches: every gated row, and no `ungated` one. A later
statement could bind another person on purpose; a marker on the
function that binds (Marks) stops the slip of calling it, and nothing
stops the statement.

## 3. The work

```kotlin
// desk.kg, which opts in to nothing
fun markPaid(env: DeskEnv, call: DeskCall, input: InvoiceRef): Either<String, Long> = either {
    val invoice = requireOpenInvoice(call, input.number).bind()
    ensure(setStatus(call.tx, invoice, true)) { "Invoice ${invoice.number} is gone." }
    invoice.totalCents
}
```

- The work takes the environment, the call and the input. It is never
  handed the request, a `Db` or a bare transaction (`gate`,
  `operation`), so it has no line that checks who is asking and none
  to leave out.
- A line refuses only if it ends in `.bind()`, is an `ensure` or a
  `raise`.
- The gate and the work run in one transaction, of the kind the
  gate's first parameter names ("Two kinds of transaction"). An
  answer commits it, a refusal rolls it back, and a conflict runs
  both again. So a step that refuses after a write has undone the
  write.
- Statements are written in model functions. Nothing enforces that:
  `call.tx` is a plain transaction, and a statement in an operation's
  file compiles. What it can reach is Postgres's to hold (6).

## 4. A step and its held value

```kotlin
// model.kg
@RequiresOptIn
annotation class DeskOnly

data class OpenInvoice @DeskOnly constructor(val number: String, val totalCents: Long)

// steps.kg, which opts in to DeskOnly
fun requireOpenInvoice(call: DeskCall, number: String): Either<String, OpenInvoice> = either {
    val total = call.tx.queryOne<Long>(sql"""
        select total_cents as "total_cents!" from invoice where number = $number and status <> 'paid' and total_cents is not null
    """) ?: raise("Invoice $number cannot be changed.")
    OpenInvoice(number, total)
}
```

- A held value is the permission. Its constructor is marked (spec
  11), and an operation's file does not opt in, so only a step makes
  one. An operation's input never makes one either, opt-in or none
  (spec 15.4).
- A step takes the call and an id of the input, and no person: it
  asks about whoever the gate found.
- A thing that is not there and a thing the caller may not touch are
  refused in the same words.
- Reading and changing are two types where they differ: a step that
  admits more callers answers the kind that reads, and a function
  that writes takes the kind that changes.

How far it reaches: the thing the id names. Any other id of the input
is tied by a statement's `where` alone (5).

## 5. The model function

```kotlin
// model.kg
fun setStatus(tx: Tx, invoice: OpenInvoice, paid: Boolean): Boolean =
    tx.execute(sql"""
        update invoice set status = case when $paid then 'paid' else 'sent' end::invoice_status
        where number = ${invoice.number}
    """) == 1
```

- The parameter is the held value and never an id. This is what makes
  the work's first line impossible to leave out.
- It takes the transaction as a transaction, `call.tx` passed on. A
  second id, as a supplier's beside the business that is held, is the
  browser's, unchecked: the `where` ties it to the held value.

How far it reaches: Kogo types the statement against the schema and
does not judge its `where` (`doc/design.md`, principle 2). A wrong
one is caught by Postgres where a policy holds the table (6), and by
the application's test that sends another's id and reads a refusal.

## 6. Postgres beneath

```sql
-- a sketch: the desk's schema has no policy
create function desk.works_at(desk uuid) returns boolean
    language sql stable security definer set search_path = desk, pg_temp as $$
    select exists (select 1 from clerk_desk where clerk_id = current_clerk_id() and desk_id = desk) $$;

create policy change_invoice on desk.invoice for update
    using (desk.works_at(desk_id)) with check (desk.works_at(desk_id));
```

- Kogo holds nothing here and needs nothing: a policy is the
  schema's, and holds a table from whatever file a statement is
  written in. With nobody bound it shows nothing.
- The step and the policy call one function of the database, so the
  check that words a refusal and the wall beneath it cannot disagree.
- A column grant keeps the web's role from a column that no request
  sets.

How far it reaches: rows, never columns, and only the tables that
have a policy. Between two callers that one policy admits alike, the
held values and the marks on reads by a bare id are the wall, and the
application's access test probes them (Marks). cusoon2's worked
example is its `doc/plans/business-rls.md`: policies on five business
tables and column grants on `person`.

## 7. Work that changes something outside

```kotlin
// printer.kg
data class Printing(val address: String)

fun mayPrint(tx: SerializableTx, env: DeskEnv, request: Request): Either<String, Printing> = either {
    val call = atDesk(tx, env, request).bind()
    Printing(call.clerk.address)
}

action fun testPrinter(env: DeskEnv, printing: Printing, input: Unit): Either<String, String> = either {
    ensure(env.printer != "") { "No printer is set up." }
    env.console.println("${env.printer}: a test page for ${printing.address}")
    env.printer
}
```

- Work that changes something outside is an `action fun` ("A function
  that acts says so"). The server commits the gate's transaction and
  then runs the work with none open. The gate runs once.
- So the work is not handed a call, whose transaction has ended. Its
  row names a gate that begins with the list's own gate and answers
  facts alone. A row of this kind whose gate answers anything that
  holds a transaction is a compile error:

  ```
  api.kg:6:25 action-gate: testPrinter is an action fun, and its gate atDesk answers DeskCall, which holds a transaction, tx, a property of DeskCall: the server ends the gate's transaction before work that is an action fun runs, so name a gate that answers the facts testPrinter needs, or make testPrinter a fun, which runs in the transaction (spec.md 15.7)
  ```
- What the work needs of the request is a fact of the gate's answer,
  as the address is here.
- Such a row has no transaction for a step to run in, so a check on
  the thing its input names is its gate's: a gate may read the
  operation's input, and then stands in that one row (spec 15.2).
- This is the one place a gate and an answer type are written for
  particular rows. In cusoon2 they are the passkey registrations,
  which share a gate on each site, and deleting a passkey.

### A function that acts says so

A transaction is all or nothing and may run again; a call that
changes something outside is neither. So a binding that changes
something outside says `action`, and a function that calls an action
is an `action fun`, as it is to its own callers (spec 7.1). An
`action fun` takes no `Tx`, calls no action in a block that holds
one, and is not kept in a value. An action has three homes: a worker,
fed by a row (`doc/work.md`); an ungated operation on a `Db`, between
its transactions (8); and gated work that is an `action fun`. A
lookup says nothing and is allowed anywhere; a slow one holds its
transaction open. Which kind of operation to write is the table in
spec 15.1.

## 8. An `ungated` row

```kotlin
// desk.kg
fun looks(tx: Tx, env: DeskEnv, request: Request, input: Unit): Either<String, Int> = either {
    tx.queryOne<Int>(sql"select count from look where address = ${request.clientAddress}") ?: 0
}
```

- It takes the request and a bare transaction, or a `Db`, and nothing
  of this design reaches inside it. The list's check still runs
  first, and nobody is bound unless the row binds.
- These are the logins, the logouts and the page's first call, which
  make the session every gate reads, and gated work may not answer a
  `Reply`.

An operation that must keep a write after a refusal, or write what an
action answered, is `ungated` on a `Db` and calls its gate itself in
each of its transactions:

```kotlin
// cut to the first transaction
    val number = db.tx(serializable = true) { tx ->
        val call = atDesk(tx, env, request).bind()
        requireOpenInvoice(call, input.number).bind().number
    }.bind()
```

A `db.tx` block hands back facts, as a gate of 7 does. A block that
answers anything that holds a transaction, a call included, is a
compile error, since the transaction ends with the block:

```
login.kg:9:19 tx-escapes: this db.tx block answers DeskCall, which holds a transaction, tx, a property of DeskCall: the transaction ends with the block, so answer the facts the lines after it need (spec.md 10.7)
```

"Holds a transaction" is one meaning for both refusals: the type is a
`Tx` or a `SerializableTx`, or has one in a property or a type
argument, at any depth (spec 15.7).

### Ungated: logins and logout

A login reads the request's own session and address, must keep the
count of a failed attempt after a refusal, calls the identity
service, and makes the session that every gate reads. It ends the
caller's session and starts another, and the browser must be given
the new one's cookies. The answer carries them, in two types of
`kogo.rpc` (spec 15.2):

```kotlin
action fun completeEmailLogin(db: Db, env: Env, request: Request, input: CodeInput): Either<String, Reply<BookerView>> = either {
    val user = redeemEmailCode(db, env, request, input.oneTimeCode).bind()
    val session = db.tx(serializable = true) { tx -> completeLogin(tx, env, request, user) }.bind()
    Reply(bookerView(session), sessionCookies(session, env.clock.now(), isSecure(request)))
}
```

The server sends `answer` as the JSON and each cookie as a
`Set-Cookie` header, when the operation answers; a refusal sets none.
Only an ungated operation answers with a `Reply`, so a search for it
lists every place a cookie is set.

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
api booker checkedBy fromOurPage {
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
and served by one that needs the tenant alone. Cookies are set
by the first call, a login and a logout, where cusoon2 sets them on
every response: the same cookies, since a session's deadline does not
move. It changes how cusoon2 behaves, so it is made in the Kotlin
first, under its tests, and ported after.

## Marks

Mark what grants, not what reads (spec 11).

| Marked | Why |
| -- | -- |
| the constructor of a held value | it is a permission |
| a function that binds the person or the tenant, and one that binds in turn | it opens a wall |
| a read or write that answers by a bare id, where no policy tells two callers apart | it answers whoever holds the id: it is for a worker and a held value's own file, and an operation reaches those rows through a held value |

A model function that reads rows Postgres already filters carries no
mark. One that answers for a held thing takes the held value, and is
marked only where its caller is not a request. The request's side is
a plain function that takes the held value and calls the marked one:

```kotlin
// a sketch: needs no mark; unscopedInvoice keeps its own
fun invoice(tx: SerializableTx, desk: DeskManager, number: String): Invoice? = unscopedInvoice(tx, number)
```

Kogo holds the opt-in and nothing else: which functions an
application marks is its own. cusoon2's are listed in its
`doc/plans/gates.md`, with what its storefront's port starts from.

## A field only an admin sets

```kotlin
// a sketch
/** The notes, which any clerk may change on an open invoice. */
fun setNotes(tx: Tx, invoice: OpenInvoice, notes: String): Boolean

/** The total, which only a manager sets: it takes the manager's access, which a clerk's call has not. */
fun setTotal(tx: Tx, manager: DeskManager, invoice: OpenInvoice, totalCents: Long): Boolean
```

One function for what any caller may change and one for each thing
only an admin may. The admin's functions take a held value, so
another's call cannot reach them. The two comments are the guidance
for where a new field goes; no rule checks it. A test for each such
field, another caller sending it and it not changing, is the
application's.

## Two kinds of transaction

The kind is the handle's type, and every function that takes a
transaction writes one of two (spec 10.7). `Tx` is any transaction: a
function that takes it assumes nothing, and locks what it needs.
`SerializableTx` is one at SERIALIZABLE, where a conflict is a
serialization failure and never a wrong read, so a function that
takes it may read with no lock; it fits wherever a `Tx` is wanted,
and not the other way (`needs-serializable`). The first parameter of
a gate, or of an ungated operation, names the kind the server opens
(spec 15.3), and a call holds the kind its gate was handed, as
`DeskCall.tx` is a `SerializableTx`.

## A gate reads and decides; it does not write

Every gate of cusoon2 obeys this already: none writes a row. A gate
binds the person or the tenant, which is a setting of the
transaction and no write. cusoon2's routes do write, in front of the
gates: each makes a session for a request that comes without a live
one ("The first call makes the session" above). A count of attempts
writes, so it is no gate's: it belongs to an ungated operation. Both
checks were run on Postgres 18.7 on 17 Nov 2026:

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

## Routes that are not operations

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
  ("An `ungated` row"); Go writes its cookies as
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

## Commands

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
    recordAbn
    setPayid
    mailTest
}

data class AbnInput(val business: String, val abn: String, val gstRegistered: Boolean)

fun recordAbn(db: Db, env: Env, input: AbnInput): Either<String, String> = either {
    db.tx(serializable = true) { tx -> recordAbnCheck(tx, input.business, input.abn, input.gstRegistered) }.bind()
    "ABN checked: ${input.business}"
}
```

```
$ cusoon2 recordAbn --business acme --abn "51 824 753 556" --gstRegistered true
ABN checked: acme
```

- **A command has one shape**, `(db: Db, env: Env, input: I):
  Either<String, String>`: no `Request` and no gate, since whoever
  runs the binary on the server is trusted, as now. It holds a `Db`,
  so it may act between its transactions.
- **Its name is its function's name**, camelCase as written, over
  the CLI's habit of `--dry-run`, so a search for a property finds
  its flag; the list's own name (`admin`) is what `main` calls it by.
  **A flag is a property's name as written**, as an operation's
  JSON key is (spec 15.4), and its value is read as JSON's are:
  `true`, an ISO date, a number, text.
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

## The table is printed

Designed and not built. A command of its own prints, from the list,
the gate functions and Postgres's answers, each operation with its
gate and the tables it touches, into a file that is checked in. A
change of who may do what, or of what an operation reads and writes,
is then a diff, made on purpose or by accident:

```
setBookingNotes     viewer
                    runs         serializable
                    gate reads   booking, booking_role, session
                    reads        booking
                    writes       booking
submitBooking       booker
                    runs         serializable
                    gate reads   booker, session
                    reads        area, booker, place, price_regime, stripe_card
                    writes       booking, passenger, journey
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
                    writes       booker, session
                    acts         idp.redeemEmailCode
```

Each list's own line heads its rows: `api booker   checked by
fromOurPage   check reads session, tenant`, and for a list with none
`api hooks   unchecked: not served to a browser`, since the absence
is what a reviewer looks for (`doc/origin.md`, Q4).

The file enforces nothing: it is what a reviewer reads.

- **It is kept fresh as the TypeScript file is** (`doc/design.md`,
  "The server"): the application's checks run the command and fail
  when `git diff` shows a change, so Kogo has no rule for a stale
  file. The command needs a database, as `kogo check` does.
- **`action` is on the first line, beside the gate**, for a row
  whose work is an `action fun`: `testPrinter  mayPrint  action`. A
  row's order of calls is then read where its gate is: a line without
  the word is gate, then work, in one transaction.
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
  has no `gate reads`. A shared gate that reads a new table
  changes a line of every operation behind it, which is noisy and
  true.
- **`locks` is a row lock, of any strength**, printed where there is
  one. Postgres reports a `select ... for update` as needing update on
  the table, as an `update` does, and marks the lock apart, which is
  how the two are told. cusoon2's requests take no locks; its daemon
  and the Google login's callback do.
- **`asks` and `acts` are the calls to outside services**, each by
  the name of the application's function that makes it: one that
  calls `ask` itself, or an `action` binding itself. It is
  printed as its package's last name and its own, `stripe.charge`. A
  gate's lookups are on a `gate asks` line. So a change to what an
  operation reaches outside is a diff, a lookup added to a helper
  three calls down included. A binding no longer says `lookup`, the
  word that was kept for this table: when the table is built, it is
  given whatever it needs to find an operation's `asks` lines.
- **A lookup made while a transaction is open is marked `(in
  transaction)`**: it holds the transaction open while the service
  answers, and a rerun asks again. An action needs no mark, since
  none runs there. A call made with a key is marked `(keyed)`.
- **These lines are as sharp as the code.** A function that makes
  two calls is printed once, and a wrapper that takes its endpoint
  as a parameter is printed as itself on every operation that uses
  it. Nothing is refused for the table's sake.
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

## What holds what

|  | Held by | How far |
| -- | -- | -- |
| No operation without a gate | the list: a row with neither word is refused | all rows |
| No list without a check | the list's head: `checkedBy` or `unchecked` | all lists |
| The person checked is the person handed on | the private constructors of the call's file | gated rows; not a statement that binds again |
| A step is not left out | the model function's parameter, and the held value's marked constructor | where a model function takes the held value |
| Another's rows | the application's policies | the tables that have one |
| Two callers one policy admits | held values, the marks on reads by a bare id, and the application's access test | what the test probes |
| A second id | its `where`, and a probe | the ids that have a probe |
| A field only an admin sets | the function that takes the admin's held value, and a test | the fields that have a test |
| A gate that writes | `gate-writes`, and the read-only savepoint of a gate on a `SerializableTx` | "A gate reads and decides" |
| An action in a transaction | `action fun` and its rules (spec 7.1) | every call the checker reads |
| A transaction used after it ended | the two refusals of 7 and 8 | gated work that runs outside, and a `db.tx` block |

### What the checker holds

- Every `api` list says `checkedBy` a function or `unchecked`, and
  every row `gatedBy` a function or `ungated`.
- A gate's result is `Either<String, P>`, and `P` is the type of the
  work's second parameter. A gate that reads an input takes the
  work's input type, and stands in one row.
- Gated work takes no `Request`, `Tx` or `Db`, and a gate answers
  none of the three bare.
- A gate reaches no statement that writes, by Postgres's answer, nor
  one that locks where its transaction is a `SerializableTx`.
- A function that takes a `SerializableTx` is not handed a `Tx`.
- A function that calls an action is an `action fun`, and an `action
  fun` calls one, takes no `Tx`, acts in no block that holds one and
  is not kept in a value.
- A row whose work is an `action fun` names a gate whose answer holds
  no transaction (`action-gate`).
- A `db.tx` block answers nothing that holds a transaction
  (`tx-escapes`).
- A private constructor is called, and copied, only in its file; a
  marked one only in a file that opts in, and by no operation's
  input.

### What it does not hold

- **Whether the policy is right.** A gate that admits a caller it
  should refuse compiles. The gate holds nothing but the policy, and
  the printed table shows a change to it.
- **Where a transaction goes.** It is kept in a name and passed to
  any function. A statement in an operation's file compiles.
- **Whether a statement's `where` scopes it.** `where number =
  $number or status = 'sent'` compiles.
- **A transaction that leaves a block another way than as its
  answer's type.** A lambda that closes over one, answered as a `()
  -> Int`, is not refused, nor one assigned to a `var` declared
  outside the block.
- **Whether a gate on a `Tx` locks enough**, or calls a function of
  the database that writes.
- **Which functions are marked**, and whether a model function takes
  the held value it should.
- **Anything in the files that make a call or a held value.** There
  are few, and a reviewer reads them whole.

So the application's access test stays: it sees policy, which no rule
here does.

## Weighed and not taken

A sentence each, with its reason; `decisions.md` has the rest.

- **A proof's marked transaction, kept from every name** (`gate-proof`,
  `proof-tx-escapes`, `outside-passes-proof`, `action-proof`). Built,
  and taken out on 21 Nov 2026 (DEV-1040): it refused a transaction
  kept in a name, which is harmless inside the work, and not the
  mistake, a transaction used after it ended; the two refusals of
  15.7 refuse that.
- **A rule that a statement binds something of its proof, or is
  written only in the model.** Nothing checks a `where`, so the rule
  refused a stand-in and passed the mistake.
- **The outside step, a row's `after`.** Built, and taken out on 21
  Nov 2026 (DEV-1039): the one operation that needed it is `ungated`
  on a `Db`.
- **A gate function for each row, answering an envelope such as
  `Admitted<P>`.** A row's own check is a step, the first line of its
  work, and a model function that takes the held value means the step
  cannot be left out; a gate for each row adds a function and a type
  to every operation and holds nothing more.
- **A `Model` around the transaction, so that the work can write no
  SQL.** Kogo is made for writing SQL, and what a statement reaches is
  Postgres's to hold, by its policies, whatever file the statement is
  written in.
- **An `enteredBy` clause on the list, run before each gate.** A gate
  that reads no input already is the function that enters, and its
  row names it: `gatedBy loggedIn` says it a row at a time, in a word
  Kogo has.
- **`requires` and `then` blocks in an operation.** A step is a plain
  function whose line ends in `.bind()`; the blocks are new syntax and
  one more departure from Kotlin.
- **`steps` and `unread ids` lines in the printed table.** The table's
  command is not built, and a second id is held by its `where` and a
  test of the application's.
- **Every mark on a model function going**, with the access test
  alone as the wall between two callers a policy admits alike.
- **`action-tx` widened to whatever holds a transaction.** No value
  that holds one reaches an `action fun` once the two refusals hold,
  so the rule would refuse a parameter's type and not a use.
- **A `routes` list in Kogo.** Two routes do not pay for a list, two
  prelude types and a section of the spec; reopen at five routes.
- **The gate as a lambda in the list**, and **the gate as the work's
  first statements**, held there by the checker: the first parts the
  policy from the work, the second needs rules on the order of
  statements.
- **A slot of the request that a login fills with its cookies**, and
  **a function the server runs before every gate, which may write**:
  the answer carries the cookies, and the first call makes the
  session.
- **`kogo check` failing on a stale printed table**, **a row lock
  printed as a read or as a write**, **a view printed as the tables
  under it**, and **the endpoint's name on the `asks` and `acts`
  lines**: each is in `decisions.md`.
- **A third kind of call, `repeatable`**: it rests on the binding's
  word, and retry is a judgment for each queue.

## Open

- The printed table's command is deferred: named when it is built
  (`doc/roadmap.md`).
- Commands are designed and not built.
