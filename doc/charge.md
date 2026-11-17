# cusoon2's charge, written in the agreed forms

DEV-900, 17 Nov 2026. The flow by which a payer on the hosted invoice
page pays by card, written in the forms of `doc/gates.md`,
`doc/work.md` and `doc/lists.md`, to find what does not fit. Read from
cusoon2 at `691aa93cf`: `createStripeCharge` and `stripeCharge`
(`fetchers.kt`), `ChargeQueue` (`StripeQueues.kt`), `runEffectQueue`
(`JobQueue.kt`). Nothing here is built or has been through `kogo
check`. cusoon2's tables and columns are named in this document alone.
Qn points into "What does not fit".

## The request

```kotlin
// booker.kg: does not opt in.
api booker {
    createStripeCharge  gatedBy createStripeChargeGate
    stripeCharge        gatedBy stripeChargeGate
}

data class ChargeInput(val invoiceId: ID, val stripeToken: String)
data class ChargeView(val id: ID, val isComplete: Boolean, val isFailed: Boolean, val failedMessage: String?)

fun createStripeCharge(env: Env, invoice: PayableInvoice, input: ChargeInput): Either<String, ChargeView> = either {
    chargeView(insertChargeJob(invoice, newId(), bookerEmail(invoice), input.stripeToken, CHARGE_WAIT))   // Q1
}
```

```kotlin
// gate.kg: opts in to UnscopedRead and BindsTenant.
data class PayableInvoice @UnscopedRead constructor(
    val id: ID, val owingCents: Long, val bookerId: ID, val tenant: AtTenant, @UnscopedRead val tx: SerializableTx)

fun createStripeChargeGate(tx: SerializableTx, env: Env, request: Request, input: ChargeInput): Either<String, PayableInvoice> = either {
    val tenant = atTenant(tx, request).bind()
    val row = invoice(tx, input.invoiceId) ?: raise(CHARGE_NOT_AVAILABLE_MESSAGE)       // the id is the bearer's secret
    ensure(row.status == InvoiceStatus.Issued) { CHARGE_NOT_AVAILABLE_MESSAGE }          // one message: no oracle for ids
    val held = PayableInvoice(row.id, owingCents(row), row.bookerId, tenant, tx)
    ensure(!hasOpenCharge(held)) { CHARGE_ALREADY_PENDING_MESSAGE }
    held
}
```

```kotlin
// model.kg: opts in, and holds the SQL.
data class ChargeJob(val id: ID, val stripeChargeId: String?, val failed: Boolean, val failedMessage: String?, val lapsed: Boolean)

fun hasOpenCharge(invoice: PayableInvoice): Boolean =          // no lock: relies on serializable
    invoice.tx.exists(sql"""
        select 1 from stripe_charge_job
        where invoice_id = ${invoice.id} and not failed and stripe_charge_id is null
          and (attempts > 0 or not_after is null or not_after > now())       -- Q3, Q5
    """)

fun bookerEmail(invoice: PayableInvoice): String =
    invoice.tx.querySingle<String>(sql"select email from booker where id = ${invoice.bookerId}")

fun insertChargeJob(invoice: PayableInvoice, id: ID, receiptEmail: String, token: String, wait: Duration): ChargeJob =
    invoice.tx.querySingle<ChargeJob>(sql"""
        insert into stripe_charge_job (id, invoice_id, amount_cents, receipt_email, stripe_source, not_after)
        values ($id, ${invoice.id}, ${invoice.owingCents}, $receiptEmail, $token, now() + $wait)
        returning id, stripe_charge_id, failed, failed_message, false as lapsed
    """)
```

- **The gate** is a bearer link's (`doc/gates.md`, "Gates that admit
  anyone"): it reads its input, so it is the operation's own. Its first
  parameter says the server opens SERIALIZABLE. It binds the request's
  tenant and reads `invoice`, `stripe_charge_job` and `tenant`; it
  writes and locks nothing, so the read-only savepoint passes it.
- **The open check is in the gate**: it refuses on the thing's state
  and only reads. The savepoint is inside the work's transaction, so
  the check and the insert are one SERIALIZABLE transaction and two
  payers racing cannot both insert, as the Kotlin's comment says. It
  carries the condition `doc/work.md` adds: a row never tried and past
  its time is not open.
- **The work** takes the environment, the proof and the input, and no
  `@Acts`: it hands the charge out by a row. Each statement binds
  `invoice.id` or `invoice.bookerId` of the proof, and each use of
  `invoice.tx` is before the dot. The insert is on the invoice's proof,
  not `AtTenant`, so it prints with no `(tenant-wide)`.
