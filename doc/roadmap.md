# Roadmap: cusoon2 onto Kogo, then operator money

Agreed with Greg on 19 Nov 2026, from three research runs: DEV-928 (what
Kogo lacks, ops site first), DEV-929 and DEV-938 (the test form) and
DEV-930 (moving piece by piece, restated on 26 Nov 2026 for dev and
CI only, DEV-1252). Every size is an estimate in code /
test lines, from counting the Kotlin a piece replaces or from the
designs' own guesses; nothing was built to measure it. The ops
site's table marks each piece built, with its PR, or not; Linear holds
the state of what is not. This file holds the order and the reasons,
and is rewritten when either moves. Last rewritten 27 Nov 2026
(DEV-1299), with `kogo.math` and `!in` built, the booker's slices 1
to 6 merged, and Greg's rule of that day on changing emitted Go.

## The goal

All of cusoon2's backend in Kogo, served from Go, the Kotlin and the
JVM gone; then the operator money plan (cusoon2's
`doc/plans/operator-money-build.md`, PRs 4 to 15) written in Kogo.
Cloud Limo launches when it is ready: no date is promised, and the
milestones' dates are hygiene, not deadlines (Greg, 19 Nov 2026).

## How it moves

- **Two steps per area** (DEV-893, 18 Nov 2026): GraphQL to Kotlin RPC
  in cusoon2, where behaviour may change under the Kotlin's tests;
  then Kotlin RPC to Kogo, where nothing may change and `kogo/run.sh`
  compares every answer and row.
- **Kogo is built by the first port** (DEV-914, 19 Nov 2026, "3a"):
  the ops site's 21 operations end to end (20 when decided; cusoon2
  PR 1523 split `driverJobs` in two), building only what they
  need, then the next area. Not taken: every designed piece first;
  the runner first.
- **Behaviour changes are made in the Kotlin first**, under its tests,
  and the port then matches them exactly, including one a port finds
  it needs (`doc/porting.md`, "Mend the Kotlin first").
- **Where a port leaves the Kotlin** (Greg, 21 Nov 2026): the test is
  whether `kogo/run.sh` can see the change. Where it can (an answer, a
  row, what Postgres is asked or holds), the Kotlin goes first, as
  above: binding the person, the policies, the commands' role and the
  column grants are of this kind. Where it cannot (the form: the
  list's rows, a gate, `call.kg`, a step's signature, where a mark
  sits), the port is written from the design, cusoon2's
  `doc/plans/gates.md`, and the Kotlin is not reshaped first. So
  row-level security's slice 4 changes the statement a gate runs in
  the Kotlin first, and the marks in the port alone; the Kotlin's
  marks stay until it is deleted.
- **The port is kept in step with Kogo the day Kogo changes**, so that
  `kogo/run.sh` keeps comparing against `main`: the list in its new
  form (cusoon2 PR 1529), with a named check (PR 1533) that became the
  Kotlin's whole Origin and CSRF check (DEV-974, PR 1535);
  its operations on `SerializableTx` (PR 1534). The port's rule for a
  transaction's kind: a read with no lock, and whatever calls one,
  takes a `SerializableTx`, as the Kotlin runs it; a one-statement
  write and the list's check stay on a `Tx`.
- **Go that cusoon2 has emitted changes only when it must** (Greg,
  27 Nov 2026): a change to it that no slice needs waits for one
  batch after the port is code complete; one that only adds output
  for a new form, or mends an emit that is wrong, does not wait.
