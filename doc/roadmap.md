# Roadmap: cusoon2 onto Kogo, then operator money

Agreed with Greg on 19 Nov 2026, from three research runs: DEV-928 (what
Kogo lacks, ops site first), DEV-929 and DEV-938 (the test form) and
DEV-930 (moving piece by piece). Every size is an estimate in code /
test lines, from counting the Kotlin a piece replaces or from the
designs' own guesses; nothing was built to measure it. Linear holds
each piece's state; this file holds the order and the reasons, and is
rewritten when either moves.

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
  the ops site's 20 operations end to end, building only what they
  need, then the next area. Not taken: every designed piece first;
  the runner first.
- **Behaviour changes are made in the Kotlin first**, under its tests,
  and the port then matches them exactly.
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
  Apache-2.0, its history kept, once DEV-941 adds `LICENSE`; DEV-942
  renames its module so that cusoon2 can fetch it. Its runtime is
  then an ordinary Go module pinned by commit and the `kogo` tool a
  mise install. Emitting in every build is the medium-term option
  once the builder has a Postgres.

## Settled for the ops site (19 Nov 2026)

- **No Google inside a request.** The driver's job pages show the
  stored address (`place.address`, kept fresh by the daemon). Made in
  Kotlin first. The booker's ten Google operations are weighed against
  this when their slice comes (booking-page-rpc Q11).
- **`driverJobs` becomes one call per operator** (c): an
  `operatorJobs` operation under `driverJob`'s gate. Not taken: a view
  across tenants; a proof holding a list of operators. The runner's
  `find` keeps its review, which looks back at this.
- **The lead's calls, Greg not objecting:** `updateJob`'s hours rounded
  in SQL; the 20 operations switch together once all 20 compare clean;
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

| # | Repo | Piece | Size |
| --- | --- | --- | --- |
| 1 | cusoon2 | Kotlin first: the first call makes the session; gates bind the storefront; Origin drops `null` | 60 / 60 |
| 1b | cusoon2 | Kotlin first: job pages show the stored address; `operatorJobs` replaces `driverJobs` | ~80 / 80 |
| 2 | Kogo | The lists in their agreed forms (`gatedBy`, `ungated`, `after`, `checkedBy`/`unchecked`) | 250 / 250 |
| 3 | Kogo | Serve a gated row: check, gate, work | 150 / 150 |
| 4 | Kogo | The Origin and CSRF check (`doc/origin.md`) | 120 / 150 |
| 5 | Kogo | `SerializableTx` beside `Tx` | 70 / 70 |
| 6 | Kogo | A proof's rules | 170 / 150 |
| 7 | Kogo | `action fun`, `lookup` and `action` bindings | 100 / 100 |
| 8 | Kogo | `Reply` and `Cookie` | 50 / 50 |
| 9 | Kogo | `kogo.http` | 360 / 250 |
| 10 | Kogo | Compare and shift an `Instant` | 30 / 30 |
| 11 | Kogo | `main`'s prelude: `rpc.Call`, `_op`, settings | 150 / 150 |
| 12 | Kogo | `ByteArray` and `kogo.mail` | 130 / 120 |
| 13–18 | cusoon2 | Port the logins and logout (with a FusionAuth client), the passkeys, the five ported operations as gated rows, the jobs with `settle`, `jobOffer`/`giveJob`, `operatorJobs` | ~1,600 / 850 |
| 19 | cusoon2 | The ops site's Go `main`; settings move | 150 / 80 |
| 20 | cusoon2 | Serve the ops site from Go | |

Beside it, before the first list goes live (DEV-930, about 290 / 220):
the release carries Go; units, health and CI; routing and the port
key; migrations from Go's `migrate`; the batch lock and the
concurrency test.

### 2. The rest of cusoon2

| Area | Size |
| --- | --- |
| The `commands` list, then the ten `--op` commands | 350 / 180 |
| The work list and the runner (`find`'s review here) | 750 / 600 |
| Mail: templates, outbox, bounces | 1,300 / 400 |
| PDF: our own small writer (about 380 Go and font data) and the layouts | 1,700 / 300 |
| The daemon's 16 queues | 1,400 / 600 |
| Stripe and Google (legacy endpoints) through `kogo.http` | 850 / 400 |
| A paged list (not designed) | |
| The booker's 70 operations, in DEV-893's slices | 4,500 / 2,000 |
| Translating cusoon2's tests into the test form, area by area | |

### 3. Operator money

Re-planned once the migration is well advanced (Greg, 19 Nov 2026):
the ops site's admin pages (planned on GraphQL), where each money
guarantee sits under Kogo's transactions and runner, the daily run,
the RCTI's PDF. The accountant's and lawyer's questions
(`operator-money-and-identity.md`, "Open, before building") have the
longest lead time and can go out early.

## Open

- How prelude names are imported (DEV-939, in discussion).
- From the test form's build (DEV-940): the `tests` list's spelling,
  and what three refusals buried in application code and two expected
  panics become.
- `doc/remains.md`'s review of `find` and `fair`, with the runner.
- A paged list's design, before DEV-893's lists slice.
- The login design's details, when the session slice is scoped.
