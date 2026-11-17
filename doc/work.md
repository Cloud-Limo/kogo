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

An application has no bindings at all, which reverses a decision of
15 Nov. `decisions.md`, "An application has no bindings", has why,
and the four tests a binding passes to enter the prelude.

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

### Mail is a message Kogo builds and Go sends

cusoon2 sends no mail in a request or a job. It builds the whole
message, attachments included, as the bytes of an EML file, stores
them in a queued row (`outbound_message.eml`, `Outbox.kt`), and the
outbox effect sends those bytes over SMTP with no transaction open:
a replay sends the same message under the same Message-ID, and the
bounce handler reads the subject back from the row (`Bounces.kt`).
Kogo builds the message as data; Go writes the EML and speaks SMTP:

```kotlin
// kogo.mail
data class Address(val name: String, val address: String)
data class Attachment(val name: String, val mimeType: String, val data: ByteArray)
data class Mail(val messageId: String, val from: Address, val replyTo: Address?,
                val to: List<Address>, val cc: List<Address>, val bcc: List<Address>,
                val subject: String, val text: String, val html: String?, val attachments: List<Attachment>)

external fun eml(mail: Mail): ByteArray              // headers, parts, Message-ID
external fun subjectOf(eml: ByteArray): String?      // for the bounce handler

external class Smtp                                  // a handle, made in main with server, port and login
@Acts external fun Smtp.send(eml: ByteArray): Either<SendFailure, Unit>
```

```kotlin
fun enqueueMail(at: AtTenant, id: ID, now: Instant, mail: Mail, label: String, notAfter: Instant?) {
    at.tx.execute(sql"""
        insert into outbound_message (id, created, label, eml, not_after)
        values ($id, $now, $label, ${eml(mail)}, $notAfter)
    """)
}
```