- **Piece by piece in dev and CI, one switch at launch** (DEV-930;
  Greg, 26 Nov 2026): Go serves each ported list beside the JVM, on
  one database, one session table and one config file, in dev, CI and
  agent runs only; Vite sends a moved list to `GO_URL` (DEV-1248).
  `renew` serves nobody until the whole port is done, staging is not
  stood up before then, and the Kotlin changes only as the port needs
  (Greg, 26 Nov 2026: "take the path of least resistance" on what
  guards both serving); then staging and production get the finished
  system once, with no JVM beside it. The session row, cookies and
  config file stay alike by what is there, with no freeze ("this
  looks fine", Greg): `kogo/pins` with `kogo/drift.sh` in CI refuses
  a Kotlin change the port has not followed, `kogo/run.sh` compares
  each cookie's fields and session row, and CI's browser job starts
  both servers on one config file. No per-queue lock is built: it
  needs a JVM daemon and a Go runner on one queue, which nothing
  outside a dev stack on mixed branches gives before launch.
  - *Fallen away*: a list moving in its own release, with nginx's
    route travelling with it and a rollback by deploy (cusoon2 PR
    1646, closed unmerged; DEV-1219 cancelled); a queue moved by a
    two-line commit on a box; the Go unit dark beside the JVM as a
    state of staging; both `check-config`s run on a box.
  - *Kept*: the comparison of every answer and row with the Kotlin;
    the Kotlin first for a change `kogo/run.sh` can see; the recorded
    answers before an area's Kotlin is deleted (DEV-894). With no
    weeks in production beside the JVM, these are the only guard
    against a wrong Go answer found at launch.
- **An area's Kotlin goes once its Go has proven itself** (19 Nov
  2026, a): measured where it can be, but mostly time in use, in dev,
  CI and agent runs, two weeks as a guideline that may be one or
  four. No area's Go serves production before its Kotlin goes:
  production gets the finished system. Its answers are recorded first
  (DEV-894), and its Kotlin, pins and dev's route to the JVM go in
  one PR.
- **cusoon2 commits its emitted Go** (19 Nov 2026, a), checked by CI
  re-emitting it as jOOQ's code is. Kogo is to be public under
  Apache-2.0, its history kept: it has its `LICENSE` (DEV-941, PR 78)
  and its module is `github.com/Cloud-Limo/kogo` (DEV-942, PR 86),
  which cusoon2 requires (cusoon2 PR 1527). Its runtime is an
  ordinary Go module pinned by commit, and the `kogo` tool is built
  from that same commit by the `tool` line of cusoon2's
  `kogo/go/go.mod`. Emitting in every build is the medium-term
  option once the builder has a Postgres.

## Settled for the ops site (19 Nov 2026)

- **What may happen inside a transaction is Kogo's rule** (Greg,
  DEV-968): an action is never made inside one; a lookup may be asked
  in one, the transaction waiting for it. An operation is one of the
  five kinds in `spec.md` 15.1's table (PR 94), which says where it
  may ask outside and where it may change it. Since cusoon2's DEV-991
  the driver's job pages read a place through
  `PlaceModel.compositePlace`, which asks Place Details when the cache
  is cold, and show the stored address (`place.address`) in three
  cases: a parked row, a render that meets not found, and a cold cache
  with Google unreachable or refusing (DEV-1046 to DEV-1049, 21 Nov
  2026). A place with no stored address is filled when read (cusoon2
  DEV-972, PR 1536). The booker's Google operations became
  actions between transactions (cusoon2 PR 1531); under the rule,
  three of them folded back into one transaction (cusoon2 DEV-975,
  PR 1537).
