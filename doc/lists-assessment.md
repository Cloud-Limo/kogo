# The lists, assessed

Asked for by Greg on 17 Nov 2026 (`doc/lists.md`, "Asked for: a deep
dive"; DEV-901). It decides nothing: each choice is named at the end
and is Greg's. Sizes are estimates of Kogo's own Go, code / tests,
and none was measured. Kotlin was run with kotlinc 2.4.21, the
version `.mise.toml` pins (the designs say 2.4.0), on files in
`/tmp/lists`; a claim not run says "unchecked".

## What is built, and what is on paper

Checked against `spec.md`, `check/`, `emit/` and `prelude/kogo/rpc/`:

- **Built: the `api` list of spec 15 alone.** A row is a name.
  `check/api.go` (274 lines) holds one shape by hand, four parameters
  and an `Either`; `emit/decls.go` writes a `rpc.Table`; `rpc.Tx` and
  `rpc.Db` serve it. Spec 15.2 still says "Nothing runs before an
  operation", and a `Tx` there is always SERIALIZABLE.
- **On paper only:** gates, proofs, the outside step, `routes`,
  `commands`, `job`, `effect`, `work`, `@Acts`, `SerializableTx` as a
  type, and the printed table. None is in `spec.md` or in code.
- **The checker is not driven by a table**, as `doc/lists.md` has it:
  that is a plan. And the fit of an operation's shape is held twice
  today, by `check/api.go` and by Go's generics in `rpc.Tx`.
- **"A gate run twice" is the outside step's alone** (`doc/gates.md`):
  a plain gated operation runs its gate once, in a read-only
  savepoint, and an operation wholly outside runs it once.

So five of the six lists can still be changed for the price of a
document.

## What the lists are for

| Need | Whose | Met by |
| --- | --- | --- |
| Which function answers this name, path, command or row; what to read for it; what to open round it | the runtime | the emitted table |
| Not to write the pipeline at each of 20 operations and 16 queues (open, gate, roll back, rerun, commit, claim, mark), nor be able to write it wrong | the author | the runtime calls the functions |
| To be told when one stage does not give what the next takes | the author | the checker's fit |
| Where a rule starts: a gate only reads, an outside step passes no proof, an input crosses the wire, a queue's table has the runner's columns | the checker | the role, which is the root of each walk |
| Who may call what, on one screen; what each touches; a change as a diff; nothing served that is not listed | the reviewer | the list and the printed file |
| The types a browser sends and gets | the page's author | `kogo ts`, from the list |

One reason stands under all of them. The runtime needs what Kogo
leaves out on purpose: catching a panic, a loop with no end, a
timer, SQL over a table's name (`doc/work.md`, "Hand-written Go").
So it is Go, and something must join Go to Kogo's functions. The
lists are that joint, and the question is only where it is written.

## How: what is not written where it is used

| What happens | What holds it | What a reader must already know |
| --- | --- | --- |
| A function is called by its role: nothing at `setNotes` says it is served, or by whom | the row; the checker's shape | to look for a list that names it |
| A shape is given by position: `act(env, key, fields)` is told from `record(tx, env, row, result)` by order and type | the checker, by the table of fifteen roles | the rule of four slots and the role's row |
| The gate's proof becomes the work's second parameter | the checker's fit | that the two are one row |
| The gate runs in a read-only savepoint: an `update` there is refused | Postgres at run, the checker before | that the function is a gate, which only the list says |
| With an outside step the gate runs twice, and its second run may refuse after the service was changed | the handler; the word `outside` | the order: gate, end, step, gate, work |
| The first parameter's type opens the transaction, sets its level, and has the function rerun on a conflict; an answer commits, a refusal rolls back | the handler; `needs-serializable` | spec 15.3; that the body may run several times |
| The input is read from JSON, or flags, by property name, and a 400 comes before the function | the handler | spec 15.4 |
| An effect's row is claimed, keyed, entered, prepared, acted on, recorded and marked done; `failed` and `recovery` run in the failing transaction and the alert after it | the runner; the checker for the fit and the columns | the runner's eight or ten rules |
| A table is emitted, and `main` mounts it | the emitter | that a function in no list is never called |
| A file is printed, and enforces nothing | a command; the application's `git diff` | to read it |

Three remarks. The transaction by type is the least hidden of these:
it is in the signature, in a code of two types. The fit and the
shapes are hidden only in that the roles' table is in the spec and
not in something a reader can jump to. And one thing is not stated
anywhere: whether a `job` or `effect` that no `work` list names is
an error. As written it would never run, in silence.

## The two examples, as designed

```kotlin
api booker { setNotes gatedBy setNotesGate }

fun setNotesGate(tx: SerializableTx, env: Env, request: Request, input: NotesInput): Either<String, HeldBooking> =
    booking(tx, env, request, input.id, OWNER_ROLES)
fun setNotes(env: Env, booking: HeldBooking, input: NotesInput): Either<String, BookingView> = either {
    saveNotes(booking, input.notes)
    bookingView(booking)
}
```

```kotlin
effect chargeCard on charge_job {
    attempts 1
    prepare  prepareCharge
    act      charge
    record   recordCharge
}

fun prepareCharge(tx: Tx, env: Env, row: ChargeJob): Either<String, ChargeFields> = ...
@Acts fun charge(env: Env, key: EffectKey, fields: ChargeFields): Act<PaymentIntent> = ...
fun recordCharge(tx: Tx, env: Env, row: ChargeJob, result: PaymentIntent) { ... }
```

Every way below keeps these five functions' bodies. Not Kotlin: the
first line of each is a syntax error to kotlinc (run). The mechanism
as a whole: perhaps 500 / 400 for the parser's six forms, the fit
from a table and the emitted tables, beside the handler and the
runner, which every way but 1 needs unchanged.

## Other ways

### 1. The application writes the pipeline

The runtime is a library, and an operation is spec 15's as built:
it takes the `Db` and the request and runs its own gate.

```kotlin
api booker { setNotes }

fun setNotes(db: Db, env: Env, request: Request, input: NotesInput): Either<String, BookingView> =
    db.gated({ tx -> setNotesGate(tx, env, request, input) }) { booking ->
        saveNotes(booking, input.notes)
        bookingView(booking)
    }

@Acts fun chargeCard(db: Db, env: Env, claim: Claim<ChargeJob>) =
    db.effect(claim,
        prepare = { tx -> prepareCharge(tx, env, claim.row) },
        act     = { key, fields -> charge(env, key, fields) },
        record  = { tx, result -> recordCharge(tx, env, claim.row, result) })
```

- **Written where:** the order of the stages is at the operation, in
  a call a reader can open. `gated` and `effect` are two prelude
  functions with ordinary generic signatures, which fit the stages:
  no rule of the checker's. kotlinc accepts both and refuses
  `chargeCard` without `@Acts` (run).
- **Still held:** proofs, the rule on statements, `@Acts`. A list of
  names is still needed for the wire and the TypeScript, and
  something in Go must still call `chargeCard` for each claimed row.
- **Lost:** the work is a lambda that sees `request` and `db`, so
  "gated work takes no `Request`, `Tx` or `Db`" goes, unless the
  checker learns `gated` by name, which is the list again as a call.
  The printed file cannot tell `gate reads` from `reads` without the
  same knowledge. A function that holds a `Db` and acts is what the
  17 Nov rules exist to refuse. Written with no helper at all, as
  two `db.tx` blocks round a call, the effect must itself mark the
  row done in `record`'s transaction, and a forgotten line sends a
  charge twice: the one invariant that must not rest on the author.
- **Size:** perhaps 150 / 150 of prelude, and three lines more at
  each of the application's operations and queues.

### 2. A role is a function of an interface

```kotlin
// the prelude
interface Gated<E, P, I, A> {
    fun gate(tx: SerializableTx, env: E, request: Request, input: I): Either<String, P>
    fun work(env: E, proof: P, input: I): Either<String, A>
}
interface Effect<E, R, P, T> {
    fun prepare(tx: Tx, env: E, row: R): Either<String, P>
    @Acts fun act(env: E, key: EffectKey, prepared: P): Act<T>
    fun record(tx: Tx, env: E, row: R, result: T)
}
```

```kotlin
data object SetNotes : Gated<Env, HeldBooking, NotesInput, BookingView> {
    override fun gate(tx: SerializableTx, env: Env, request: Request, input: NotesInput) =
        booking(tx, env, request, input.id, OWNER_ROLES)
    override fun work(env: Env, proof: HeldBooking, input: NotesInput) = either {
        saveNotes(proof, input.notes)
        bookingView(proof)
    }
}

data object ChargeCard : Effect<Env, ChargeJob, ChargeFields, PaymentIntent> {
    override fun prepare(tx: Tx, env: Env, row: ChargeJob): Either<String, ChargeFields> = ...
    @Acts override fun act(env: Env, key: EffectKey, prepared: ChargeFields): Act<PaymentIntent> = ...
    override fun record(tx: Tx, env: Env, row: ChargeJob, result: PaymentIntent) { ... }
}

api booker { SetNotes }        // a list of objects is still wanted
```

- **Written where:** the role is at the function, as `override fun
  gate`, and the shape is one jump away, in the interface. The table
  of roles becomes about eight interfaces in the prelude.
- **Still held:** the fit, by the rule for implementing an interface
  that spec 3.4 has, with no list code. kotlinc compiles this,
  refuses a `work` whose proof is another type, and refuses an `act`
  that drops `@Acts` (all run). A missing `failed` on a job is a
  missing member. Every rule that starts at a role still has its
  root.
- **Lost:** an operation is no longer an ordinary function (spec
  15.1). Spec 3.4 has no default bodies, so a shared gate is written
  again in each of its operations, or 3.4 opens; optional `failed`
  and `recovery` need the same. `attempts` and the table have no
  place: a `data object` implements no `val`. The wire name is
  `SetNotes`, or a name changed out of sight. `act` as an
  interface's `fun` is what "an action is not kept in a value"
  refuses. The row no longer shows the gate, so the one screen
  shows names alone.
- **Size:** perhaps 300 / 250, and four type names at each entry.

### 3. A marker on each function, and no list

```kotlin
@Operation("booker", gatedBy = "setNotesGate")
fun setNotes(env: Env, booking: HeldBooking, input: NotesInput): Either<String, BookingView> = ...

@Prepare("chargeCard", table = "charge_job", attempts = 1)
fun prepareCharge(tx: Tx, env: Env, row: ChargeJob): Either<String, ChargeFields> = ...
@Acts @ActOf("chargeCard")
fun charge(env: Env, key: EffectKey, fields: ChargeFields): Act<PaymentIntent> = ...
@Record("chargeCard")
fun recordCharge(tx: Tx, env: Env, row: ChargeJob, result: PaymentIntent) { ... }
```

- **Written where:** the role is at the function. What joins the
  three steps is a name in a string.
- **Still held:** everything the lists hold, by the same fit code,
  after a pass that gathers the marks. kotlinc compiles it and
  checks none of it; an annotation cannot take `::setNotesGate`
  ("invalid type of annotation member", run), so a gate is named in
  a string or guessed from the proof's type.
- **Lost:** the table on one screen, wholly: the printed file would
  be the only table, and it is the one that enforces nothing. A new
  file anywhere adds an operation. The steps of one effect are tied
  by spelling. And it is a second annotation form, refused on 15 Nov
  ("markers are the one annotation form").
- **Size:** perhaps 450 / 400: more than the lists.

### 4. Routes in Go, in `main`

This way changes neither example; it takes the `routes` list out.

```go
mux.HandleFunc("POST /backend/smtp2go-webhook", func(w http.ResponseWriter, r *http.Request) {
    if !hook.Matches(r.Header.Get("Authorization")) { w.WriteHeader(401); return }
    event, ok := readEvent(r.Body)                  // message-id, rcpt, event
    if !ok { w.WriteHeader(400); return }
    hooks.Call(r.Context(), "recordDeliveryFailure", event)   // an ungated operation, in a table not mounted
    w.WriteHeader(200)
})
```

- **Written where:** the method, the path and the translation are in
  Go; the write is an ungated operation of an `api` list that `main`
  calls and does not serve.
- **Still held:** the operation's shape and wire types, and its
  lines in the printed file. The secret is compared in Go either
  way.
- **Lost:** the route's path in the printed file, and the checker's
  sight of a dozen lines of translation for each route. Go grows
  with each route: cusoon2 has two.
- **Size:** minus about 140 (`doc/gates.md`: 40 of prelude, 70 of
  handler, 30 of checker), with `Response`, `Secret`, `Request.query`
  and `Request.body` out of the spec; plus a way for Go to call one
  operation of a table, perhaps 20 / 20, and about 15 lines of Go a
  route in the application.

Taken to the two examples it is the table written by hand:

```go
booker := rpc.Table[Env]{Operations: []rpc.Operation[Env]{rpc.Gated("setNotes", app.SetNotesGate, app.SetNotes)}}
runner.Effect("charge_job", runner.Attempts(1), app.PrepareCharge, app.Charge, app.RecordCharge)
```

Go's generics fit the stages, as they do in `rpc.Tx` today. But the
checker no longer knows a gate from any function, so the read-only
check, the rule on an outside step, the wire types, the TypeScript
and the printed file all go. That is too much for what it saves,
perhaps 250 / 200.

### 5. The forms refused on 15 Nov, with six lists

**A call.** Valid Kotlin, and the form DEV-894 sketches:

```kotlin
val booker = api(gated(::setNotesGate, ::setNotes))

@Acts val chargeCard = effect("charge_job", attempts = 1,
    prepare = ::prepareCharge, act = ::charge, record = ::recordCharge)
```

With signatures that say the shapes, as `fun <E, P, I, A> gated(gate:
(SerializableTx, E, Request, I) -> Either<String, P>, work: (E, P, I)
-> Either<String, A>): Op<E>`, kotlinc fits the stages and refuses a
misfit (run). It keeps the one screen, the printed file and nothing
by accident. Two things are new since 15 Nov. The roles' table would
be about fifteen signatures in the prelude, which a reader opens,
where today it is prose. And `::charge` needs `@Acts` on the list or
an opt-in (run: refused without, accepted with either), against "no
`::` reference" to an action. Greg's objection stands as it was: it
looks like a call, and the compiler does the work. Perhaps 400 /
350: the lists less their parser.

**A block that holds the functions, with the context declared once.**

```kotlin
effect chargeCard on charge_job(env: Env, row: ChargeJob) {
    attempts 1
    prepare(tx: Tx): Either<String, ChargeFields> = ...
    act(key: EffectKey, fields: ChargeFields): Act<PaymentIntent> = ...
    record(tx: Tx, result: PaymentIntent) { ... }
}
```

It fits an effect better than it fitted `api`: three steps of one
row are one thing, and a role is a keyword. It still hides
parameters, the steps stop being functions a test can call, and a
gated operation gains nothing by it. Not Kotlin. Perhaps 800 / 600.

**A file outside the source** (`lists.toml`: `setNotes = { gatedBy =
"setNotesGate" }`). Every `.kg` file is then valid Kotlin with no
prelude trick, and the table is on one screen. But names in it are
unknown to an editor, errors point into a second syntax, and a queue
is declared away from its functions. Perhaps 550 / 450.

**Every public function of a named package.** An effect has three
roles and a table, which a package's name cannot say, and a gate
cannot be paired. It fails for five of the six lists.

**An annotation** is way 3.

## Laid side by side

| Way | One screen | Printed file | Nothing by accident | The fit is held by | Kotlin |
| --- | --- | --- | --- | --- | --- |
| The lists | yes | yes | yes | the checker's table | no |
| 1. Pipeline as code | names only | no `gate reads` | yes | a generic signature | yes |
| 2. Interfaces | names only | yes | yes | implementing an interface | yes |
| 3. Markers | no | yes | yes | the checker, by strings | yes, unchecked |
| 4. Routes in Go | all but routes | all but paths | yes | as the lists | Go |
| 5. A call | yes | yes | yes | a generic signature | yes |
| 5. A block | yes | yes | yes | the checker | no |
| 5. A file outside | yes | yes | yes | the checker, by strings | not a `.kg` |

## Recommendation

1. **Keep the mechanism: the runtime calls Kogo through a table,
   and a list is the one place a function gets its role.** No other
   way keeps all three of the reviewer's goods, and the explicit
   pipeline gives back the two things the 17 Nov designs were made
   to take away: work that holds the request, and an effect whose
   safety is the author's care.
2. **Take routes out, into Go (way 4).** Two routes do not pay for a
   list, three prelude types and a section of the spec, and what
   each writes stays an operation in the printed file. Reopen at
   five routes. Commands stay: ten, read by property name, with no
   Go to grow.
3. **Put the roles' shapes where a reader can open them.** Most of
   the hiding found above is that a shape is known only from the
   spec. If DEV-894 takes a call form, the prelude's signatures are
   the table. If it keeps Kogo's own syntax, the printed file can
   carry it: a line for each entry saying what runs, in order
   (`runs  gate, renameLogins, gate, work`), perhaps 15 / 10.
4. **Refuse a `job` or `effect` that no `work` list names**, perhaps
   10 / 10, so that nothing is left unserved by accident either.
5. **Do not take** markers, interfaces, the block, or the file
   outside. Each costs as much as the lists and loses the row.

**What DEV-894 should know.**

- Compiles as Kotlin (kotlinc 2.4.21): way 1, way 2, way 3 with
  strings, and the call. Needs Kogo's own syntax: the lists as
  drawn, and the block.
- Its sketch, `(Nothing, E, Request, Nothing) -> Either<String,
  Any?>`, accepts any first and last parameter, so kotlinc fits
  nothing there. Signatures with real type parameters fit the
  stages; kotlinc's message for a misfit is "cannot infer type for
  type parameter 'A'", so Kogo's checker keeps its own (principle 7).
- A list that names `act`, an outside step or `alert` by `::` must
  carry `@Acts` or opt in, and Kogo today lets no file opt in and
  refuses `::` on an action. `@Acts val chargeCard = effect(...)`
  compiles, and reads as true.
- An annotation takes no function reference.
- The call form needs `vararg` in a prelude declaration, which spec
  13 already allows. Way 2 needs default bodies in an interface,
  which spec 3.4 refuses, for a shared gate and an optional role.
- Unchecked: the rows of `routes` and `commands` as Kotlin, and
  whether Kogo's own inference (spec 5.1) settles `P` from a
  function reference as kotlinc does.

## The choices, each Greg's

1. Keep the lists as the joint, or have the application write the
   pipeline (way 1).
2. Routes in a `routes` list, or in Go in `main`.
3. Whether the roles' shapes are prelude signatures (with a call
   form) or stay a table in the spec.
4. Whether a list that names an action carries `@Acts`.
5. Whether the printed file states the order of calls.
6. Whether a queue in no `work` list is an error.