- The outbox's `act` is `env.smtp.send(row.eml)`. `send` is an
  action, so what was a habit (`sendOpsEmail` "sends at once and any
  job may call it") is a rule: no mail is sent in a transaction.
- The application keeps each address once across To, Cc and Bcc, the
  templates and the HTML; the Message-ID is written where it is
  chosen (`"outbound-$id"`, as the Kotlin's). Go holds the MIME and
  the SMTP conversation, with a deadline on the connection, since Go's
  `net/smtp` takes none.
- Perhaps 80 lines of Go and 20 of prelude. Weighed and not taken:
  the message's parts in columns, built into an EML when sent, which
  changes cusoon2's schema and outbox for nothing a replay or the
  bounce handler lacks. Agreed with Greg on 17 Nov 2026.

### A PDF is data that Kogo builds

cusoon2 draws two PDFs, the invoice (attached to its email) and the
driver's run sheet, about 980 lines on PDFBox's mutable page stream,
over a small vocabulary (`pdf/markup.kt`): text in a built-in font, a
line, a box, a PNG, a link, and a text's width, from which the Kotlin
centres, right-justifies and wraps. Kogo has no mutable object, so a
page is a list of marks that Kogo builds and Go draws:

```kotlin
// kogo.pdf
enum class Font { Helvetica, HelveticaBold }                      // PDF's standard fonts, added as needed
sealed interface Mark
data class Text(val x: Double, val y: Double, val text: String, val font: Font, val size: Double) : Mark
data class Line(val x: Double, val y: Double, val toX: Double, val toY: Double) : Mark
data class Box(val x: Double, val y: Double, val width: Double, val height: Double) : Mark
data class Image(val x: Double, val y: Double, val width: Double, val height: Double, val png: ByteArray) : Mark
data class Link(val x: Double, val y: Double, val width: Double, val height: Double, val url: String) : Mark
data class Page(val marks: List<Mark>)

external fun textWidth(text: String, font: Font, size: Double): Double   // from the font's own metrics
external fun render(pages: List<Page>): ByteArray
```

```kotlin
// The application's layout, in Kogo.
fun rightJustify(x: Double, y: Double, text: String, font: Font, size: Double): Text =
    Text(x - textWidth(text, font, size), y, text, font, size)
```

- **Go holds** the fonts' metrics, the replacement of a character a
  font has no glyph for (the Kotlin's `cleanString`), and drawing.
  The layout is the application's; the Kotlin's faux pass, which
  measures a line item by drawing it unseen, is the same function
  with its marks ignored.
- **`bytea` is Kotlin's `ByteArray`, with no operations**: Kogo
  code names it, reads it from a row, passes it, stores it and hands
  it to a binding, and nothing else. No index, `set` or `size`, so the
  spec's "no arrays" has one exception that cannot change; `==` on it,
  or on a data class that holds one, is refused, since Kotlin's is
  identity; and it is in no operation's input or answer. cusoon2 only
  carries bytes (`tenant_logo.logo_bytes` into a drawing,
  `outbound_message.eml` stored and sent) and reads none, and Kotlin's
  name keeps a port's signatures as the Kotlin has them. Weighed and
  not taken: a type of Kogo's own, `Bytes`, which needs the same two
  refusals and is not Kotlin's.
- Perhaps 150 lines of Go, 30 of prelude and 20 for `ByteArray` in the
  SQL pass. Weighed and not taken: a drawing API on a stream, which
  needs a mutable object; and a binding that draws the whole invoice
  from its data, which moves the layout into Go. Agreed with Greg on
  17 Nov 2026.

### An endpoint is declared once

```kotlin
// kogo.http: the whole outbound binding, hand-written Go.
external class Http                         // a handle for one service
external class Lookup                       // asks, changes nothing
external class Action                       // changes something outside
external class KeyedAction                  // an action that needs a key
external class Field                        // one name and its value
external class Json                         // an answer

@RequiresOptIn
annotation class Acts                       // on a function that changes something outside

external fun lookup(method: String, path: String): Lookup
external fun lookup(method: String, before: String, id: String): Lookup
external fun lookup(method: String, before: String, id: String, after: String): Lookup
// action and keyedAction have the same three forms.

external fun field(name: String, value: String): Field      // and Long, Double, Boolean
external fun group(name: String, fields: List<Field>): Field
external fun field(name: String, values: List<String>): Field   // Stripe's expand[]=a&expand[]=b, or a JSON array

external lookup fun Http.ask(endpoint: Lookup, fields: List<Field>): Either<HttpError, Json>
@Acts external fun Http.perform(endpoint: Action, fields: List<Field>): Either<SendFailure, Json>
@Acts external fun Http.performOnce(key: EffectKey, endpoint: KeyedAction, fields: List<Field>): Either<SendFailure, Json>

external fun Http.withHeader(name: String, value: String): Http   // a new handle with one more header

external fun Json.string(key: String): String?
external fun Json.long(key: String): Long?
external fun Json.double(key: String): Double?
external fun Json.boolean(key: String): Boolean?
external fun Json.at(key: String): Json?                    // a nested object
external fun Json.list(key: String): List<Json>?            // an array of objects

external fun Json.raw(): String                             // the object as JSON text, untouched
external fun Json.Companion.parse(text: String): Json?      // JSON text as a Json (also a route's body)
external fun field(name: String, json: Json): Field          // a JSON object sent as it is
```

`raw`, `parse` and a field of JSON carry what Kogo hands on and never
reads: a passkey's options from the identity service to the browser,
and the browser's signed credential back (`AuthConfig.kt`). The answer
to the browser is a `String` that holds JSON, as it is today. Agreed
with Greg on 17 Nov 2026.

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
val NEW_CUSTOMER: KeyedAction = keyedAction("POST", "/v1/customers")      // a booker's first card, and the customer with it

fun customerEmail(id: String): Action = action("POST", "/v1/customers/", id)
fun attachCard(id: String): KeyedAction = keyedAction("POST", "/v1/customers/", id, "/sources")

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

@Acts fun charge(stripe: Http, key: EffectKey, fields: ChargeFields): Either<SendFailure, PaymentIntent> = either {
    val intent = stripe.performOnce(key, CHARGE, chargeFields(fields)).bind()
    PaymentIntent(
        intent.string("id") ?: raise(unreadable("id")),
        intent.string("status") ?: raise(unreadable("status")))
}

@Acts fun updateCustomerEmail(stripe: Http, id: String, email: String): Either<SendFailure, Unit> = either {
    stripe.perform(customerEmail(id), listOf(field("email", email))).bind()
}
```

- **A handle is one service**, made in the application's `main` with
  its address and credentials. Kogo code holds no secret.
- **A handle states its encoding**, made in `main` beside its address
  and key: a form body (Stripe) or JSON (the identity service). A
  `GET` sends its fields in the query string whatever the handle says,
  which is Google's every call. Kogo code writes fields and never says
  how they travel. Weighed and not taken: each endpoint stating it,
  one fact repeated on every endpoint of a service. Agreed with Greg
  on 17 Nov 2026.
- **A handle is narrowed by a header, in Kogo.** cusoon2 calls Stripe
  in each tenant's connected account (`Stripe-Account`) and the
  identity service in each of its tenants (`X-FusionAuth-TenantId`),
  with one key for all. Neither value is a secret, so the client adds
  it where the call is made:
  `stripe.withHeader("Stripe-Account", accountId)`. A header that
  `main` set, as `Authorization`, cannot be set again: the binding
  panics. Perhaps 10 lines of Go. Agreed with Greg on 17 Nov 2026.
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
  `Lookup`, `perform` only an `Action`, `performOnce` only a
  `KeyedAction`, so `stripe.ask(CHARGE, ...)` does not compile.
- **The call that changes something is `perform`**, since a weaker
  word would not carry it. It is not the method: an endpoint states
  its own, a `DELETE` is an action, and a lookup may be a `POST`, as
  a search is. `Smtp.send` keeps its name. Agreed with Greg on 17 Nov
  2026; first written `send`.
- **A function that acts says so.** `perform`, `performOnce` and
  `Smtp.send` carry the marker `Acts`, and so does every function
  that calls one: `charge` and `updateCustomerEmail` above. One that
  only calls `ask` carries nothing and may run in a transaction
  (`doc/gates.md`, "A function that acts says so").

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
  argument of `perform` (`performAt(endpoint, id, fields)`), which
  doubles the three kinds of endpoint and their calls; and a string
  template, which escapes nothing.
- **The walk that reads an operation's input**, loosened to read an
  answer into a data class.
- **A handle for each tenant, made in `main`**, which would read the
  tenants at start and miss one added later; and **headers as an
  argument of every `ask` and `perform`**, which grows every call
  for the few that need one.

### What Go would decide, Kogo states

A binding's behaviour is Kogo's: the spec states it, and where Go has
a default the spec does not state, the binding turns it off and a
test shows it. `doc/design.md` has the rule for values ("never
inherit Go's by accident"); this is the same rule for what reaches
the network.

The case that raised it, read in Go 1.27.4 (`net/http`,
`request.go` and `transport.go`): a request that carries an
`Idempotency-Key` header, which is Stripe's, is sent a second time by
Go's transport when a kept connection fails before the first byte of
an answer. No line of Kogo would have written that second send.

| Go's default | Kogo's rule |
| --- | --- |
| Sends a keyed request again when a kept connection fails | `performOnce` may be sent a second time; `perform` never is; `ask` may be |
| Follows up to 10 redirects, a redirected `POST` as a `GET` with no body | No redirect is followed: a 3xx is reported with its status, as a 4xx is |
| Takes a proxy from `HTTP_PROXY` and its kin in the environment | No proxy unless `main` states one: the environment is ambient state (spec 7) |
| Reads an answer of any length | An answer past a limit set in `main` is a failure: for an action, `Unknown` |
| Reads every JSON number as a `float64` | `Json.long` is exact, and `null` for a number that is not whole or does not fit |

**The second send is kept, and stated.** A `performOnce` may be sent
again, at once and under the same key, when a kept connection fails
before any answer, and never after a timeout. A key says the service
treats a repeat as the same call, and the runner already relies on
that after a crash.

- **It is none of the retries cusoon2 refused** (DEV-714 there,
  `effect-runner.md`): stripe-java's, which sent again after a read
  timeout, so a slow charge met its own first request; a delayed one
  under the same key, with a hand payment or a void in the gap; and a
  delayed one under a new key.
- **For a charge it changes two cases of three.** A first send that
  never arrived is today `Unknown`, an alert and a payer told it
  failed with nothing charged, and becomes a charge that is recorded.
  One that arrived and finished is today charged and not recorded,
  and Stripe gives the second its stored answer. One still running
  gets a 409, which the client already reads as `Unknown`.
- **`perform` has no key**, so nothing makes a repeat the same call,
  and the binding stops Go sending it again. **`ask` changes
  nothing**, so it may be.
- **Each second send is logged** with the request's id.
- **cusoon2 very probably does this today.** Its JDK 21 sends an
  unsuccessful `POST` on a kept connection again, once, unless
  `sun.net.http.retryPost` is off; it defaults to on, cusoon2 sets it
  nowhere in its repo, and stripe-java 31.4.1 uses the client that
  reads it. The JDK's exact conditions were not traced.

Each row has a test against a local server that misbehaves on
purpose: for the first, one that drops a kept connection once it has
read the request. Perhaps 25 lines of Go and 80 of tests. Weighed and
not taken: the second send turned off, which is the letter of DEV-714
and leaves a dead connection as an alert for a charge that was never
made. Not checked against this rule: the Go already written (the
handler and the prelude) and `kogo.mail`. Agreed with Greg on 17 Nov
2026.

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

Its transactions are at READ COMMITTED, and it locks what it reads.
So a worker's functions take a `Tx`, not a `SerializableTx`, and each
read writes its lock in its statement (`doc/gates.md`, "Two kinds of
transaction"). A queue whose steps took a `SerializableTx` would be
run that way.

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

@Acts fun charge(env: Env, key: EffectKey, fields: ChargeFields): Act<PaymentIntent> =
    charge(stripeFor(env.stripe, fields.accountId), key, fields).fold(
        { failure ->
            when (failure) {
                is SendFailure.NotSent -> Act.NotMade(failure.why)       // it never left
                is SendFailure.Unknown -> Act.Unknown(failure.why)       // a 5xx, or it left and no answer came
                is SendFailure.Refused ->                                // a 4xx: Stripe's own rules sort it
                    if (failure.status == 409 || stripeError(failure.body) == "idempotency_error") Act.Unknown("Stripe may have charged")
                    else if (stripeError(failure.body) == "card_error") Act.Refused(declineWords(failure.body))
                    else Act.NotMade("Stripe did not process it: ${failure.status}")
            }
        },
        { intent -> Act.Done(intent) })

fun stripeError(body: Json?): String? = body?.at("error")?.string("type")
```

The binding sorts a failed send by how far it got and by status
alone, for every service: never left is `NotSent`; a 3xx or a 4xx
is `Refused(status, body)`; a 5xx or no answer is `Unknown`, since
the service may have acted. What a refusal means is the client's: for
Stripe a 409 or an `idempotency_error` may have charged
(`StripeQueues.kt`, DEV-714 there), a `card_error` is a decline, and
any other 4xx was not processed. Agreed with Greg on 17 Nov 2026.

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
queue states its attempts, with no default, as the Kotlin has it. An
attempt is one call of `act`: the one second send of a keyed call on
a dead connection is the binding's, inside it ("What Go would decide,
Kogo states").

### A key comes only from the runner

A `KeyedAction` is sent with an `EffectKey`, which the runner makes
from the effect's table and the row's id and hands to `act`.

- A keyed call is reached only from an effect's `act`. A login or an
  outside step may `perform`; neither can charge a card. Three of
  Stripe's four calls are keyed in the Kotlin: the charge, and both of
  the card job's (`cusoon2-card-<id>` on making a customer and on
  attaching a card), so a replay makes no second customer.
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
- **The order is not the runner's**: the oldest `next_attempt_at`
  first, unless the queue says otherwise ("Soonest is each queue's
  stated choice" below).
- **A queue's own result stays**: `record` writes `stripe_charge_id`
  as before, and the runner no longer reads it.

In the Kotlin's runner (`runEffectQueue`) it is small: `done_at` is
one line beside `attempts = 0` where a done act is recorded, in the
one `update` that carries the queue's own columns; `not_after` is
only read, in the claim; three `owed` go, and the charge's `order`
goes by `not_after` where it went by the customer's id; mail's
`sent_at` becomes `done_at`. That runner is not yet live, so no
finished row needs its `done_at` filled. Made in the Kotlin first,
under its tests. Weighed and not taken: a view for each effect that
holds its condition, one more object of the schema for each queue;
and a claiming statement of the queue's own, as a job's `next` is,
which has each author write the lease and the back-off again.

### Soonest is each queue's stated choice

Whether a row is done is a fact, and the runner holds it. Which row
goes first is a choice, and the Kotlin's queues make two: mail and
the card job take the oldest `next_attempt_at`, and the charge puts a
payer on the hosted page before an invoice run (`StripeQueues.kt`,
`order`). So, as with retries, the queue says:

```kotlin
effect chargeCard on stripe_charge_job {
    soonest   not_after       // optional: rows with a time here go first, the soonest first
    attempts  1
    ...
}

effect sendMail on outbound_message {
    // no soonest: the oldest next_attempt_at first, as the Kotlin has it
    ...
}
```

- **With `soonest`** the runner orders by that column, rows with no
  value last, then by `next_attempt_at`. With none it orders by
  `next_attempt_at` alone.
- **A watched request stamps its row** ("An effect someone is
  watching" below), so a waiting payer's row goes before an invoice
  run's, as the charge's own order has it.
- **`not_after` means one thing for every queue**: the time after
  which the write is not made.
- **The checker holds**, by Postgres's answer, that the column is one
  of what the runner claims from, the `find` view where there is one
  and the table where not, and that it is a `timestamptz`.

Perhaps 10 lines of checker, 5 of runner and 20 of tests. Weighed and
not taken:

- **One order for every queue**, rows with a `not_after` first, as
  this section first had it. It was drawn from the charge alone. In
  mail a `not_after` marks a reminder, no use once its pickup has
  passed (`Outbox.kt`), so every reminder of a tenant would go before
  its other mail, a confirmation queued earlier included: a change to
  mail's order that nobody chose.
- **The name `first`**, which reads as a step the runner calls, as
  `enter` is, and does not say which end goes first; and **`order`**,
  which invites a fragment.

Agreed with Greg on 17 Nov 2026.

### The list of work

A queue is declared beside its functions, and a list says what a
worker runs.

```kotlin
// charge.kg
effect chargeCard on stripe_charge_job {
    enter    bindTenantOf
    soonest  not_after
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
    alert   opsAlert            // "An alert is the application's to send" below
    queues  saveCard, chargeCard, sendRunSheet, frequently, platformDaily, tenantDaily,
            runBatchJob, notifyBooker, remindBooker, notifyDriver, remindDriver, warnAdmin,
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
- a column named by `soonest` is a `timestamptz`, by Postgres's
  answer;
- no step of a job acts;
- every job states `failed`;
- every `work` list names an `alert`, a function marked `@Acts` that
  takes the environment and an `Alert`.

The compiler prints each queue with its kind, table and attempts
into a file that is checked in, as it prints the gates, and
under each the tables it reads, writes and locks (`doc/gates.md`,
"The table is printed"):

```
chargeCard     effect   stripe_charge_job   attempts 1
               runs     plain
               reads    booker, invoice, stripe_card
               writes   invoice, mail_job, payment
               locks    invoice
               acts     stripe.charge (keyed)
notifyBooker   job
               runs     plain
               reads    booker, booking
               writes   booking, mail_job
               locks    booking
               asks     google.placeDetails (in transaction)
```

- **A queue has one set of lines**, over every function it names
  that holds a transaction: `enter`, `prepare` and `record`, or `next`
  and `run`. Which step touches a table is in the code. `act` holds no
  transaction and adds no table: what it calls is on the `acts` line
  (`doc/gates.md`, "The table is printed").
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
handler: claiming, leases, backing off, parked rows, alerts, and a
loop for each queue.

### Each queue is served on its own

The Kotlin's daemon does one thing at a time, and its two lanes exist
to work round that: the urgent one, saving and charging a card, is
swept before and after each unit of the other (`daemon.kt`,
`DaemonPass`). It is a soft priority. A send to a slow SMTP server
still holds a payer's charge, and the Kotlin says what would end it:
"give the urgent lane its own thread." Here a late charge is not made
at all ("An effect someone is watching"), so the wait would cost
more.

So the runner gives every queue a loop of its own: claim one row, do
it, and again, waiting a little when there is none. No queue waits
for another, and the `queues` of the `work` list above are a list
whose order means nothing. There are no lanes, no sweep and no pass.

- **What the Kotlin needs of one thing at a time still holds**, since
  both of its notes are about one queue. The lease "is safe only
  because one daemon thread claims, sends and records one row at a
  time" (`JobQueue.kt`), or a second worker takes a slow send's row
  for abandoned: a queue still has one worker, and `act` has the
  lease as its deadline besides. And "two concurrent jobs for one
  card can still both insert" (`StripeQueues.kt`): both are the card
  queue's, served one after the other.
- **`soonest` and `fair` stay.** They order the rows of one queue,
  which is still served a row at a time.
- **A deadlock between two queues can now happen**, and the runner
  runs the transaction again (`doc/gates.md`, "Two kinds of
  transaction").
- **Each busy queue holds a connection**: up to 16 for cusoon2, where
  the daemon holds one.
- **Idle queues ask no more than the pass did.** The Kotlin waits at
  most 0.2 seconds when there is no work; 16 queues asking at that
  rate is fewer statements than its pass makes.
- **A test steps one queue at a time**, through an entry of the
  runner that serves one row of a named queue, so that a run can be
  repeated and compared with the Kotlin's.

**It is made in Go alone, and the Kotlin's daemon stays as it is.**
What goes into the Kotlin first is what the side-by-side run would
see: the schema, a statement, a result. This changes when a row is
served and nothing of what serving it does, and the runner is new Go
and no port. So the two differ in timing, a difference Greg accepted
on 17 Nov 2026. What the Kotlin would have shown is that no two
queues rely, unsaid, on not running at once: the card job and a
charge for one booker, say. That was not established. The daemon's
functions lock what they read, since requests run beside them, which
suggests they are written for company and proves nothing. So before
the runner replaces the daemon, one test runs every queue side by
side over a seeded database and one at a time over a copy of it, and
the rows must be the same.

Weighed and not taken: the two lanes as Kogo's words, which fixes
their number and names them for cusoon2's daemon; lanes as rows of
the application's own naming, in order of priority, with the sweep
stated in the spec, which is three rules for what one sentence now
says; and a pool of workers over all the queues, which needs a
priority again once the pool is full. Agreed with Greg on 17 Nov
2026.

### When a row fails

Two things are the queue's own when a row fails for good: what to
write so that the customer's page says something true, and what to
tell ops. The Kotlin's runner marks the row and calls two functions
of the queue in that transaction: `failed` and `recovery` for an
effect, each optional, and `failJob`, required, and `recovery` for a
job. Mail
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
no words the alert carries none, and the statement that resets the
row beside it ("An alert is the application's to send"). Weighed and
not taken: one function that writes
and gives the words, which leaves the Kotlin's shape and makes words
for a refusal that are thrown away; and `record` taking every
outcome, which loses the fit of `act` to `record`.

**A job states `failed`, and must.** A job's rows are the
application's, found by its own `next` (the booker notice's is a
status column of `booking`), so only the queue knows how to mark one
so that `next` stops finding it. Without that, a job that panics is
claimed again on every pass and alerts ops each time. The Kotlin
requires it for this reason (`failJob`, `JobQueue.kt`). After a
panic the runner rolls back, calls `failed` in a fresh transaction,
commits, and then alerts ops:

```kotlin
job notifyBooker {
    next    nextBookerNotice
    run     sendBookerNotice
    failed  bookerNoticeFailed      // required: marks the row so next no longer finds it
}

fun bookerNoticeFailed(tx: Tx, env: Env, job: BookingIdent, why: String) {
    tx.execute(sql"update booking set booker_notified = 'FAILED' where id = ${job.id}")
}
```

The checker holds that every job states it, taking the job's row
type and the reason; that it stops `next` finding the row is a
test's. An effect's `failed` stays optional, since the runner marks
its rows in its own columns; `recovery` stays optional for both.
Perhaps 10 lines of checker. Agreed with Greg on 17 Nov 2026.

The safety does not rest on the list: no action in a transaction, and
a key only from the runner, hold however queues reach the runner. The
list adds the fit of the three steps, the table's check and the one
screen. It is built after the request side.

### An alert is the application's to send

The Kotlin's runner tells ops when a row fails for good: an email
sent straight to SMTP, not through the outbox, to
`deployment.operators`, once the transaction that failed the row has
committed, "so the alert never holds a connection open on SMTP and
never rolls the failure back" (`JobQueue.kt`). It is best effort: a
send that fails is logged. Kogo's runner cannot know that an alert is
an email, who the operators are or how the application words it. So
the runner says when and hands over what it knows, and a function of
the application's says where it goes and how it reads:

```kotlin
// daemon.kg
work daemon {
    alert   opsAlert
    queues  saveCard, chargeCard, sendRunSheet, frequently, platformDaily, tenantDaily,
            runBatchJob, notifyBooker, remindBooker, notifyDriver, remindDriver, warnAdmin,
            chargeAndSendInvoice, resendInvoice, sendPaymentInvite, sendMail
}

// The runner's package. A sketch: the fields are to be settled.
data class Alert(val queue: String, val table: String?, val id: Uuid?,
                 val cause: String, val recovery: String?, val reset: String?)

// The application's.
@Acts fun opsAlert(env: Env, alert: Alert): Either<SendFailure, Unit> =
    env.smtp.send(eml(opsMail(env.operators, alert)))
```

- **The runner decides when**, as the Kotlin does: after the commit
  that failed the row, for a failure that is not a refusal, and after
  a job's `failed` has committed.
- **The function takes the environment and the alert, and no
  transaction**, so it may send at once, and it carries `@Acts`.
- **It is best effort.** Where it answers a failure or panics the
  runner logs that and carries on: the record of the failure never
  rests on it.
- **Every `work` list names one, with no default.** A worker with
  none would fail rows in silence; an application that wants silence
  writes a function that does nothing, where it is seen.
- **The runner hands over what it knows, not words.** `recovery` is
  the queue's own words where it gave any; `reset` is the statement
  that resets an effect's row, which the runner knows; the sentence
  round them is the application's.

It is what the Kotlin does, so nothing goes into the Kotlin first.
Perhaps 15 lines of checker, 15 of runner and 20 of tests. Weighed
and not taken: each queue's `failed` queuing the alert as mail, which
needs no form and outlasts an SMTP server that is down, but makes
"every failed row alerts ops" a habit of 16 queues, and writes a row
the Kotlin does not; and the runner sending the mail, which puts the
operators and the wording in Go. Agreed with Greg on 17 Nov 2026.

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
         next_attempt_at                          -- within one value, the queue's own order
limit 1 for update skip locked
```

With `a1 a2 a3` owed to one tenant, `b1` to a second and `c1 c2` to
a third, it serves a1, b1, c1, a2, c2, a3. Kogo knows nothing of
tenants: the column is a name that Postgres checks against the view.
Run on 17 Nov as two statements, the second for the start again; the
one above was not run. Greg, 17 Nov: "Pretty magical. But I accept,
subject to overall review."

A job claims in its own `next` and takes the oldest across tenants. A
job is work in the database, and its lookups are mostly answered from
cusoon2's cache: the notice jobs build their payload through Google's
place client (`daemon.kt`), a cached read for 28 days and a call of up
to its limit on a miss. So a flood in one tenant, an import of 300
bookings say, delays another's jobs by the flood's length,
milliseconds a row when the cache answers, where the loop delayed them
by one row. Kept on purpose, 17 Nov 2026: such a flood is rare and
slow only on misses, and if it ever bites, the lookup leaves the job
before fairness comes in.

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

A row left untried past its time must not block the payer's retry.
cusoon2 allows one open charge for each invoice (DEV-674,
`hasOpenStripeChargeJob`), and counts as open any row not failed and
with no charge id, which a late row stays for ever. So its check
gains one condition, made in the Kotlin when `not_after` lands there:
a row never tried (`attempts = 0`) and past its `not_after` is not
open. A row that was tried stays open, late or not, since it may have
reached Stripe. The runner leaves a late row as it is. Agreed with
Greg on 17 Nov 2026; weighed and not taken, the runner failing such a
row, an outcome of its own and an alert ops does not need.

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

- **13:** an outbound binding says whether it is a lookup or an
  action, the second with `@Acts`. Application code holds no
  `external`, as now.
- **12, the prelude:** `kogo.http`, and `Act`, `EffectKey` and
  `SendFailure` with the runner's package; `kogo.pdf`.
- **10.4 and 3:** `bytea` maps to `ByteArray`, which has no
  operations and no `==`; the one array.
- **12, the prelude:** `kogo.mail`.
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
- A Go library for PDF: the built-in fonts' widths, PNGs and links.
- Which package holds `Acts`: `kogo.mail` uses it as `kogo.http`
  does.
- A hash in the prelude. The run sheet's job keeps a SHA-1 of what it
  last sent, to see a change (`daemon.kt`, `sha1Hex`); Kogo has none.