- **`driverJobs` becomes one call per operator** (c): an
  `operatorJobs` operation under `driverJob`'s gate. Not taken: a view
  across tenants; a proof holding a list of operators. The runner's
  `find` keeps its review, which looks back at this: made on 23 Nov
  2026, and the loop over tenants kept ("A worker keeps the loop over
  tenants", `decisions.md`).
- **The lead's calls, Greg not objecting:** `updateJob`'s hours rounded
  in SQL; the 21 operations move to Go together, in dev and CI, once
  all 21 compare clean;
  a PDF compared by its marks (text, positions to 0.01 pt); only Go
  writes log lines for now; Go's port has a key of its own; the
  printed table's command is named when it is built.

## The test form (DEV-940)

A test is a value, `test("sentence") { tx, env -> ... }`, named in a
`tests` list, run in a transaction always rolled back, checked with
Kotlin's assertions, imported from `kogo.test`. A statement Postgres refuses is seen through
`tx.refusal(sql"...")`, a value; panics stay bugs and are never caught.
Every prelude name is imported from a `kogo.` package (DEV-939, DEV-954).

## The work, in order

### 1. The ops site from Go (about 2,000 / 1,900 Kogo, 1,500 / 800 port)

Built is a merged PR, Kogo's unless it says cusoon2; "no" names the
issue where there is one.

| # | Repo | Piece | Size (estimate) | Built |
| --- | --- | --- | --- | --- |
| 1 | cusoon2 | Kotlin first: the first call makes the session; gates bind the storefront; Origin drops `null` | 60 / 60 | cusoon2 PR 1505, 1524 |
| 1b | cusoon2 | Kotlin first: job pages show the stored address; `operatorJobs` replaces `driverJobs` | ~80 / 80 | cusoon2 PR 1523 |
| 2 | Kogo | The lists in their agreed forms (`gatedBy`, `ungated`, `checkedBy`/`unchecked`) | 250 / 250 | PR 88; `after` out, PR 117 |
| 3 | Kogo | Serve a gated row: check, gate, work | 150 / 150 | PR 91 |
| 4 | Kogo | The Origin and CSRF check (`doc/origin.md`) | 120 / 150 | PR 92 |
| 5 | Kogo | `SerializableTx` beside `Tx` | 70 / 70 | PR 93, 102 |
| 6a | Kogo | The gate design: a gate's answer holds its transaction, the proof's rules (PR 98) out, two refusals (DEV-1040) | -60 / -20 | PR 119 |
| 6b | Kogo | A gate only reads (DEV-977) | (in PR 98's 170 / 150) | PR 96 |
| 7 | Kogo | `action fun` and the `action` binding (`lookup` out, PR 100) | 100 / 100 | PR 90 |
| 8 | Kogo | `Reply` and `Cookie` | 50 / 50 | PR 89 |
| 9 | Kogo | `kogo.http` (DEV-978) | 360 / 250 | PR 97 |
| 10 | Kogo | Compare and shift an `Instant` | 30 / 30 | PR 79 |
| 11 | Kogo | `main`'s prelude: `rpc.Call`, `_op`, settings | 150 / 150 | PR 101, 106 |
| 12 | Kogo | `ByteArray` and `kogo.mail` | 130 / 120 | PR 103 |
| 13a | cusoon2 | Ported as `ungated` rows: the business page's five, me and logout, four logins with a FusionAuth client, the four passkey operations | (in 13l's) | cusoon2 PR 1494, 1544, 1548, 1550 |
| 13b | cusoon2 | Kotlin first: bind the person (DEV-1025) | 15 SQL / 20 / 20, 15 port | cusoon2 PR 1564 |
| 13c | cusoon2 | Kotlin first: row-level security's slice 2, the policies on `supply_agreement`, `invite` and `debt_entry` | 112 SQL / 10 / 140 | cusoon2 PR 1567 |
| 13d | cusoon2 | Kotlin first: slice 3, `business` and `admin_grant`, `create_business`, the commands' role, `person`'s column grants | 60 SQL / 0 / 60, 10 port | cusoon2 PR 1569 |
| 13e | cusoon2 | The ops port's entry and the business page's five to the gate design (DEV-1030) | ~80 added, 75 removed, port | cusoon2 PR 1566 |
| 13f | cusoon2 | Slice 4: the gate asks the database; its statement Kotlin first, the marks in the port alone | -25 / 0, -10 port | cusoon2 PR 1571 |
| 13g | cusoon2 | The passkey rows as a gate that answers facts (`gates.md` section 7), in the ops port's entry | (in 13e's) | cusoon2 PR 1566 |
| 13h | cusoon2 | Kotlin first: a place with no stored address filled when read (DEV-972) | | cusoon2 PR 1536 |
| 13i | Kogo | `Json.strings`; `split`, `padStart`, `toIntOrNull`, `toLongOrNull`; a log line for every failed call | 45 / 65 | PR 120 |
| 13j | cusoon2 | A Google stand-in for the comparison and the port's tests | ~160 / 40 | cusoon2 PR 1572 |
| 13k | cusoon2 | The port of Place Details, the time zone and `compositePlace` | ~230 / 120 | cusoon2 PR 1576, 1580 |
| 13l | cusoon2 | The six job operations: `driverOperators`, `operatorJobs`, `driverJob`, `updateJob` with `settle`, `jobOffer`, `giveJob` | (13a's and 13l's: ~1,600 / 850) | cusoon2 PR 1597, 1600, 1603, 1604, 1605, 1608 |
| 18 | cusoon2 | Kotlin first: ten keys move into `settings`; `deployment.goPort` | ~60 / 50 | cusoon2 PR 1609 |
| 19 | cusoon2 | The ops site's Go `main` | 150 / 80 | cusoon2 PR 1611 |
| 20 | cusoon2 | The ops site on Go in dev, the browser tests and CI | | cusoon2 PR 1649, 1658; the Lima stacks' origin and `GO_URL`, a chore with no PR (DEV-1257) |