- **The insert gives a plain row, not a proof**: nothing writes after
  it, and a proof made from a proof is Q10.

## The effect

```kotlin
// charge.kg: the daemon's file; opts in to UnscopedRead and BindsTenant.
effect chargeCard on stripe_charge_job {
    find      stripe_charge_queue     // a view: id, tenant_id, not_after and the runner's columns
    enter     bindTenantOf
    soonest   not_after
    attempts  1
    prepare   prepareCharge
    act       chargeInvoice
    record    recordCharge
    failed    chargeFailed
    recovery  chargeRecovery
}

data class Claim(val id: ID, val tenantId: ID)
data class ChargeRow(val id: ID, val tenantId: ID, val invoiceId: ID, val receiptEmail: String,
                     val stripeSource: String, val stripeCustomerId: String?)
data class ChargeFields(val accountId: String, val jobId: ID, val amountCents: Long, val feeCents: Long,
                        val receiptEmail: String, val customerId: String?, val paymentMethod: String)
data class Charged(val paymentId: String, val amountCents: Long)

fun bindTenantOf(tx: Tx, env: Env, claim: Claim) { bindTenant(tx, claim.tenantId) }       // Q7

fun prepareCharge(tx: Tx, env: Env, row: ChargeRow): Either<String, ChargeFields> = either {
    val invoice = checkNotNull(sharedInvoice(tx, row.invoiceId)) { "charge ${row.id} has no invoice" }
    ensure(invoice.status == InvoiceStatus.Issued) { "Invoice ${invoice.number} is ${invoice.status}" }
    val owing = owingCents(invoice)
    ensure(owing > 0) { "Invoice ${invoice.number} has nothing owing" }
    val account = checkNotNull(sharedTenant(tx, row.tenantId)?.stripeAccountId) { "tenant has no Stripe account" }   // Q6
    ChargeFields(account, row.id, owing, feeWithin(invoice.platformFeeCents, owing),
                 row.receiptEmail, row.stripeCustomerId, row.stripeSource)
}

@Acts fun chargeInvoice(env: Env, key: EffectKey, fields: ChargeFields): Act<Charged> =    // Q8
    charge(env.stripe.withHeader("Stripe-Account", fields.accountId), key, fields).fold(     // stripe.kg's client
        { failure ->
            when (failure) {
                is SendFailure.NotSent -> Act.NotMade(failure.why)
                is SendFailure.Unknown -> Act.Unknown(failure.why)
                is SendFailure.Refused ->
                    if (failure.status == 409 || stripeError(failure.body) == "idempotency_error") Act.Unknown("Stripe may have charged")
                    else if (stripeError(failure.body) == "card_error") Act.Refused("Your card was declined")   // Q9
                    else Act.NotMade("Stripe did not process it: ${failure.status}")
            }
        },
        { intent ->
            if (intent.status == "succeeded") Act.Done(Charged(intent.id, fields.amountCents))
            else Act.Refused(NEEDS_VERIFICATION_MESSAGE)                                    // a 3DS card
        })

fun recordCharge(tx: Tx, env: Env, row: ChargeRow, result: Charged) {
    val invoice = checkNotNull(lockedInvoice(tx, row.invoiceId)) { "charge ${row.id} has no invoice" }
    check(invoice.status == InvoiceStatus.Issued) {                                         // Q6
        "Invoice ${invoice.number} was ${invoice.status} when its charge ${result.paymentId} came back" }
    setChargeId(tx, row.id, result.paymentId)
    val paid = recordPayment(WorkerInvoice(invoice.id, tx), invoice, result.amountCents, env.clock.now(), result.paymentId)
    enqueueMail(WorkerTenant(tx), newId(), env.clock.now(),
                paidEmail(sharedBooker(tx, paid.bookerId), paid), "paid ${paid.number}", null)
}

fun chargeFailed(tx: Tx, env: Env, row: ChargeRow, why: Failure) {
    when (why) {
        is Failure.Refused -> setChargeFailure(tx, row.id, ChargeFailure.Refused, why.reason)
        is Failure.NotMade -> setChargeFailure(tx, row.id, ChargeFailure.NotMade, NOT_MADE_MESSAGE)
        is Failure.Exhausted, is Failure.Unrecorded -> setChargeFailure(tx, row.id, ChargeFailure.Unknown, UNKNOWN_MESSAGE)
    }
    if (why is Failure.Refused && row.stripeCustomerId != null) {     // an invoice run's decline: nobody watched
        val invoice = checkNotNull(sharedInvoice(tx, row.invoiceId)) { "charge ${row.id} has no invoice" }
        if (invoice.status == InvoiceStatus.Issued && owingCents(invoice) > 0)
            enqueueMail(WorkerTenant(tx), newId(), env.clock.now(),
                        paymentFailedEmail(sharedBooker(tx, invoice.bookerId), invoice, why.reason), "declined ${invoice.number}", null)
    }
}

fun chargeRecovery(tx: Tx, env: Env, row: ChargeRow, why: Failure): String? =
    when (why) {
        is Failure.Exhausted, is Failure.Unrecorded ->                // the payment may exist
            stripeSearchSteps(sharedTenant(tx, row.tenantId)?.slug ?: "${row.tenantId}", row.id, row.invoiceId)   // Q11
        is Failure.Refused, is Failure.NotMade -> null
    }

// daemon.kg: the alert is doc/work.md's opsAlert, unchanged.
work daemon { alert opsAlert   queues chargeCard, ... }
```

