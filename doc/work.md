# Workers, outside services and time limits

Agreed with Greg in discussion on 17 Nov 2026, as the design to work
from, beside `doc/gates.md`. It is not built and not in `spec.md`:
until it is, the spec stands as written. No example here has been
through `kogo check`, the names in them are invented, and every size
is a guess.

## Outside services

### One binding in Go, the clients in Kogo

The only outbound binding is a general HTTP call, in Kogo's prelude
(`kogo.http`). The clients of Stripe, Google and the identity service
are Kogo code that builds a request and reads the answer. So
`external` stays out of the application, the application stays out of
this repo, and a charge is a dozen lines Greg can read.

cusoon2 makes about 30 distinct calls: Stripe 4 (make a customer,
attach a card, change an email, charge), Google Maps 5, the identity
service about 20, and one send of mail. Its Kotlin already builds the
charge's fields as a map and has the library only post them.

A survey of the vendors' Go libraries on 17 Nov 2026, by a subagent
and not checked again, found:

- **Stripe's** is generated from its OpenAPI spec and runs on Go's
  plain HTTP client. Its main features are retries and generated
  idempotency keys, both of which cusoon2 turns off. A direct caller
  encodes a form body with nested keys (`metadata[x]`), sends a
  `Stripe-Version` header (without it a change in Stripe's dashboard
  changes the answers), and reads the error's `type`, where
  `card_error` is a decline.
- **Google Maps'** is a thin hand-written wrapper of the services
  Google now calls legacy, last released in December 2023, and its
  README says the new APIs will not be added. A legacy error is an
  HTTP 200 with a `status` field.
- **The identity service's** (FusionAuth) is generated and thin.

A port calls the endpoints the Kotlin calls, the legacy Google ones
included, so that it can be compared side by side. Moving to Google's
newer APIs changes behaviour and is a later step of its own.

Mail and PDF are bindings too. For PDF the survey's match for the
Kotlin's drawing (the core fonts, their widths, a PNG) is `fpdf`, now
kept on Codeberg; that its widths cover the core fonts was from
memory. It is to be checked before anything is promised.

### An endpoint is declared once

```kotlin
// kogo.http: the whole outbound binding, hand-written Go.
external class Http                         // a handle for one service
external class Lookup                       // asks, changes nothing
external class Action                       // changes something outside
external class KeyedAction                  // an action that needs a key
external class Field                        // one name and its value
external class Json                         // an answer

external fun lookup(method: String, path: String): Lookup
external fun lookup(method: String, before: String, id: String): Lookup
external fun lookup(method: String, before: String, id: String, after: String): Lookup
// action and keyedAction have the same three forms.

external fun field(name: String, value: String): Field      // and Long, Double, Boolean
external fun group(name: String, fields: List<Field>): Field

external lookup fun Http.ask(endpoint: Lookup, fields: List<Field>): Either<HttpError, Json>
external action fun Http.send(endpoint: Action, fields: List<Field>): Either<SendFailure, Json>
external action fun Http.sendOnce(key: EffectKey, endpoint: KeyedAction, fields: List<Field>): Either<SendFailure, Json>

external fun Json.string(key: String): String?
external fun Json.long(key: String): Long?
external fun Json.double(key: String): Double?
external fun Json.boolean(key: String): Boolean?
external fun Json.at(key: String): Json?                    // a nested object
external fun Json.list(key: String): List<Json>?            // an array of objects
```

```kotlin
// google.kg: every Google endpoint the application uses.
val TIME_ZONE: Lookup = lookup("GET", "/maps/api/timezone/json")

fun timeZone(google: Http, at: LatLng, now: Instant): Either<HttpError, ZoneId> = either {
    val answer = google.ask(TIME_ZONE, listOf(
        field("location", "${at.lat},${at.lng}"),
        field("timestamp", now.epochSeconds))).bind()
    ensure(answer.string("status") == "OK") { unreadable("status") }
    zoneOf(answer.string("timeZoneId") ?: raise(unreadable("timeZoneId")))
}
```

