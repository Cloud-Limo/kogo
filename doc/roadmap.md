# Roadmap: cusoon2 onto Kogo, then operator money

Agreed with Greg on 19 Nov 2026, from three research runs: DEV-928 (what
Kogo lacks, ops site first), DEV-929 and DEV-938 (the test form) and
DEV-930 (moving piece by piece). Every size is an estimate in code /
test lines, from counting the Kotlin a piece replaces or from the
designs' own guesses; nothing was built to measure it. The ops
site's table marks each piece built, with its PR, or not; Linear holds
the state of what is not. This file holds the order and the reasons,
and is rewritten when either moves. Last rewritten 21 Nov 2026
(DEV-1041).

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
- **Piece by piece, never one switch** (DEV-930): Go serves each ported
  list beside the JVM, on one database, one session table and one
  config file. A list moves in one release; nginx's routes travel with
  the release, a whole list at a time. The session and config formats
  are frozen while both serve. Effect queues get a per-queue lock
  before the first one moves.
- **An area's Kotlin goes once its Go has proven itself** (19 Nov
  2026, a): measured where it can be, but mostly time in use, in dev,
  CI, agent runs and staging before launch, two weeks as a guideline
  that may be one or four; about four weeks in prod after launch.
  Its answers are recorded first (DEV-894), and its Kotlin, pins and
  JVM route go in one PR.
- **cusoon2 commits its emitted Go** (19 Nov 2026, a), checked by CI
  re-emitting it as jOOQ's code is. Kogo is to be public under
  Apache-2.0, its history kept: it has its `LICENSE` (DEV-941, PR 78)
  and its module is `github.com/Cloud-Limo/kogo` (DEV-942, PR 86),
  which cusoon2 requires (cusoon2 PR 1527). Its runtime is an
  ordinary Go module pinned by commit and the `kogo` tool a mise
  install. Emitting in every build is the medium-term option
  once the builder has a Postgres.

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
  in SQL; the 21 operations switch together once all 21 compare clean;
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
| 18 | cusoon2 | Kotlin first: ten keys move into `settings`; `deployment.goPort` | ~60 / 50 | no, DEV-1124 |
| 19 | cusoon2 | The ops site's Go `main` | 150 / 80 | no, DEV-1125 |
| 20 | cusoon2 | Serve the ops site from Go | | no |

`sendInvite` compares with its `outbound_message` rows forgiven as
owed until the port queues the invite's email (DEV-885), so "the 21
operations switch together once all 21 compare clean" waits on it: it
stands before row 20, not row 19.

Beside 13b to 13l, as tests in the Kotlin: the probes of each second id
and admin-only field, the storefront's (DEV-1031, cusoon2 PR 1563)
and then the ops site's (DEV-1038, 0 / 40).

Beside it, before the first list goes live (DEV-930, about 290 / 220):
the release carries Go; units, health and CI; routing and the port
key; migrations from Go's `migrate`; the batch lock and the
concurrency test.

### 2. The rest of cusoon2

Mail's, the PDF's, Stripe's and Google's sizes were measured by four
explorations of cusoon2's Kotlin (DEV-1046 to DEV-1049, 21 Nov 2026);
the rest are estimates.

| Area | Size |
| --- | --- |
| The `commands` list, then the ten `--op` commands | 350 / 180 |
| The work list and the runner | 750 / 600 |
| Mail: templates, outbox, bounces | 1,345 / 415 in the port, 295 / 260 in the prelude |
| PDF: our own small writer (about 380 Go and font data) and the layouts | 1,610 / 640, the tests holding a comparison harness of about 200 |
| The daemon's 16 queues, Stripe's two among them (245 / 200) | 1,400 / 600 |
| Stripe's client through `kogo.http`, with the keyed call (`keyedAction`, `performOnce`, `EffectKey`) 45 / 90 in Kogo | 127 / 150 |
| Google's client (legacy endpoints) through `kogo.http`, of which 13k's 230 / 120 is in table 1, with 70 / 80 in the prelude | 600 / 300 |
| A paged list (not designed) | |
| The booker's 70 operations: all in Kotlin RPC since GraphQL left cusoon2 (DEV-893's first step, cusoon2 PR 1557); its second step, the port, is sliced when this row is reached | 4,500 / 2,000 |
| Translating cusoon2's tests into the test form, area by area | |

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
- A paged list's design, before DEV-893's lists slice.
- The login design's details, when the session slice is scoped.
