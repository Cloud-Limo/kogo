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
    chargeView(insertChargeJob(invoice, newId(), bookerEmail(invoice), input.stripeToken, env.clock.now()))
}
```

```kotlin
// gate.kg: opts in to UnscopedRead and BindsTenant.
data class PayableInvoice @UnscopedRead constructor(
    val id: ID, val owingCents: Long, val bookerId: ID, val tenant: AtTenant, @property:UnscopedRead val tx: SerializableTx)

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
data class ChargeJob(val id: ID, val stripeChargeId: String?, val failed: Boolean, val failedMessage: String?)

fun hasOpenCharge(invoice: PayableInvoice): Boolean =          // no lock: relies on serializable
    invoice.tx.exists(sql"""
        select 1 from stripe_charge_job
        where invoice_id = ${invoice.id} and not failed and stripe_charge_id is null
    """)

fun bookerEmail(invoice: PayableInvoice): String =
    invoice.tx.querySingle<String>(sql"select email from booker where id = ${invoice.bookerId}")

fun insertChargeJob(invoice: PayableInvoice, id: ID, receiptEmail: String, token: String, now: Instant): ChargeJob =
    invoice.tx.querySingle<ChargeJob>(sql"""
        insert into stripe_charge_job (id, invoice_id, amount_cents, receipt_email, stripe_source, payer_waiting_since)
        values ($id, ${invoice.id}, ${invoice.owingCents}, $receiptEmail, $token, $now)
        returning id, stripe_charge_id, failed, failed_message
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
  payers racing cannot both insert, as the Kotlin's comment says. It is
  the Kotlin's `hasOpenStripeChargeJob` unchanged: a charge has no
  `not_after` ("simple", `decisions.md`).
- **The insert stamps the row for the order**: `payer_waiting_since`
  takes the handed clock's time, and an invoice run leaves it null, so
  the effect's `soonest` serves a waiting payer first. The column and
  its name are cusoon2's to choose; the name is the sketches'.
- **The work** takes the environment, the proof and the input, and no
  `action fun`: it hands the charge out by a row. Each statement binds
  `invoice.id` or `invoice.bookerId` of the proof, and each use of
  `invoice.tx` is before the dot. The insert is on the invoice's proof,
  not `AtTenant`, so it prints with no `(tenant-wide)`.
- **The insert gives a plain row, not a proof**: nothing writes after
  it. Q10 now lets it make a proof; a plain row is enough here.

## The effect

```kotlin
// charge.kg: the daemon's file; opts in to UnscopedRead and BindsTenant.
effect chargeCard on stripe_charge_job {
    find      stripe_charge_queue     // a view: id, tenant_id, payer_waiting_since and the runner's columns
    soonest   payer_waiting_since     // a waiting payer first, across tenants; no fair (Q12)
    attempts  1
    enter     bindTenantOf            // before each of the row's transactions (Q7)
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

// model.kg: the writes a request shares, each on an interface (doc/gates.md, "A proof made both ways")
interface OnInvoice { val id: ID; @UnscopedRead val tx: Tx }       // as doc/gates.md's OnBooking
data class WorkerInvoice @UnscopedRead constructor(override val id: ID, @property:UnscopedRead override val tx: Tx) : OnInvoice
fun recordPayment(on: OnInvoice, invoice: Invoice, amountCents: Long, at: Instant, reference: String?): Invoice = ...  // the invoice as it now is
fun enqueueMail(at: InTenant, id: ID, now: Instant, mail: Mail, label: String, notAfter: Instant?) = ...

fun bindTenantOf(tx: Tx, env: Env, claim: Claim) { bindTenant(tx, claim.tenantId) }

fun prepareCharge(tx: Tx, env: Env, row: ChargeRow): Either<String, ChargeFields> = either {
    val invoice = checkNotNull(sharedInvoice(tx, row.invoiceId)) { "charge ${row.id} has no invoice" }
    ensure(invoice.status == InvoiceStatus.Issued) { "Invoice ${invoice.number} is ${invoice.status}" }
    val owing = owingCents(invoice)
    ensure(owing > 0) { "Invoice ${invoice.number} has nothing owing" }
    val account = checkNotNull(sharedTenant(tx, row.tenantId)?.stripeAccountId) { "tenant has no Stripe account" }   // a setup fault: panics (Q6)
    ChargeFields(account, row.id, owing, feeWithin(invoice.platformFeeCents, owing),
                 row.receiptEmail, row.stripeCustomerId, row.stripeSource)
}

action fun chargeInvoice(env: Env, key: EffectKey, fields: ChargeFields): Act<Charged> =    // key: stripe_charge_job-<id> (Q8)
    stripe.charge(env.stripe.withHeader("Stripe-Account", fields.accountId), key, fields).fold(     // the client's package
        { failure ->
            when (failure) {
                is HttpFailure.NotSent -> Act.NotMade(failure.why)
                is HttpFailure.Unknown -> Act.Unknown(failure.why)
                is HttpFailure.Unreadable -> Act.Unknown("Stripe charged, and ${failure.why}")
                is HttpFailure.Refused ->
                    if (failure.status == 409 || stripeError(failure.body) == "idempotency_error") Act.Unknown("Stripe may have charged")
                    else if (stripeError(failure.body) == "card_error") Act.Refused("Your card was declined")   // the binding logs Stripe's body (Q9)
                    else Act.NotMade("Stripe did not process it: ${failure.status}")
            }
        },
        { intent ->
            if (intent.status == "succeeded") Act.Done(Charged(intent.id, fields.amountCents))
            else Act.Refused(NEEDS_VERIFICATION_MESSAGE)                                    // a 3DS card
        })

fun recordCharge(tx: Tx, env: Env, row: ChargeRow, result: Charged): Either<String, Unit> = either {
    val invoice = checkNotNull(lockedInvoice(tx, row.invoiceId)) { "charge ${row.id} has no invoice" }
    ensure(invoice.status == InvoiceStatus.Issued) {                                        // a Left: unrecorded (Q6)
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
            stripeSearchSteps(sharedTenant(tx, row.tenantId)?.slug ?: "${row.tenantId}", row.id, row.invoiceId)   // step 4: "the reset statement below" (Q11)
        is Failure.Refused, is Failure.NotMade -> null
    }

// daemon.kg: the alert is doc/work.md's opsAlert, unchanged.
work daemon alertedBy opsAlert {
    chargeCard
    ...
}
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
  marking paid, whose proof also implements `OnInvoice`, and
  `enqueueMail` every request's. So `enqueueMail` takes `InTenant`, not
  `AtTenant` as `doc/work.md` declares it: `doc/gates.md` refuses a
  `WorkerTenant` passed as an `AtTenant`. `doc/work.md`'s job sketch,
  `enqueueMail(tx, ...)`, cannot be called at all.
- **The row** is `ChargeRow`, which names the columns the steps read.
  The runner reads it from `stripe_charge_job` by id, locked, in each
  of the row's transactions after `enter`, by the rule SQL's columns
  have, checked by the SQL pass (Q13).
- **`prepare`** runs in the transaction that claimed the row. A `Left`
  is a refusal: no alert, and its words go to the payer's page through
  `chargeFailed`, as the Kotlin's `Refusal` does. A missing invoice or
  a tenant with no Stripe account is a broken invariant and panics,
  which fails the row as not made, with the alert (Q6).
- **`act` alone is an `action fun`**; it takes the environment, the key
  and the prepared value, and no transaction. `stripe.charge` is
  `doc/work.md`'s client, in its own package, so the act's name does
  not clash with it; the client must also send `receipt_email` and
  `metadata[cusoon_charge_job]` and read `status`. `doc/work.md`'s act
  differed from cusoon2 in two places, put right there in this PR: an
  intent that did not succeed (a 3DS card) is a refusal
  (`StripeQueues.kt:309-314`), and a decline stores fixed words, never
  Stripe's, on a public page (`:301-304`). A third is of shape alone:
  the result carries `prepare`'s amount, which is the intent's, since
  Stripe refuses a replay whose parameters changed (`:277-282`).
- **`record`** locks the invoice, since an admin may be recording a
  payment by hand. The Kotlin changes the invoice's record in place
  and mails from it; here `recordPayment` gives the invoice as it now
  is, and the mail is built from that. An invoice no longer issued is
  a `Left`, which fails the row as unrecorded, with the alert (Q6); a
  missing one is a bug and panics.
- **`attempts 1`**, so `Act.Unknown` fails the row at once as
  exhausted, with the alert. **`soonest payer_waiting_since`** puts a
  waiting payer first across all tenants, the longest waiting first,
  then every other row by `next_attempt_at`. It names no `fair`: one
  tenant's invoice run may delay another's, where the Kotlin's pass
  serves the queue a tenant at a time (Q12).
- **`failed` and `recovery`** run in the transaction that fails the
  row, after the runner marked it. A refusal alerts nobody. The words'
  step 4, which in the Kotlin carries the reset statement, says "run
  the reset statement below", and `opsAlert` prints `alert.reset`
  after them; for a charge not made `chargeRecovery` gives no words,
  so the alert names no invoice unless cusoon2 chooses words for that
  case too (Q11).

## The poll

```kotlin
// gate.kg
data class HeldCharge @UnscopedRead constructor(val job: ChargeJob, val tenant: AtTenant, @property:UnscopedRead val tx: SerializableTx)

fun stripeChargeGate(tx: SerializableTx, env: Env, request: Request, input: ChargeIdInput): Either<String, HeldCharge> = either {
    val tenant = atTenant(tx, request).bind()
    HeldCharge(chargeJob(tx, input.id) ?: raise(CHARGE_NOT_AVAILABLE_MESSAGE), tenant, tx)
}

// model.kg
fun chargeJob(tx: SerializableTx, id: ID): ChargeJob? =
    tx.queryOne<ChargeJob>(sql"""
        select id, stripe_charge_id, failed, failed_message
        from stripe_charge_job where id = $id
    """)

// booker.kg
data class ChargeIdInput(val id: ID)

fun stripeCharge(env: Env, charge: HeldCharge, input: ChargeIdInput): Either<String, ChargeView> = either { chargeView(charge.job) }

fun chargeView(job: ChargeJob): ChargeView =
    ChargeView(job.id, job.stripeChargeId != null, job.failed, job.failedMessage)
```

On the wire: open, failed as not made, and refused.

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
                    reads        booker, stripe_charge_job
                    writes       stripe_charge_job
stripeCharge        stripeChargeGate
                    runs         serializable
                    gate reads   stripe_charge_job, tenant

chargeCard     effect   stripe_charge_job   attempts 1
               runs     plain
               reads    booker, invoice, stripe_charge_job, tenant
               writes   invoice, outbound_message, stripe_charge_job
               locks    booker, invoice, tenant
               acts     stripe.charge (keyed)
```

`stripe_charge_job` is on both lines of each: `insert ... returning`
needs SELECT as well, and `setChargeId` and `setChargeFailure` are an
`update` with a `where`, which reads the table it writes
(`doc/gates.md`). The runner's own claims and marks are not listed. So
this reads `doc/work.md`'s "on its first line and not below" as "the
printer adds nothing of the runner's", where that document's example
leaves the queue's table out. That a request's row is stamped for a
waiting payer shows nowhere.

## What the Kotlin takes first

Two changes cusoon2 makes before the port, so that a side-by-side run
sees the same rows and the same requests. No issue is filed for them
here.

- **The order of the charge queue.** A column the hosted page's
  request stamps with the application's clock and an invoice run
  leaves null (`payer_waiting_since` here; the name is cusoon2's to
  choose), ordered by nulls last, then `next_attempt_at`, in place of
  `order by stripe_customer_id is null desc` (`StripeQueues.kt:245-249`).
  The view the runner claims from carries it.
- **The key's text**, `<table>-<id>`, for the charge
  (`stripe_charge_job-<id>`, `cusoon2-charge-<id>` today) and the
  card job (its table's name, `cusoon2-card-<id>` today), on both of
  the card job's calls. Safe only before the Kotlin's effect runner is
  live, or with the charge and card queues empty: a row that crashed
  under the old key and is replayed under the new within Stripe's 24
  hours is a second charge. To confirm before the change: what key
  production's present charge path sends (Q8).

## What does not fit

All thirteen are settled or closed, by Greg on 17 and 18 Nov 2026,
and recorded in `decisions.md`; the key's text under Q8 is the
lead's. Q1, Q2, Q4 and Q5 are closed by one answer that is none of
the thirteen: a charge has no `not_after` ("simple", `decisions.md`;
`doc/work.md`, "An effect someone is watching"). Every one of the four
came from stamping the charge with a time after which it is not made,
and that rule is withdrawn for the charge.

**Q1. Where does the work learn the limit, and whose limit is it?**
The request was to stamp the row with a limit, and gated work is
handed no deadline; the page's 40 seconds is a second fact.
**Closed by "simple":** the row is stamped with no limit.

**Q2. A row that was tried and is past its time is never finished.**
A crashed watched row would be neither replayed nor failed once its
time had passed, and would block every later payment of the invoice
with nobody told. **Closed by "simple":** with no time on the row, a
crashed row is replayed under its key after the lease, as the
Kotlin's is.

**Q3. Which clock judges `not_after`?** The request stamped with the
database's time, the runner's claim takes its handed clock and "never
says `now()`", and the open check and the poll read it too.
- a. The database's everywhere.
- b. The handed clock everywhere: the stamp binds a time from it, and
  every check binds `$now`.
- c. Mixed, with a margin.
- **Settled: b** (Greg, 17 Nov 2026), for mail alone: the handed
  clock stamps and judges `not_after`, never `now()`.

**Q4. What the poll says of a row never tried.** A late row was not
done and not failed, and the wire has only those two. **Closed by
"simple":** no row is late, and the poll's three fields are as they
are.

**Q5. The application's SQL restates the runner's rule, and
`attempts = 0` is not "never tried".** The open check, the poll and
the runner's Go each wrote "never tried and past its time" over the
runner's columns, and the Kotlin's reset zeroes `attempts`. **Closed
by "simple":** the open check is the Kotlin's, `not failed and
stripe_charge_id is null`, no statement of the application's reads
`attempts`, and the reset statement is as the Kotlin prints it.

**Q6. `record` and `prepare` can fail a row only by a panic.** An
admin voids the invoice while its card is charged: the Kotlin's
`check` fails the row as unrecorded, telling the payer and ops. A
tenant with no Stripe account fails `prepare` as not made, with the
alert; a `Left` there is a refusal, which alerts nobody. Both are
expected, and spec 8 keeps a panic for bugs.
- a. `check(...)` and `checkNotNull(...)` panic, and the runner makes
  them `Unrecorded` and `NotMade`, each with a trace.
- b. `record` gives `Either<String, Unit>`, a `Left` being
  `Unrecorded`; and `prepare`'s `Left` says which it is,
  `Stop.Refused(words)` or `Stop.NotMade(why)`.
- **Settled: b for `record`, a for `prepare`** (Greg, 18 Nov 2026):
  `record` gives `Either<String, Unit>`; a broken setup panics.

**Q7. `enter` once for a row, or once for each of its transactions?**
A row has two or three: prepare's, record's, and a fresh one that
fails it after a panic. A bound tenant ends with its transaction.
- a. At the start of each, with the claim kept from the first.
- b. Once, and each later step binds for itself; but the runner reads
  the row before it calls the step, and unbound sees none.
- **Settled: a** (Greg, 18 Nov 2026), as the Kotlin's `withRow` does.

**Q8. The key's text changes.** The Kotlin sends
`cusoon2-charge-<id>`; the runner makes its own from the table and the
id. A side-by-side run differs in that header, and a row in flight at
the change is replayed under a new key.
- a. The Kotlin takes the runner's text first.
- b. The effect states a prefix: `key "cusoon2-charge-"`.
- **Settled: a** (Greg, 18 Nov 2026), the text `<table>-<id>`, the
  lead's; see "What the Kotlin takes first".

**Q9. A decline's detail has nowhere to go.** The Kotlin logs
Stripe's code and request id for support and stores fixed words. Kogo
code has no log.
- a. The binding logs every refused send, status and body, in Go.
- b. `Act.Refused(words, detail)`, and the runner stores `detail` in
  `last_error`.
- **Settled: a** (Greg, 18 Nov 2026): `kogo.http` logs every refused send.

**Q10. "Making a thing gives its proof" breaks "used only before the
dot".** A model function that makes the new row's proof must pass the
old proof's transaction to a constructor: `HeldCharge(job,
invoice.tenant, invoice.tx)` was `proof-tx-escapes`.
- a. A marked constructor is excepted from the rule.
- b. The new proof holds the old: `HeldCharge(job, invoice)`, with
  statements on `charge.invoice.tx`.
- **Settled: a** (Greg, 18 Nov 2026): also allowed as a proof's
  marked `tx` in a constructor (`doc/gates.md`).

**Q11. What the alert can say.** The Kotlin's recovery words end "4.
Otherwise run" and the reset statement, and its alert opens with the
queue's `label(row)`, "hosted-page charge for invoice N". `recovery`
is not handed the statement, and `label` has no role.
- a. The words say "the reset statement below", `opsAlert` prints
  `alert.reset` under them, and the label is lost.
- b. `recovery` takes the statement as a fifth parameter.
- c. A role `label` whose words `Alert` carries.
- **Settled: a** (Greg, 18 Nov 2026): step 4 points at the reset
  statement printed below the words.

**Q12. Is the charge queue fair between tenants?** The Kotlin runs
this queue one tenant at a time (`runEffectQueue(tenantId, ...)`).
- a. `fair tenant_id` with `soonest` within a tenant. A waiting payer
  may wait behind one act for each busy tenant.
- b. No `fair`: one tenant's invoice run delays other tenants'
  invoice runs, but never a payer.
- c. Fair only among unwatched rows, a new rule.
- **Settled: b** (Greg, 18 Nov 2026), reversing item 9 of the Ruling
  on PR 66.

**Q13. How does the runner read the row a step takes?** `doc/work.md`
said `Claim` is "read from the view, by the rule SQL's columns have";
nothing said how `ChargeRow` is read, which `prepare`, `record`,
`failed` and `recovery` take.
- a. The same rule: the runner selects the type's properties by name
  from the effect's table, checked by the SQL pass.
- b. A step of the application's: `row chargeRow`.
- **Settled: a** (Greg, 18 Nov 2026): by id, locked, after `enter`.