- **Every step with a transaction takes a `Tx`**: the runner opens
  READ COMMITTED, and each read writes its lock. `sharedInvoice`,
  `sharedTenant` and `sharedBooker` say `for share`, where the Kotlin's
  flag added it; `lockedInvoice` says `for update`, as `record` asks.
  A rerun on a deadlock is safe: no step in a transaction acts.
- **The daemon's own functions take a `Tx` and a bare id**
  (`setChargeId(tx, row.id, ...)`), so the rule on statements never
  fires there. A write shared with a request takes an interface the
  daemon wraps its transaction for: `recordPayment` is also an admin's
  marking paid, `enqueueMail` every request's (`WorkerInvoice`,
  `WorkerTenant`: "A proof made both ways shares an interface").
  `doc/work.md` spells `enqueueMail` both ways; this is the one that
  fits `doc/gates.md`.
- **The row** is `ChargeRow`, which names the columns the steps read.
  That the runner reads it from the table by the rule SQL's columns
  have is said of `Claim` alone, and assumed here.
- **`prepare`** runs in the transaction that claimed the row. A `Left`
  is a refusal: no alert, and its words go to the payer's page through
  `chargeFailed`, as the Kotlin's `Refusal` does. A missing invoice is
  a bug and panics, which fails the row as not made.
- **`act` alone carries `@Acts`**; it takes the environment, the key
  and the prepared value, and no transaction. `charge` is
  `doc/work.md`'s client, which must also send `receipt_email` and
  `metadata[cusoon_charge_job]` and read `status`. That sketch differs
  from cusoon2 in three places, put right here: an intent that did not
  succeed is a refusal, a decline stores fixed words and never
  Stripe's, and the result carries the amount `prepare` chose.
- **`record`** locks the invoice, since an admin may be recording a
  payment by hand. The Kotlin changes the invoice's record in place
  and mails from it; here `recordPayment` gives the invoice as it now
  is, and the mail is built from that.
- **`attempts 1`**, so `Act.Unknown` fails the row at once as
  exhausted, with the alert. **`soonest not_after`** puts a waiting
  payer first; no `fair`, which would put another tenant's invoice run
  between two payers.
- **`failed` and `recovery`** run in the transaction that fails the
  row, after the runner marked it. A refusal alerts nobody.

## The poll

```kotlin
// gate.kg
data class HeldCharge @UnscopedRead constructor(val job: ChargeJob, val tenant: AtTenant, @UnscopedRead val tx: SerializableTx)

fun stripeChargeGate(tx: SerializableTx, env: Env, request: Request, input: ChargeIdInput): Either<String, HeldCharge> = either {
    val tenant = atTenant(tx, request).bind()
    HeldCharge(chargeJob(tx, input.id) ?: raise(CHARGE_NOT_AVAILABLE_MESSAGE), tenant, tx)
}

// model.kg
fun chargeJob(tx: SerializableTx, id: ID): ChargeJob? =
    tx.queryOne<ChargeJob>(sql"""
        select id, stripe_charge_id, failed, failed_message,
               coalesce(attempts = 0 and not failed and stripe_charge_id is null and not_after <= now(), false) as lapsed   -- Q3, Q5
        from stripe_charge_job where id = $id
    """)

// booker.kg
data class ChargeIdInput(val id: ID)

fun stripeCharge(env: Env, charge: HeldCharge, input: ChargeIdInput): Either<String, ChargeView> = either { chargeView(charge.job) }

fun chargeView(job: ChargeJob): ChargeView =
    if (job.lapsed) ChargeView(job.id, false, true, NOT_TRIED_MESSAGE)                    // Q4: never tried, nothing charged
    else ChargeView(job.id, job.stripeChargeId != null, job.failed, job.failedMessage)
```