Row 20's earlier waits are over: the second stress of the Go `main`
(DEV-1139), the trial of generated worlds (DEV-1162), and the invite's
email queued by the port (DEV-885, cusoon2 PR 1612), so `sendInvite`
compares its `outbound_message` rows like any other. So are its later
ones, by 26 Nov 2026: DEV-1055's lines in the Go unit (DEV-1218,
cusoon2 PR 1638), the mends DEV-1208, DEV-1211 and DEV-1225, and the
lead's reading of the entries DEV-1198 marked wrong value or outage.
Its route on a box (DEV-1219) went with the ruling of that day, and
Greg answered that the row is now the ops site on Go in dev and CI
("yes", 26 Nov 2026): it is built.

Beside 13b to 13l, as tests in the Kotlin: the probes of each second id
and admin-only field, the storefront's (DEV-1031, cusoon2 PR 1563)
and then the ops site's (DEV-1038, 0 / 40).

Beside it, DEV-930's pieces (about 290 / 220 as it sized them): in
dev and CI, Go started beside the JVM and Vite's second target,
built with row 20, and the port key, with row 18; for the finished
system, the release carrying Go (DEV-1217, cusoon2 PR 1636), its
unit, health probe and migrations from Go's `migrate` (DEV-1218),
built; before the first queue moves, the concurrency test.

### 2. The rest of cusoon2

Mail's, the PDF's, Stripe's and Google's sizes were measured by four
explorations of cusoon2's Kotlin (DEV-1046 to DEV-1049, 21 Nov 2026),
and the booker's by slicing it (DEV-1146, 23 Nov 2026); the rest are
estimates.

| Area | Size |
| --- | --- |
| The `commands` list, then the ten `--op` commands | 350 / 180 |
| The work list and the runner | about 1,050 / 960 (DEV-1141's sketch) |
| Mail: templates, outbox, bounces | 1,345 / 415 in the port, 295 / 260 in the prelude |
| PDF: our own small writer (about 380 Go and font data) and the layouts | 1,610 / 640, the tests holding a comparison harness of about 200 |
| The daemon's 16 queues, Stripe's two among them (245 / 200) | 1,400 / 600 |
| Stripe's client through `kogo.http`, with the keyed call (`keyedAction`, `performOnce`, `EffectKey`) 45 / 90 in Kogo | 127 / 150 |
| Google's client (legacy endpoints) through `kogo.http`, with 70 / 80 in the prelude; 13k's 230 / 120 is built, in table 1 | about 370 / 180 left |
| The booker's 72 operations, all in Kotlin RPC since GraphQL left cusoon2 (DEV-893's first step, cusoon2 PR 1557), in 17 slices; its one paged list, `bookings`, seeks with a row comparison that passes `kogo check` as written (DEV-1146, "The paged list: facts") | about 3,930 / 2,860, with 225 / 295 in the prelude as DEV-1146 counted it, of which G2's 110 / 150 fell to Postgres's `pickup()` (DEV-1178) |
| Translating cusoon2's tests into the test form, area by area | |

#### The order (Greg, 24 Nov 2026)

