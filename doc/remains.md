# What remains after 17 Nov 2026

For whoever takes up the designs of `doc/gates.md` and `doc/work.md`.
Read those two first, then `doc/measured.md` for the counts they rest
on. All three record a discussion with Greg on 17 Nov 2026: agreed as
the design to work from, not built, not in `spec.md`, no example run.

## Not yet discussed

- **The name of the command that prints the table** of gates, queues
  and their tables (`doc/gates.md`, "The table is printed";
  `doc/work.md`, "The list of work").

## Not yet filed

- **The runner's columns are one Go list**, read by the runner's SQL
  and by the checker's check of each queue's table (`doc/work.md`,
  "The list of work"), where as designed they would be written twice.
  About 5 lines.
- **A command's flags are read by the walk that reads an operation's
  input** (`prelude/kogo/rpc/wire.go`), not by a reader of their own
  (`doc/gates.md`, "Commands").

## Agreed, and made in cusoon2's Kotlin first

- **cusoon2's routes.** Agreed: they stop making a session for any
  request that lacks one, and the page's first call makes it
  (`doc/gates.md`, "The first call makes the session"). Made in the
  Kotlin first, under its tests.
- **The ops site's tenant.** Agreed: the gates of sending an invite,
  accepting one and the suppliers page bind the storefront, and the
  model stops switching tenant (`doc/gates.md`, "The gate binds the
  tenant"). `driverJobs` stays open until the loops over tenants are
  replaced. Made in the Kotlin first.
- **cusoon2's runner.** Agreed: `done_at` and `not_after` among its
  columns, and no condition of a queue's own (`doc/work.md`, "Owed is
  the runner's rule"). The order stays the queue's: the charge's
  goes by a column the hosted page's request stamps, and mail's is
  unchanged ("Soonest is each queue's stated choice"). Made in the
  Kotlin first. A charge has no `not_after` and
  `hasOpenStripeChargeJob` is unchanged (18 Nov 2026, "simple";
  `doc/work.md`, "An effect someone is watching").
- **cusoon2's schema.** Agreed, subject to the review below: a view
  for each queue that the runner finds its owed rows in, run with its
  owner's rights (`doc/work.md`, "Discover, then enter"). And a
  column a queue is fair by, in place of the loop's one job for each
  tenant in a pass. Made in the Kotlin first, and not yet run against
  cusoon2's schema.

## For whoever reviews the whole design

Greg, 17 Nov, asked for one thing more: a deep-dive assessment of
what the lists are trying to do, a dispassionate look at how, and
other ways of doing the same, more explicit ones and ones with as
much magic expressed differently (`doc/lists.md`, "Asked for: a deep
dive"). Done: `doc/lists-assessment.md` (DEV-901), and Greg's four
choices on it, 17 Nov, are in `decisions.md` (DEV-902): the lists
stay, and routes are Go in `main`.

Greg, 17 Nov: the design has kept clear of magic, needs a little, and
any of it makes him nervous. Look hard at each place where something
happens that is not written where it is used. The test that came of
the day, for a name that is turned into text out of sight:

- **Defensible where Kogo owns both ends.** An operation's input and
  answer (spec 15.4) take their keys from property names, and the
  browser's types are written from the same properties, so no one
  ever writes a key and its spelling is a detail of the
  implementation. SQL's columns (10.3) are matched by name and
  Postgres refuses a mismatch when the program is compiled.
- **Not where the names are someone else's.** Stripe's field names
  are its interface: they are checked against its documents by eye,
  and renaming a property would change what is sent. So a request to
  an outside service writes each name as a literal string, as
  cusoon2's Kotlin does (`chargeParams`), and an answer is read by
  key (`doc/work.md`, "An endpoint is declared once"). Agreed 17 Nov,
  against the data classes and the path's `{id}` that section first
  had.

Agreed subject to this review:

- **The runner's view across tenants** (`doc/work.md`, "Discover,
  then enter"). Greg took `find` subject to this review: a check
  that a view which passes row-level security on purpose, for an id,
  a tenant and the runner's columns, is as narrow as it reads, and
  that replacing the loop over tenants loses nothing the loop gave.
- **Fair by a column** (the same section). Greg: "Pretty magical.
  But I accept, subject to overall review." The runner keeps the
  value it served last and takes the next owed row past it. Check
  that the one statement given does what the two that were run did,
  that a value kept in the runner's memory is enough when it
  restarts, and whether jobs, which are left oldest first, need it.

## To check before relying on it

- **Whether cusoon2's Kotlin sends a Stripe call twice today.** Its
  `effect-runner.md` says one is never retried. The JDK under it
  sends an unsuccessful `POST` on a kept connection again unless
  `sun.net.http.retryPost` is off, and cusoon2 does not set it
  (`doc/work.md`, "What Go would decide, Kogo states"). The JDK's
  exact conditions were not traced; that sentence of cusoon2's is
  Greg's to keep or change.
- **The Go already written against the rule on Go's defaults**: the
  handler, the prelude, and `kogo.mail` when it is built. And against
  the rule that it is handed its clock (`doc/work.md`, "How it is
  tested"): the handler reads the system clock for the limit on
  reruns.
- **What sits behind cusoon2's PDF extension** (`doc/work.md`, "An
  application's own extensions"): a small writer of its own, or a
  wrapped Go library. A survey named `fpdf`, now kept on Codeberg;
  that its widths cover the standard fonts was from memory. Also
  unchecked: whether PDFBox kerns when it measures a string, and the
  Kotlin's 32-bit floats against Kogo's `Double` in the layout.
- **Reading Postgres's limit on a session idle in a transaction** when
  the server starts, as cusoon2's role is set.
- **What the printed table and the gate's check miss.** A cascade, a
  trigger and `for no key update` were not in the runs of 17 Nov; the
  first two are expected to list nothing, the last to be marked as the
  other locks are.
- **The survey of the vendors' Go libraries** (`doc/work.md`, "Outside
  services") was a subagent's and was not checked again. Not
  established: when Google stopped enabling its legacy services for
  new projects, and the endpoints of its newer APIs.
- **Every size.** None of this is sized as a whole. Guesses given: the
  rule that a statement binds something of its proof, 30 to 50 lines
  of checker, and that a proof's transaction is used only before the
  dot, 20 to 30; the functions a gate reaches, for the read-only check,
  60 to 100; the HTTP binding, 150 to 250 lines of Go, before its
  fields, its paths' ids and its `Json`, perhaps 90 more; the runner,
  about the Kotlin's 500; the tables of the printed table, about 90
  lines beside that walk, 10 more for the queues' and 10 for marking
  a write made under `AtTenant`; `failed` and
  `recovery`, 25 of checker; `find`, 20; `fair`, 30; `soonest`, 15;
  `@Acts`, 40; the two kinds of
  transaction, 40 of checker and 30 of Go; the five places where
  `kogo.http` states what Go would decide, 25 of Go; the table's
  `asks` and `acts` lines, 25; the `alert` function, 15 of checker
  and 15 of runner; extensions, 50.

## Choices that sit above these

- **How SQL's parameters are spelt, and whether Kogo source is valid
  Kotlin** (DEV-894). It decides how `gatedBy`, `ungated`, `outside`,
  `commands`, `job`, `effect`, `work` and a binding's
  `lookup` are spelt, and whether `@Acts` stays a marker or becomes
  `action fun` (`doc/gates.md`, "A function that acts says so"). Each
  design here was written to hold either way. One constraint on it,
  agreed 17 Nov: the lists have at most two forms, a list of rows and
  a block of named parts (`doc/lists.md`).
- **Where Kogo ends and Go begins.** Settled 17 Nov 2026
  (`decisions.md`, "Where Kogo ends and Go begins"): Kogo for what
  touches the database, acts outside or decides who may do what; Go
  for pure code at the edge where it is clearly simpler, as an
  extension Kogo calls or in `main`, which calls Kogo; and an
  application's Go reaches the database and outside services only by
  calling an operation. The review of 17 Nov had leaned on Go more as
  it went (each service's handle states its encoding, the binding
  sorts a failed send by status, Go draws a PDF from Kogo's marks and
  holds the fonts' metrics), and "maybe that is not a bad thing".
  Also settled that day: an application has no bindings with an
  effect (`decisions.md`, "An application has no bindings with an
  effect"); the lanes are gone (`doc/work.md`, "Each queue is served
  on its own"); routes are Go (`doc/gates.md`, "Routes that are not
  operations").
- **The test form.** How a test makes a call time out, so that the
  rare path runs every day, waits on it; so does moving cusoon2's
  tests (DEV-893), and the record of Google's answers they replay
  (`GoogleMapsFileCache`, a test's files kept beside the code), which
  `kogo.http` would need a mode for.
- **The order of the migration** (DEV-893): GraphQL to Kotlin RPC and
  then to Kogo, or straight to Kogo; all at once or piece by piece.
  Not discussed on 17 Nov. The gate and its work are the shape a
  booker's operation would be written in either way, and adopting it
  is mostly work in the model: each of some 150 functions of `Model`
  says which proof it demands.
- **What is built first.** Agreed only this far: the request side
  before the list of work, since the rules that protect (no action in
  a transaction, a key only from the runner) do not need the list.

## What the spec and the design doc would say

Nothing has changed in the language, so `spec.md` is as it was, and
`decisions.md` gained entries of 17 Nov, among them that an
application has no bindings and where Kogo ends and Go begins
(DEV-902).
When any of this is built, the sections it changes
are listed at the end of `doc/gates.md` and of `doc/work.md`; the
largest are 15.2 ("Nothing runs before an operation", reversed for a
gated one), 7 (a proof beside `Tx` and `Db`) and 13 (an outbound
binding says `lookup` or `action`).