```
{"type": "ok", "value": {"id": "0193a1f2-...", "isComplete": false, "isFailed": false, "failedMessage": null}}
{"type": "ok", "value": {"id": "0193a1f2-...", "isComplete": false, "isFailed": true, "failedMessage": "Payment could not be processed. Your card has not been charged. ..."}}
{"type": "refused", "message": "This payment is not available"}
```

- **The gate is the whole read**: the charge's id is the bearer's
  secret, the gate reads the row to prove it, and the work is a pure
  function of the proof's facts ("A proof carries facts"). It reads
  oddly and fits; the other bearer links will read the same way. Work
  that read the row again would be two reads of one row.
- **The keys are the property names** (spec 15.4), and the page's
  TypeScript is written from `ChargeView`. A missing or foreign id is a
  refusal with the gate's words, a 200, where GraphQL had an error.

## What the printed table would show

```
createStripeCharge  createStripeChargeGate
                    runs         serializable
                    gate reads   invoice, stripe_charge_job, tenant
                    reads        booker
                    writes       stripe_charge_job
stripeCharge        stripeChargeGate
                    runs         serializable
                    gate reads   stripe_charge_job, tenant

chargeCard     effect   stripe_charge_job   attempts 1
               runs     plain
               reads    booker, invoice, tenant
               writes   invoice, outbound_message, stripe_charge_job
               locks    booker, invoice, tenant
               acts     stripe.charge (keyed)
```

`stripe_charge_job` is under `writes` because `record` and `failed`
write the queue's own columns; the runner's marks are not listed. That
a request's row is stamped for a waiting payer shows nowhere.

## What does not fit

In the order to bring them, and all Greg's. Q2, Q5, Q6, Q7 and Q10
change a rule of the designs; the rest choose among forms that exist.

**Q1. Where does the work learn the limit, and whose limit is it?**
Gated work is handed the environment, the proof and the input. And the
payer waits out the page's poll, 40 seconds of many short requests,
not one request: the handler's limit is another fact, even where it is
the same number.
- a. A constant beside the work, as sketched: `val CHARGE_WAIT:
  Duration = Duration.ofSeconds(40)`. The page's 40 is a second copy,
  unless the answer carries `notAfter` and the page polls until then.
- b. The environment carries the handler's limit, set in `main` from
  the same value: `insertChargeJob(..., env.requestLimit)`.
- c. The server hands the work its deadline: `fun createStripeCharge(
  env: Env, invoice: PayableInvoice, input: ChargeInput, until:
  Instant)`, a new column in the table of roles.
- Lean: a. It is the application's fact about its own page.

**Q2. A row that was tried and is past its time is never finished.**
The runner dies in `act`. The Kotlin replays the row under its key
when the 2 minute lease ends. A watched row's 40 seconds have passed
by then, so it is not owed: never replayed, never failed, nobody
alerted, and it stays open, so every later payment of that invoice is
refused "already in progress" for ever. `doc/work.md` promises both
"a crash between `act` and `record` sends again under the same key"
and "past its `not_after` is never acted on".
- a. A replay is exempt: owed is `... and (claimed_at is not null or
  not_after > now)`. It may charge a payer whose page gave up two
  minutes before.
- b. The runner fails it when its lease ends, as `Failure.Exhausted`:
  `chargeFailed` stores the unknown words and ops are alerted with the
  Stripe search.
- c. Left as it is, and found by a query ops run: `where claimed_at <
  now() - interval '2 minutes' and not failed`.
- Lean: b. It is the one late row ops do need to hear of.

**Q3. Which clock judges `not_after`?** The request stamps with the
database's time (`doc/work.md`), the runner's claim takes its handed
clock and "never says `now()`", and the open check and the poll read
it too. A poll can say "never tried" by one clock while the runner
claims by another. Postgres's `now()` is also the transaction's start,
which a rerun moves.
- a. The database's everywhere, as sketched; the runner's claim says
  `not_after > now()`, and its tests move the row, not the clock.
- b. The handed clock everywhere: the stamp is `${env.clock.now() +
  wait}`, and the check and the poll bind `$now`.
- c. Mixed, as written, with a margin: `not_after + interval '2 s' <=
  now()` in the poll.
- Lean: b. One rule (spec 7), and a test moves one clock; it reopens
  if web and batch run on two machines. Under any of them a poll that
  reads just before a claim commits says "never tried" of a row that
  is then tried; `act` has almost none of its time left by then, and
  no card is charged twice.

**Q4. What the poll says of a row never tried.** The row is not done
and not failed, and the wire has only those two.
- a. As sketched: the answer says failed, in the not-made words, and
  the row is unchanged: `ChargeView(job.id, false, true,
  NOT_TRIED_MESSAGE)`. The page does not change.