```kotlin
// stripe.kg
val CHARGE: KeyedAction = keyedAction("POST", "/v1/payment_intents")

fun customerEmail(id: String): Action = action("POST", "/v1/customers/", id)
fun attachCard(id: String): Action = action("POST", "/v1/customers/", id, "/sources")

fun chargeFields(charge: ChargeFields): List<Field> =
    listOf(
        field("amount", charge.amountCents),
        field("currency", "AUD"),
        field("confirm", true),
        field("application_fee_amount", charge.feeCents)) +
    if (charge.customerId != null)
        listOf(field("customer", charge.customerId), field("payment_method", charge.paymentMethod), field("off_session", true))
    else
        listOf(group("payment_method_data", listOf(field("type", "card"), group("card", listOf(field("token", charge.paymentMethod))))))

fun charge(stripe: Http, key: EffectKey, fields: ChargeFields): Either<SendFailure, PaymentIntent> = either {
    val intent = stripe.sendOnce(key, CHARGE, chargeFields(fields)).bind()
    PaymentIntent(
        intent.string("id") ?: raise(unreadable("id")),
        intent.string("status") ?: raise(unreadable("status")))
}

fun updateCustomerEmail(stripe: Http, id: String, email: String): Either<SendFailure, Unit> = either {
    stripe.send(customerEmail(id), listOf(field("email", email))).bind()
}
```

- **A handle is one service**, made in the application's `main` with
  its address and credentials. Kogo code holds no secret.
- **Every name of the service's is a literal string**, in a request's
  fields and where an answer is read. They are the service's
  interface, checked by eye against its documents, and no name of
  ours is turned into one: renaming a property changes nothing that
  is sent. cusoon2's Kotlin writes the charge so (`chargeParams`),
  with its two shapes as an `if`. An operation's own input and answer
  take their keys from property names (spec 15.4), which is another
  case: the browser's types are written from the same properties, so
  no one writes a key. The vendors do not agree on a spelling in any
  case: Stripe's keys are `snake_case`, the identity service's
  `camelCase`, and Google's are both.
- **An id in a path is an argument where the endpoint is made**, so
  an endpoint with one is a function of it. The parts are joined as
  written: `/v1/customers/` and `cus_123` and `/sources`. The binding
  escapes the id alone, so one from a browser that holds a `/` or a
  `?` stays one piece of the path, and it panics where the text
  before an id does not end in `/` or the text after does not begin
  with one. By the vendors' APIs as remembered, 2 of Stripe's 4 calls
  and about 4 of the identity service's 17 have an id in the path,
  and none has two.
- **An answer is read by key.** A read gives `null` where the key is
  absent, is JSON's `null` or holds another type, and what that means
  is said where it is read. A client function gives an ordinary value
  of the application's, and nothing past it sees JSON or a service's
  names. A mistyped key reads as `null` on every call; a test that
  makes the call finds it.
- **The kind is said once, with the path.** `ask` takes only a
  `Lookup`, `send` only an `Action`, `sendOnce` only a `KeyedAction`,
  so `stripe.ask(CHARGE, ...)` does not compile.