Three rules, each against a kind of rework:

1. **One booker slice is open in cusoon2 at a time**, with at most one
   piece beside it that touches other files (the pricing, Google's
   client, Stripe's client). Every port PR shares `schema.kg`,
   `kogo/pins` and the comparison's Go, and one that falls behind
   `renew` costs a Fix round.
2. **A Kotlin-first change that alters the wire lands before the first
   slice that meets it.** A small one goes into its slice's own PR.
3. **Kogo's own work lands ahead of the slice that needs it.** The
   clerk merges it, so it runs beside cusoon2's.

In cusoon2, in this order:

1. DEV-1213: the port reads a booking's price as its two columns,
   which releases Kogo's DEV-1208.
2. Slice 1 (DEV-1214), with the unknown storefront's 403 Kotlin first
   in the same PR.
3. Slices 2, 3 and 4.
4. Slices 5 to 8, with the pricing (slice 10) beside them.
5. Kotlin first, one PR with the frontend: a time on the wire as its
   parts (a date, an hour, a minute), so the reader's own 400 covers
   one that does not read; `bookingStatus`'s 500; and the Kotlin's
   `bookingTime` onto `pickup()`, which ends the port's deviation
   from DEV-1178. It lands before slice 9, the first to read a time
   from a caller.
6. Slice 9, with Google's Directions and Autocomplete beside it.
7. Slices 11 to 14.
8. Stripe's client; the login design's sitting with Greg, at any time
   before this; then slices 15 to 17.
9. The commands, mail, the PDFs, then the daemon's queues, which wait
   on the runner.

The slices are DEV-1146's, with its later answers applied. Sizes are
Kogo code / tests; slice 1's code is measured, the rest are the
Kotlin's lines at 0.85. The slices' 3,820 / 2,860 and about 110 lines
of row types in `schema.kg` make the area's total.

| # | Slice | Operations | Size | Waits on |
| --- | --- | --- | --- | --- |
| 1 | Mount the storefront's list, with its `admin` gate and the drivers page | `drivers`, `upsertDriver` | 115 / 150 | Kotlin first in its PR: the unknown storefront and a missing header become the list's check's 403 |
| 2 | What a page asks first: the `anyone`, `maybe` and `customer` gates, and `booker` | `booker`, `logout`, `showTestModeBand`, `googleMapsBrowserKey`, `branding`, `stripeConfig`, `areas`, `defaultArea`, `airportAirlines`, `commonPlaces`, `placeNicknames`, `bookerByNewBookerSecret`, `dismissNamePrompt` | 260 / 150 | 1 |
| 3 | The places an admin and a customer keep | `place`, `journey`, `updateJourney`, `upsertPlaceNickname`, `deletePlaceNickname`, `upsertCommonPlace`, `deleteCommonPlace` | 200 / 130 | 2 |
| 4 | The `viewer` gate and the steps to a booking | `resendBookingEmail`, `pastPassengers` | 150 / 120 | 2 |
| 5 | The booking page's read | `booking` | 430 / 180 | 4 |
| 6 | The booking's one-field writes | `cancelBooking`, `uncancelBooking`, `setBookingsHidden`, `setBookingPromotion`, `setBookingNotes`, `setBookingDriver`, `setBookingBooker`, `bookingDriverUpdate` | 135 / 200 | 5 |
| 7 | The invoices | `invoice`, `invoiceByNumber`, `recordInvoicePayment`, `voidInvoice`, `resendInvoice`, `resendInvoiceStatus`, `createStripeCharge`, `stripeCharge`, `invoiceBookings` | 260 / 220 | 5 |
| 8 | The cards and the payment link | `stripeCards`, `stripeCard`, `createStripeCard`, `bookingPaymentLink`, `useBookingPaymentLink` | 110 / 90 | 4 |
| 9 | The lists | `bookings`, `bookingsSummary`, `bookers` | 200 / 160 | 5; the time on the wire as its parts |
| 10 | The pricing, as functions compared by generated cases | none | 330 / 150 | `kogo.math` |
| 11 | The wizard's quotes | `charges`, `discountedTotal`, `loyaltyProgramDiscount` | 295 / 200 | 5, 10; the time on the wire as its parts |
| 12 | The price and the payment details | `setBookingPrice`, under `admin` (cusoon2's DEV-1298), `setPaymentDetails` | 185 / 170 | 6, 10 |
| 13 | The journeys and the place search | `ensureJourney`, `bookingStatus`, `placeSuggest` | 280 / 220 | 3; the time on the wire as its parts; Google's Directions and Autocomplete |
| 14 | Submitting and editing a booking | `submitBooking`, `setBookingDetails` | 335 / 260 | 11, 13; `submitBooking` builds its own caller's `ViewerCall` in `call.kg` for `bookingAnswer`, which keeps its signature (DEV-1286's Ruling) |
| 15 | The customer's logins | `startEmailLogin`, `completeEmailLogin`, `startPasskeyLogin`, `completePasskeyLogin` | 225 / 200 | 2; the login design |
| 16 | The customer's passkeys | `hasPasskey`, `passkeys`, `startPasskeyRegistration`, `completePasskeyRegistration`, `deletePasskey` | 70 / 80 | 15 |
| 17 | The profile | `bookerProfile`, `createBooker`, `updateBooker` | 240 / 180 | 15; Stripe's client; Kotlin first in its PR: `bookerProfile` refuses a visitor, so its row is `gatedBy viewer` |

Slices 1 to 6 are merged as of 27 Nov 2026, the last by cusoon2 PR
1677, and slice 7 is open.

What the answers to DEV-1146 changed in it:

- **A pickup's wall time** (G2) is Postgres's `pickup()` (DEV-1178):
  `kogo.time` gains none of the fourteen names, and `LocalDateTime`
  stays out. The 14 operations that hold a row read the view; the 5
  whose time is not yet a row ask the function once each.
- **The two enums Kogo cannot name** (G4) are a cast and a literal in
  the port, a deviation by name in each slice's Decided; renaming them
  is the cleanup's, after the Kotlin is deleted.
- **`kogo.math` is Go's functions** (Q2): `sin`, `cos`, `asin` and
  their kind give Go's result, arithmetic gives Kotlin's bits, and
  the pricing's comparison compares the area chosen, never a distance
  at full width.
- **Five gates** (Q3), with six rows under `maybe`: `placeNicknames`,
  `hasPasskey`, `passkeys`, `pastPassengers`, `charges` and
  `loyaltyProgramDiscount`.

In Kogo, beside it:

- The prelude's names are built (DEV-1216, PR 189): spec 12's that it
  lacked (G6) and three of Kotlin's four approved (a `Set`'s `+` and
  `-`, `getOrNull`), the rest written the longer way inside the spec.
- `!in` is built (DEV-1283).
- `kogo.math` is built, with `roundToLong` (DEV-1299, PR NNN).
- The keyed call before Stripe's client, 45 / 90.
- The runner after "Guide DEV-1141": four of its sketch's twelve gaps
  are Greg's, so its design is a sitting, not a run.
- Mail's prelude and the PDF writer, before mail and the PDFs.

No slice puts Go in front of a user or deletes Kotlin, so none waits
on a research run. There is one switch-over, at launch, when staging
and then production get the finished system; it waits on research
runs, and names them when it is next.

### 3. Operator money

Re-planned once the migration is well advanced (Greg, 19 Nov 2026):
the ops site's admin pages (planned on GraphQL), where each money
guarantee sits under Kogo's transactions and runner, the daily run,
the RCTI's PDF. The accountant's and lawyer's questions
(`operator-money-and-identity.md`, "Open, before building") have the
longest lead time and can go out early.

## Open

- From the test form's build (DEV-940): what three refusals buried in
  application code and two expected panics become.
- `doc/remains.md`'s review of `find` and `fair`, with the runner.
  Settled on 23 Nov 2026 ("A worker keeps the loop over tenants").
- The login design's details, in a sitting with Greg before Stripe's
  client and slices 15 to 17.