- b. A state on the wire: `enum class ChargeState { Pending, Complete,
  Failed, NotTried }`, and the page gains a case.
- c. The poll's work closes the row: `update stripe_charge_job set
  failed = true ... where id = ${charge.job.id} and attempts = 0`. A
  read that writes; the lock does end Q3's race.
- Lean: a.

**Q5. The application's SQL restates the runner's rule, and
`attempts = 0` is not "never tried".** The rule is written in
`hasOpenCharge`, in `chargeJob` and in the runner's Go, over the
runner's columns (`failed` is cusoon2's `is_failed`). The Kotlin's
runner also sets `attempts` back to 0 when an act is done and when ops
reset a row, so the poll needs `stripe_charge_id is null` beside it,
and a watched row that ops reset reads as never tried and is never
owed again.
- a. As sketched: the spec names the runner's columns and what each
  means, and each statement spells the rule out.
- b. The runner keeps a column of its own that says it, set at the
  first claim and never cleared: `where tried_at is null and not_after
  <= now()`.
- c. cusoon2's schema holds it once: `create view charge_state as
  select id, (...) as lapsed`.
- Lean: b. The rule `doc/work.md` agreed rests on a column that means
  three things.

**Q6. `record` and `prepare` can fail a row only by a panic.** An
admin voids the invoice while its card is charged: the Kotlin's
`check` fails the row as unrecorded, telling the payer and ops. A
tenant with no Stripe account fails `prepare` as not made, with the
alert; a `Left` there is a refusal, which alerts nobody. Both are
expected, and spec 8 keeps a panic for bugs.
- a. As sketched: `check(...)` and `checkNotNull(...)` panic, and the
  runner makes them `Unrecorded` and `NotMade`, each with a trace.
- b. `record` gives `Either<String, Unit>`, a `Left` being
  `Unrecorded`: `ensure(invoice.status == InvoiceStatus.Issued) {
  "..." }`; and `prepare`'s `Left` says which it is, `Stop.Refused(
  words)` or `Stop.NotMade(why)`. Two rows of the table of roles.
- Lean: b for `record`; a for `prepare`, until a second queue asks.

**Q7. `enter` once for a row, or once for each of its transactions?**
A row has two or three: prepare's, record's, and a fresh one that
fails it after a panic. A bound tenant ends with its transaction, and
`doc/work.md` says only "before anything else".
- a. At the start of each, with the claim kept from the first:
  `enter, prepare`, then `enter, record`.
- b. Once, and each later step binds for itself: `bindTenant(tx,
  row.tenantId)` as the first line of `recordCharge`. But the runner
  reads the row before it calls the step, and unbound sees none.
- Lean: a; one sentence in `doc/work.md`.

**Q8. The key's text changes.** The Kotlin sends
`cusoon2-charge-<id>`; the runner makes its own from the table and the
id. A side-by-side run differs in that header, and a row in flight at
the change is replayed under a new key.
- a. The Kotlin takes the runner's text first:
  `stripe_charge_job-<id>`.
- b. The effect states a prefix: `key "cusoon2-charge-"`.
- Lean: a. Its runner is not yet live.

**Q9. A decline's detail has nowhere to go.** The Kotlin logs
Stripe's code and request id for support and stores fixed words. Kogo
code has no log.
- a. The binding logs every refused send, status and body, in Go.
- b. `Act.Refused(words, detail)`, and the runner stores `detail` in
  `last_error`.
- Lean: a.

**Q10. "Making a thing gives its proof" breaks "used only before the
dot".** A model function that makes the new row's proof must pass the
old proof's transaction to a constructor: `HeldCharge(job,
invoice.tenant, invoice.tx)` is `proof-tx-escapes`. Stepped round here
with a plain row; `submitBooking` cannot.
- a. A marked constructor is excepted from the rule.
- b. The new proof holds the old: `HeldCharge(job, invoice)`, with
  statements on `charge.invoice.tx`.
- Lean: a.

**Q11. What the alert can say.** The Kotlin's recovery words end "4.
Otherwise run" and the reset statement, and its alert opens with the
queue's `label(row)`, "hosted-page charge for invoice N". `recovery`
is not handed the statement, and `label` has no role.
- a. The words say "the reset statement below", `opsAlert` prints
  `alert.reset` under them, and the label is lost.
- b. `recovery` takes the statement: `chargeRecovery(tx, env, row,
  why, reset)`.
- c. A role `label` whose words `Alert` carries: `label chargeLabel`.
- Lean: a, until an alert proves hard to read.