- **The checker needs no new rule.** A function that calls only `ask`
  is a lookup and may run in a transaction; one that calls `send` or
  `sendOnce` is an action and may not (`doc/gates.md`, "Outside
  calls").

Whether a call changes anything is still someone's word: the author of
a service's list of endpoints, about 30 lines in Kogo, where it is read
on one screen. An outbound binding that says neither `lookup` nor
`action` is an error.

Weighed and not taken:

- **A request and an answer as data classes**, an endpoint stating
  the type of each, as this section first had it. A property's name
  became a field's out of sight, by the rule SQL's columns have,
  which fits no vendor but Stripe; and with the names kept letter for
  letter it still needed a rule for a `null` property, one for a
  nested class, and a renamed nullable property read as `null` on
  every call.
- **A placeholder in the path**, `{id}`, filled from the request's
  property of that name: a match by name. And a hole filled by an
  argument of `send` (`sendAt(endpoint, id, fields)`), which doubles
  the three kinds of endpoint and their calls; and a string template,
  which escapes nothing.
- **The walk that reads an operation's input**, loosened to read an
  answer into a data class.

## Workers

Some work cannot be done in a request: it is due later, or on a
schedule, or it touches another service and must outlast a failure. A
worker finds the next owed piece, does it once and records it; a
failure neither loses the work nor does it twice, and someone is told.

cusoon2's daemon is 2,431 lines: 13 queues of jobs and 3 of effects
(saving a card, charging one, sending mail), and about 500 lines of
runner. Its rule, agreed there on 25 Sep 2026: a job is database work
in one transaction, which may look things up outside and causes an
effect only by inserting a row; "a remote write happens in no
transaction, ever." Every job keeps to it, and nothing holds them to
it: `sendOpsEmail` sends at once and any job may call it.

### A job cannot call an action

A job is a function that holds a `Tx`, so the rule for requests holds
it, with nothing added:

```kotlin
fun sendBookerNotice(tx: Tx, env: Env, job: BookingIdent) {
    val payload = bookingPayload(tx, env.google, job.id)        // a lookup: allowed
    enqueueMail(tx, bookingConfirmedEmail(payload), "notifyBooker for ${job.number}")   // a row
    setBookerNotified(tx, job.id, JobStatus.COMPLETE)
}
```

### An effect's `act` is the only place a worker acts

An effect is one outside write for one row, in three steps. It is the
shape of a request's gate, outside step and work, started by a row
where that is started by a browser.

```
request:  gate     (transaction)  ──►  outside  (none)  ──►  work    (transaction)
effect:   prepare  (transaction)  ──►  act      (none)  ──►  record  (transaction)
```

```kotlin
fun prepareCharge(tx: Tx, env: Env, row: StripeChargeJob): Either<String, ChargeFields> = ...
fun recordCharge(tx: Tx, env: Env, row: StripeChargeJob, result: PaymentIntent) { ... }

fun charge(env: Env, key: EffectKey, fields: ChargeFields): Act<PaymentIntent> =
    charge(env.stripe, key, fields).fold(
        { failure ->
            when (failure) {
                is SendFailure.NotSent -> Act.NotMade(failure.why)       // it never left
                is SendFailure.Refused -> Act.Refused(declineWords(failure.body))
                is SendFailure.Unknown -> Act.Unknown(failure.why)       // it left and no answer came
            }
        },
        { intent -> Act.Done(intent) })
```

`prepare` and `record` hold a `Tx`, so neither can act. `act` is
handed no `Tx` and no `Db`, and Kogo has no globals (spec 7), so it
cannot reach the database. That it is called with no transaction open
is the runner's doing.

Kogo has no exceptions. What a throw from the Kotlin's `act` meant,
"back off and offer the row again", becomes a value beside done,
refused and not made, so the choice to try again is written where the
Kotlin leaves it to whatever throws. A binding turns its library's
error into the `Left` side once, in its Go; a bug there is a panic,
which the runner catches at the job's edge, as the server does at a
request's.

### Retry is each queue's stated choice

A third word for a binding, `repeatable`, was weighed and dropped.
Retry is not a property of the call: cusoon2's mail is not safe to
repeat and is retried without limit, since a second message is a
nuisance and a lost one is worse; its charge is safe to repeat under
its key and is never retried (DEV-714 there, 27 Sep 2026), since every
retry weighed had a silent double charge as its worst case. So each
queue states its attempts, with no default, as the Kotlin has it.

### A key comes only from the runner

A `KeyedAction` is sent with an `EffectKey`, which the runner makes
from the effect's table and the row's id and hands to `act`.

- A keyed call is reached only from an effect's `act`. A login or an
  outside step may `send`; neither can charge a card.
- The key is not built by hand, as the Kotlin's is.
- A row has one key, so a replay after a crash sends the same one.

### Owed is the runner's rule

Each queue of the Kotlin says which of its rows are owed as a
fragment of SQL that the runner puts into its claim: mail's
`sent_at is null and (not_after is null or not_after > now)`, and
`stripe_card_id is null` and `stripe_charge_id is null`; and the
charge gives an order of its own, a waiting payer before an invoice
run. Kogo has no fragments, and the three say one thing: owed until
the result is recorded. So the runner's columns gain two, and a queue
writes no condition:

```
id, attempts, next_attempt_at, claimed_at, last_error, failed      -- the Kotlin's EffectFields
done_at       -- set by the runner in the transaction of record
not_after     -- null, or the time after which the write is not made; set by what inserts the row
```

- **Owed**, for every queue: `done_at is null and (not_after is null
  or not_after > now)`, with the runner's own rules for a lease, a
  back-off and a failed row.
- **The order**, for every queue: rows with a `not_after` first, the
  soonest first, then by `next_attempt_at`. A watched request stamps
  its row ("An effect someone is watching" below), so a waiting
  payer's row goes before an invoice run's, as the charge's own order
  has it.
- **A queue's own result stays**: `record` writes `stripe_charge_id`
  as before, and the runner no longer reads it.

In the Kotlin's runner (`runEffectQueue`) it is small: `done_at` is
one line beside `attempts = 0` where a done act is recorded, in the
one `update` that carries the queue's own columns; `not_after` is
only read, in the claim; three `owed` and one `order` go; mail's
`sent_at` becomes `done_at`. That runner is not yet live, so no
finished row needs its `done_at` filled. Made in the Kotlin first,
under its tests. Weighed and not taken: a view for each effect that
holds its condition, one more object of the schema for each queue;
and a claiming statement of the queue's own, as a job's `next` is,
which has each author write the lease and the back-off again.

### The list of work

A queue is declared beside its functions, and a list says what a
worker runs and in what order.

```kotlin
// charge.kg
effect chargeCard on stripe_charge_job {
    enter    bindTenantOf
    attempts 1
    prepare  prepareCharge
    act      charge
    record   recordCharge
}

// notices.kg
job notifyBooker {
    next  nextBookerNotice      // claims one owed row, or none
    run   sendBookerNotice
}

// daemon.kg
work daemon {
    urgent      saveCard, chargeCard
    background  sendRunSheet, frequently, platformDaily, tenantDaily, runBatchJob,
                notifyBooker, remindBooker, notifyDriver, remindDriver, warnAdmin,
                chargeAndSendInvoice, resendInvoice, sendPaymentInvite, sendMail
}
```

The checker holds that:

- what `prepare` gives is what `act` takes, and what `act` answers is
  what `record` takes;
- `act` takes the environment, the key and the prepared value, and no
  transaction;
- the effect's table has the runner's columns, by Postgres's answer
  (the Kotlin's `EffectFields`, written by hand for each table);
- `attempts` is stated;
- no step of a job acts.

The compiler prints each queue with its kind, table, attempts and
lane into a file that is checked in, as it prints the gates, and
under each the tables it reads, writes and locks (`doc/gates.md`,
"The table is printed"):

```
chargeCard     effect   stripe_charge_job   attempts 1   urgent
               reads    booker, invoice, stripe_card
               writes   invoice, mail_job, payment
               locks    invoice
notifyBooker   job      background
               reads    booker, booking
               writes   booking, mail_job
               locks    booking
```

- **A queue has one set of lines**, over every function it names
  that holds a transaction: `enter`, `prepare` and `record`, or `next`
  and `run`. Which step touches a table is in the code. `act` holds no
  transaction and adds nothing.
- **An effect's table is on its first line and not below.** The
  runner claims and marks its rows in Go, which the SQL pass never
  sees, so the lines are what the queue's own functions touch. A job
  claims its row in `next`, which is Kogo, so its table is in the
  lines.
- **A job's `locks` may nearly repeat its `reads`**: cusoon2's daemon
  locks every row it reads, `for share` unless told `for update`.

Weighed and not taken: lines for each step, up to nine a queue, where
a queue has no part shared as a gate is; no tables for queues, which
leaves the unattended writes out of the file; and the printer adding
an effect's table to the lines, which has the file state what
Postgres did not report.

The runner is hand-written Go in this repo, beside the request's
handler: claiming, leases, backing off, parked rows, alerts, the lanes
and their fairness.

### When a row fails

Two things are the queue's own when a row fails for good: what to
write so that the customer's page says something true, and what to
tell ops. The Kotlin's runner marks the row and calls two functions
of the queue in that transaction, each optional: `failed` and
`recovery` for an effect, `failJob` and `recovery` for a job. Mail
has the words alone, saving a card the writes alone, the charge both.
The forms take the same two:

```kotlin
effect chargeCard on stripe_charge_job {
    enter     bindTenantOf
    attempts  1
    prepare   prepareCharge
    act       charge
    record    recordCharge
    failed    chargeFailed        // optional: the queue's own writes when a row fails
    recovery  chargeRecovery      // optional: the words for ops, or null for the runner's own
}

fun chargeFailed(tx: Tx, env: Env, row: StripeChargeJob, why: Failure) { ... }
fun chargeRecovery(tx: Tx, env: Env, row: StripeChargeJob, why: Failure): String? = ...
```

`Failure` is a sealed type beside `Act`, with the Kotlin's four
cases: refused, not made, attempts exhausted, and done but not
recorded. Both functions run in the transaction that fails the row,
after the runner has marked it, and their statements count in the
queue's printed lines. A refusal alerts nobody. Where a queue gives
no words the runner's are used: fix the cause, then run the statement
that resets the row. Weighed and not taken: one function that writes
and gives the words, which leaves the Kotlin's shape and makes words
for a refusal that are thrown away; and `record` taking every
outcome, which loses the fit of `act` to `record`.

The safety does not rest on the list: no action in a transaction, and
a key only from the runner, hold however queues reach the runner. The
list adds the fit of the three steps, the table's check and the one
screen. It is built after the request side.

### Discover, then enter

cusoon2's daemon binds each active tenant in turn and asks each queue
for work, because under row-level security an unbound transaction
sees no rows (`doc/plans/tenant-job-tables.md` there, agreed 3 Sep
2026). It costs about 30 queries for an idle pass at two tenants, and
grows as queues times tenants. It protects nothing: the daemon may
bind any tenant already.

Kogo has no "for each". A queue finds its row unbound and may name a
function, `enter`, that the runner calls on the claimed row before
anything else; that cusoon2 binds a tenant there is cusoon2's. Its one
queue that already works this way, the tenant's daily job, finds its
tenant in a table with no row-level security and binds it.

The runner claims by its own rule ("Owed is the runner's rule"), and
unbound it sees no row to claim, nor whose a row is until it sees
one. So an effect may name where the runner looks: a view that holds
the runner's columns and what `enter` needs, and no more.

```kotlin
effect chargeCard on stripe_charge_job {
    find      stripe_charge_queue     // optional: the table itself when absent
    enter     bindTenantOf
    ...
}

data class Claim(val id: ID, val tenantId: ID)      // read from the view, by the rule SQL's columns have
fun bindTenantOf(tx: Tx, claim: Claim) { ... }
```

The runner claims from `find`, calls `enter` with what it read there,
and from then on works the table. The checker holds, by Postgres's
answer, that the view has the runner's columns and those `enter`'s
data class names. An application with no row-level security writes
neither. A job claims in its own `next`, and reads such a view there.

Run on Postgres 18.7 on 17 Nov 2026, as a role with no tenant bound,
over a table under row-level security by tenant and a view of its id,
its tenant and the runner's columns: the table shows no rows and the
view all of them; a row is claimed through the view with
`for update skip locked`; its other columns cannot be read until its
tenant is bound, and then they can, and it is marked claimed; another
tenant's rows stay unseen; and the view refuses a column it does not
hold. The view's owner there was the superuser.

For cusoon2 each such view runs with its owner's rights, against its
standing rule that every view is `security_invoker`, made so that no
view passes row-level security by accident. These would pass it on
purpose, for three columns. Its migrations force row-level security
on no owner, so the same is expected of its schema, and was not run
there. Greg agreed to `find` on 17 Nov subject to the review of the
whole design (`doc/remains.md`): he has never liked the loop over
tenants. The change alters how the daemon behaves, so it is made in
the Kotlin first, under its tests, and ported after.

**Fair by a column.** The loop gave a bound that its plan states and
a test pins (`DaemonPassTest`): a flood in one tenant's queue delays
every other tenant by at most one job in a pass. Claiming the oldest
row across tenants loses it, and 300 of one tenant's mails would
stand before another's confirmation. So an effect may name a column
of its view to be fair by:

```kotlin
effect sendMail on outbound_message {
    find      outbound_message_queue
    fair      tenant_id               // optional: a row for each value of this column in turn
    enter     bindTenantOf
    ...
}
```

The runner keeps, for each queue, the value it served last, and
takes the next owed row past it, starting again from the lowest when
there is none:

```sql
select id, tenant_id from outbound_message_queue
where <owed>
order by tenant_id <= $last,                      -- values past the last one first, then the rest
         tenant_id,
         not_after nulls last, next_attempt_at    -- within one value, the order every queue has
limit 1 for update skip locked
```

With `a1 a2 a3` owed to one tenant, `b1` to a second and `c1 c2` to
a third, it serves a1, b1, c1, a2, c2, a3. Kogo knows nothing of
tenants: the column is a name that Postgres checks against the view.
Run on 17 Nov as two statements, the second for the start again; the
one above was not run. A job claims in its own `next` and takes the
oldest across tenants, a change from the loop that is small, since a
job is work in the database and no slow call. Greg, 17 Nov: "Pretty
magical. But I accept, subject to overall review."

Weighed and not taken: no fairness, the oldest first for every
queue, which drops what the Kotlin has a test for where a flood is
felt, in mail and charges; fairness for jobs, by handing each `next`
the value served last, which all 13 statements would have to use
rightly; nothing in Kogo, with cusoon2 letting its
daemon see the job tables across tenants, which needs a second role
or a switch and shows every column; and a function of the
application's that claims the row, which puts the rule for what is
owed back into each application's SQL.

## Time limits

A timeout is a budget that something outside the call sets: whoever
waits, and whatever is held open meanwhile. cusoon2 has several, one
inside another, tuned by hand in `UpstreamTimeouts` and held together
by a comment: Stripe's 5 and 30 seconds sit inside the browser's 40;
Google's 45 inside the 60 after which Postgres ends a session idle in
a transaction. Each step's timer restarts, so a whole-call deadline
was added by running the call on a thread and abandoning it.

### One deadline at the outer edge

Whoever waits sets the limit once, and each call inside has what is
left of it. An inner budget is never longer than its outer one, since
it is the same clock. Kogo code writes no timeout and sees no timer:
the handle carries the deadline (spec 13), and Go ends a call cleanly
when it passes.

| Outer edge | Its limit | Set by |
| --- | --- | --- |
| a request | how long its caller waits | one option of the handler, in `main` |
| a transaction | Postgres's limit on a session idle in one | read from the database at start |
| an effect's `act` | the runner's lease on the row | the runner, for each queue |

- A lookup in a transaction has the less of the request's time left
  and the database's limit, with a margin, so it cannot outlast its
  session.
- An action in an outside step has the request's time left.
- An action in `act` has the lease, with a margin, so a row is never
  taken for abandoned while its call is still out: the Kotlin only
  warns of this.

Nothing is set for Stripe, Google or mail. What is left as a number
is how long a request may take and each queue's lease.

### The rare case takes the common path

A timeout almost never happens, so it has no path of its own.

- A lookup that runs out of time is a failed lookup: a `Left` like any
  other, which its caller must handle.
- An action can run out of time after it has left, so its failure has
  three cases, `NotSent`, `Refused` and `Unknown`, and a `when` over
  them covers all three or does not compile. The binding knows the
  case by how far the call got.
- A request whose own time runs out is answered 503, as one whose
  reruns run out is today.

### An effect someone is watching

A payer who presses "pay" inserts a row and polls for 40 seconds; the
daemon is not in their request and has none of its time. Today
Stripe's 35 seconds starts when the row is claimed, so a claim at 30
runs to 65 and may charge a payer who was told it timed out.

The row says when its answer is needed: the request stamps it with
the database's time and the handler's limit. `act` has the less of
the lease and what is left until then, and a row claimed after it is
never acted on, as cusoon2's outbox never sends a reminder after its
pickup. In no case weighed is a card charged twice; what differs is
whether the payer is told the truth.

Weighed beside it: nothing special, which tells the payer the wrong
thing in just the rare case; and the request running the effect
itself in an outside step, with the daemon as the net for a process
that dies, which ends the polling and needs no stamp. The last is the
direction, and a change of behaviour for after the port.

## Hand-written Go

It is part of Kogo, as `doc/design.md` has the prelude and the
handler. This adds the runner (about 500 lines in the Kotlin), the
HTTP binding (150 to 250 lines by the survey's guess), mail and PDF.
The prelude and `rt` are about 2,150 lines today, with 1,170 of
tests.

What goes in Go is what another application of this shape would need
unchanged: leases, backing off, HTTP, reading JSON. What knows what a
booking is goes in Kogo. To write the runner in Kogo the language
would need to catch a panic, loop for ever, keep a timer and build SQL
over a table's name, each of which it leaves out on purpose. This is
code Greg does not read and the base everything trusts: it has the
heaviest tests.

## What it would change in the spec

- **13:** an outbound binding says `lookup` or `action`. Application
  code holds no `external`, as now.
- **12, the prelude:** `kogo.http`, and `Act`, `EffectKey` and
  `SendFailure` with the runner's package.
- **A new section:** `job`, `effect` and `work`.
- `doc/design.md`: "Not designed, on purpose" loses workers, and the
  timeouts of outbound calls.

## Open

- Errors a service puts in its body: Stripe's `card_error`, legacy
  Google's `status`. Each client reads its own.
- Reading Postgres's idle limit at start, as the role is set there.
- How a test makes a call time out, so that the rare path runs every
  day: it waits on the test form.
- Whether `job`, `effect` and `work` are Kogo's own forms or calls
  that are valid Kotlin (DEV-894).
- A Go library for PDF.
