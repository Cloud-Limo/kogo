# Kogo decisions

Each entry: what was decided, why, and what would reopen it.

## Agreed with Greg

- **Target is Go** (15 Nov 2026). The language is garbage-collected; Rust and Zig are not, and
  Go has the Postgres driver, HTTP and TLS ready. Kotlin/Native is not a target: Kogo never
  involves the Kotlin compiler. Go's compile speed is untested here.
- **SQL is literal and static, as sqlx does it; no query builder** (15 Nov 2026). A statement
  is a constant, the only thing that varies is a bound parameter, and a choice between
  statements is ordinary `if`/`else` over whole literals. A typed builder would re-implement
  Postgres's type system. Accepted cost: applications that are mostly dynamic queries (search
  screens with many optional filters, report builders) are outside Kogo's domain.
- **No inline choice inside a literal** (15 Nov 2026). `${if (x) sql"desc" else sql"asc"}` was
  considered and dropped as going funky; two whole literals do the same job.
- **Named fragments are the one SQL extension on the table, not built** (15 Nov 2026). A
  fragment would be a declaration with a signature, spliced at compile time and checked with
  the statement. Reopen when an access rule is repeated in three or more statements. Until
  then: share the query rather than the predicate, or use a database function case by case
  (one with a subquery is not inlined by the planner; checked on stack B).
- **Fragment-typed function parameters are out.** They turn functions into templates. In
  cusoon2 this means `bookingNotice` becomes one statement per notice and each effect table
  gets its own statements.
- **The API is typed RPC; there is no GraphQL** (15 Nov 2026). An operation is a function, and
  its input and answer types generate the TypeScript client, so the schema is derived from
  signatures and never declared twice. cusoon2's GraphQL layer is not ported; it moves to its
  typed RPC first. Accepted cost: no client-chosen field selection, so each screen's answer is
  its own type.
- **Principle 7 is precise errors, not a fast loop** (15 Nov 2026). Stage 1 measured
  cusoon2's warm Gradle loop at 1.1 s for a one-line edit and 4.4 s through one database
  spec; Go saves a second per edit at most, and Kogo adds a parse, a check and a prepare of
  its own. Kogo's case rests on what it leaves out, not on build speed. Reopen if a measured
  loop (CI, cold start, the full suite) shows Kogo winning by more than seconds.
- **An extension's receiver is not nullable** (15 Nov 2026). Allowing `fun String?.f()` makes
  the checker handle a `this` that may be null, and no example or stage-1 port writes one; a
  parameter does the job. The parser refuses it at the `?.` (`nullable-receiver`). Reopen
  when a port needs one to read naturally.
- **The columns under `exists` and `execute` are not typed** (15 Nov 2026). Both discard
  them, so `tx.execute(sql"select pg_advisory_xact_lock($key)")` passes though `void` has no
  Kogo type. Mapping `void` to `Unit` was the other way; it would let a row type carry a
  `Unit` field under `query<R>`, which means nothing. `void` stays an error under the query
  calls. Reopen if a call that discards columns comes to need one of them.
- **`exists` sends its statement as written and reads the first row** (15 Nov 2026). The
  emitted Go is `tx.Query`, then one `rows.Next()`, giving a `bool`. Counting rows affected
  would be false for a statement whose command tag has no count, and wrapping it in
  `select exists(...)` refuses `insert ... returning` and changes `for update`. pgx still
  drains the remaining rows, so Postgres produces them all: write `limit 1` where there may
  be many. Reopen if that cost shows up in a port.
- **Outer-join nullability is read from Postgres's own parse tree** (15 Nov 2026). The
  checker's session sets
  `debug_print_parse` and `client_min_messages = log`, so preparing a statement also returns
  the analysed tree as a notice. In it Postgres marks every column reference with the outer
  joins and grouping sets that can null it (`varnullingrels`), which is section 10.5's rule
  computed by Postgres, not by us. Nothing is written to the database. Not taken: reading the
  plan as sqlx does. A spike of it over eight statements (not kept) called two columns non-null
  that can be null, a left-joined column selected through a materialized CTE and a `rollup`
  column, and each fix would be a rule of ours about plans. It got one right that the tree
  does not: a `where` that excludes the null rows, which the planner turns into an inner join.
  Also not taken: a cgo parser (build speed, and its raw tree has no names resolved, so we would
  redo the analysis); our own parser (principle 4). Costs: the tree is an undocumented text
  format tied to the major version, so the checker refuses Postgres before 18 (17 does not mark
  grouping sets, 15 has no marks); and each check also writes the tree to the server's log.
  A statement that comes back without a tree stops the check with an error: passing every
  column as nullable would hide that the session, through a pooler say, lost its settings.
  Where it stops is spec 10.5: a reference is followed through a subquery in `from` and a
  `group by`, and no further. Reopen when a Postgres release changes the tree (`TestSignatures`
  fails) or when `"name!"` on CTE columns becomes a habit (following a CTE is about 15 lines).
- **The module stays `module kogo` for now** (15 Nov 2026). Everything planned builds on
  one machine, where a local override reaches it, and the rename to the remote's path is a
  search and replace. The emitter keeps the import path of the prelude's Go in one place,
  so the rename is one line there. Not taken: renaming at once, which collides with the
  branches in flight. Reopen when emitted Go must build where a local override cannot
  reach (CI, another machine), or when the repo's home changes.
- **An application may have binding files of its own** (15 Nov 2026), in a directory it
  names to the compiler; the Go behind them lives with the application. Only the prelude
  may declare a binding today, and a Stripe client cannot go there (principle 8). Not
  taken: every binding in the prelude; or none for applications, with outbound calls
  written in Kogo over a general HTTP handle, which means signing, JSON and retries in a
  language built for none of them. Accepted cost: a binding is Go the checker cannot see
  into, so a careless one can bring in the hidden state section 7 rules out. Not built:
  no example makes an outbound call. Build it with the handle design for outbound calls
  (see the gaps below) when a port first needs one. Narrowed on 17 Nov 2026 to pure
  functions: see "An application has no bindings with an effect" below.
- **`db.tx` keeps its two outcomes: commit when the block finishes, roll back on a failure
  or a panic** (15 Nov 2026). cusoon2's `commit = false` appears five times and every block
  under it only reads, where rolling back and committing are the same. A true dry run,
  writes that are looked at and undone, needs no new form: the block raises a failure that
  carries the preview, `raise(Preview(changed))`, and the caller reads it. Not taken:
  `db.tx(commit = false)`, a second switch no port needs; `db.read { }`, a read-only
  transaction Postgres would enforce. Reopen if dry runs become common, since the idiom
  calls a success a failure, or if a code path should be held to reading by Postgres.
- **A constraint violation gets no construct; an expected conflict is written in SQL**
  (15 Nov 2026; spec 10.6). cusoon2 handles a possible conflict with `on conflict` in 11
  places and by catching the violation and reading its constraint's name in one. Not
  taken: a block in a savepoint whose violation comes back as a failure naming the
  constraint, a new construct whose names are strings the compiler would have to check
  against the database. Accepted cost: an `update` has no `on conflict`, so its guard is an
  `exists` first, and a request that slips between the two still panics. Reopen if a port
  has several such sites or that window causes trouble.
- **An outbound capability is a handle, and an `external object` has no effect**
  (15 Nov 2026; spec 13). Section 13 bound HTTP as a global while section 7 says outbound
  calls arrive as parameters, and a global has no handle to carry the request's context.
  cusoon2 calls out from five of its hundred files, and mail goes through an outbox table
  that a worker reads, so most request code writes rows. Not designed, on purpose:
  timeouts, retries, and whether workers are written in Kogo; they wait, with an
  application's own binding files, for the first port that calls out.
- **Principle 3 is strict in the core and graded in the prelude** (15 Nov 2026;
  `doc/design.md`). The core is finite and nearly covered, and every disagreement with Go
  met there was settled by a prelude function, an emitter rule or a line of spec. The
  prelude mirrors the JVM's library, some 110 functions each with corners (Java uppercases
  `ß` to `SS`, Go leaves it), and matching all of them is the fight with Go not to have.
  So a Kotlin name means Kotlin's behaviour in ordinary use; a corner may differ when
  matching means reimplementing a JVM algorithm or table and no side-by-side run depends
  on it, and is listed in spec 12; a function that would differ in ordinary use takes
  another name, because an author who knows Kotlin trusts the name. Counting a string in
  UTF-16 units stays: it was about 60 lines, so never required to differ. Signs this is
  going wrong: unsafe code or cgo; a Go workaround for each new feature and not only at
  the edges; the list in spec 12 outgrowing a page.
- **The first module run side by side is cusoon2's invoice ledger and pay arithmetic; the
  second, suppliers and businesses** (15 Nov 2026; `doc/design.md` "MVP steps"). The ledger
  is pure, so it needs only the emitter's first slice; it is where a silent difference in
  rounding costs money; one of cusoon2's 127 test files names any of its functions; and
  stage 1 ported it, so redoing the port measures what the decisions since cost. Not
  taken first: the pricing engine, pure but built on `object`s and a generic bounded by
  `Enum`, which would test porting more than the emitter.
- **"Same result" is exact, with no tolerance, over one shared file of cases**
  (15 Nov 2026). A ledger one cent out is wrong, and a tolerance hides what the run is
  for. Checking that a `Double` can match bit for bit already found two places where Go
  differs: it may fuse a multiply and an add, and its conversion of an out-of-range
  `Double` to a whole number depends on the processor. Not taken: a handful of cases by
  eye; cents within one.
- **The comparison lives in cusoon2, pinned to a commit of it** (15 Nov 2026). The Kotlin
  half must, since it calls cusoon2's code, and the port is cusoon2's code in another
  language; `poc-rs` is the precedent. Not taken: a third repo, which spreads one
  experiment over three; the stage-1 directory, which is not a repository.
- **The MVP is served over typed RPC** (15 Nov 2026; `doc/design.md` "MVP steps"). It
  is a real cusoon2 module, with SQL, that matches its Kotlin original exactly side by
  side and answers HTTP requests through an `api` list; the server is its fifth step.
  Serving typed RPC is what Kogo is for. Not taken: stopping at the side-by-side runs,
  which proves the emitter but leaves Kogo a library no request reaches. Reopen if the
  server's issues stall the ports.
- **Kogo adds no member to a type Kotlin has; what is Kogo's own is a function or a type
  of a `kogo.` package, imported by name** (15 Nov 2026; `doc/design.md` principle 3, spec
  12). cusoon2 prints a percentage through Java's `BigDecimal`
  (`valueOf(x).stripTrailingZeros().toPlainString()`), and Kotlin has no function of its own
  for it. The promise that a Kotlin name means Kotlin's behaviour helps only a reader who
  can tell which names are Kotlin's, and `percent.toPlainString()` reads as Kotlin's. So it
  is `plain(percent)`, imported as `kogo.text.plain` (DEV-843), as `kogo.time` stands for
  `java.time`: a file's imports show what in it is not Kotlin's. The case will come back,
  since printing is where Kotlin leans on Java most: cusoon2's 95 hand-written files hold
  `Locale` 25 times, a `.format(` call 22, `DateTimeFormatter` 19 and `BigDecimal` 16.
  Measured: Go's shortest plain digits gave Java's text for 500,000 numbers from a fixed
  seed and for the first 2,000,000 by bit pattern, all but negative zero, `NaN` and the
  infinities, where Java throws, and eight subnormals, 1, 2, 10, 12, 14, 16, 18 and 20
  times the smallest. Random numbers miss those: Java writes at least two digits, the
  closest two, and that differs from one shortest digit only below `1.0E-322`. Not taken: the method on `Double`, agreed first and reversed; writing it in
  the application from Kotlin's names, `toString().removeSuffix(".0")`, which agrees only
  from 0.001 up to 10,000,000; a slice of `BigDecimal`, three functions that must match
  Java's everywhere. `Uuid`, `newId` and `Decimal` are Kogo's own with no import; none is
  a member of a Kotlin type, and they stay. Reopen if imports of `kogo.` functions come to
  crowd a file, or a function keeps reading worse than the member would.
- **`Console` is `kogo.io.Console`, imported** (15 Nov 2026; spec 12 and 14). Kotlin's
  `println` and `readln` are global functions; Kogo's are members of a handle passed in,
  as section 7 has every effect, and the handle is Kogo's own, so the entry above puts it
  in a `kogo.` package. The leakage2 port, written before, used it with no import and
  gains one line. Not taken: `Console` beside `Uuid`, `newId` and `Decimal` in `kogo`, with
  no import, which grows the list of exceptions to that entry.
- **An enum label must be a name a constant can have** (15 Nov 2026, Greg; spec 10.4).
  `cusoon_b`'s `booking_paid_by` has `paid_in car_income`, which no constant can spell. An
  enum with such a label has no Kogo type: the label is renamed in Postgres, or the column
  is cast to text and read as a `String`, and the checker says both. So `select *` cannot
  fill the row of a table that has one until the label is renamed. Not taken: mapping such
  a label to a name (a space to `_`), a second spelling to keep in step with the database,
  which could make two labels one constant.
- **A prelude declaration always lands with its Go and a test** (15 Nov 2026; spec 12 and
  13). `TestPrelude` compiles a use of every declaration against the Go behind it, and
  stays that strict: a declaration the checker accepts is one the emitter can write. Not
  taken: declarations ahead of their Go, on a list the emitter refuses, which lets a port
  check against names that cannot run; a second state for a prelude function, which every
  reader of the prelude would then have to tell apart.
- **An operator on numbers is not also a named function** (15 Nov 2026; spec 3 and 12).
  `+` on numbers is built into the checker, so `pay?.plus(x)` on a `Long?` has no binding;
  a port writes `pay?.let { it + x }`. Not taken: `Long.plus(Long)` for the one call in
  cusoon2's pay arithmetic, which opens every operator on every pair of number types to
  the same request. Reopen if ported code keeps reaching for the named form.
- **A program names its operations in an `api` list, a declaration of Kogo's own**
  (15 Nov 2026; spec 15.1): `api ops { me, updateJob }`. An operation stays an ordinary
  top-level fun with explicit parameters, and the list is what the compiler reads to write
  the server's table and the TypeScript. First proposed as a call, `api(::me, ::updateJob)`,
  which Kotlin 2.4 compiles given two library declarations; Greg refused it for looking like
  an ordinary call while the compiler does the work, the worry behind the rule on Kogo's own
  names. Rust has no construct either, but marks every such thing as a macro, `#[...]` or
  `name!`. Not taken: a block with the operations written inside it and the context declared
  once, which makes an operation a thing of the language but hides a parameter and keeps an
  audience in one file; a file outside the source; every public fun of a named package,
  where a helper of the right shape becomes an operation by accident; an annotation, since
  markers are the one annotation form. cusoon2 keeps its 20 operations in a hand-written
  list today. Reopen if lists and funs drift apart in ways the checker cannot say.
- **An operation's first parameter says who runs the transaction, and a refusal rolls
  back** (15 Nov 2026; spec 15.3). With a `Tx`, the server opens one SERIALIZABLE
  transaction, reruns the operation on a conflict, commits an answer and rolls back a
  refusal or a panic: a refused request has changed nothing. With a `Db`, the server opens
  nothing and the body writes its own `db.tx`. cusoon2 runs every request in one such
  transaction, but its RPC commits on a refusal and holds "a body checks before it writes"
  by convention; its older GraphQL side rolls a refusal back through a savepoint, in 54
  places, and exempts the logins by a comment. No reason for the RPC's rule is recorded;
  our reading is that the logins, among its first seven operations, need a failed attempt
  to stay counted, and one rule had to serve all. Here the exemption is a `Db` in the
  signature. Not taken: the server always owning it, under which the login cannot be
  written; the operation always owning it, which puts `db.tx(serializable = true)` round
  all 20 bodies. Accepted cost: an RPC operation ported from cusoon2 that relies on the
  commit becomes the `Db` kind. Reopen if `Db` operations become the common kind.
- **An operation takes the database, the environment, the request and the input, and
  nothing runs before it** (15 Nov 2026; spec 15.2). `Request` is `kogo.rpc`'s: cookies,
  headers, the caller's address. The environment is the application's data class of what
  lives as long as the server. cusoon2 packs these into one `call` that the server builds;
  its operations use it 38 times for the database, 11 for the identity provider, 7 for the
  session and 3 for the address and user agent, and 14 of the 20 open with their own
  `requireDriver(call)`. Not taken: a context that an application's function builds and the
  list names, a step the reader does not see, which for a `Db` operation would need a
  transaction of its own. An application that wants the context calls its builder on its
  first line. Accepted cost: four parameters on every operation.
- **The wire carries what cusoon2 sends today** (15 Nov 2026; spec 15.4): `Boolean`,
  `Int`, `Long`, `Double`, `String`, `Uuid`, enums, data classes, `List`, `T?` and `Unit`;
  names as written, nothing optional, and strict reading, a missing, unknown or mistyped
  key being a 400. cusoon2's 20 operations send 35 data classes with 107 fields: 53
  `String`, 14 ids, 13 data classes or enums, 12 `Int`, 7 lists, 4 `Boolean`, 3 `Long`, 1
  `Double`; 22 may be null. Sealed interfaces were added on Greg's question of what they
  are good for, and taken off again the same day (DEV-855): cusoon2's `AcceptInviteInput`
  has two nullable fields whose three valid combinations are stated in a comment, which a
  sealed input would let TypeScript and `when` hold, but no operation sends one. They cost
  about 115 lines of code and 40 of tests in the checker and the server, more in the
  emitter's table and `kogo ts` once written, and rules of their own: the `type` key, a
  `data object` alone, a property named `type`, and whether a variant sent alone carries
  `type`. A sealed interface or a variant on the wire is a compile error that asks for a
  data class with an enum. Adding them back breaks no program; taking them out once used
  would. Reopen when an operation's input has field combinations that only a comment
  states. Not taken: dates and times, which cusoon2 sends as text it has formatted; maps.
  Each can be added and break nothing. Forced, not chosen: a `Double` that is `NaN` or an
  infinity has no JSON, so an answer holding one panics.
- **A `Long` crosses as a number, within what JavaScript holds exactly** (15 Nov 2026;
  spec 15.4). JavaScript read `9007199254740993` from JSON as `9007199254740992`, with no
  error (run in node). cusoon2 sends a `Long` as a number unguarded; its three are cents,
  whose limit is about 90 trillion dollars. An answer holding a `Long` past the limit
  panics, and an input holding one is a 400. Not taken: a string, exact for every value,
  which makes each page convert before any arithmetic. Reopen if a real value passes the
  limit.
- **A refusal is words: every operation returns `Either<String, A>`** (15 Nov 2026; spec
  15.2), as all 20 of cusoon2's do, and every page handles a refusal one way. An operation
  that must tell the page which of several things happened says so in its answer, which
  may hold an enum, and commits. Not taken: any wire type as the refusal, which lets a
  page branch on a refusal that rolled back, and lets an internal error type onto the
  wire. Widening later breaks no operation, though the answer's object gains a key.
- **`kogo ts` writes the types and the table of operations, and no client** (15 Nov 2026;
  spec 15.6). The types are where a mistake between backend and frontend is caught.
  cusoon2's client is 79 lines of `fetch`, a CSRF header and a React hook, which depend on
  the application. The file is checked in and a stale one is caught by the application's
  own checks with `git diff`, so Kogo has no flag for it. cusoon2's generator is 111 lines
  and works by reflection at run time; this one reads the checker's facts.
- **The server is an emitted table, a Go library and the application's own `main`**
  (15 Nov 2026; `doc/design.md` "The server"). `main` is Go that reads the
  configuration, opens the pool, builds the environment and mounts the handler: "about
  fifteen lines" here, which DEV-903 found to be about 240 for cusoon2 (`doc/main.md`). Not
  taken: an entry point in the language or a `kogo serve`, which would make Kogo say how
  configuration is read, how a handle is built from a secret and how other routes are
  added. An application already writes Go behind its own bindings.
- **At the boundary a refusal is a 200, a bug is a 500, and reruns that run out are a
  503** (15 Nov 2026; spec 15.5), with a 400 for input that does not read and a 404 for no
  such operation. One id for each HTTP request, given before any rerun; cusoon2 learned
  that an id for each attempt misleads. Logged: the operation, the outcome, the time, each
  rerun with its cause, a panic's message and trace. Two changes from cusoon2: it answers
  500 when reruns run out, and here a 500 always means a bug; it logs inputs and answers
  at debug level, and here they are not logged at all until someone needs a switch.
  The server's reruns stop after 90 seconds unless `main` says otherwise, cusoon2's figure,
  while `db.tx(serializable = true)` written in a body still reruns without a limit.
- **Packages do not import each other, directly or through others** (15 Nov 2026; spec 2).
  Kotlin allows it and Go refuses it; a program with a cycle passed `kogo check` and `kogo
  emit` and failed in the Go compiler. The rule needs no mention of Go, and a cycle means
  neither package reads alone (principle 2). Measured in cusoon2: 21 of 29 packages are in
  one cycle; making them layers moves at least 52 of the 602 names imported inside it, and
  16 of 53 once GraphQL and the root package are left out. Not taken: one Go package for
  the whole program, which accepts every program but ends one Go package for each Kogo
  package, puts the package into every Go name, and rebuilt a synthetic 60,600-line
  program in 1.2 to 1.5 s after a one-line edit against 0.15 s as thirty packages; one Go
  package for each cycle group, whose Go names shift when an import joins two groups. Both
  only accept more, so either can follow later and break nothing. Reopen if ports spend
  their time untangling, or new code keeps meeting the error.
- **`kogo check` is the only gate** (15 Nov 2026; design "Pipeline"). A program that
  passes it builds and runs. A limit that stands is a checker error with a line of the
  spec; where the emitter cannot yet write a form the checker accepts, it refuses with
  `not-yet-emitted`, a debt with an issue (DEV-847), never a rule. So five refusals moved
  into the checker: an interface's fun with type parameters or a default value (spec
  3.4), a local fun with type parameters (spec 5), a whole-number literal outside the
  type it is read as (spec 3), and a top-level val whose value refers to itself or to a
  val below it in its file (spec 6). The last follows the funs and vals a value refers
  to, as Go does: Go refuses `val c: Int = g()` with `fun g(): Int = c` as an
  initialization cycle, and `val a: Int = f()` with `fun f(): Int = b` and `b` declared
  below prints 0 under Kotlin 2.4 and 1 under Go. Nor may a package's files use each
  other through their vals (agreed in review): Kotlin 2.4 sets up a file's vals when the
  file is first used, a call of one of its funs included, so f1 `val a: Int = f()` with
  f2 `fun f(): Int = 1` and `val c: Int = a` gives `c` 0 when main reads `a` first and 1
  when it reads `c` first (checked), and Go gives 1. Only one package's files can do
  this, since packages do not import each other; a rough count found no such files in
  cusoon2 and none in the stage-1 ports. Not taken: two gates, with a list in
  the spec of what the emitter refuses, which leaves an author to learn limits by running
  the emitter; counting only a read written in the value, as first agreed, which let both
  of those programs through. A rough count found none of the first three forms in
  cusoon2. Reopen if a port needs a generic fun in an interface, or files whose vals use
  each other.
- **A value that holds a function is not compared** (15 Nov 2026; spec 3). Kotlin
  compares functions by reference and Go cannot compare them; the emitter's first slice
  answered `false`, so `rule == rule` was `true` in Kotlin and `false` in Kogo with no
  error, in the core where the two must agree. The checker refuses `==` where it can see a
  function in the type, and the runtime panics where it cannot. Not taken: leaving
  `false` and stating it in the spec, a silent difference in `==`; comparing Go's code
  pointers, which calls two closures of one lambda equal whatever they captured. Accepted
  cost: a data class that holds a function cannot be compared, be a `Map`'s key or go in
  a `Set`, which Kotlin allows. Reopen if ports that turn anonymous objects into lambdas
  need to compare them.
- **Behind a type parameter, `==` panics once it reaches a function** (16 Nov 2026; spec
  3). The runtime's walk stops at the first part that differs, so `eq(Rule("a", f),
  Rule("b", f))` answers `false` and `eq(Rule("a", f), Rule("a", f))` panics; the spec
  says so. No answer differs from Kotlin's: `false` comes only where Kotlin's is `false`
  too, and `true` is never reached past a function. Not taken: panicking whenever either
  value holds a function anywhere, about 15 code lines and a full walk of both values on
  every unequal comparison, to make a rare failure earlier. Reopen if a port passes its
  tests and then panics on data whose other parts are equal.
- **The prelude's signatures are Kogo declarations, read by the checker like any other
  source** (15 Nov 2026; spec 13). The author sees real signatures instead of guessing from
  Kotlin, the checker has one way to check a call, and its tests can use a prelude of five
  lines. Not taken: tables inside the checker, which make every prelude function a compiler
  change and put the prelude in the core (principle 8). Built into the checker all the same,
  because the core sections define them: the operators on numbers, `==`, ranges, the forms
  of section 9, the five SQL calls and `db.tx`, and what data classes and enums provide.
  The other operators are bindings (the entry below). A binding may have a nullable receiver (`isNullOrBlank`,
  `orEmpty`), which application code may not. Reopen if a prelude function's type cannot be
  written in Kogo and is too useful to leave out.
- **Type arguments are settled in one fixed order, with no solver** (15 Nov 2026; spec 5.1):
  the receiver, the arguments that are not lambdas, the type the result must have, then
  what each lambda returns. Kotlin weighs every clue at once, in thousands of lines and
  with errors that are hard to act on; this is a few hundred, and each refusal names its
  fix. Walked by hand over the 20 or so generic calls of the examples: all pass, and two
  are already written as it demands. Accepted cost: some valid Kotlin is refused
  (`val r = either { ... }` with no stated type). The prelude's `List`, `Set`, `Map`, `Pair`
  and `Either` are `out`, as Kotlin's are. Reopen if a port needs written type arguments
  often enough to read badly.
- **An overload is chosen by fit, never by rank** (15 Nov 2026; spec 5.2). Kotlin ranks the
  candidates that fit and picks the most specific; the ranking is the complicated part and
  it interacts with inference. Here exactly one candidate may fit, and two that fit are an
  error that says to rename one. The exception is the one Kotlin itself has for `sumOf`:
  candidates that differ only in what a lambda returns are chosen by reading the lambda.
  The same answer serves ordering: the prelude declares `sortedBy` once per key type, since
  Kogo cannot say "any ordered type". Reopen if those lists become a burden; the other way
  is an "ordered" bound that only bindings may use.
- **An operator on anything but a number is a binding** (15 Nov 2026; spec 6): `a + b` is
  the prelude's `plus`, `a < b` its `compareTo`, `x in c` its `contains`, `c[i]` its `get`,
  `a to b` its `to`. Only an `external fun` counts, so application code still defines no
  operator. `Decimal + Decimal` and `Instant < Instant` then take a line of prelude and no
  checker change, and the emitter sees an ordinary call; about 25 lines of checker against
  about 60 that grow with each type. Accepted costs: `xs.plus(x)` is a second spelling of
  `xs + x`, as in Kotlin; whoever writes bindings decides which types have an operator;
  `"a" + "b"` needs `String.plus` in the prelude. The two stage-1 ports use only string
  `+`, `list[i]` and `to`, so the gain is expected, not yet shown. Reopen if an operator's
  meaning comes to depend on more than its receiver's and its argument's types, or if the
  emitted call costs at run time.
- **A whole-number literal is an `Int` unless a declared type, a parameter or a return type
  asks for a `Long`** (15 Nov 2026; spec 3 and 5.1), and that holds for an argument that
  settles a type parameter and for one side of `?:` or `if` with nothing wanted: `val x =
  cents ?: 0`, `firstOr(longs, 0)` and `val xs: List<Long> = listOf(1, 2)` are refused with
  "write 0L", though Kotlin takes all three. One rule says what a literal is, and the author
  of a stage-1 port already wrote `0L` three times in 262 lines from the spec alone. Not
  taken: letting a literal follow a `Long` beside it (about 15 lines), which still leaves
  `==` and `listOf(1, 2)` needing the `L`; and also settling a literal after the wanted
  type (about 25), a fifth step in the order for type arguments. Both only accept more, so
  either can be added later and break nothing. Reopen if ports show repeated rounds of
  checking over this.
- **Only a name narrows** (15 Nov 2026; spec 4): a local `val` or a parameter, by `!= null`,
  `== null` and `is`, through `&&`, `||` and `!`, and for the rest of a block when the other
  branch cannot finish. A name that narrows never changes, so nothing learned is ever
  forgotten; Kotlin's version is large because it follows `var`s and which properties are
  stable. Not taken: narrowing a path (`booking.price`), and narrowing after `require`,
  `check` and `ensure`, both of which Kotlin does. No example or stage-1 port needs either.
  Reopen if ports keep binding a path to a `val` only to test it.
- **Only a statement narrows for the rest of its block** (15 Nov 2026; spec 4): an `if`
  whose other branch cannot finish, and `x ?: return y`. An `if`, `?:` or `when` used as a
  value narrows nothing after it, though Kotlin's does: `val p = x ?: return y` gives `p`,
  and the examples use `p`. Narrowing from a value would have to cover all three forms at
  once (about 15 lines of checker, 40 of tests) and track what is known after any
  expression, which the entry above avoids. Reopen if ports keep splitting
  `val y = if (...) return ... else ...` into two statements.
- **`Int` or `Long` mixed with `Double` is refused** (15 Nov 2026; spec 3). The two stage-1
  ports wrote one sum both ways, `amount * percent` and `amount.toDouble() * percent`; the
  second shows where cents become a floating-point number. `==` between `Int` and `Long` is
  an error and a whole-number literal is a `Long` where one is expected, both as in Kotlin.
- **The checker hands the emitter facts beside the syntax tree** (15 Nov 2026;
  `doc/design.md`), as Go's type checker does, and marks what can fail in one way whichever
  spelling was used. Not taken: a second, simplified tree for the emitter, about 1,000
  lines whose right shape depends on Go output that is not designed. Accepted cost: the
  emitter handles every surface form, perhaps 15 to 20 extra cases, tested only through the
  Go they produce. Reopen when the emitter's cases tangle; a simplifying step can then be
  added between the two without changing the checker.
- **The first emitter writes a Go library, in three slices, each proven by running**
  (15 Nov 2026; `doc/design.md` "The emitter"): pure code, failures, SQL. A hand-written
  driver calls it, and the first side-by-side run with Kotlin follows the first slice.
  Not taken: an entry point in the language (`fun main`, `kogo run`), a language change
  before anything has run; the server first, which puts the emitter's first test behind a
  second design.
- **A failure is a panic that its own raise scope recovers** (15 Nov 2026). Early return,
  as hand-written Go does it, cannot leave a lambda (`xs.map { parse(it).bind() }`), so
  failures in lambdas would be refused or every prelude function taking a lambda doubled;
  and it needs statements where Kogo has values. Measured through four calls: a failure
  costs 193 ns against 4 ns, a success 14 ns against 6 ns, beside a database round trip of
  about 500,000 ns. A Kogo failure is typed, in the signature and caught in exactly one
  place the checker has proved, so the panic is plumbing and not an exception in the
  source. Accepted costs: the Go is not idiomatic, a failure shows as a panic in a
  debugger, and the prelude's Go must be safe to unwind through.
- **`T?` is a pointer, for every `T`** (15 Nov 2026). It is what the Postgres driver reads
  a nullable column into. Not taken: a struct with a "present" flag, which allocates
  nothing but needs adapting wherever values come from outside. Reopen if a profile shows
  the allocations.
- **`==` is Go's where the type is a number, string, `Boolean` or enum, and a runtime walk
  elsewhere** (15 Nov 2026). Go compares a pointer by address and not slices at all. Not
  taken: generated comparison code for every data class, which is fast, large, and awkward
  for a type parameter. Reopen if the walk shows in a profile.
- **A `String` is a Go string counted in UTF-16 units, as Kotlin counts** (15 Nov 2026;
  spec 3), because `"😀".length` is valid in both languages (principle 3) and the emitter
  is proved side by side with Kotlin. Half a surrogate pair becomes U+FFFD: a Go string
  cannot hold one, and a panic would fail a request on `name.substring(0, 20)`. Not taken:
  counting whole characters, which is simpler and agrees with Postgres's `char_length`
  but changes what valid Kotlin means; storing UTF-16, which converts at every boundary.
  Accepted cost: `length` walks the string.
- **A `Double` product is rounded before anything adds to it** (15 Nov 2026; spec 3). The
  Go spec lets an implementation fuse `x*y + z` into one instruction that rounds once, across
  statements too, and on arm64 Go does; Kotlin rounds each step, and the side-by-side run
  compares `Double`s bit for bit. The emitter writes every `Double` multiplication as
  `float64(x*y)`, the conversion the Go spec names as stopping fusion. Not taken: wrapping
  only a product that feeds an addition, which has to follow values across statements.
  Reopen if Go stops fusing, or a profile shows the lost instruction.
- **`Double.toLong()` and `Double.toInt()` are Kotlin's, not Go's conversion** (15 Nov 2026;
  spec 3). Go leaves an out-of-range conversion to the machine: `int64(1e30)` saturates on
  arm64 and gives the minimum on amd64. The prelude spells Kotlin's rule out: truncate
  toward zero, stop at the type's ends, `NaN` gives 0. Accepted cost: this arm64 VM already
  saturates in hardware, so the tests that cover the ends cannot fail here; they guard amd64.
- **A value-producing `if`, `when`, `?:` or `?.` is written toward where its value goes,
  and wrapped in a function elsewhere** (15 Nov 2026; spec 6). A `return`, `break` or
  `continue` buried in a larger expression is refused, though Kotlin allows it; the
  examples and both stage-1 ports hold two jumps in a value's place, both
  `val x = y ?: return z`. Not taken: flattening every expression into statements and
  temporaries, about 400 lines and the simplified tree under another name.
- **A lambda holds no `return`** (15 Nov 2026; spec 6). Kotlin allows one only in a lambda
  it inlines, where it leaves the enclosing fun. A Kogo lambda is a function value, so
  leaving the fun through it would take the panic that a failure uses, at every such
  `return`. The checker refuses it (`kotlin-lambda-return`) and says to end the lambda
  with its value and return after the call; `break` and `continue` in a lambda were
  refused already. No example or stage-1 port writes one. Reopen if ports keep rewriting
  `xs.forEach { if (...) return x }` as loops and it reads badly.
- **The prelude's Go is our own package with no new dependency** (15 Nov 2026). `Decimal`
  holds what Postgres sent and has no arithmetic: the ports use it once and convert it to a
  `Double`, and cusoon2 keeps money in cents. Reopen when a port needs decimal arithmetic,
  and choose then between a library and our own.
- **`Map` and `Set` are one ordered implementation that compares keys by value**
  (15 Nov 2026; spec 12). A lookup asks whether two keys are equal, so it must answer as
  `==` does: Go's own map would not find `Leg("A", "B")` under an equal key whose `String?`
  is another allocation. Not taken: Go's map with keys restricted to plain types, which
  puts a Go limit in the spec and makes the checker follow type parameters to see which
  become keys. The examples use no `Map` and the ports one `Set`.
- **The request's context is held by `Tx` and `Db`, not passed as a hidden parameter**
  (15 Nov 2026). A request belongs to one shape of program and so to the prelude
  (principle 8): a hidden parameter would put it on every function of every program, a
  Kogo compiler written in Kogo included, and would give the prelude's Go an ambient
  channel that section 7 says does not exist. Go's convention against storing a context
  was weighed and set aside, since nobody reads the Go. Not gained: stopping pure code
  when its request is cancelled, and request-wide data without a handle. Reopen if either
  is needed; Kogo source does not change either way.
- **`copy` and a sql row need whatever the constructor needs** (15 Nov 2026; spec 3.1, 10.3
  and 11): its visibility, `private` or `internal`, and its marker. Each builds a value
  without calling the constructor, and the access walls rest on who may construct the
  type: otherwise `held.copy(business = other)`, or `tx.queryOne<HeldBusiness>(...)`, makes
  a `HeldBusiness` in a file with no opt-in. The refusal is the one a call of the
  constructor gets. Kotlin follows the constructor's visibility for `copy` in recent
  versions and, as far as we know, not its marker; refusing is the safe direction.
  Destructuring and `==` read a value and build none.
- **`Either` is the only spelling of a fallible function** (15 Nov 2026). A program declares
  no function with a `Raise<E>` receiver or `context(Raise<E>)`, and `withError` is gone;
  each is refused where it is written, with the fix. Two spellings of one thing went against
  principle 6. With `Either` only, every place a body can fail is marked in it, at a call by
  `.bind()`, so it reads without opening the callee, and a forgotten `bind` is always caught
  as a dropped or mistyped `Either`. `withError` served calling receiver code of another
  error type; without that, it was a second way to write
  `either<E2, A> { ... }.mapLeft { ... }.bind()`. `Raise<E>` stays inside `check/core.kg`,
  the receiver of `raise`, `ensure` and `ensureNotNull` and of either's block. Accepted
  cost: cusoon2's 26 receiver functions each become `= either { ... }`, with `.bind()` at
  their calls. Reopen if ports read worse for `.bind()` at every call.
- **The TypeScript file names no built-in, and no wire type takes its list's interface
  name** (15 Nov 2026; spec 15.4, 15.6). A type the file declares shadows TypeScript's own
  of that name, so `Unit` is written `{[k: string]: never}`, not `Record<string, never>`,
  and a wire type named like the list's interface (`Ops` for `api ops`) is refused, as one
  named `Outcome` is. Not taken: a list of TypeScript's global names to refuse, which is
  long, changes with TypeScript, and only `Record` was ever written. Reopen if the file
  must name another built-in.
- **Kogo's async runs are `opus medium 40m`, `fable high 40m` for heavy design** (16 Nov
  2026). Greg set these for Kogo, reviews included at 40m; cusoon2's `pr opus high 40m` is
  cusoon2's default, not Kogo's. `doc/lead.md` has the table.
- **The Linear tasks run from `~/Dev/cusoon2-tasks`** (16 Nov 2026), a cusoon2 checkout on
  `renew` used for nothing else, which the lead pulls when it starts. Not taken:
  `~/Dev/cusoon2`, where other sessions switch branches and the tasks vanish on any branch
  older than cusoon2 PR 1486. Reopen if the tasks move out of cusoon2.
- **The lead merges** (16 Nov 2026). The step after a Review is Fix, which pushes and ends
  with a `Fixed:` comment, the issue still In Review; the lead reads the fix commits, runs
  the tests and merges. A Review covers the original diff, and the lead answers for what
  merges, so the one step that cannot be undone is the lead's; a fixing session that meets
  a choice it should not make has somewhere to stop, and an async fix run ends In Review
  like every other run. Not taken: the fixing session merges, as first written (DEV-858),
  which merges commits nobody else has read. Reopen if waiting for a lead session holds
  merges up.
- **The lead merges a PR with nothing left to change** (16 Nov 2026, lead's ruling on
  DEV-861). On a `Review:` or `Ruling:` that leaves nothing to change and that it agrees
  with, the lead runs the tests and merges at the reviewed sha, without a `Fixed:`. Not
  taken: a Fix session that changes nothing, which adds a step and no reader.
- **A merge needs both checks against Kotlin** (16 Nov 2026, DEV-880): `mise run kotlin`
  for the built-ins and cusoon2's `kogo/run.sh` for the ports, skipped only for a PR of
  `.md` files. A failure of either is tried once on `main` before the PR is blamed. The
  comparison's last line, `0 differ`, carries no count, so the lead also needs its
  `N cases` above zero and quotes both in `Merged:`. The comparison runs from any
  directory since cusoon2 PR 1494 made `run.sh` export its own `CUSOON_CONFIG_FILE`
  (DEV-889). Reopen if a check that ends `0 differ` lets a difference through.
- **The lead approves a prelude name Kotlin has, with Kotlin's meaning** (16 Nov 2026,
  Greg; DEV-889). A name a port needs that the spec does not list, from Kotlin's standard
  library or `java.time`, wanted with exactly Kotlin's meaning, is the lead's to approve and
  report to Greg; it joins section 12's list and the prelude together. Principle 3 already
  says what it must do and `mise run kotlin` holds it to that, so there is nothing to
  choose. An operator on a prelude type is such a name (`<` is `compareTo`). A name of
  Kogo's own, or one differing from Kotlin's in ordinary use, stays Greg's. Not taken:
  every unlisted name to Greg, which makes him the bottleneck for names with nothing to
  choose; any name at the port's own word, which lets the prelude grow unseen. Reopen if
  an approved name turns out to differ from Kotlin's.
- **The lead hands over through the issues** (16 Nov 2026, Greg and the lead). Each agreed
  next step is on its issue before a lead session ends, as a launch comment, a `Ruling:` or
  a `Lead:` comment, so a new lead needs only the board. Not taken: a handover note or
  document, which is a second copy of the board. Reopen if an agreed step has no issue to
  go on.
- **cusoon2 and Kogo follow one set of steps** (18 Nov 2026, Greg; DEV-917). The Workflow
  of each repository's `AGENTS.md` is one text between markers, the same in both but for
  where a step works (a worktree per issue here, a clone with its stack in cusoon2) and who
  merges (the lead here; in cusoon2 Greg, after the lead says it is ready). Every step's
  comment goes in full on the PR and its first line on the issue, and an agent files into
  Todo in the project of the repository the work lands in, cusoon2's in Cloud Limo
  Relaunch. A Zed session on cusoon2 had loaded cusoon2's steps, which posted a handoff on
  the issue alone and had no Code review or Fix, whatever a `Lead:` comment said. Taken by
  the lead where Greg's table was silent: cusoon2's `linear-file` now defaults to Todo and
  requires `--project`, since its docstring called Triage the rule and an issue in no
  project shows on no board; cusoon2's `launch` keyword goes, as launching is the lead's
  (`doc/lead.md`); cusoon2 work with no stack to run, docs and scripts, may take a worktree.
  A session loads the `AGENTS.md` of the branch its folder is on, so until no open cusoon2
  branch predates this, a `Lead:` comment for one sends the session to `renew`'s Workflow
  first (the lead's ruling on the review, 18 Nov 2026). Not taken: one file both repositories link, which a session on either would have to
  find outside its own repository; Triage as the default, which files a forgotten
  `--state` where only Greg looks. Reopen if the two repositories' steps need to differ
  in more than the two rows.
- **The lead labels runs, and a run's comment lands in full once** (18 Nov 2026, Greg;
  DEV-919). `lin launch` posts the launch comment, then adds `agent`; the lead lists every
  labelled issue, running or queued, after the boxes of each reply. The lead labels the
  speculative runs too, up to three a night (Ruling on DEV-919's Review); `--no-label`
  is for a brief posted before it is ready. Not taken: Greg picking them, a manual gate
  he dropped. A run's comment goes whole on its PR and its first line on the issue; the
  wrapper's rule and the run VM's setup are recorded in cusoon2's
  `doc/plans/async-agent-runs.md`. A `Lead:` comment that nothing has followed is changed
  with `commentUpdate`, not restated. Not taken: a `lin comment --edit`, until a lead
  still reposts. Reopen if the labelled list grows past what Greg can check.
- **Todo is soon, Backlog is parked** (18 Nov 2026, Greg; DEV-919). Todo holds only what
  is meant to start within days; anything else is in Backlog, with a `Lead:` comment whose
  first line says what brings it back, and the lead moves it when that happens or Greg
  says so. An agent files to Todo what is ready and to Backlog what waits. Not yet holds
  at most three rows, moves that wait on running work or a question just asked; a step
  of the lead's own after a run is not a row, since the labelled line already says the
  run is out (Ruling on DEV-919's Review). Not taken: letting the cap give way to running
  work, or showing three of more rows. `lin list backlog` prints the parked issues as the
  board prints the open ones, which the lead took so that what brings each back is read,
  not remembered. Not taken: due dates, a milestone per day.
- **The emitted Go names its source inline, by a relative path** (15 Nov 2026). Each line of
  Go begins with its own position, `/*line ../../src/a.kg:26*/`, padded so that a file's
  code starts at one column, then indented a tab for each block or closure it is in. Nobody
  reads the Go (principle 5) until an agent chases a panic or an emitter fault, and then every
  line it loads costs: in example 22 the `//line` lines above each line were half the file's
  lines and 56% of its bytes. Inline and relative, the same mapping holds on half the lines
  with about a third of the path, and the same program emits the same bytes on any machine.
  The indentation shows which closure a line is in, where `either`, `db.tx` and the scan
  functions nest. Go resolves a relative path from the directory of the Go file that holds
  it. Limitation: move `<out>` or the sources relative to each other and traces name a path
  that is not there, with the line still right; emit again. Not taken: a directive on a line
  of its own, which doubles the lines; directives only where the line jumps, which saved 8 of
  101 in example 22 and lets positions drift into code that has none; one at the end of the
  line, which sets the newline's position and so must name the line before; an option to
  omit them, which costs an agent the trace it needs most, when one `sed` strips them;
  keeping `//line` lines and running gofmt over the output, which keeps every position
  (checked over example 22's 156 statements, calls and closures) in about 5 lines of code,
  but doubles the lines, and an agent's tools count lines (a read's limit, grep's context, a
  diff's hunks), while inline each line carries its own position; gofmt cannot run over the
  inline form, since it moves a directive before `func` or `}` to a line of its own, which
  shifts that line by one; absolute paths, half the file's bytes and different on every
  machine. Reopen if Go stops resolving a relative path from the file's directory, or an
  application must move `<out>` after emitting.
- **`newId()` is not a parameter** (16 Nov 2026; spec 7). It reads the clock and randomness,
  yet is the one exception to "no ambient state", because an id's value never changes what a
  program does. Not taken: an `Ids` handle passed as `Clock` is, which adds a parameter to
  every function that writes a row and moves every port a step from its Kotlin. The
  side-by-side run of DEV-853 compares rows without pinning ids, by pairing each side's own.
  Reopen if a test needs fixed ids and cannot pair them.
- **Ids are UUID version 7, whatever an application used before** (16 Nov 2026). cusoon2's
  ids are its own layout, 65.5 µs ticks since 23 Feb 2018 in the first 48 bits, made for
  index locality, which version 7 gives; Postgres 18 has `uuidv7()`. The two never sort
  together: a version 7 id made now sorts after cusoon2's ids from before about November
  2021 and before every later one. So a Kogo module must not write a table that Kotlin code
  orders by id until the application either makes its ids in Postgres, by a function both
  sides call, or moves to version 7 and orders those queries by a time. That choice is the application's migration, not Kogo's. Not taken: cusoon2's
  layout in the prelude, which is specific to one application.
- **cusoon2's databases are transient, not forbidden** (16 Nov 2026, Greg). `cusoon` and
  `cusoon_b` are the stacks' own and re-created all the time, so a session uses a database of
  its own for writes and reports a mistake in its step's comment rather than stopping. Not
  taken: "read-only to you, never write or migrate them", whose weight turned one wiped
  database (DEV-853) into a halt, an incident report and a guard, when nothing in it was
  needed. Reopen if a stack's database comes to hold data that is not re-created.
- **The spec's prelude lists are the target, built as ports need them** (16 Nov 2026;
  DEV-875). About fifty listed names are not declared; building all is some 600 lines no
  program needs yet. Not taken: building them all now; cutting the spec to what exists,
  which hides what is coming. Reopen if ports keep meeting the same names.
- **A port aims at exact behaviour, shown by running; a difference is fixed or accepted on
  the record** (16 Nov 2026, Greg; `doc/design.md`, "Ports"). Code moved from Kotlin to Kogo
  aims to behave exactly as it did. Principle 3 does not give this, since a port's SQL and
  what Kogo leaves out are rewritten, so each piece is run beside its Kotlin and compared
  exactly. A difference is fixed, or Greg accepts it, recorded with what differs, why
  matching costs too much and the cases it covers, which the comparison then forgives and
  nothing else. Not taken: stating the language's aim a second time beside principle 3;
  requiring zero differences with no way to accept one, which makes the smallest corner a
  blocker; passing a documented corner without a decision, which lets a port drift. Reopen
  if accepted differences pile up in one place, which says the prelude should match there.
- **A port transliterates; SQL is covered, not marked** (16 Nov 2026, Greg;
  `doc/porting.md`). A port keeps the Kotlin's names, order and structure, and marks each
  departure outside SQL with a comment, a line in the handoff and a case that reaches it.
  Every SQL statement departs, so instead each is run by the side-by-side script, both ways
  where it can go two, a read that can return nothing (`query`, `queryOne`, `exists`)
  included; `querySingle` is one-way. A service the Kotlin had by injection becomes a `tx`,
  `db` or `clock` parameter with no mark. Not taken: idiomatic Kogo resting on the
  comparison alone, which a reader cannot check against the Kotlin; a marker on every
  statement or every handle, which says nothing; running each read once, which misses the
  found-nothing branch where ports differ most. Reopen if a tool counts statements run, or a
  port's comparison misses a departure a mark would have shown.
- **The side-by-side run is Kogo's regression test** (16 Nov 2026). The lead runs cusoon2's
  `kogo/run.sh` against a PR's worktree before merging it, unless it changes only `.md`
  files, and a changed result does not merge (`doc/lead.md`, "Merging"). Not taken: running it in every
  session's own tests, which needs cusoon2's Kotlin build where a run has none; running it
  after the merge, which finds the break once it is on `main`. Reopen when Kogo has a
  comparison of its own that needs no cusoon2 checkout.
- **The lead lists Greg's next moves in two lists** (16 Nov 2026, Greg; `doc/lead.md`,
  "Talking to Greg"). You can do now, each with its exact phrase or label, and Not yet, each
  with what it waits on and who will say when it is ready; a move changes list only when the
  lead says so, and every reply opens with what waits on Greg, in bold. Not taken: one list
  mixing the two, in which a move not ready sits among ready ones and can be started too
  soon. Reopen if Greg finds the split hides what matters most.
- **The lead does not run other sessions' steps** (16 Nov 2026, Greg; `doc/lead.md`, "What
  you own"). A Pickup, a Code review or a Fix runs in Zed or as an async run, chosen by the
  lead and started by Greg; the Zed session already open on an issue is the first choice for
  its next step while its context is warm, except a Code review of what that session wrote:
  a session cannot review its own code, though it can fix it, and the session that reviewed
  can fix too. Not taken: the lead's own subagents, whose output fills the lead's session
  and whose review would be the lead's, not the reviewing model's. The mistakes behind it
  were made in DEV-880 and DEV-881 on 16 Nov 2026. Reopen if Greg asks the lead to make a
  step itself.
- **A port's difference may be owed, not only fixed or accepted** (16 Nov 2026, Greg;
  DEV-884; `doc/design.md`, "Ports"). Three outcomes. Fixed: the port or Kogo was wrong.
  Accepted: a corner where matching costs too much; Greg decides, it stays for good and
  does not block replacing the Kotlin. Owed: the port leaves out something the Kotlin does
  because Kogo cannot yet do it; Greg decides, as with accepted, since nothing the
  comparison forgives is forgiven unless he said so; recorded with what is left out, the
  issue that will close it and the cases it covers, forgiven until that issue closes, and
  it blocks replacing. The MVP is done at zero differences beyond those accepted and owed,
  since it stops at the comparison. The case was DEV-879: `sendInvite` queues an email and
  Kogo has no mail. Not taken: recording such a gap as accepted, which reads as kept for
  good; holding the MVP until Kogo can send mail; the lead agreeing an owed difference,
  which lets a gap be forgiven before Greg has seen it; the porting session deciding
  alone. Reopen if owed differences are left open after their piece is otherwise ready to
  replace its Kotlin.
- **Each step's comment is posted once** (16 Nov 2026, Greg; `AGENTS.md`, "Workflow").
  A Handoff, Review, Fixed, Ruling or Merged goes in full on the PR and its first line alone
  on the issue, which carries the PR, the sha and the counts and is all the board and the
  lead's polling read. An async run is the exception: its `answer.md` is the full comment,
  which the wrapper posts whole, and it holds an interrupted run's handoff. A comment with
  no PR, a launch or `Lead:` comment, stays on the issue in full. Not taken: both copies in
  full, as before, one more post a step and two texts that can differ; the issue alone,
  which a run cannot read; a one-line `answer.md`, which costs a cusoon2 PR and loses an
  interrupted run's handoff. Reopen if a reader needs the issue to tell the whole story.
- **Greg answers the lead with numbers and letters** (16 Nov 2026, Greg; `doc/lead.md`,
  "Talking to Greg"). Each move in You can do now is numbered and a question's options are
  lettered, so `1 3 b` says two moves done and option b; a Zed session back is `884 back`.
  Numbers are those of the lead's latest list, restated each reply. Not taken: letters for
  moves, which a question's options already use; a "Done:" prefix, which numbers make
  needless. Reopen if a renumbered move gets answered by its old number.
- **A type with fields is a data class, and its body holds only overrides** (16 Nov 2026,
  Greg; spec 3.1, 3.4, 14). Every property is a `val` of its constructor, the body holds
  only the `override fun`s of its interfaces, and everything else is a top-level or
  extension function. Locality first (principle 2): a plain class can hold private, mutable
  state that its methods read, as cusoon2's `Model` holds `tenantId`, and a call then
  depends on what is not in its signature; with data classes a value's state is its
  constructor and a function's inputs are its parameters, and the `tx` in
  `held.acceptInHouse(tx)` is that rule showing. One way to do each thing second (principle
  6): Kotlin gives a method three homes, Kogo two that mean the same. The compiler third:
  `==`, the wire, a SQL row and `toString` are each one walk over a data class, where a
  plain class would need `==` defined, which in Kotlin is identity and in Go a pointer. The
  cost, measured on cusoon2's backend (about 18,000 lines outside generated code, by grep,
  so rough), is small restated fields and one wall. `HeldBusiness`, two fields and three
  subclasses, cost DEV-879's port about five `override val` lines, and `Failure`, one field
  and four subclasses, would cost four. The email classes share a field or two (`payload`;
  `booker` and `invoice`), restated at a line each, as well as functions (`closing`, `bcc`)
  and a lazy database read, which become top-level functions and a parameter whatever this
  rule says; the thirteen GraphQL value classes extend generated types that typed RPC
  replaces. The wall: a base class whose constructor carries a marker binds every subclass,
  and an interface carries none, so a wall written as an interface can be implemented by a
  file that does not opt in; DEV-879's port of `HeldBusiness` had that hole for reads. So a
  class that is a wall becomes a data class with a marked constructor that the kinds hold
  (`AtBusiness` holds a `HeldBusiness`; DEV-888), which walls reads and writes and restates
  no field. It works where the shared part is data; a wall that needs different behaviour
  behind one type still needs an interface, and has the hole. A marked data class is never
  read from a request (DEV-890). Not taken: plain classes, which bring private state and
  identity; inheritance, to save the restated fields. The two halves differ: no private or
  mutable state carries the principle, while no other functions in the body is nearer
  taste, since allowing them would change nothing about state. Reopen that half if ports
  keep reaching for a method in the body; reopen the restating if a port meets a base with
  many fields and many implementers, or a wall needs an interface, which would then have to
  carry a marker that binds its implementers.
- **The two boxes end each reply that reports or changes the state of work** (16 Nov 2026,
  Greg; DEV-897; `doc/lead.md`, "Talking to Greg"). They exist so that Greg never starts a
  move too soon, so they are restated whole, each row standing on its own, and a reply that
  only carries on a discussion leaves them out whole. Not taken: boxes in every reply, a
  discussion's included, which pushes the open question off his screen. Reopen if Greg
  starts a move too soon.
- **An application has no bindings with an effect; it may add pure functions in Go, as
  extensions** (17 Nov 2026, Greg; spec 13; `doc/work.md`, "An application's own
  extensions"). It narrows "An application may have binding files of its own" (15 Nov),
  which was never built. Every binding that reaches a database, a network or a clock is
  in this repo, and an application's `main` makes the prelude's handles and implements
  none.
  - **Why effects are closed.** The rules of 17 Nov hold only over a closed set of
    bindings with an effect. "`act` cannot reach the database" and "no action runs in a
    transaction" are true while no binding does either out of the checker's sight, and an
    application's binding is Go that this repo never tests: one that kept a connection
    pool of its own would write from `act`, and the checker would pass it.
  - **Why pure functions are open.** This entry first closed every binding, the same
    morning, and named what would reopen it: a dependency Kogo should not carry. PDF was
    that, the first time it was tested. Greg: a PDF library in Kogo, as pgx is, is
    premature and overkill; and other languages let a program wrap a library. Only
    effects needed closing.
  - **An extension**: its functions take values and give a value, with no handle and no
    `action fun`; each carries a marker of the application's, so a file that calls one opts
    in at its top; its binding file, its Go and that Go's dependencies live with the
    application. Kogo's own Go stays with no new dependency (15 Nov).
  - **What the entry of 15 Nov feared.** It refused clients written in Kogo over a general
    HTTP handle for "signing, JSON and retries". None stands: no call of cusoon2's signs a
    request (searched on 17 Nov), and the credentials sit on the handle, made in `main`;
    JSON is read by key through `Json`, in Go; retries are off, and the runner's.
  - **What may enter the prelude.** A binding that passes four tests: it is mechanism, and
    decides nothing an application would decide differently; its declaration names nothing
    of one application; it says whether it has no effect, is a `lookup` or is an `action`;
    and it lands with its Go and a test (15 Nov). One that fails a test and is taken anyway
    is recorded here by name, with the test it fails.
  - Not taken: every binding closed, with PDF in the prelude behind a library or a writer
    of our own; and every binding open, under which each guarantee of `doc/gates.md` and
    `doc/work.md` gains "unless an application's binding does".
  - Accepted cost: that an extension is pure is its author's word, and its Go is tested
    in the application's repo, not here. It is handed no connection, no credentials and
    no context, so a mistake there does not make an effect by accident.
  - Reopen if an application needs an effect the prelude cannot give in a general form.
- **The runner holds what is a fact for every queue, and a queue states what is a choice**
  (17 Nov 2026, Greg; `doc/work.md`, "Workers"). The line for hand-written Go is what
  another application would need unchanged, and cusoon2's own queues are its first test:
  where two of them differ, the matter is a choice, and the queue states it as a number or
  as a name that Postgres checks.
  - **The runner's.** Whether a row is done (`done_at`), what is owed, the lease and the
    back-off: no queue of cusoon2's varies them.
  - **The queue's.** `attempts`, `soonest`, `fair` and `find`.
  - **First applied to the order.** `doc/work.md` had one order for every queue, rows with
    a `not_after` first. It was drawn from the charge alone, and under it every reminder
    in the outbox went before a tenant's other mail. `soonest` replaced it: a queue may
    name the column of times its rows go first by, and mail names none.
  - Not taken: the one order, with the change to mail recorded as a change of behaviour.
  - The charge's half of "An effect someone is watching" (`doc/work.md`, agreed 17 Nov), a
    hosted-page charge stamped with a `not_after`, is withdrawn: see "A hosted-page charge
    has no `not_after`" below.
  - A choice the runner makes for every queue is recorded here by name. Its two lanes are
    gone, and where an alert goes is the application's (both below).
  - Reopen if a queue needs an order that one column of times cannot give.
- **A function that acts says so, with the marker `Acts`, and the call that changes
  something outside is `perform`** (17 Nov 2026, Greg; respelt `action fun` on 18 Nov
  2026, below; `doc/gates.md`, "A function that acts says so"; `doc/work.md`, "An
  endpoint is declared once").
  - **Why written.** A function's type and its failures are in its signature; whether it
    changes something outside was the one thing a caller relied on that was worked out
    through what it calls. A line added to a helper changed what its callers were with no
    word of theirs changed, the error came up files away, and a lambda kept in a value, a
    `::` reference and an interface's `fun` carried an action past the walk.
  - **Why a marker.** It adds no syntax, and it is Kotlin's own rule: a function that uses
    a marked declaration carries the mark and demands it of its callers (run on Kotlin
    2.4.0). Kogo only restricts it: no file opts in, the mark must be true, a marked
    function keeps out of a transaction, and an action is not kept in a value.
  - **Accepted cost.** `Acts` is the one marker held by the function and never by the
    file, against spec 11's rule for every other, and spec 11 says so. It is also a marker
    the checker knows by name, which comes near the annotation magic principle 2 refuses.
  - **Why `perform`.** `send` was too weak a word for a call that changes something, and
    the method does not say it: a `DELETE` is an action and a lookup may be a `POST`.
  - Not taken: the kind left worked out; `action fun`, a word of Kogo's own, which is
    plainer and keeps spec 11 to one rule; two environments, one that can act.
  - Respelt `action fun` on 18 Nov 2026 (DEV-894, below), which reopened it.
- **A transaction's kind is its handle's type: `Tx` is any, and `SerializableTx` is one
  that is rerun on a conflict** (17 Nov 2026, Greg; `doc/gates.md`, "Two kinds of
  transaction"). Kogo is to support explicit locking as naturally as cusoon2's way.
  - **Why.** The designs assumed SERIALIZABLE in three places: spec 15.3 has the server
    always open it, a gate could not lock, and no level was stated for a worker. cusoon2
    itself runs both ways, its requests serializable and its daemon and logins at READ
    COMMITTED with locks, and chooses a read's lock by a flag on `Model` when it runs, so
    one call sends three statements. Which kind of transaction a function is in was the
    last thing it relied on that nothing wrote.
  - **The rule.** A function that takes a `Tx` assumes nothing and locks what it needs;
    one that takes a `SerializableTx` may read with no lock, and is not handed a `Tx`. A
    `SerializableTx` fits a `Tx`. The type says which `db.tx` gives and which the server or
    the runner opens, with no default. A gate on a `Tx` may lock.
  - **Accepted cost.** A read used both ways is two functions in a port, or one that
    takes a lock the Kotlin does not. A gate on a `Tx` cannot run in Postgres's read-only
    mode, so a `select` of a function that writes passes it.
  - Not taken: the level on the list or in `main`; a flag on the handle.
  - Reopen if the two functions of a shared read become the common case in a port.
- **A binding's behaviour is Kogo's: the spec states it, and a Go default it does not
  state is turned off, with a test** (17 Nov 2026, Greg; `doc/design.md`, "Keeping the
  general language open"; `doc/work.md`, "What Go would decide, Kogo states"). The rule
  for values, never to inherit Go's by accident, now holds for what reaches the network.
  For `kogo.http` it settles five things: a second send, redirects, a proxy from the
  environment, an answer's length and JSON's numbers.
  - The rule does not say which way a default goes, only that it is written and tested.
- **A keyed call may be sent a second time, at once and under its key, when a kept
  connection fails before any answer** (17 Nov 2026, Greg; the same section). It is Go's
  own behaviour, kept on purpose and stated: `performOnce` may be sent again, `perform`
  never, `ask` may be, and each second send is logged.
  - **Why kept.** It is none of the three retries cusoon2 refused in its DEV-714: it
    never follows a timeout, has no delay and keeps the key. Of a charge's three cases it
    turns two that are `Unknown` today into a recorded answer and leaves the third as it
    is. A key already means a repeat is the same call, and the runner relies on that after
    a crash. cusoon2's JVM very probably does the same today, unchosen.
  - Not taken: the second send turned off, the letter of DEV-714.
  - Reopen if a service is found that charges twice under one key, or if a second send is
    ever seen to end worse than `Unknown`.
- **A restriction is carried by a form that looks special, or by a type, and not by a rule
  on how an ordinary call's argument is written** (17 Nov 2026, Greg; `doc/gates.md`, "The
  table is printed"). The `sql` literal and the lists look like Kogo's own, so their
  rules surprise nobody; a call that looks like any function should take what any function
  takes.
  - **First applied to the printed table.** Its `asks` and `acts` lines name the function
    that calls the service, which needs no rule. Naming the endpoint needed its argument
    written as a top-level `val` or a call of a top-level function: a sharper table, and a
    straitjacket on `perform`. Greg took the slightly less useful table.
  - **Rules of this kind already taken**, each because nothing else held what it holds: a
    proof's transaction written only before the dot (`doc/gates.md`), the five SQL calls
    on a `Tx` written before the dot (spec 10.2), and a lambda that fails or acts passed
    straight to a prelude function (spec 9; `doc/gates.md`). Another is recorded here.
    The first was widened on 18 Nov 2026 to the position of a proof's marked `tx` in a
    proof's constructor (below).
- **Each queue is served on its own, one row at a time, and there are no lanes**
  (17 Nov 2026, Greg; `doc/work.md`, "Each queue is served on its own"). cusoon2's urgent
  lane exists because its daemon does one thing at a time; a loop for each queue ends the
  cause, where lanes in Kogo would have carried the work-round into the language.
  - **Why it is safe to try.** Both of the Kotlin's notes on running one thing at a time
    are about one queue, which still has one worker. A deadlock between queues is rerun.
  - **Made in Go alone.** What goes into the Kotlin first is what the side-by-side run
    would see: the schema, a statement, a result. This changes when a row is served and
    nothing of what serving it does, and the runner is no port. The Kotlin's daemon stays
    as it is, so the two differ in timing: a difference Greg accepted.
  - **Not established:** that no two queues rely, unsaid, on not running at once. One
    test stands in for the Kotlin's proof before the runner replaces the daemon: every
    queue side by side, and one at a time over a copy, must leave the same rows.
  - Not taken: two lanes as Kogo's words; lanes the application names, with a stated
    sweep; a pool of workers.
  - Reopen if that test, or production, finds two queues that must not run at once: the
    answer is then to state that pair, not to make every queue wait.
- **The runner says when to alert, and a function of the application's sends it**
  (17 Nov 2026, Greg; `doc/work.md`, "An alert is the application's to send"). Every
  `work` list names one, with no default: it takes the environment and an `Alert` of what
  the runner knows, holds no transaction and is an `action fun`. It is called after the
  commit that failed the row and is best effort, as the Kotlin's is, so nothing goes into
  the Kotlin first.
  - Not taken: each queue's `failed` queuing the alert, a habit of 16 queues and a row
    the Kotlin does not write; the runner sending mail, with the wording in Go.
  - Reopen if an alert lost to a server that was down is ever missed: a row would then
    hold it.
- **The lists are one mechanism, stated once: one rule for a role's shape and one table of
  roles** (17 Nov 2026, Greg; `doc/lists.md`). Six lists and fifteen roles had each been
  designed alone, against principle 6. A function the runtime calls takes the handle it
  runs in, the environment, what it is about and what it works on, in that order; the
  checker fits stages from the table; spec 1 keeps three additions, the third being lists
  (four since DEV-894, with `action fun`).
  `enter` gains the environment, the one shape that broke the rule. The spelling has at
  most two forms, which DEV-894 inherits.
  - Asked for by Greg on agreeing: a deep dive into what the lists are for, how they do it,
    and other ways, more explicit or as magic and expressed differently. The mechanism
    stands until then. Done: kept, below.
- **Kogo's hand-written Go keeps spec 7 itself, and its cases are listed once, in a test**
  (17 Nov 2026, Greg; `doc/work.md`, "How it is tested"). It reads the time and waits only
  through a clock it is handed, the runner's SQL takes the time as a parameter, and every
  boundary can be made to fail in a test. The spec states the runner's rules in prose; the
  full list of cases is a Go test over a table whose rows read as sentences.
  - Not taken: the cases as a table in the spec with a test for each row, two copies of
    one thing; Go's fake time for tests, which keeps off the network where the runner's
    tests need Postgres.
- **That Greg does not read Go is no hard rule** (17 Nov 2026, Greg). Agents write and
  review it, and he will glance at it. The line between Kogo and Go rests on what the
  checker can see, not on who reads: what moves into Go has no `action fun`, no proofs and no
  printed table. So no decision of 17 Nov changes. One earlier reason leaned on it, for
  declaring routes in Kogo (`doc/gates.md`, "Weighed and not taken"): left for the deep
  dive on the lists. Settled below: routes are Go.
- **The lists stay: the runtime calls Kogo through a table, and a list is the one place a
  function gets its role** (17 Nov 2026, Greg; `doc/lists-assessment.md`, choice A). This
  closes the deep dive that `doc/lists.md` and `doc/remains.md` held the mechanism open
  for, and leaves DEV-894 free to spell the lists as valid Kotlin (it kept them Kogo's
  own: below).
  - Not taken: the application writing the pipeline over prelude helpers (`db.gated`,
    `db.effect`). The list would not go away, since the wire names and the TypeScript need
    it; a gate's rules would start at an argument of a prelude call, against "A restriction
    is carried by a form that looks special" above; and an effect written without the
    helper can charge twice.
- **Routes are Go in the application's `main`** (17 Nov 2026, Greg; `doc/lists-assessment.md`,
  choice B; `doc/gates.md`, "Routes that are not operations"). Each route calls an ungated
  operation of an `api` list that `main` does not serve to the browser. This reverses the
  `routes` list agreed earlier the same day: the language loses that list, `Response`,
  `Secret`, `status`, `redirect`, `Request.query` and `Request.body`, and `doc/design.md`'s
  "paths and other routes stay out of the language" stands again. Commands are not
  affected and stay a list. `Json.Companion.parse` stays, for passkeys.
  - **Why.** cusoon2 has two routes, which do not pay for a list, two prelude types, two
    members of `Request` and a section of the spec. The reason the list was taken, that
    Greg does not read Go, is no hard rule (above), and a route's translation is pure code
    at the edge, which the seam's rule below lets be Go.
  - **Lost:** a route's path in the printed file, and the checker's sight of each route's
    translation. What a route writes stays on its operation's lines.
  - **How Go calls the operation** was left open here and is settled: by the emitted
    types, through a handler `main` does not mount (18 Nov, below).
  - Not taken: the `routes` list.
  - Reopen at five routes.
- **The printed table names an operation's outside step on its first line, beside the
  gate** (17 Nov 2026, Greg; `doc/lists-assessment.md`, choice C; `doc/gates.md`, "The
  table is printed"). It is the one part of a role's order the table hid; the rest follows
  from the row's form and an effect's three steps. About 5 / 5. That the gate runs a second
  time after an outside step is left to the spec alone. If DEV-894 takes a call form, the
  prelude's signatures come with it and this still stands.
  - Not taken: only the spec's table; a line of order on every entry, which prints
    `gate, work` on most.
  - Reopen if a reader is found misreading a row's order from the table.
- **A `job` or `effect` that no `work` list names is an error** (17 Nov 2026, Greg;
  `doc/lists-assessment.md`, choice D; `doc/work.md`, "The list of work"). The checker
  refuses it, with a message that says to add it to a list's queues. About 10 / 10.
  - **What it refuses:** a declared queue parked switched off.
  - Not taken: allowed and never run, as the designs had left it, where its rows pile up
    and nobody is told.
  - Reopen if a queue needs to be declared and not served, for a deploy that switches one
    off.
- **Where Kogo ends and Go begins** (17 Nov 2026, Greg). The line rests on what the
  checker can see:
  1. Kogo is required for code that touches the database, acts outside, or decides who
     may do what: the three things its checker guards.
  2. Pure code at the edge may be Go where Go is clearly simpler. Code that Kogo calls is
     an extension, as now (`doc/work.md`, "An application's own extensions"). Code that
     calls Kogo is in `main`, as a route is.
  3. An application's Go reaches the database and outside services only by calling an
     operation. Nothing enforces it: `main` opens the pool, so a route written there
     could query it. It is the rule a reviewer holds `main` to.
  - **Why.** Two rules were written and disagreed on pure application code. "Go holds what
    another application of this shape would need unchanged" (`doc/work.md`, "Hand-written
    Go") put a route's translation and a PDF's layout in Kogo; the entry above, that the
    line rests on what the checker can see, says that hardly matters, since neither
    touches the database, acts, or decides access. This takes the second. The first stays
    as the rule for Kogo's own Go. Application Go that calls Kogo had no rule at all.
  - **The PDF's layout**, laid out in Kogo by the 17 Nov choice (`doc/work.md`, "A PDF is
    data that Kogo builds"), may be reopened under this rule when the PDF is ported, not
    before.
  - Not taken: keeping "what another application would need unchanged" with routes as an
    exception; writing no rule until cusoon2's `main` is on paper.
  - Reopen if a reviewer finds `main` querying the pool or calling a service in practice:
    the rule would then need the checker or the handler to hold it, not a reviewer.

- **A hosted-page charge has no `not_after`** ("simple"; 18 Nov 2026, Greg; `doc/work.md`,
  "An effect someone is watching"; `doc/charge.md`). It is not stamped with a time after
  which it is not made: "An effect someone is watching" (`doc/work.md`, agreed 17 Nov) is
  withdrawn for the charge. The charge behaves as the Kotlin's does today: owed until
  recorded, open while not failed and with no charge id (`hasOpenStripeChargeJob`
  unchanged), a crashed row replayed under its key after the lease, the poll's three
  fields as they are, the reset statement as the Kotlin prints it. `not_after` stays a
  runner's column with its one meaning, used by mail as in the Kotlin. It replaces
  `doc/charge.md`'s Q1, Q2, Q4 and Q5, which it closes.
  - **Why.** Greg, on Q5: the code had become "a nest of gotchas", from handling the
    0.99% and the 0.999% cases; he leans to simple code even where a rare, probably
    immaterial event falls through. Every gotcha of Q1 to Q5 came from this one rule:
    where the 40 seconds comes from, a crashed row stranded past its time, which clock, a
    "never tried" answer, application SQL over the runner's columns, a `tried_at` column,
    and a reset that must not revive a late row. The Kotlin's main cause of a late claim,
    one daemon thread shared with mail, is already gone: each queue has its own loop.
  - **Accepted cost.** When the daemon is down or the charge queue is backed up past the
    page's 40 seconds, the payer is told "Timed out" and the card is charged later:
    minutes, or hours after a long outage. One charge, recorded and emailed; the open row
    refuses a second attempt meanwhile. It is production's behaviour today.
  - **Kept: a waiting payer goes before an invoice run's charges.** `soonest` names a
    column of cusoon2's own, a `timestamptz` that the hosted page's request stamps with the
    handed clock's time and the invoice run leaves null (`payer_waiting_since` in the
    sketches; the name is cusoon2's to choose). The view `find` names carries it. The
    runner orders by it, nulls last, then `next_attempt_at`. It replaces the Kotlin's
    `order by stripe_customer_id is null desc`, and goes into the Kotlin first.
  - **Direction, unchanged.** The request makes the charge itself in an outside step, with
    the daemon as the net, which ends the polling: after the port.
  - Not taken: keeping `not_after` on the charge, with an answer to each of Q1 to Q5 and a
    guarded reset.
  - Reopen if a payer is in fact charged long after "Timed out" and it matters.
- **A row's `not_after` is stamped and judged by the handed clock** (17 Nov 2026, Greg;
  `doc/charge.md`, Q3). What inserts the row binds a time from it, and the runner's claim
  binds its own. No statement that reads or writes `not_after` says `now()`. It is spec
  7's rule and the Kotlin's habit (`JobQueue.kt:386`, `Outbox.kt:36-45`, `:99`), and
  `doc/work.md`'s `enqueueMail` already does it. With no `not_after` on a charge it stands
  for mail alone; the stamp for the charge's order is the handed clock's too.
  - Not taken: the database's `now()` everywhere, which reverses "the runner never says
    `now()`" (17 Nov); two clocks and a margin.
  - Reopen if web and batch run on two machines.
- **`record` gives `Either<String, Unit>`; `prepare` stays as it is** (18 Nov 2026, Greg;
  `doc/charge.md`, Q6; `doc/lists.md`). A `Left` from `record` fails the row as
  unrecorded, with the alert, and is how a step reports an event that is nobody's bug, as
  an invoice voided while its card was charged. A `Left` from `prepare` is a refusal, and
  a broken invariant there, as a tenant with no Stripe account, is a panic, which fails
  the row as not made with the alert. A panic in any step is a bug that the runner
  survives, never a way to report an outcome (spec 8 and 9 unchanged).
  - **Why.** Greg's concern: panics used as exceptions, a Go no-no. Catching one at the
    row's edge is the accepted pattern; raising one on purpose for a normal event is not,
    and a panic for `record` would have had the spec say "`check` is also how a step gives
    up".
  - Not taken: a panic for both; a `Left` for both, with a type `Stop.Refused` /
    `Stop.NotMade` for `prepare`, a new type for a case that is a setup fault.
  - Reopen if a second queue has an expected, alerting stop in `prepare`.
- **The runner calls `enter` at the start of each of a row's transactions** (18 Nov 2026,
  Greg; `doc/charge.md`, Q7; `doc/work.md`, "Discover, then enter"). With the claim it read
  in the first: before `prepare`, before `record`, and before `failed` and `recovery` where
  they run in a transaction of their own. What `enter` sets ends with its transaction, and
  the runner reads the row before it calls a step, so a step cannot do it for itself. It is
  the Kotlin's: each `withRow` binds the tenant (`JobQueue.kt:385`).
  - Not taken: `enter` once and each later step binding for itself: unbound, the runner
    finds no row to hand the step.
  - No reopen condition was given.
- **The runner's key has one text for every effect: `<table>-<id>`** (18 Nov 2026, Greg;
  the text the lead's, 18 Nov; `doc/charge.md`, Q8; `doc/work.md`, "A key comes only from
  the runner"). The effect's table name without its schema, a hyphen, and the row's id as
  Postgres writes it as text, as `stripe_charge_job-0193a1f2-...`. No effect states a
  prefix. The Kotlin adopts that text for the charge and the card job
  (`cusoon2-charge-<id>` and `cusoon2-card-<id>` today) before its effect runner is live,
  so a side-by-side run sends the same header and a row in flight when Kogo takes over is
  replayed under the same key.
  - **Condition, to confirm before the Kotlin changes.** The change is safe only before
    the Kotlin's effect runner is live, or with the charge and card queues empty: a row
    that crashed under the old key and is replayed under the new within Stripe's 24 hours
    is a second charge. `doc/work.md` says that runner is not live. Not checked: what key
    production's present charge path sends.
  - Not taken: `key "cusoon2-charge-"` in the effect, a word of the language to keep a
    string nothing depends on.
  - No reopen condition was given.
- **`kogo.http` logs every refused send** (18 Nov 2026, Greg; `doc/charge.md`, Q9;
  `doc/work.md`, "What Go would decide, Kogo states"). A 3xx or 4xx is logged in Go with
  its method, path, status and body. So a decline's code and request id reach support with
  no word from the application, and what is stored for the payer stays fixed words.
  - To settle when built: what of a refused body is safe to log for every service.
  - Not taken: `Act.Refused(words, detail)`, with the runner storing `detail` in
    `last_error`: it widens `Act` for one caller.
  - No reopen condition was given.
- **A proof's transaction may also be passed to a proof's constructor, as that proof's
  marked `tx`** (18 Nov 2026, Greg; `doc/charge.md`, Q10; `doc/gates.md`, "A proof's
  transaction is used only before the dot"). It is written in two places: as the receiver
  of one of the five SQL calls, and as the argument of a proof's constructor in the
  position of that proof's own marked `tx` (`HeldBooking(id, booker.tenant, booker.tx)`).
  So "making a thing gives its proof" can be written. Still refused: kept in a `val`,
  passed to a function, returned, or passed to a constructor whose property is not a
  proof's marked `tx`.
  - **Why it holds.** The transaction goes from one proof into another, where both rules
    hold it again; only a file that opts in can call the constructor. Not held, as for
    any proof: that the facts put into the new proof are the new row's.
  - Not taken: the new proof holding the old (`booking.from.tx`): two shapes for one
    proof, and the rule on statements would have to say which nested proof it means.
  - No reopen condition was given.
- **`recovery` is not handed the reset statement, and no role gives a label** (18 Nov 2026,
  Greg; `doc/charge.md`, Q11; `doc/work.md`, "An alert is the application's to send"). A
  queue's recovery words refer to "the reset statement below", and the application's
  alert function prints `alert.recovery` and then `alert.reset`, so the statement has one
  source, the runner. A queue that wants its row named in the alert ("hosted-page charge
  for invoice N") says so in its recovery words, which have the row.
  - **Accepted cost.** Step 4 of the charge's words points at the statement and no longer
    contains it. The alert's opening line has the queue, the table and the id and not the
    Kotlin's `label(row)`; for a charge not made, where the Kotlin gives no recovery
    words, ops see no invoice unless `chargeRecovery` gives words for that case too,
    which is cusoon2's to choose.
  - Not taken: `recovery` taking the statement as a fifth parameter; a role `label` whose
    words `Alert` carries.
  - Reopen if an alert proves hard to read.
- **The charge queue is not fair between tenants** (18 Nov 2026, Greg; `doc/charge.md`,
  Q12; `doc/work.md`, "Fair by a column"). It names no `fair`: it says `soonest` of the
  column a waiting payer's row is stamped with, so waiting payers go first across all
  tenants, the longest waiting first, then every other row by `next_attempt_at`. Mail
  keeps `fair tenant_id`. This reverses item 9 of the Ruling on PR 66.
  - **Why.** Fairness is so that one tenant's flood is not felt by another. In mail it is
    felt. In charges the only one who feels a wait is a payer, and with `fair` a payer
    waits behind one unwatched charge for each other busy tenant, where without it never.
    With no `not_after` on a charge (above), the order is the only protection a waiting
    payer has.
  - **Accepted cost.** One tenant's invoice run delays another's until it is done; nobody
    watches either. It differs from the Kotlin's pass, which serves this queue a tenant at
    a time: a difference of timing, of the kind accepted for "Each queue is served on its
    own".
  - Not taken: `fair tenant_id` with `soonest` within a tenant (the Ruling on PR 66, item
    9); fair only among unwatched rows, a new rule of the runner's.
  - Reopen if a tenant's invoice run is ever held long enough by another's to matter.
- **The row a step takes is a data class the application declares, read by the runner**
  (18 Nov 2026, Greg; `doc/charge.md`, Q13; `doc/work.md`, "Discover, then enter"). The
  runner fills it from the effect's table by id, locked, in each of the row's
  transactions after `enter`: the compiler builds the `select` from the class's properties
  by the rule SQL's columns have, the SQL pass checks it against Postgres, and the emitter
  writes its text and scan into the queue's entry. The row is plain columns of the
  effect's own table: no join and no expression; anything more is a query in the step. An
  error code was sketched for a property that is no such column, `effect-row-column`.
  - **The seam, as Greg asked for it stated.** What the runtime hands a function is a data
    class the application declares, filled by name and checked at compile time (an
    operation's input, a command's flags, the claim, the row). Every query a function
    makes for itself is an `sql` literal in Kogo. The runner's hand-written Go names only
    its own columns and the names an effect's declaration states; an application's
    columns appear only in generated Go that Postgres has checked. Day-to-day work on an
    application needs no Go: his test, and it passes.
  - **Why.** Greg's four conditions hold: seamless, type safe, no judgement in the read for
    an author to exercise, and the Go is written once. It keeps a port's steps as the
    Kotlin has them (`JobQueue.kt:389`).
  - Not taken: a role `row chargeRow` of the application's, one more role and function for
    every effect, with the lock the author's to remember; no row type, each step handed
    the claim and reading the row itself, which rested on "every query is a literal",
    stricter than the design is anywhere else.
  - No reopen condition was given.
- **The checker's own rules each live in a file of their own in `check/`** (18 Nov 2026,
  Greg; the Ruling on PR 69; `doc/design.md`, "The checker"). The checker stays one
  package. Kogo's own rules, those on proofs, gates, `action fun` and the lists with their
  roles, each live in a file of their own in `check/`, run after type checking over its
  result, and each has its own error code. The files that check types (`expr.go`,
  `call.go`, `stmt.go`, `types.go`, `decls.go`) hold none of them. The spec states each
  such rule in a section of its own, apart from the general language, as sections 10, 11
  and 15 do. It binds the rules agreed on 17 and 18 Nov, none of which is built; code
  already written is not moved by it.
  - **Why.** The checker is domain specific on purpose, with rules meant to make it hard
    for an agent to go wrong, so the plain type checking is marked apart from them. A
    reader finds a rule in one file, and the type checker can be read without the rules.
  - Not taken: a second package that reads the checker's result, where Go's imports would
    hold the line, at the cost of exporting that result (a guess, about 150 code / 50
    tests) and of changing rule 3's list of components; nothing, each rule written where
    its construct is checked.
  - Reopen if a rule cannot be written over the type checker's result without reaching
    into it.
- **A route calls its operation through a handler `main` does not mount, by the emitted
  types** (18 Nov 2026, Greg; `doc/main.md`, Q1; `doc/gates.md`, "Routes that are not
  operations"). The call goes through a handler of the route's list, so it has a request
  id, a `served` line, the limit and reruns as a browser's call has; and it hands the
  operation its input as the emitted type, so a renamed property or a removed operation
  is a compile error in `main`. The emitter writes a typed value for each operation beside
  its table (`routes.Logout_op`, an `rpc.Op[Env, I, A]`), and
  `rpc.Call(h, r, op, input) (A, error)` runs it; `rpc.SetCookies` writes a `Reply`'s
  cookies. The handler of a list with no check is made by a constructor of its own
  (`rpc.NewCalls`, the session's name) and is no `http.Handler` (the list's check, below).
  About 10 emitter / 25 `rpc` / 50 tests. The spec does not change: a route is not the
  language's.
  - **Why, against the seam.** It is the only option where what Go fills is checked at
    compile time. Its one break, that a property added later is Go's zero, the chain below
    closes.
  - **With it, the lead's:** `rpc.Call` checks an input's enums against their entries, as
    the strict reader does (about 5 / 10 of the above), and `rpc.Request` stays impossible
    to make outside `rpc`, with a line saying so where it is declared.
  - Not taken: by wire name with JSON (`h.Call(r, name, body)`), where the keys and the
    operation's name are strings Go does not check, so a rename shows at the first bounce;
    the route running the emitted function in its own transaction, which copies the
    handler's rules into each route and has `main` open a transaction on the pool,
    against the seam's third rule of 17 Nov.
  - No reopen condition was given.
- **Hand-written Go builds a data class Kogo declares by a chain of named steps** (18 Nov
  2026, Greg; `doc/main.md`, Q13; `doc/design.md`, "The emitter"). The emitter writes the
  chain: one step for each property, named for it, in the class's order, each returning a
  type that has only the next step, and the last returning the class
  (`routes.DeliveryFailure_().MessageId(id).Rcpt(rcpt)...`). A property left out,
  misplaced or misnamed is a compile error in `main`, so the value is fully formed,
  filled by name and checked at compile time, as the seam of 18 Nov asks. Each step's
  type is named for the property it needs (`DeliveryFailure_needsRcpt`, the session's
  name), so Go's error names what is missing. The emitter writes a chain for each list's
  environment and each data class an input reaches; its own Go keeps the keyed literal.
  A guess: 20 emitter / 20 tests, two emitted lines for each property. The spec does not
  change.
  - **In cusoon2** it is used twice: the webhook's input (`DeliveryFailure`, five
    properties) and `Env`, built once in `environment` (six, since settings are one
    property, below). Logout's input is `Unit` and needs none.
  - **Accepted cost.** More to read in `main` than a constructor call (Greg: "My worry
    with c is the verbosity, but it is clearly safer"), and a rename or reorder of a
    property in Kogo needs an edit of `main`, which the build announces. `app.Env{}` still
    compiles: a reviewer holds `main` to the chain, and no check does.
  - **A comment says so at the routes in `main`**, as Greg asked: that the chain is fine
    for two routes and is revisited, with routes in Go, if routes multiply.
  - Not taken: a positional constructor, fully formed and not filled by name, so two
    neighbours of one type exchanged compile (four `String`s in `DeliveryFailure`; three
    `Http` handles in a row in `Env`); the keyed literal, filled by name,
    where a property left out is Go's zero.
  - Reopen (Greg): "if there is an explosion of these hand written Go handlers we will
    need to revisit". He gave no number; the session read it as the five routes that
    reopen "Routes are Go in the application's `main`".
- **Origin and CSRF: the list names its check, and a list with none cannot be mounted**
  (18 Nov 2026, Greg; `doc/main.md`, Q4). They get an issue of their own, on paper first,
  that designs this and nothing wider:
  1. **The list names its check** (`api booker checkedBy sameSite { ... }`; after Q6 c,
     the check that makes both compares, `fromOurPage` in the session's names).
     `sameSite(tx, env, request): Either<String, Unit>` is one Kogo function that holds
     cusoon2's `verifyOrigin` (Sec-Fetch-Site, then Origin against the configured extras,
     the ops origin and the tenant table); after Q6 c the check the list names makes the
     token's compare too (`fromOurPage`, the session's name). The handler runs it before
     every operation of the list, ungated ones and `gatedBy anyone` included. `main` has
     no line for it.
  2. **A list that names no check cannot be served to a browser.** The emitter writes it
     as a Go type the handler cannot mount and only `rpc.Call` accepts, so mounting it is
     a compile error in `main`. In cusoon2 that is `api routes` alone (logout and the
     webhook's operation, which have no Origin check in the Kotlin either); `booker`, `ops`
     and `storefront` each name `sameSite` (after Q6 c, the check that makes both
     compares, `fromOurPage` in the session's names).
  3. **Replaced by Q6 c (18 Nov 2026, Greg; the entry "The list's check makes the token's
     compare too").** It read: "The token is the session reader's. The one Kogo function
     that turns a cookie into a session (`liveSession`) compares `X-CSRF-Token` with the
     row's token, so every gate and login that reads a session has the check and none
     writes a line for it." The token is now compared by the list's check, before every
     operation of the list, and `liveSession` compares nothing.
  cusoon2's `main` serves no browser until it lands. A guess, built: checker 20, emitter
  10, `rpc` 25, tests 40, and spec 15.1 and 15.2, whose "Nothing runs before an
  operation" it amends.
  - **Why.** Each check is written once, in Kogo; no operation, gate or login repeats a
    line or can forget one; and "has the check" is the same set as "served to a browser",
    so the guarantee is by construction and no reviewer holds it.
  - **Accepted cost.** A word on the list; one more issue and PR before code. (It also
    read: "the token check holds only while every read of a session goes through the one
    function, which a reviewer of the gates' file holds and the checker does not"; gone
    with rule 3 under Q6 c, and Q10 b makes the reader `private` to `session.kg`.)
  - **Left to the issue, and answered there** (`doc/origin.md`, DEV-909; recorded by
    DEV-915): the check takes a `Tx` (Q1 a) and runs in its own transaction (Q2 a); a
    refusal is a 403 (Q3 b); the printed table has a line for each list (Q4 c); Origin
    `null` is refused always (Q5 b). Each has its entry below.
  - Not taken: Sec-Fetch-Site in Go in `main` with Origin and the token in the session
    gates, which do not run for a login or a public read, and the login is the case
    `verifyOrigin` was written for (cusoon2 PR 1473); a handler option in `main` that
    calls a Kogo function, which `main`'s author sets for each list; one declaration of
    the check for the whole program, a new top-level form to save two words.
  - No reopen condition was given.
- **The ops site names the storefront it administers in a header, which a shared gate
  reads** (18 Nov 2026, Greg; `doc/main.md`, Q5). The header (`X-Storefront`, the
  session's name) is read with `request.header` by one shared gate of `api storefront`,
  which checks the grant and binds the tenant, as cusoon2's `requireStorefront` does with
  the path's value. The storefront is where a request happens, as the tenant is, which a
  gate already reads from `Host` or `X-Dev-Tenant`; it is no part of an operation's
  input. `Request`, the inputs, the handler and `main` stay as they are, and the list is
  mounted at one fixed path. 0 / 0 in Kogo.
  - **Why.** The Kotlin's route runs the whole tenant schema as an admin (36 queries and
    37 mutations), so the name is stated once and not in each operation.
  - **Accepted cost.** The header's name is a string in the ops site's fetch wrapper and
    in the gate, checked by nobody at compile time: a mismatch refuses every call at the
    first try. The storefront no longer shows in nginx's access log. cusoon2's path
    `/backend/ops/s/{storefront}/graphql` goes; only two service tests call it.
  - Not taken: the storefront in each input, up to 73 inputs and a gate for each, and the
    tenant site's inputs carrying a storefront that means nothing there; `Request`
    gaining the path, a member of `Request`, the spec and a wrapper in `main` for what a
    header already carries.
  - No reopen condition was given.
- **Kogo declares an application's settings and the runtime fills them** (18 Nov 2026,
  Greg; `doc/main.md`, Q7 and Q11; `doc/design.md`, "The server"). A setting is a value
  of the configuration file that Kogo code reads. The application declares its settings
  as a data class (`Settings`), the file holds them in one object (`"settings"`), a key
  for each property, and `main` hands that object to Kogo's strict reader, the one an
  operation's input is read by, in one line that never changes
  (`rpc.ReadSettings`, the session's name); `Env` holds the result as
  one property. Adding a setting is a property in Kogo and a key in the file, and no Go.
  1. **Every setting is stated in every file**: nothing defaults, as nothing does in an
     input. A missing or misspelt key, or a value of the wrong type, stops the start and
     fails `check-config`, naming the key (`settings.opsOrigin is missing`).
  2. **A key the class does not have is reported, at start and by `check-config`, and
     ignored**: one file must serve two versions of the program across a deploy and a
     rollback, and with every key required, refusing extras would leave no file both
     accept.
  3. **Kogo's keys and Go's keys do not share an object.** Go's keys are what makes a
     handle or opens the server (the database, SMTP, each service's address and secret,
     the port), read by `main`'s own struct, which refuses a key it does not know; it
     grows with a service and not with a setting. Inside `settings` the application nests
     as it likes.
  A guess: 20 code in the handler's package, 30 tests, nothing in the checker or the
  emitter, after an issue that writes it into `doc/main.md` and the spec.
  - **Why.** It is the seam of 18 Nov applied to the one data class the runtime hands
    Kogo that hand-written Go was filling, and Greg's test (day-to-day work needs no Go)
    passes for a setting. On paper a setting cost a field of `Config`, an entry of
    `load`'s list and a step of `environment`, and a missing key was a silent zero where
    the Kotlin refuses to start. One object each: were both readers to read one object,
    each would see the other's keys as unknown, so either Go names every setting to skip
    it or neither refuses an unknown key. It also keeps secrets out of what Kogo's reader
    is handed.
  - **Accepted cost.** cusoon2's file changes shape: ten keys move from `deployment`,
    `stripeConfig` and `authConfig` into `settings`, with the Kotlin's reader, the deploy
    script's `jq` and each file moving first. It differs from the Kotlin twice: no
    defaults, and an unknown key is reported where the Kotlin refuses it. Nothing stops a
    secret being put in `Settings`: a reviewer's rule. A setting of a type the reader
    cannot read fails at start, not when Kogo compiles.
  - **Left to the issue:** the reader's Go name and package; enums in settings, left out
    until one is needed; how a prelude class such as `kogo.mail`'s `Address` is read
    (`doc/work.md`, Open); the identity service's public address, wanted by Go for a
    handle and by Kogo as a setting; where the spec states it.
  - Not taken: every key read by Go in `main`, as `doc/main.md` had it; Kogo's classes
    mirroring the file's present sections, which mix the two owners; defaults for a
    setting left out, as the Kotlin has for five.
  - No reopen condition was given.
- **The rest of cusoon2's `main`, answered by the lead** (18 Nov 2026, the lead, not
  Greg; `doc/main.md`, Q2, Q3 and Q6 to Q12).
  - **A handle states its query fields beside its headers** (Q2), so Google's key is
    given to the handle in `main` and no Kogo code holds it; `khttp.New` requires
    `MaxAnswer`. It amends the handle agreed on 17 Nov by the query fields alone. Not
    taken: Google's client adding the key, a secret in Kogo.
  - **`Stripe-Version` is a `val` in `stripe.kg`** that `main` reads from the emitted Go
    (Q3), so the version and the keys read under it change in one file. Not taken: the
    version in `main`.
  - **A queue's lease is a constant of the runner** (Q6). Not taken: the effect's form; a
    map passed to `work.Run`. Reopen when a queue needs a lease of its own.
  - **Go's own keys are read by `main`'s struct, with a list in `main` of those that must
    be present** (Q7). Not taken: the strict reader for them too, since some are optional
    and nothing is optional there.
  - **The clock goes in twice**, in `Env` and to the handler and runner (Q8).
  - **The handler and the runner read Postgres's idle limit from the pool** (Q9).
  - **For Google's answers in CI, `main` points the handle at a local server that
    replays** (Q10). Nothing in Kogo.
  - **cusoon2 moves its keys into `settings` in the issue that writes its `main`**, and
    not before (Q11): the Kotlin's reader, the deploy script's `jq` and each file first.
  - **Waiting for a connection is left at the request's limit** (Q12). Reopen if a full
    pool is ever seen to hold requests.
- **The list's check makes the token's compare too; the session reader compares nothing**
  (18 Nov 2026, Greg; `doc/origin.md`, Q6 c). The function a list names with `checkedBy`
  makes both checks: the Origin's, then the token's. The token's compare reads the
  session's row by the cookie's id and compares its `csrf_token` with `X-CSRF-Token`, a
  missing header reading as `""`; no cookie, or no row, passes. So every operation of a
  mounted list has it, one that reads no session included. `liveSession` compares nothing
  and gives `Session?`. It replaces rule 3 of the Q4 entry ("the token is the session
  reader's"). As shown to Greg, with the session's names `fromOurPage` and `tokenMatches`,
  not decided: `fromOurPage` binds `sameSite`, then `tokenMatches`, and
  `api booker checkedBy fromOurPage`.
  - **Why.** It covers exactly what the Kotlin covers (every request of the three routes
    that carries a session, the 13 public reads and the four bearer links included, an
    expired session and another tenant's included), so cusoon2 does not change for it.
    "Has both checks" is the same set as "served to a browser", by construction: the
    reason given on 18 Nov for the Origin's rule. No gate, login or operation writes a line
    for it or can drop it.
  - **Accepted cost.** One more `select` of the session for each request that carries one,
    which the Kotlin also makes (`checkCsrfToken`, then `ensureSession`). A new list must
    name the right check: a reviewer holds it, on the list's first line. The compare is not
    written in any operation or gate.
  - Not taken: (a) accept that the 17 keep the Origin check alone, sound since they read no
    session, but the port then differs from the Kotlin, which would change first (a guess,
    10 / 3) or the difference be recorded; (b) one line in `anyone` and in each of the four
    bearer gates, five places to forget and one more `select` on each public read. Option c
    was the session's, not among those weighed on 18 Nov.
  - No reopen condition was given.
- **A wrong token is a refusal, where no session is needed too** (18 Nov 2026, Greg;
  `doc/origin.md`, Q7 a). A session whose token does not match the request's
  `X-CSRF-Token` is a refusal wherever it is read, `whoAmI` and the `maybeBooker` reads
  included. **Changed in form by Q6 c, and Greg was told so before he chose it:** the
  refusal is the list's check's, `liveSession` gives `Session?` as `doc/gates.md` wrote it,
  and `whoAmI` and `maybeBooker` bind nothing for the token. The answer as first given,
  `liveSession` giving `Either<String, Session?>` that `whoAmI` and `maybeBooker` bind, is
  replaced by Q6 c, and its accepted cost (a caller's `.getOrNull()` where `.bind()`
  belongs answering a forged request as a stranger) is gone with it.
  - **Why.** It is what the Kotlin does (`checkCsrfToken`, a 400 whatever the operation),
    so nothing in cusoon2 changes first; and a forged request, or a bug of the page that
    drops the header, shows as an error and not as the person being logged out by
    `whoAmI`.
  - Not taken: (b) `liveSession` gives `Session?` and folds a wrong token into "no
    session". A gate that needs a person would answer "not logged in", `whoAmI` would make
    a new session and log the person out, and cusoon2's Kotlin would change first.
  - No reopen condition was given.
- **One reader of a session; every login reads its session in a serializable
  transaction** (18 Nov 2026, Greg; `doc/origin.md`, Q8 b; `doc/gates.md`, "Two kinds of
  transaction"). `liveSession` takes a `SerializableTx` and is the only reader of a
  session. A login, which opens its own transactions, reads its session in
  `db.tx(serializable = true) { }`; one that holds a plain `Tx` there is refused by
  `needs-serializable`.
  - **Why.** It is what the Kotlin does for every login that stays (`ensureSession` and
    `ensureOpsSession`, no lock, in the request's one SERIALIZABLE transaction,
    `CusoonAppState.kt:252`, `:277`, `:299`; the one locked read is the Google callback's,
    `CusoonApp.kt:243`, which goes with the Google login), so a port sends the Kotlin's
    statements. A rerun is safe: no `action fun` runs in a transaction. It was
    first also that the token's compare stays in one function; **after Q6 c** (Greg was
    told) that reason weakens, since the compare is no longer in the reader. What remains
    is one reader, the Kotlin's statements, and no reader without a caller.
  - Not taken: (a) two readers, `liveSession` and `lockedSession(tx: Tx, ...)` with `for
    update`, over one private function: about 6 lines of cusoon2's port for a reader with
    no caller today.
  - Reopen (put to Greg with the recommendation, and not objected to): (a) is taken at the
    first login that cannot read its session in a serializable transaction. None is known.
- **A list's check takes a `Tx`, which the server opens** (18 Nov 2026, Greg;
  `doc/origin.md`, Q1 a). A list's check takes a `Tx` as its first parameter. The server
  opens that transaction for every request of the list, holds it to reading, and rolls it
  back. A check that takes a `Db` is `check-shape`. Code: a read-only transaction in the
  prelude (`kogo.ReadOnlyTx` in the sketch), a guess of 10 / 10 inside the 18 Nov
  estimate.
  - **Why.** It is the gate's rule one step earlier: code the server runs in front of an
    operation reads and decides, and Postgres holds it to that (principle 1). The opening
    is stated by the `Tx` in the signature, as an operation's and a gate's is. After Q6 c
    most requests read (the token for any that carries a session, the tenant's table for
    any tenant's page), so little is opened for nothing. Greg asked whether this was the
    Kogo / Go seam or splitting hairs; the session's answer, which he took: not the seam,
    which is the same in both, mostly splitting hairs and cheap to change later.
  - **Accepted cost.** An empty transaction for a request the check reads nothing for: no
    session, and an Origin absent or matched by a setting (curl, a test, the ops page's
    first call). "Read only, rolled back" is the spec's to say and is not written in the
    check.
  - Not taken: (b) a `Db`, the check opening a transaction where it reads, as the Kotlin's
    Origin read does. Nothing would hold the check to reading, a tenant's page with a
    session would open two transactions unless the author threads one through, and each
    would commit.
  - No reopen condition was given.
- **A refused check is a 403** (18 Nov 2026, Greg; `doc/origin.md`, Q3 b). A check's
  refusal is answered 403, `text/plain`, with the check's words ("Invalid origin",
  "Invalid CSRF token"), and logged by its status. It is not an operation's refusal: the
  page gets a failure, not words to show. Asked after Q6 c as one question, so the token's
  half (d, e) was not asked and is gone.
  - **Why.** A refused check is a forged request or a bug of ours, never the person's
    mistake, so the page treats it as a failure, as cusoon2's clients do today (`rpc.ts:33`,
    Apollo's `HttpLink`). And the `served` line tells it from a body that could not be
    read, which is a 400. No client reads the number.
  - **Accepted cost.** It differs from the Kotlin by the number, so cusoon2's Kotlin
    changes first, under its tests: the two throws of `HttpClientException`
    (`CusoonApp.kt:368`, `:407`) state 403. A guess, 2 / 2.
  - Not taken: (a) 400, as the Kotlin, which the log cannot tell from a bad body; (c) 200
    and `{"type":"refused",...}`, which the page shows as an operation's words, and which
    changes both clients.
  - No reopen condition was given.
  - **The `served` line logs a check's status alone** (18 Nov 2026, the lead; the Ruling
    on PR 72, item 11 of its Review). The log line is 403 for either refusal and the
    handler logs no refusal's words; which of the two it was is in the response. Why:
    Kogo cannot hold a check's `Left` to constants, so logging it would let a check that
    writes `"Invalid origin $origin"` put input in the log. Not taken: (b) logging the
    `Left`, about 1 line of `rpc`. Reopen: a refused check that cannot be diagnosed from
    its status and the response.
- **An Origin of `null` is refused always** (18 Nov 2026, Greg; `doc/origin.md`, Q5 b).
  The check refuses a request whose `Origin` is `null`, whatever `Sec-Fetch-Site` says.
  `sameSite` has no line for `null`: it falls to the last test, where it is no setting's
  origin and no tenant's domain.
  - **Why.** Passing `null` on the browser's `same-origin` or `none` has a hole on Safari
    16.4 to about 17.x, which sent `none` for any request with no referrer (WebKit bug
    256472): another site's `no-referrer` frame posts as `null` with `none`, the request
    PR 1473 was written to refuse. Refusing `null` always closes it, is one line fewer with
    no line to get wrong, and breaks nothing cusoon2 ships: its calls are `fetch()` in the
    default mode, which sends the real Origin, and no page of it sets a referrer policy.
    Answered after research Greg ran, which moved the session's recommendation from a to b.
  - **Accepted cost.** A form post, or a `no-cors` fetch, from a page of our own that sets
    `Referrer-Policy: no-referrer` would get the 403. It differs from the Kotlin, so
    cusoon2's Kotlin changes first, under its tests: `verifyOrigin` loses its `null` branch
    (`CusoonApp.kt:411` to `:416`) and `OriginCheckTest`'s `verify("null", "same-origin")`
    becomes a refusal. About 4 lines fewer, 1 test changed. When cusoon2 makes that fix is
    not decided: it is a hole in production today if the research is right, and need not
    wait for Kogo.
  - Not taken: (a) as the Kotlin, `null` passes when `Sec-Fetch-Site` is there, and the
    hole stays; (c, the session's) `null` passes only with `same-origin`, never `none`,
    which also closes the hole and keeps a line that must be right for a form cusoon2 does
    not have.
  - No reopen condition was given.
- **The check runs in its own transaction** (18 Nov 2026, the lead, not Greg;
  `doc/origin.md`, Q2 a; told to Greg): once for a request, before the request's, read
  only and rolled back. Why: a `Db` operation and one run wholly outside have no
  transaction of the server's for it to join, so (b) needs (a) anyway; and under (b) the
  check reruns with the operation and its reads join a SERIALIZABLE read set. Q6 c makes
  it safe for the token too: a row's token is set when the row is made and never updated
  (`database.kt:1848` is its only write), and a login deletes the row and makes another,
  so a compare made before the request's transaction cannot go stale. Accepted cost: a
  second transaction for each request with a check, one after the other; the token is
  compared once and not on each rerun, where the Kotlin compares it on each. Not taken:
  (b) the request's transaction, in the gate's savepoint.
- **The printed table has one line at the head of each list, and a list with none says
  so** (18 Nov 2026, the lead, not Greg; `doc/origin.md`, Q4 c):
  `api booker   checked by fromOurPage   check reads session, tenant`;
  `api routes   no check: not served to a browser`.
  Why: the absence is what a reviewer looks for. Not taken: (a) without the line for a
  list that has none; (b) a `check reads` line on every operation.
- **The check runs first in `serve`, as the Kotlin** (18 Nov 2026, the lead, not Greg;
  `doc/origin.md`, Q9 b): before the name's 404 and the method's 405, so a stranger's
  request to an unknown name is refused, not a 404. Why: (a) differs from the Kotlin, so
  cusoon2 would change first, or the comparison record a difference, to save an empty
  read-only transaction on a path that is not there. Not taken: (a) after the name and the
  method. Reopen: a check whose cost on a 404 shows in the `served` lines.
- **`sessionId` and the cookie's name are `private` to `session.kg`** (18 Nov 2026, the
  lead, not Greg; `doc/origin.md`, Q10 b), which exports `tokenMatches`, `liveSession` and
  `endSession`. After Q6 c the token no longer rests on "every read goes through the one
  function", so this is cheap tidiness. A second `select ... from session` elsewhere is a
  reviewer's. Not taken: (a) a reviewer alone.
- **A cookie that is no UUID is no session** (18 Nov 2026, the lead, not Greg;
  `doc/origin.md`, Q11 a). It is met first in the check: `tokenMatches` passes it and
  `liveSession` gives null, so the browser is given a new session. Why: under the Kotlin's
  400 a browser holding such a cookie is refused on every request until the cookie is
  cleared, `whoAmI` included. Accepted cost: it differs from the Kotlin, which changes
  first, in the same cusoon2 PR as Q3 and Q5 (a guess, 2 / 1). Not taken: (b) a refusal.
- **`NewCalls` takes an `Unserved` alone** (18 Nov 2026, the lead, not Greg;
  `doc/origin.md`, Q12 a); an operation a route calls is listed in `routes` too (spec 15.1
  lets a fun stand in two lists). cusoon2 has no such route. Not taken: (b) `NewCalls`
  taking a `Table` and skipping its check. Reopen: the first route that must call an
  operation of a mounted list and cannot be listed in `routes`.
- **`Unserved`'s fields are unexported** (18 Nov 2026, the lead, not Greg;
  `doc/origin.md`, Q13 b; told to Greg). The emitter writes `rpc.NewUnserved(ops, enums)`,
  so `main` cannot lift `routes`' operations into a `Table` it mounts; a `Table` of
  operations `main` writes itself, or a check that always passes, stays a reviewer's.
  `NewHandler` still panics on a nil `Check`. Why: rule 2 of the Q4 entry asks for "by
  construction", and this is the most Go gives for about 4 lines of `rpc`. Not taken: (a)
  exported fields.
- **Not asked, and still not** (the lead, 18 Nov 2026): the request's `Content-Type`, left
  "with the CSRF checks"; open in `doc/origin.md`.

## Taken in the v0 spec, not yet discussed

- Markers (`@RequiresOptIn`, `@file:OptIn`) are the one annotation form, so the access walls
  port. A wall is a data class with a marked constructor, as example 19's `HeldBusiness` is.
  `copy` and a sql row no longer get round that constructor (see "Agreed with Greg").
- Value classes are out: cusoon2 has none, and binding distinct id types to SQL columns needs
  a design (foreign keys could supply it).
- No `apply`/`run`/`with`, no labelled returns, no `object`.
- `Regex` is RE2: no lookaround or backreferences.

## Taken building the SQL pass (DEV-818), not yet discussed

- **A name is one parameter, an expression one per occurrence.** `$active` twice is `$1` twice,
  so Postgres types it once and the cast in 10.6's idiom is optional; `${newId()}` twice is
  two values, as a string template would evaluate it. Since DEV-827 the parser tells the pass
  where each parameter lies, so `${...}` holds any expression. There is no way to write the text `$name`, and so no tagged dollar
  quoting.
- **The emitted Go is a shape for the emitter to replace.** (Replaced by DEV-832, which
  removed `sqlpass.Emit` and `kogo-sql emit`; see its section below.) One function per literal over a
  `pgx.Tx`, returning `([]Row, error)` or the rows affected, named `Q<path>L<line>C<column>`
  with `_2`, `_3` added when two paths map to one Go name. The row count rules of the five
  calls and turning an error into a panic belong to the emitter, which knows the call. Every
  scalar parameter is a pointer, since without the checker nothing says which arguments are
  `T?`. `Uuid` is `[16]byte`, `Decimal`, `LocalDate`, `LocalTime` and `Duration` are pgx's
  `pgtype` structs and an enum is a `string`, until the prelude has types. Not handled: an
  array of an enum or of `citext`, which pgx must be taught per connection.
- **An enum's Kogo name is its Postgres name in PascalCase** (`invite_kind` is `InviteKind`) in
  a signature. The spec matches enums by their constants; the checker will do that.
- **Layout.** Module path `kogo` until there is a remote to name it after. `sqlpass` is the
  pass; `cmd/kogo-sql` is the command, and since DEV-819 finds literals through the parser.
- **Errors** are one JSON object a line with a code such as `sql-rejected`; the command exits
  0 when all pass, 1 when a literal fails and 2 when it could not run. Postgres's message is
  kept, with its detail, hint and SQLSTATE, and `$1` spelt as the literal spells it.
- **A parameter Postgres does not see is found by preparing again.** A `$name` inside a quoted
  string or a comment is not a parameter to Postgres, which then refuses the statement as
  unable to type it, the same refusal as for a parameter it sees but cannot type. Each
  parameter so refused is declared `text` and the statement prepared again; the parse tree
  then shows whether the parameter is referred to. Postgres raises that refusal only once the
  rest of the statement is sound, so a different refusal on the second try means the declared
  type was used and the parameter seen. Not taken: lexing the SQL for quotes and comments
  (dollar quoting, `E''` strings and nested comments are Postgres's to know, principle 1).
  Reopen if the extra round trip, one per such parameter, shows in check time.

## Taken building the parser (DEV-819), not yet discussed

- **One package, `syntax`, holds the tree, the lexer and the parser.** The checker and the
  emitter import the tree and always get it from the parser, so a separate tree package would
  only add a name to import. Reopen when something builds trees without parsing.
- **The parser stops at the first fault in a file.** Recovery would guess at what was meant,
  and the guesses produce follow-on errors that an author then has to tell from real ones.
  `kogo-sql` reports the file, checks none of its literals, and goes on to the next. Reopen
  if fixing one error at a time makes the edit loop slow.
- **Line ends follow Kotlin's parser, not its published grammar** (spec 6.1). The grammar
  says a line end may come before `?:`, `&&`, `||`, `.` and `?.` and after any operator; the
  parser also ignores line ends inside `( )` and `[ ]`, and Kotlin code relies on that. Each
  token records whether a line end came before it, and the parser asks only where a statement
  could end. A line end inside a block comment does not count, as in Kotlin.
- **Precedence is Kotlin's, and comparisons do not chain**, as Kotlin rejects `a < b < c`.
  Unary minus is an operator, as in Kotlin, so a negative literal is `-` and a literal.
- **`<` opens type arguments when they parse and are closed by `>`, then `(` or `{` on the
  same line**; the parser tries that and otherwise reads less than. Where it stops:
  `a < b > (c)` is a call, which is also Kotlin's reading.
- **Destructuring in lambda parameters is in**: example 03's `sumOf { (d, w) -> d * w }`
  uses it, and it means the same as in Kotlin.
- **Escapes are Kotlin's** (`\t \b \n \r \' \" \\ \$ \uXXXX`), and raw strings have none.
  A `$` that begins no template is text, as in Kotlin.
- **A surrogate escape is half of a pair, and the pair is one character.** Kotlin allows a
  lone half; a Go string is UTF-8 and cannot hold one, so the lexer refuses it rather than
  emit U+FFFD. A pair is two UTF-16 units, so it is not a `Char`. Reopen if Go output ever
  needs raw UTF-16.
- **`data`, `enum`, `sealed` and the refused modifiers are names unless a declaration
  follows**, as in Kotlin: `data.size` and `value to key` are expressions. A word followed
  by `in`, `is`, `to`, `until` or `as`, or by a line end in a body, is not a modifier.
- **What Kotlin has and the spec does not is refused with a message that names the
  replacement.** Section 14's rows that syntax can see have codes `kotlin-<row>`
  (`kotlin-not-null-assertion`, `kotlin-try`, ...); so do `public` and `protected`, which
  section 2 rules out (`kotlin-visibility`). Others outside the spec (`!in`, `!is`, `as`,
  `===`, `++`, `do`-`while`, an infix call) get code `syntax` and the Kogo spelling. The rows
  that need to know what a name refers to (`println`, `apply`, `runCatching`, `java.time`,
  `Sequence`) are left to the checker, and so is whether an annotation names a marker. The
  parser refuses only annotations that cannot be markers: ones with arguments other than
  `@go("...")`, with a use-site target other than the file's `@file:OptIn`, or inside a
  function body.
- **A sql literal is parsed only as the one argument of `.query<R>`, `.queryOne<R>`,
  `.querySingle<R>`, `.exists` or `.execute`**, called on something; that the receiver is a
  `Tx` is the checker's to say. Its `${expr}` parts are parsed as Kogo, and it ends at the
  first closing quote not inside a `${}`. The parser does not refuse `$1`: the SQL pass does.
- **A file's tree lists its sql literals** (`File.SQL`, in source order), so `kogo-sql` needs no
  walk over the tree. Reopen when a general walker exists anyway.
- **Some section 3 rules are checked by the parser**, because they are about shape: a data
  class's properties are `val`s, its body holds only `override fun`s, an interface's members
  have no body, an enum has no constructor or body, and a type parameter has no bound.

## Taken writing the checker's rules (DEV-824), not yet discussed

- **A type that Go implements is `external class Name<T>`, with no body.** Its members are
  `external fun` and `external val` with the class as the receiver, the one form that can
  also say `List<Long>.sum()` and `String?.isNullOrBlank()`; members in a body would be a
  second way to write most of them. An `external object` of the same name gives
  `Uuid.parse(text)`. `class` is refused everywhere else, so the word marks the type as not
  Kogo's to construct.
- **The prelude's declarations live in `prelude/`, and package `kogo` is in scope in every
  file**, as `kotlin.*` is in Kotlin. The examples already call `listOf` with no import and
  import `kogo.time` names one by one.
- **A statement that is only a value is an error** (spec 6), where Kotlin warns. Section
  6.1 reads `val y = a` and `- b` on two lines as two statements, as Kotlin does, and so
  with `return` and `x`, and `f` and `{ x }`; this makes each of them an error instead of a
  silent misreading.
- **The branches of an `if` or a `when` used as a value need a common type** by the same
  small rule as type arguments (spec 5.1), since there is no `Any` for them to widen to.
- **Type arguments are written all or none**, as in Kotlin.

## Taken building the checker (DEV-825), not yet discussed

- **A binding may take `vararg`** (spec 13), the third thing only a binding may write:
  `listOf(10, 1, 3)` cannot be declared without it, and 5.1's "common type of several
  arguments" assumes it. Application code still takes a `List`.
- **A binding with a receiver is a member, in scope with no import** (spec 13). Example 20
  calls `clock.now()` importing only `Clock`, as Kotlin code does of a class's members.
  An import of one is an error that says to remove it, not "declares nothing named", which
  would be false.
- **A package is the files that share a package line** (spec 2); directories are not
  checked. `examples/` holds three packages in one directory.
- **A range is not a value** (spec 6): `a..b` and `a until b` are written only after `in`.
  Section 3 has no range type and this keeps it so, at the cost of `(1..10).map { }`. No
  example or stage-1 port writes one elsewhere. Reopen when a port wants to pass one.
- **The common type is the wider of two types when one fits the other** (spec 5.1), as well
  as the sealed interface of two variants. As first written, `Int` and `Int?` had none.
- **`==` needs one side to fit the other, `?` aside** (spec 3). This is the rule that makes
  `Int == Long` an error and lets `x == null` through.
- **A nested variant is named in full, `Outcome.Ok`, everywhere** (spec 3.2), also inside
  its sealed interface, where Kotlin allows `Ok`: one table of names for each package.
- **`when (x)` over a type that is not sealed, an enum or `Boolean` needs `else` only when
  used as a value**, as in Kotlin (spec 6). An `if` with no `else` that ends a lambda whose
  result nothing wants is a statement, as in Kotlin.
- **A top-level `val` states its type** (spec 5): "inference works inside a function body
  only", applied to vals. Both stage-1 ports already do.
- **Overloads: a local hides what is outside; otherwise every function of the name that
  the file can see is a candidate** (spec 5.2), its own package's, imported or the
  prelude's alike, and another file's `private` one is not. A value's property comes before
  an extension val, and its interface's `fun` before an extension fun, as in Kotlin. Where
  the receiver, the count and the names leave one candidate its parameters type the
  arguments; where they leave several, each argument is read on its own, and a
  whole-number literal still fits a `Long` parameter.
- **The facts are tables keyed by tree node** (`check.Info`): `Types`, `Uses`, `Calls`,
  `Narrowed`, and what each declaration declares. `Calls` also holds a read of an
  extension val and an operator that is a binding, so the emitter has one notion of a
  call. A failed expression has an unknown type that fits anything, and an error is
  reported once, at the start of the expression it is about.
- **The checker knows four prelude types by name**: `List`, `Set` and `Map` for `for`,
  `Pair` for destructuring and for a map's entries, `List` for an enum's `entries`.
- **The tree marks a trailing lambda** (`Arg.Trailing`): it fills the last parameter, where
  a lambda inside the parentheses fills the next one, and the two differ when a default is
  skipped.
- **Sections 9 to 11 are named, not checked**: `Either`, `Raise`, `Tx`, `Db`, the forms of
  section 9, a sql literal and a marker each give `not-yet-checked`. DEV-826 and DEV-827
  replaced that.
- **A file that does not parse stops `kogo check`**: its syntax error is reported, the
  other files are parsed, and none is checked. Checked without its declarations, the
  others show false errors, each naming something that is declared. Not taken: checking
  them anyway (one typo, many wrong errors) or a parser that recovers past an error (its
  size, to save one rerun). Reopen if ports show syntax errors fixed one rerun at a time.
- **`check.Check` takes the prelude's files apart from the program's, and only the
  prelude's may declare a binding** (spec 13). `kogo check` loads the prelude itself, so
  it knows which is which. Not taken: allowing `external` in package `kogo` and under it,
  which any file could claim with its package line. An `external` in a program's file is
  refused where it is declared; with it go the nullable receiver, `out` and `vararg` that
  only a binding may write. Reopen if a program needs Go bindings of its own.

## Taken checking expected failures (DEV-826), not yet discussed

- **The forms of section 9 are Kogo declarations in the checker's own file, `check/core.kg`,
  with `Either` and `Raise`** (spec 9). A raise scope acts as a `Raise<E>` receiver that every
  call inside it can use: `raise`, `ensure` and `ensureNotNull` are funs with that receiver, checked as any call is, and `either` takes a block of type
  `Raise<E>.() -> A`. What is code: `bind` (it needs both its `Either` and a scope), the
  scope's own rules, and the facts, about 170 lines. This keeps the agreed "built into the
  checker": the file is the checker's, read before the prelude, and no prelude can change
  it. Not taken: a special case in Go for each form, each settling its own type arguments.
  The checker knows `Either` and `Raise` by name, as it knows `List`. Reopen if a form's
  type cannot be written in Kogo.
- **The raise scope is apart from `this`.** Inside `either { }` in an extension, `this` is
  still the extension's receiver. This is not as in Kotlin, corrected in DEV-833: there
  `this` inside `either { }` is the `Raise`, and the extension's receiver is reached
  unqualified or by a label. What follows: `this.raise(...)`, `this.ensure(...)` and
  `this.ensureNotNull(...)` on a member of `this` are refused (spec 9), the one place the
  same text is valid in both and means something different. `this.name` on any other
  member is accepted by Kogo and refused by Kotlin, so it differs in nothing that both
  accept; nothing is done about it. A bare `this` passed as a value is the receiver in
  Kogo and the `Raise` in Kotlin, which accepts it only where a `Raise` is wanted, and no
  program can write one since DEV-833. Reopen if a program can again.
- **A lambda inside a raise scope is in it; a local fun is not** (spec 9). Kotlin allows
  both. The lambda is Arrow's habit (`ids.map { load(it).bind() }`), and the facts mark
  each such lambda as able to fail, so the emitter knows. A local fun's failures are read
  from its signature, like any fun's, and do not depend on where it is declared. Reopen if
  ports nest helper funs inside `either` blocks.
- **A function type with a receiver is refused in programs** (spec 5); only `check/core.kg`
  writes `Raise<E>.() -> A`. Allowing it would make calling a value of that type a call
  that fails, about 15 lines, and no example needs it. Accepted cost: framework code that
  takes `Raise<String>.(C, I) -> A` bodies, as cusoon2's RPC does, takes
  `(C, I) -> Either<String, A>` instead. Reopen when a port's framework code needs it.
- **`bind` takes an `Either<E2, A>` where `E2` fits the scope's `E`**, checked by hand,
  since nothing in its signature ties it to the scope. An `Either` of one variant is then
  bound where its sealed interface is raised.
- **A call can fail when it makes the raise scope it is in fail** (`Info.Fails`): `bind`,
  `raise`, `ensure` and `ensureNotNull`. A call that gives an `Either` value cannot; `bind`
  on its value can. A fun can fail with `E` when it returns `Either<E, A>`; a lambda when it
  is a raise scope's block or holds a call that fails into the scope around it. A call
  records the `E` it fails with (`bind` of an `Either<S.A, X>` where `S` is raised fails
  with `S.A`); a lambda records the scope's. Reopen when the emitter wants the callee's
  notion on the call too.
- **A lambda that returns an `Either` is not in `Info.Fails`.** Its type says so already,
  and with it in, one entry held two facts: a lambda that binds into a `String` scope and
  returns `Either<Int, X>` was recorded only as `String`, so the emitter could not tell a
  lambda that must short-circuit from one that gives a value. Reopen if the emitter wants
  the lambda's `Either` there.
- **A lambda that fails into the raise scope is passed straight to a call** (spec 9).
  Kept in a `val`, returned, or passed where a type parameter is wanted (`listOf({ ... })`),
  it could run after the scope has ended, where nothing catches its failure. The rule is
  where the lambda is written, about 8 lines. Accepted holes: a callee that keeps its
  function parameter, in a data class, say, can still run it later, as a non-inline Kotlin
  function can; and a value of a function type called with such a lambda is refused,
  though it may run it at once. Closing the first needs an effect on function types.
  Reopen when a port keeps such a lambda, or the emitter finds a callee that does.
- **`Either` is built only with `left()` and `right()`**: the variants' constructors are
  `internal` to package `kogo`, and `Either.Left(e)` is refused with a message naming the
  two; any other hidden constructor gets `not-visible`. Kotlin accepts it; one way to build an `Either` leaves the emitter one form.
- **The functions on an `Either` value are bindings in `prelude/either.kg`**, and the
  checker's tests read that file as it is, so they check the real declarations. The
  prelude also gained `String.lowercase` and `String.contains` for example 07.

## Taken writing the emitter's design (DEV-829), not yet discussed

- **A data class is a Go struct passed by value, an enum a named string, and `Either` an
  interface with two variants** like any sealed interface. The enum follows Postgres,
  which sends one as text.
- **Ids, instants and dates are our own small Go types.** Go's time value carries a
  location and a monotonic reading that break comparison by value.
- **One naming rule gives every overload its Go name**, the prelude's and a program's; its
  spelling is under DEV-830 below.

## Taken checking SQL and markers (DEV-827), not yet discussed

- **The checker reads the SQL pass's own `Statement`** for a literal's signature, and the
  facts carry it for the emitter (`Info.SQL`, with what each parameter binds and the
  property each column fills). Not taken: a copy of the type in `check`, which would keep
  pgx out of its build at the cost of a conversion. `check` still runs with no database: a
  test answers with signatures of its own, written as the literal's text.
- **A data class `R` may take any number of columns; a scalar `R` takes one** (spec 10.3).
  As written, a one-column statement could only fill a scalar, so a one-property data class
  was refused. A data class `R` written nullable is refused: a row is never null.
- **A property takes the column named by its name in snake case**, `legalName` from
  `legal_name`, compared exactly; a quoted mixed-case alias fills nothing. Of two columns
  with one name, the second fills nothing and is reported.
- **Inside a nullable nested data class, a column that may be null fills a property that
  may not** (spec 10.3): on the outer join's absent side every column is null. Accepted
  hole: a row where only some of them are null passes the check, and the emitted code must
  panic on it.
- **An enum maps by its set of constants**, in any order, whatever either side calls the
  type. A list's elements may be `T` or `T?`, since Postgres does not say whether an array
  holds a null.
- **`db.tx` is two declarations in `check/core.kg`**, one with a plain block and one whose
  block is a raise scope, and the checker keeps the second exactly where the enclosing fun
  returns an `Either<E, ...>`, settling `E` from it (spec 10.7). So `val r = db.tx { ... }`
  in such a fun is an `Either` too, and dropping it is an error. In an `either { }` of a fun
  that returns no `Either` the plain one applies, and its block is in that raise scope as
  any lambda is.
  `tx.atomically` is one plain declaration for the same reason. Not taken: choosing by the
  type wanted, which `val r = db.tx { }` does not state.
- **Markers count only the file's opt-in** (spec 11). Kotlin also lets a marked declaration
  use another of the same marker, and its declaring file needs no opt-in; example 19 opts in
  all the same. One rule, checked at each call, constructor and read of a val. A type
  carries no marker, and `@RequiresOptIn` goes on nothing but a marker.
- **A lambda whose value is a wanted `Unit` may end with `Unit`**: it is the lambda's
  value, not a statement (spec 6). Example 18 ends `db.tx`'s block so.
- **Where an error is reported**: Postgres's refusal at the place it names, a parameter's
  wrong type at the parameter, and a column's fault at the literal, since Postgres does not
  say where a column is written; the message names the column and the property.
- **`kogo check` connects only when the program holds a sql literal**, to `$DATABASE_URL`
  as `kogo-sql` does. A failure to ask the database stops the check with exit 2. The test
  that goes through the SQL pass to the test database is `cmd/kogo`'s, over the examples;
  the checker's own tests need none (`doc/design.md`).
- **The five calls need their `Tx` written before the dot.** In a fun on `Tx`, a bare
  `execute(sql"...")` is refused with a fix naming `this.execute`. No example or port calls
  one on an implicit receiver, and taking it would have the checker and the emitter both
  find that receiver. Reopen if a port writes helpers as funs on `Tx`.
- **The examples changed to pass**: 12, 13 and 16 import `kogo.time.Instant` (spec 2), and
  19 is `package example.markers`, since 12 also declares `Business`. The prelude gained
  `Uuid`, `newId`, `List.size` and `check`, which they use.

## Taken building the emitter's first slice (DEV-830), not yet discussed

- **The naming rule.** A top-level Go name is the Kogo name with its first letter in upper
  case, so that a driver in another Go package can call it, and never ends with `_`. A type
  is its name and a nested one joins with `_` (`Outcome_Ok`); an enum constant is
  `InvoiceStatus_draft`. A top-level val and a fun with no receiver are `Name`. Anything
  with a receiver is `Receiver_Name`, the receiver's type without its arguments: an
  extension (`Booking_SupplierPay`), an extension val, a binding (`List_Filter`,
  `String_Length`, `T_Let`) and an object's fun (`ZoneId_Of`). Where two declarations of
  one package come out the same, each fun among them is spelled in full, its receiver with
  its type arguments and then each parameter's type (`String_Substring_Int_Int`,
  `ListT_SumOf_TToLong`, and `Regex_String` for the fun `Regex(pattern)` beside the type);
  one with no parameters adds `_fun` and a val `_val`; a type keeps its name. Still the
  same is an error, `go-name`. A `private` declaration adds `_f` and its file's number in
  the package, since two files may each have one. cusoon2 has 5 overloaded funs, 5
  overloaded extensions and 8 funs named as a type of their package among 161: the other
  143 get the name a driver would guess. Not taken: always spelling the types, which is
  one rule with no neighbours to look at and gives `JobWorth_Booking_ListBookingExtraCharge_Boolean`.
  Accepted cost: adding an overload renames the fun it joins, which every Go caller sees
  as a compile error. Reopen if generated callers (the RPC layer) want names that do not
  depend on the neighbours.
- **Inside a body a name is the Kogo name with `_` after it** (`price_`, `this_`): a local,
  a parameter, a local fun and a type parameter, numbered where a declaration has two of
  one name, so that Go's scopes never decide what a name means. Temporaries are `t1`,
  imports `p1`. No local can then be a Go keyword (`type`, `func`) or hide a top-level name.
- **A property is a Go field, and an interface's val a method `GetName()`**, since Go
  refuses a field and a method of one name; a data class that implements the interface
  gets the method. Kotlin has the same clash between `val label` and `fun getLabel()`;
  a property `getLabel` beside it is refused (`go-name`), since the struct would then not
  implement the interface.
- **Layout.** `kogo emit <dir> <out>` writes one Go file for each Kogo package, `a.b`'s at
  `<out>/a/b/kogo.go` in Go package `b`, and nothing else. The import path of `<out>` is
  read from the `go.mod` above it, and the Go imports `kogo/rt` and `kogo/prelude/...`, so
  a program's module requires this one. Not taken: copying the runtime into `<out>`,
  which would free a program from this repo and is about 40 lines. Reopen when a program
  is built outside this repo.
- **`rt` apart from the prelude's Go** (principle 8). `rt` is what the core needs: `Unit`,
  `Nothing`, `Char`, the walk behind `==` and `toString()`, and the emitter's helpers.
  `prelude/kogo` and `prelude/kogo/time` are the Go of Kogo's packages `kogo` and
  `kogo.time`, each binding named by the naming rule, so the emitter has no table of them.
  A type with an `out` parameter has a `Name_widen` beside it (`List_widen`, `Pair_widen`),
  which the emitter calls to use it as a wider type.
- **`Unit` is a Go value, and every fun returns one.** `(T) -> Unit` and `(T) -> R` are
  then one Go type, which `x.let { ... }` needs. `toString()` gives `kotlin.Unit`, as in
  Kotlin.
- **`when` is one Go `switch` over conditions for every subject**: `is` is a type test,
  a constant an `==`, a range a call. `doc/design.md` had a type switch for a sealed
  subject; that is a second path for the same result, and `null`, constants and `is` mix
  in one `when`.
- **A sealed interface is a Go interface with no methods.** A variant then needs no
  marker, and an `Outcome<Nothing>` is an `Outcome<Int>` by Go's own conversion.
- **A data class's struct carries its Kotlin names**: a blank first field tagged with the
  class's name and each field tagged with its property's, which `toString()` reads. Not
  taken: a generated method for each class.
- **`==` inside a value compares a `Double` as Kotlin's `equals` does**: `NaN` is equal to
  `NaN` and `0.0` is not equal to `-0.0`. Between two expressions that are each a `Double`
  or a `Double?` it is IEEE's, as in Kotlin: Go's `==`, or `rt.EqualDouble` once either
  may be null. A value that holds a function is not compared: see "Agreed with Greg".
- **Arithmetic on two literals is kept from Go's constant folding**: the left one is read
  through `rt.Val`, so that `2147483647 + 1` wraps and a `Double` gives `Infinity`, as at
  run time. A minus on a whole literal is one literal (`int32(-2147483648)`); on anything
  else that Go reads as a constant it is a run-time minus, since Go's constants have no
  `-0.0`.
- **A constructor's default is written at the call with the call's type arguments**: for
  as long as it is written, each type parameter of the class names the Go type of its
  argument. A type parameter with no Go name where it is written stops the emitter: it is
  the emitter's mistake, never Go with a name missing.
- **Evaluation order.** Go orders calls left to right and nothing else, so what must be
  ordered is a call: a read of a local `var` (`rt.Val`), since a lambda called later in
  the expression may assign it, and `/` and `%` (`rt.Div`, `rt.Rem`), which panic. A
  call's operands written out of the order of their slots (named arguments, `x in c`, a
  constructor's defaults) are read into temporaries first. Not ordered: nothing known.
- **A parameter with a default is a pointer in Go**, nil when the call leaves it out, and
  the fun gives it its default, which may read the parameters before it. A constructor's
  default is written at the call, since it can read no parameter.
- **`return`, `break` and `continue` in a value's place: `n += x ?: break` is refused**
  (spec 6), since `n += v` is `n = n + v` and the value is an operand. A `break` inside a
  loop that is itself inside the larger expression is allowed. `break` and `continue`
  are written with their loop's label, because a `when` is a Go `switch`.
- **The checker's facts gained three things**: a name that reads a member of a narrowed
  `this` is in `Narrowed`, with the type `this` is narrowed to; `Info.Core` is the
  checker's own file; and `check.Subst` gives a declaration's type as one use reads it.
- **Refused as not yet emitted**, each with its position (`not-yet-emitted`):
  - what can fail (spec 9): a fun, a lambda or a call in `Info.Fails`, and a call of any
    fun of `check/core.kg`. The functions on an `Either` value have Go already, since
    `prelude/` declares them.
  - SQL and transactions (spec 10): a `Tx` or a `Db` wherever one is written, and each of
    the five calls. Spec 7 puts one of the two in every fun that touches the database.
    (Emitted since DEV-832.)
  - a type argument that is nullable where the type parameter is used as `T?`
    (`x.takeIf { }` on an `Int?`): two pointers in Go, and the outer one would not be null
    when the value is.
  - a type argument settled as `Nothing` (`none(null)`), so that no value's Go type holds
    `Nothing` where its declaration holds a type parameter; that is what lets an
    `Outcome<Nothing>` be used as an `Outcome<Int>` for free.
  - a sealed value used as one whose type argument is another Go type (`Outcome<Ok<Int>>`
    as `Outcome<Outcome<Int>>`): the value keeps the Go type it was made with, and an `is`
    test would miss it. A list is copied instead; a sealed value would have to be rebuilt.
  - an interface that implements another or has members and an `out` parameter, a
    reference to a fun with a default, a `?.` call of a property that holds a function,
    and a `for` over a `Map` or a `Set`. A local fun or an interface's fun with type
    parameters, and an interface's fun with a default, were here; since DEV-846 the
    checker refuses them (see "Agreed with Greg").
- **A marker is written nowhere** (spec 11): the checker has enforced it, and it does
  nothing at run time, so a program with markers is emitted, not refused.
- **Not handled**: nothing left of the first list. A literal outside its type's range
  and two top-level vals that read each other are refused by the checker since DEV-846,
  and two Kogo packages that import each other since DEV-844 (see "Agreed with Greg").
- **`lowercase()` is Go's `strings.ToLower`**, stated in spec 12 as a corner where the
  prelude differs (principle 3): matching `İ` and a final sigma means Java's case tables,
  and no ported module depends on them. `uppercase()` is not in the prelude yet, so it
  has no corner to state.

## Taken making `Either` the only spelling (DEV-833), not yet discussed

- **Example 08 is deleted and the gap left.** Renumbering 09 to 21 would rename thirteen
  files that the tests, the parser's tree and open branches name; a gap costs nothing.
  Reopen when the examples are next reorganized.
- **The refusals' codes**: `raise-receiver` for `Raise<E>` written anywhere in a program,
  reported at the type, so at the receiver of a fun; `kotlin-context` from the parser for
  any `context(...)` before a declaration or a local fun, since Kogo has no context
  parameters of any kind, while a call of something named `context` is still a call;
  `with-error` for a call of `withError`, from the table that refuses Kotlin's scope
  functions. Not taken: keeping `withError` in `check/core.kg` only to refuse it.
- **A fun whose receiver is refused says no more of what its body raises**, so a ported
  receiver function gets one error, not one for each `raise` in it.
- **In a raise scope, a member of `this` does not hide `raise`, `ensure` or
  `ensureNotNull`** (spec 9), though it hides any other extension (5.2). Hiding them, as
  the issue asked, would make `either { raise("no") }` in a fun on a type with its own
  `raise` give `Right` where Kotlin gives `Left`. Both are candidates: where one fits it is
  the call; where both do, `ambiguous-call` says to rename the member. Not taken: ranking
  the scope's first, as Kotlin does, since spec 5.2 ranks nothing, and suggesting
  `this.raise(...)` for the member, which in Kotlin is the scope's. The choice among
  overloads by the scope's `E` went with receiver funs.
- **A name Kogo refuses from Kotlin is refused only where no package declares it.** A
  program's own `withError`, or `run`, in a package the file does not import gets the
  "add import" hint, not "is not Kogo". One order for the whole table, not a case for
  `withError`.

## Taken refusing three programs (DEV-834), not yet discussed

- **`out` is checked by Kotlin's whole rule**, not only a parameter's type (spec 3.2): a
  result or a `val` of type `(T) -> Unit` or `Map<T, Int>` lets the same `null` in, and
  the walk over positions costs the same. The fix the error gives is an extension fun.
- **Two declarations of one name clash on their kind, receiver and parameter types**
  (spec 5.2): a declared type by what it is, not its printed name, and a type parameter by
  its first use there, so `fun <T, U> h(a: T, b: U)` and `fun <U, T> h(a: T, b: U)` clash.
  Parameter names do not separate them, though a call could choose by name: Kotlin refuses
  the pair, and the emitter would have to tell them apart in Go. Two `private` ones in
  different files do not clash, as a test of section 2 already had; a fun and an extension
  `val` of one name do not either. A type that failed to read clashes with nothing, so its
  error is the only one. Reopen if the emitter cannot give private overloads in two files
  their own Go names.
- **An interface's funs clash among themselves, and a fun clashes with a constructor**
  whose type has its name and whose parameter types it has. The second is reported at the
  fun, before or after the type: the fun can be renamed without renaming the type.
- **Bindings with a receiver clash across packages whatever their visibility.** A call
  sees every one of them (spec 13), `internal` or not, so two internal ones of one shape in
  different packages make the call ambiguous. The review of this PR said to let them pass;
  that would move the error back to the call.
- **A data object that cannot implement a `val` keeps the code `override`**, at the
  declaration as the other `override` errors are; only the fix changes.
- **Not refused yet**: the receiver of a function type, `T.() -> R`, is a place a value
  is taken, and an `out` type parameter there is not checked. No program can write one
  (DEV-826; DEV-833 removes the last way in), so there is nothing to refuse. Reopen if
  receiver function types return. Also `private` or `internal` on a binding with a
  receiver, which is accepted and hides nothing.

## Taken emitting expected failures (DEV-831), not yet discussed

- **A raise scope's block is a Go function whose first parameter is the scope's handle**,
  a `kogo.Raise[E]`, and `either` is an ordinary call of the prelude's Go, under the name
  the naming rule gives it (`Either_RaiseEToA`, beside the type `Either`). The lambdas
  inside capture the handle. `raise`, `ensure`, `ensureNotNull` and `bind` are given the
  handle of the innermost scope they are written in, which is the one the checker read
  them against. Not taken: writing each block inline with its own `defer`, more emitter
  for the same Go. The raising `db.tx` can take its handle the same way.
- **The forms fail with the scope's `E`, converted where they are written.** `raise(e)`
  is called with the scope's `E` as its type argument and `e` converted to it; `bind` is
  given a function that widens its own `E` to the scope's. The scope then only asserts the
  value's Go type, and an `Either<S.A, X>` bound in a scope of `S` gives a `Left` of `S`.
- **A panic a scope does not own is panicked again, unchanged**: another scope's failure
  and a bug alike. Go keeps the frames of the first panic in the trace, so it still names
  the Kogo line, and a driver recovers the same value; a test shows both.
- **The handle points to a struct of one byte**, since Go may give every new value of an
  empty struct the same address, and two scopes would then share a handle.
- **An `Either` used with wider type arguments is rebuilt** by the prelude's
  `Either_widen`, as a list is copied by `List_widen`. `StoreError.Missing.left()` settles
  `E` as `StoreError.Missing`, and example 09 returns it as an `Either<StoreError, String>`;
  DEV-830 refused that for every sealed type. Other sealed types are still refused: no
  example needs them, and rebuilding one means code for each of its variants.
- **An `Either` writes itself as Arrow does, `Either.Left(value)`**, where the rule for
  data classes gave `Left(value=...)`. `"$result"` is valid in both languages and must
  read the same (principle 3).
- **A lambda that fails into a raise scope is passed only to the prelude's funs**, which
  run their blocks before they return. A fun of the program's could keep the lambda, as
  `fun <T> later(b: () -> T) = b` does, and a failure raised after its scope has returned
  would be a panic no scope recovers: a 500 where section 9 promises a value. Kotlin
  allows it, and Arrow throws when it happens. Not taken: following each function-typed
  parameter through its fun's body to see that it is only called, which makes a call's
  legality depend on a body elsewhere (principle 2). Reopens when a program needs its own
  fun to run a failing block, and then by Kotlin's marker on the signature, `inline fun`,
  which forbids keeping the parameter, at the cost of a keyword Kogo does not have.
- **The prelude's Go is safe to unwind through**, a rule stated at the top of
  `prelude/kogo/kogo.go`: a function that calls a lambda holds no lock and leaves nothing
  half done while it runs. Every one there already keeps what it builds to itself until it
  returns.

## Taken mapping composite types (DEV-837), not yet discussed

- **A field keeps its column's `NOT NULL` only in a table's whole row**, `b`, followed
  through a subquery or a `group by` as a column is. A column whose type is a table's row
  type, a cast as `null::tenant` or a function's result has every field nullable, at any
  depth: a table's constraints do not cover its row type stored elsewhere. The issue said
  "a field of a table's row keeps its column's NOT NULL" without the distinction.
- **Inside a composite, a field that may be null never fills a property that may not**,
  even in a nullable data class. Postgres sends the absent side of an outer join as NULL,
  not as a row of nulls (`(,)`), so unlike the prefix form, a null field is a row that is
  there with a null in it. Reopen if the run-time reading (DEV-832) finds otherwise.
- **A column of the property's name wins over the prefix form**: `business` fills
  `val business: Business` whole, and `business_id` beside it is then unused. A scalar
  column of that name is a type mismatch, not a reason to try the prefix.
- **A nested data class with no column at all is one error**, saying to select the whole
  row as `b as business`, and an unused column named after one of its properties is then
  not reported: under `select b.*, t.*` those are the ones the whole row replaces, and an
  error for each would bury the fix. Other unused columns still are. Before, it was one
  error per property. A column that a sibling property takes by name, as `business_id`
  for `businessId`, does not count as the prefix form of `business`.
- **Refused as `sql-unmapped-type`**: a parameter of a composite type, or one Postgres
  infers as `record` (`where total = $total`), since there is no way to send a data class;
  an array of a composite type, deferred with `array_agg(t)`; a composite type with no
  fields, since no data class has none; one with a field outside 10.4, naming the field.
  The `record` column keeps the code, with "select from a table, or name the columns".
- **New codes `sql-field-missing` and `sql-field-unused`**. A field's type and
  nullability keep `sql-column-type` and `sql-nullable`, said of "field f of column c",
  without the cast or `"name!"` fix, which Postgres has not for a field.
- **Not done: reading a composite at run time.** DEV-832 is not done, so the issue leaves
  it out; `sqlpass.Emit` refuses a composite column, as `kogo-sql emit` then does with
  `sql-emit`, rather than write Go that does not build. (DEV-832 reads one, and removed
  both.)

## Taken emitting SQL and transactions (DEV-832), not yet discussed

- **A row is read by Go the emitter writes, not by reflection.** Each of the three query
  calls passes the prelude's Go a function that scans every column into a variable of its
  own and builds `R` from them by the checker's paths. Go's compiler then checks each row
  type against its columns, which a run-time walk of `R` would find only when the
  statement runs. Not taken: one reflecting reader in the prelude, a smaller emitter and
  the same logic unchecked.
- **A column that may be null is read as a pointer, any other as it is.** The driver
  refuses a null read into what is not a pointer, and that refusal is the panic of a
  column named `"name!"` (spec 10.5): the SQL pass strips the `!`, so the emitter cannot
  tell such a column from one Postgres declares `NOT NULL`, and need not. Inside a
  nullable nested data class every column is a pointer; the data class is `null` when all
  are, and a null beside one that is not panics, naming the column (spec 10.3, the hole
  DEV-827 accepted). A `List` is read as a pointer whatever its column says: the driver
  reads a null into a slice as an empty one, with no error, so `NotNull` panics instead.
- **The prelude's types take themselves to and from the driver's**, by the interfaces pgx
  reads and writes its own types through, so a `Uuid`, an `Instant` or a `Decimal` is an
  argument and a scan target as it stands, alone, in a `T?` and in a list. Each refuses a
  null. `prelude/kogo` and `prelude/kogo/time` now import pgx's `pgtype`, and `Db` its
  `pgxpool`, which brings pgx's own `puddle` and `x/sync` into `go.mod`.
- **`Decimal` is the text Postgres writes**, `12.50`, `NaN`, `Infinity`: a Go string. Two
  are equal when their text is, so `1.5` is not `1.50`, as with Java's `BigDecimal.equals`.
  The driver reads a zero, which Postgres sends with no digits, as `0` whatever its scale,
  so the prelude registers a numeric codec of its own on each connection as a transaction
  begins: it reads a zero's scale from the header and leaves every other value to pgx's.
  `LocalTime` is the nanoseconds since midnight and `Duration` seconds and nanoseconds, as
  java.time holds them, each writing itself as java.time does. They are declared with no
  members yet: nothing uses one, and the test of every type reads them from a row.
- **An `interval` with months is a panic, and its day is 24 hours** (spec 10.4). A month
  has no length, and `java.time.Duration` counts a day as 24 hours. A `Duration` is sent
  as microseconds alone, so `ofDays(1)` is stored as `24:00:00`, which Postgres compares
  equal to `1 day` and adds to a timestamp across a clock change as 24 hours, as Kotlin
  would. `infinity` in a `timestamptz` or a `date` is a panic. Sub-microsecond digits are
  cut, not rounded, as the driver does. Reopen if a port stores calendar intervals.
- **A panic from the database carries the driver's error unchanged**, so `db.tx` can tell
  a serialization failure by its code and a server can log Postgres's own message. A
  wrong row count panics with a text that holds the statement. `queryOne` and
  `querySingle` stop at a second row, so the text says "more than one row", not how many.
- **`24:00` in a `time` is a panic, as is sending a `Duration` past an int64 of
  microseconds** (spec 10.4). Postgres holds `24:00:00`; `java.time.LocalTime` does not.
  A `Duration` of over about 292,000 years would wrap to a wrong interval. The line directives
  give the Kogo line.
- **`db.tx(serializable = true)` reruns without a limit**, as the spec says, on SQLSTATE
  40001 and 40P01 only, whether the block or the commit met it. Without `serializable`
  neither is rerun. Reopen with a limit if a workload livelocks.
- **`tx.atomically` is the driver's nested transaction**, a savepoint, rolled back on any
  panic that leaves the block, a failure or a bug. The block keeps using the outer `Tx`.
- **The raising `db.tx` is `either` around the plain one**: the scope is made once, so a
  rerun raises on the same handle.
- **The driver is told of a composite type on the connection that first reads one**
  (`Tx_Known`, called where a statement has a column of one). The types its fields are
  made of come with it. A `Db` is made from a pool that already exists, so there is no
  hook when a connection opens; the cost is a map lookup for each such statement. `citext`
  is an extension's type that the driver cannot derive, and a table's row may hold one, so
  it is registered as text on the same occasion. The emitter passes the name as
  `format_type` writes it, quoted where it must be (`"Price"`), and unqualified where the
  type is on the search path; the prelude finds it with `to_regtype` on the connection's
  own search path, tells the driver of it by schema and catalog name, and registers it
  under the written name too. Not taken: carrying the catalog name in `sqlpass.Type`,
  which would bind the type to the schema it was checked against.
- **A connection knows a composite type by name until it closes.** A migration that drops
  and makes again a type a running server reads leaves the old OID on each connection
  that read it, and those connections panic on it. Restart the server after such a
  migration. Reopen if migrations run under a live server.
- **Postgres sends the absent side of an outer join as NULL, not as a row of nulls**: the
  run-time reading confirms what DEV-837 assumed, and a whole row that an outer join nulls
  is `null`.
- **A list of an enum is refused** (`not-yet-emitted`), as a parameter or a column, with
  the cast to write instead. The driver knows no enum's array type until it is told, and
  the SQL pass keeps an enum's Kogo name, not its Postgres one, so there is nothing to
  tell it with. A list of `citext` reads and writes as text. Reopen when a port needs one:
  carry the Postgres name in `sqlpass.Type` and tell the driver as for a composite.
- **An empty `List` is sent as an empty array.** Go's nil slice is an empty list, and the
  driver would send it as `NULL`. The prelude finds a nil slice by reflection on every
  argument. Not taken: having the emitter wrap each argument the checker knows is a list,
  more emitter code to save a walk that costs nothing next to the round trip.
- **The five calls on `tx?.` have no Go**: the checker already refuses a `Tx?` before the
  dot, so the emitter has no case for it.
- **`check.Snake` is exported**: a field of a composite type fills the property of its
  name, and the facts hold a path for a column but none for a field.
- **A test that runs statements has a schema of its own** (`internal/testdb`): a
  transaction commits, and `go test ./...` runs packages side by side, so in one schema
  each would read the rows another commits. The schema is `testdata/schema.sql` loaded
  again under a name of the driver's process, first on the pool's search path; a literal
  is checked against the tables of `public` and run against the copy. A driver that is
  killed leaves its schema behind until `mise run testdb`.
- **Not tested: a function type with a `Raise<E>` receiver converted to a wider one**
  (`convert`, DEV-831). The issue expected the raising `db.tx` to reach it. It does not:
  the checker gives the block exactly the parameter's type, the wanted result included,
  and refuses any other function value there (`type-mismatch`), so no program converts
  one. The nearest case, a block that gives an `Int` where `db.tx` gives an
  `Either<String, Int?>`, is tested and converts the value inside the block.
- **Not handled**: a deadline for a statement or a rerun, a statement cache across
  builds, and `kogo schema` (the issue leaves them out).

## Taken adding what the invoice ledger calls (DEV-841), not yet discussed

- **A `Set` walks its keys to find one.** The one ordered implementation is a slice of
  keys compared with `rt.Equal`, so a lookup costs the set's size and `setOf` its square.
  A hash would need a second walk that agrees with `==` everywhere. Reopen when a set or
  a map is big enough for the walk to show.
- **`rt`'s `==` compares a Go type that embeds `rt.Unordered` by its `Keys()` regardless
  of order**, each key once. `rt` knows no prelude type by name, and `Unordered`'s
  unexported method means no data class can be taken for a set by a member's name; `Map`
  can give its entries the same way. Not taken: an exported `Elements() []any`, which a
  member could match and which copied every key on every comparison.
- **One declaration where Kotlin has two or more.** `listOfNotNull(vararg elements: T?)`
  covers Kotlin's single-element overload; `List<T>.plus(elements: List<T>)` stands for
  Kotlin's `Collection<T>.plus(Iterable<T>)`, since Kogo has no `Iterable`, and the
  one-element `plus` waits for a call; `coerceAtLeast` and `coerceAtMost` are on `Long`
  only. `Set<out T>.contains(element: T)` takes a `T`, as Kotlin's does by
  `@UnsafeVariance`.
- **Three more than the issue listed, because the ledger calls them**: `list[i]`
  (`List.get`), `Int.toLong()` and `LocalDate.plusDays`, each in section 12.
- **A `for` over a `Set` is still refused by the emitter**, as before; no call needs it.

## Taken growing the prelude for the stage-1 ports (DEV-828), not yet discussed

- **The ports are checked as copies, beside the originals**: `~/kogo-stage1/<port>/checked/`
  holds round 1's files, corrected where a rule of the spec changed, and `missing_*.kg`, the
  declarations they refer to and do not hold, with stand-in bodies (a program may not
  write `external`). `round1/` is left as written, so a diff shows what each change cost.
- **`trim(char: Char)` takes one character**, where Kotlin's takes `vararg chars`. With
  `vararg`, `trim()` fits both declarations and the checker ranks neither, where Kotlin
  prefers the one without `vararg`; a second character is refused, never misread. Reopen
  when a port trims two characters, with Kotlin's ranking in the checker.
- **`Decimal.toDouble` parses the text Postgres sent**, as closely as Java does; `NaN`
  and `Infinity`, which a `Decimal` holds and a `BigDecimal` cannot, give `Double`'s.
- **`replace(regex, text)` reads the text as Java does**, by hand: Go's own expansion
  takes `$1x` as a group named `1x` and has no backslash escape. A reference to a group
  that does not exist panics, as Java throws. The empty-match corner is in spec 12.
- **`atStartOfDay` finds the gap's end itself**: Go's `time.Date` gives the day before
  where midnight is skipped, and may give the second of two midnights.
- **An enum column that does not fit an enum class names the labels and constants that
  differ**, where it printed the class it wanted, which had the same name.

## Taken serving a table of operations (DEV-850), not yet discussed

- **A table is a Go value built by two generic constructors**, `rpc.Tx(name, f)` and
  `rpc.Db(name, f)`, so Go checks each fun's four parameters and its `Either`; the input
  and answer types come with `f`. Besides the operations it holds each enum's entries,
  the `Name_entries` slice the emitter already writes. Not taken: a reader and writer
  emitted for each type, which the design refused; names in a string table, which Go
  would not check.
- **An `Int` or a `Long` is read only as digits** (spec 15.4): `1.0` and `1e3` are refused
  as not whole, though each is a whole number. `JSON.stringify` writes every whole number a
  `Long` can carry as digits, so only a client of another language meets this. Reading
  digits exactly is then `strconv.ParseInt`; a parser of decimal fractions and exponents
  would be the rest of the walk again. Reopen if a client sends `1.0` for a whole number.
- **A `Uuid` is read in the form `toString()` writes, 8-4-4-4-12 hex digits, in either
  case**, as Java's `UUID.fromString` takes upper case. Java also takes groups of other
  lengths; this does not.
- **A key given twice is not refused**: the last one counts, as `encoding/json` reads it
  and as `JSON.parse` does. A body holding a second value after the first is a 400.
- **The 400's body is text: the operation, the key's path and what is wrong**, as
  `updateJob: input.stops[1].state is not one of Offered, Confirmed`. A path, not a bare
  key, since one name may stand at many places. An enum's constants are listed; the value
  never is. The 500's and 503's body is `request <id>`, the id the log holds; the 404's
  and 405's are empty, with `Allow: POST` on a 405.
- **The answer is written inside the transaction**, so an answer with no JSON, a `NaN`
  or a `Long` past the limit, rolls back as any other panic does.
- **The reruns' limit is checked before each rerun, from when the request arrived**: an
  attempt under way is never cut, and there is no pause between reruns, as `db.tx` has
  none. The rerun code is `db.tx(serializable = true)`'s, now `kogo.SerializableTx`,
  which asks the caller before each rerun.
- **The handler is `rpc.NewHandler(table, pool, env, logger)`, with a `Limit` field**,
  and serves `POST /<name>`; `main` mounts it under its path with `http.StripPrefix`.
  It makes each request's `Db` from the pool and the request's context, so a client that
  goes away cancels its statements, which panic: a 500 in the log.
- **The log is `log/slog`**: per request a line `served` with `request` (a new `Uuid`),
  `operation`, `outcome` (`ok`, `refused` or the status) and `duration`, since slog's
  `time` is the line's own; a line `rerun` with
  its `cause`, `serialization failure` or `deadlock`; a line `panic` with `message` and
  `trace`. A 400's problem is not logged; its body tells the caller. A panic anywhere
  in the request, its reading too, is a 500 with that line.
- **`NewHandler` panics if the table lacks an enum's entries that some input reaches**,
  by one walk over each input type, so a table the emitter got wrong fails when the
  server starts, not on the first request to use it.
- **`clientAddress` is the host of the connection's address.** Behind a proxy, `main`
  sets the request's `RemoteAddr` from the proxy's header before the handler sees it,
  since only `main` knows which proxy to trust. A header's first value is its value;
  `header("Host")` is the request's host, which Go keeps out of its headers.
- **Not handled**: a limit on the body's size, which `main` sets with Go's
  `http.MaxBytesHandler` (a body past it reads as not JSON, a 400); the request's
  `Content-Type`, which belongs with the CSRF checks.

## Taken refusing import cycles (DEV-844), not yet discussed

- **A cycle is told as one line for each file on it**: the file, the line of its first
  import from the next package, and every name it takes from there, so the message shows
  what would have to move. A package with two files that import the next one is on the
  cycle once, through the file that comes first.
- **Of a group's shortest cycles, the one through the earliest import is told**: earliest
  by the order the files were given, then line, as the checker orders its other errors,
  so a path spelled another way changes nothing. From there the cycle takes, at each
  package, the earliest import that keeps it shortest, and the error stands at the first.
  Comparing whole cycles import by import was written first and is not simpler to tell.

## Taken making `kogo check` the only gate (DEV-846), not yet discussed

- **A reference counts wherever it is written**: in a lambda that never runs, and in a
  fun that is named and not called, as in Go's rule, which speaks of what a declaration
  refers to. So `val h: () -> Int = { b }` with `b` below is refused, though Kotlin runs
  it right. Counting only what runs would take flow analysis. A local fun's body counts
  as the body of the fun around it, as Go counts a closure's. A call of an interface's
  fun on a data class or a data object follows its override, as Go follows a call on a
  struct; a call through an interface is not followed, and Go does not follow one
  either. A data class's default values count as its constructor's.
- **Only a file's top-level vals and funs make another file use it**: a data class or a
  data object is its own class in Kotlin, set up apart from its file's vals. The rule
  refuses some cycles of files that read no val early, such as a val that calls a fun of
  a file whose vals use the first one's funs alone: telling those apart is more code than
  any port needs. A val on a cycle of vals leads to no file, being refused already.
- **`-9223372036854775808L` is refused, as in Kotlin 2.4** (checked), while
  `-2147483648` is an `Int`: Kotlin takes the minus with an `Int` literal, but checks a
  `Long`'s digits alone. Write `-9223372036854775807L - 1L`.
- **Section 14 gained two rows the issue did not list**, `val n = 3000000000` and a val
  that reads one below it through a function: both are valid Kotlin and errors in Kogo,
  which is what the table holds.
- **A val's error stands at the val and names each val and fun on the way**, which is
  what to move or to make a fun. A cycle is told once, at its first val in the order the
  files were given. A read of a val below is told at the reader unless the two are on
  one cycle, and a val on a cycle still has its other reads told. A cycle of files is
  told at the val that leads out of the first file on it, the shortest from that file.
- **Codes**: `interface-fun`, `local-fun`, `number-range` and `val-order`.

## Taken checking api lists (DEV-849), not yet discussed

- **A data class on the wire has no type parameters** (spec 15.4). cusoon2's generator
  refuses a generic class other than `List` and its own `Outcome` (`rpc/typescript.kt`),
  and its operations send none. Allowing one would have the checker read each use of the
  type apart, and `kogo ts` write generic types. Reopen when an application wants one page
  type over several kinds of item.
- **`Unit` crosses only as the whole input or answer** (spec 15.2 and 15.4), where it
  means none; a property of type `Unit` has nothing to send.
- **A name in a list is one fun's** (spec 15.1): where two funs of the name are in scope
  the list could not say which is the operation, and the name is the operation's on the
  wire, so the error asks to rename all but one.
- **An operation's errors stand at its name in the list**, even where the fun is in
  another file: the list is what makes it an operation, and the fun alone is correct. The
  message names the fun, and for a type that does not cross, its path from the input or
  the answer (`In.at: Instant`), the first type on the path giving its root.
- **A list names at least one fun, and no two lists of a program share a name** (spec
  15.1): an empty list serves nothing and has no environment to record, and `kogo ts`
  writes each list to `<name>.ts`, so two lists of one name, even in two packages, would
  write one file. Reopen if lists get a package-qualified output path.
- **An operation whose result is not written is told to write it**, not that it returns
  `Unit`: the list is checked before bodies, when an unwritten result still reads as
  `Unit`.
- **The facts give the environment once for a list**, not on each operation, since a list
  takes one; and the wire types the list reaches in the order first reached. A type is
  read once for each list, which ends a type that holds itself.

## Taken writing TypeScript for api lists (DEV-852), not yet discussed

- **A list's types stand in the order of their names** (spec 15.6), not the order the
  operations reach them: reordering a list or a data class's properties then moves no type
  in the file, so `git diff` after `kogo ts` shows only what changed. The interface keeps
  the list's order, as the author wrote it. Names are unique in a list (15.4), so the order
  is total.
- **The first line names the list and not its file or package**: a list's name is unique in
  the program (15.1), and a path would change the bytes with the directory `kogo ts` is run
  from.
- **The interface is the list's name with its first letter in upper case** (spec 15.6),
  `Ops` for `ops`, as 15.6's example had it. The function lives in `check`, which refuses a
  wire type of that name, and `ts` calls it, so the two cannot drift.
- **A wire type's name begins with an upper-case letter** (spec 15.4). TypeScript refuses
  `type string = ...`, and `string`, `never` and the rest of its own type names and
  reserved words all begin in lower case, so one rule covers them, and Kotlin names classes
  in upper case anyway. Not taken: a list of TypeScript's reserved names, which is long and
  refuses a lower-case name only when it clashes. Reopen if a program needs a lower-case
  wire type.
- **List names that differ only in case are refused, and so are `outcome` and `Outcome`**
  (spec 15.1): `ops.ts` and `Ops.ts` are one file on macOS, and either of the second pair
  would name its interface as the answer's type.
- **`kogo ts` removes nothing from `<out>`**, as `kogo emit` does not: a renamed list leaves
  its old file. An application that wants `<out>` to hold only what Kogo wrote empties it
  before running `kogo ts`. Not taken: removing `.ts` files that start with the generated
  line, which lets a tool delete files it was not asked to. Reopen if a stale file misleads
  a frontend in practice.
- **An enum with no constants is written `never`**: Kotlin and Kogo accept one, and an empty
  union is not TypeScript. No value of it can cross, so the type says so. Not taken:
  refusing it on the wire, a rule for a case no program writes.

## Taken emitting the table of an api list (DEV-851), not yet discussed

- **A list's table is named by the naming rule, as a top-level name of its package**:
  `api ops` is `Ops`, and where another declaration of the package is `Ops` too, a type or
  a fun, the table is `Ops_api`, as a val would be `Ops_val`. The driver of an
  application reads it by that name; nothing on the wire does.
- **An enum's entries take part in the naming rule as a type does**: `Kind_entries` keeps
  its name, so a val `kind_entries` beside `enum class Kind` is `Kind_entries_val`, and a
  constant `entries` is the error `go-name`, not a Go build that fails. The emitter
  writes the slice for the table as well as for `Kind.entries`, so the name has one
  source.
- **A table holds an enum with no constants**: the server records its type though it
  has no names, so `enum class Kind {}` reached by an input starts, and any value for
  it is refused as not one of its names. Not taken: refusing an empty enum on the wire
  in the checker, which Kotlin allows and the server can serve.
- **The table holds the entries of every enum the list's operations reach**, the answers'
  too, from the checker's one list of wire types. The server needs only the inputs'; an
  answer's enum costs a slice it never reads. Not taken: a walk over the inputs alone, in
  the checker or the emitter, for no change in what is served.
- **Example 22 counts looks in a table of its own, `look`, added to the test schema**: a
  count that stays after a refusal needs a row to keep it, and no table of the schema
  held one. It counts only looks for an invoice there is not, as spec 15.3 counts
  failed logins, so an address that knows its numbers is never refused. The example is package `example.ops`, so that its `InvoiceStatus`, whose
  constants are the Postgres labels, is not example 05's.

## Taken writing the workflow (DEV-858), not yet discussed

- **A branch has the name Linear gives its issue.** An async run checks out the issue's pushed
  branch by that name and starts from `main` when it finds none, so a branch an agent named
  itself (`dev-852-kogo-ts`) would be missed by a run sent to review or fix it. Not taken:
  short names with the wrapper searching for `dev-NNN-*`, which is more code in cusoon2 for
  shorter names nobody types. Reopen if the long names get in the way.
- **Handoff and Review comments go on the PR as well as the issue.** A run has no Linear
  access and is shown none of the issue's earlier comments; the PR is the one place both a
  run and a Zed session can read. Reopen if runs are given the issue's comments.
- **A Question without a Ruling stops the merge, not the fixes.** Fix makes the fixes,
  pushes, and lists the Question under Open in its `Fixed:` comment; the lead does not merge
  while anything is Open. Not taken: refusing to start, which loses the fixes for one open
  point; merging and filing the Question, which merges what the review doubted.
- **The lead removes worktrees and deletes merged branches.** It merges, so it removes them
  then, and at Starting any left behind. `git push origin --delete`, not `gh pr merge
  --delete-branch`, which tries to check out `main` in the worktree and fails because
  `~/Dev/kogo` holds it.
- **The lead does not commit.** Its decisions reach the repository through the issue or the
  PR they belong to, so every change has had a Code review; the merge it makes is of such a
  PR. Reopen if that makes small corrections slow.

## Taken writing line directives inline (DEV-857), not yet discussed

- **`Emit` is told the directory it writes to**, as `out` after `root`, since a path relative
  to the Go file needs it; `kogo emit` passes its `<out>`. Not taken: giving `Emit` each
  file's path and leaving it to the caller to make them relative, which every caller would
  repeat.
- **One width for a Go file, the widest of its sources' directives.** A package of several
  `.kg` files is one Go file, and its code starts at one column. The width is that of the
  directive naming each source's last line, with one space after it. Not taken: a width per
  source, which starts a package's code at a column for each file.
- **The parser counts a source's lines** (ruled on the review of DEV-857): `syntax.File` has
  `Lines`, the line the source ends on, set by `Parse`, and `emit` takes the width from it.
  A count of the source's lines is the parser's to know, and `emit` already reads
  `syntax.File`. Not taken: leaving `syntax` alone and finding the largest line of any node
  by a reflection walk over the tree, 24 lines in `emit`; reading the source again, which
  tests that parse text in memory cannot; a fixed five digits, spaces on every line of a
  short file.
- **A type's fields and a case's statements are a level in, as gofmt has them.** A struct, an
  interface, a block, a `switch` and a closure each open a level; `} else {` closes one and
  opens the next, and a `case` and its conditions are written at the `switch`'s level. A
  line with no position (none is written today inside a declaration) gets its tabs and no
  directive.

## Taken refusing a stale out directory (DEV-840), not yet discussed

- **A file is kogo's when it is named `kogo.go` and begins with the emitter's header**,
  `// Code generated by kogo emit` (the lead's, 16 Nov 2026). `kogo emit` refuses when
  `<out>` holds such a file that this emit would not write: a package the program no longer
  has. Every other file under `<out>` is the user's, left alone and never refused. `<out>`
  lies in a Go module and commonly holds the user's own `main.go` beside the packages. Not
  taken: refusing every file kogo did not write, which refuses that documented layout;
  deleting the stale file, which removes something from a directory the user named.
- **Code `stale-output`, on stderr, exit 2, before anything is written**, as `no-module`:
  the fault is in the out directory, not in a source, so it names the stale file in `file`
  with no line or column. The walk is lexical, so the first such file is the same each run.
  Reopens if `kogo emit` grows a cleaning mode.
- **What under `<out>` cannot be opened or read counts as the user's**, not as an error: a
  `kogo.go` that cannot be opened or read past its header's length, and a directory that
  cannot be listed, which the walk passes over. Only a file shown to be kogo's is refused,
  and only `<out>` itself failing to be read, other than its absence, stops emit with
  `read`. Not taken: stopping on a directory of mode 000, such as a database's data
  directory in a module root, which is the user's business.
- **An `<out>` that is a link is followed before the walk**, as the write follows it, and
  the stale file is named under `<out>` as given.
- **`<dir>` is walked with a separator appended** in `kogo check`, `emit` and `ts`
  (DEV-864, the lead's ruling), so a `<dir>` that is a link is followed and every name the
  walk makes, a source's or an unreadable's, lies under `<dir>` as given. Links below
  `<dir>` stay unfollowed. A dangling link, a missing path and a file are each `read`,
  exit 2: `kogo check <dir>` is all the README, the spec and the usage line promise, and
  one rule for the argument is worth more than a second way in that nothing uses. Not
  taken: resolving the link with `EvalSymlinks` and joining each name back onto `<dir>`,
  which kept a file argument working. Reopen if a file argument is wanted.
- **A `<dir>` with no `.kg` file under it is `no-source`, exit 2**, for `check`, `emit`
  and `ts` (DEV-864, the lead's ruling): a mistyped path to an empty directory would
  otherwise pass the only gate in silence. The message names `<dir>` and says to give the
  directory that holds the program. Not taken: an empty program has no errors, exit 0.

## Taken refusing `==` on a function (DEV-845), not yet discussed

- **`x == null` is allowed where `x` holds a function**, and so is a `when`'s `null ->`
  branch: it tests for null, compares no function, and is how a `T?` is narrowed. Not
  taken: refusing it with every other `==`, which leaves `((Int) -> Boolean)?` with no
  null test.
- **The message names the declaration and property that hold the function**, as `Rule's
  property test`, even where it is reached through `List<…>` or a variant. Not taken:
  printing the whole path, longer for the cases where the type is already in view.
- **An interface's `val`s are read like a data class's properties** (ruled on the review
  of DEV-845), so an interface that declares a function `val` is refused: every value of it
  holds one, since its implementers declare the `val`. An interface with no function `val`
  passes, its implementers unknown, and is left to the runtime. Not taken: leaving every
  interface to the runtime, though the checker can see the function.

## Taken building the tests' emitted Go apart from its run (DEV-865), not yet discussed

- **A test that runs emitted Go builds it with `go build`, then runs the binary**, and
  reads only the binary's output (stdout and stderr, so a panic's trace stays). With `go
  run` the go command's own stderr, `go: downloading …` on a cold module cache, was read as
  the program's: neither test binary of `cmd/kogo` or `emit` needs `puddle` or `x/sync`,
  which the prelude's `pgxpool` does. Not taken: `go mod download` in `mise run testdb`,
  which leaves the tests wrong wherever the cache is cold for another reason; or a filter
  on `go:` lines, which hides what a program prints.
- **`emit`'s `build` appends the binary's exit status when not 0**, as `exit status N`,
  the line `go run` added, so a program that prints the expected lines and then exits
  non-zero still fails the comparison. Not taken: dropping it because nothing exits quietly
  today; a test made to stop failing wrongly should not let one pass wrongly.

## Taken giving section 8's panics Kotlin's messages (DEV-870), not yet discussed

- **`rt.Div` stays one generic function**, and tells a whole number from a Double by
  `T(1)/2 == 0` inside its `b == 0` branch, so the hot path is one comparison and the
  emitter is unchanged. Not taken: a separate `DivInt` for whole numbers, which moves the
  choice into the emitter for no gain.
- **`list[i]` outside the list is fixed here too** (`Index 5 out of bounds for length 3`,
  the JVM's message for `listOf(...)[5]`), one unsigned comparison in `List_Get`. Not taken:
  filing it apart, for a change of three lines.
- **`single()` is not added**: section 8 and the stdlib list name it, but the prelude has
  no `single` at all, so there is no message to correct; adding it is a feature, left to be
  filed. Not taken: adding it here. (DEV-873 adds it.)

## Taken adding `single()` and the list ends (DEV-873), not yet discussed

- **`first`, `last` and `single` are built on their `OrNull`**, so each is one line and the
  null and the panic cannot disagree. Not taken: three bodies that each test the length.
- **`take` and `drop` share the list's array** rather than copy it: no list operation writes
  an array it was given (`plus` copies), so a shared one is safe and costs nothing. Not taken:
  a copy, which matches `firstOrNull`'s reason only if something writes through a list.
- **`substring(start)` is `substring(start, length)`**, so both give the same message, as
  Kotlin's do (`Range [4, 3)` for `"abc".substring(4)`).

## Taken adding `Uuid.parse` (DEV-881), not yet discussed

- **`Uuid.parse` reads what the wire reads, and the wire calls it**: 36 characters, hyphens
  at 8, 13, 18 and 23, hex of either case. It is stricter than Java's `UUID.fromString`,
  which takes short groups (`1-2-3-4-5`). `Uuid` is Kogo's own, so nothing holds it to
  Java's. Reopen if a port's comparison depends on a text Java reads and this does not.

## Refusing a marked data class as an input (DEV-890), agreed with Greg 16 Nov 2026

- **A data class whose constructor is marked is never a wire input**, opt-in or not: a row
  comes from the database, which the program trusts, a request does not. Answers stay open.
  Not taken: reading from the wire as a use of the constructor, allowed where the file opts
  in as a row is: an opt-in made for another purpose would open the wire silently, the error
  would name a one-line way round the wall, and no case was found where a request should build a marked value. The error reuses `wire`.
- **An input's walk does not stop at a type an answer or another input reached first**, so
  a marked type behind it is still found; such a type's other wire errors are then named
  for each operation that takes it. Not taken: a second walk for marks alone.

## The check against Kotlin (DEV-877)

- **One entry point, `call`, in the shared source, and a case is a branch of a `when`** on
  its name, so each driver is one call and both run the same code. Not taken: plain
  functions of differing signatures, which need a table of calls in each driver.
- **Every case takes the same ten inputs and gives a `String`**, written by a template, so
  `toString` is compared too. Not taken: typed results and an encoding in each driver.
- **Inputs are tab-separated numbers** (a text as code points, a `Double` as its bits), not
  JSON: kotlinc has no JSON reader, and a driver must stay small.
- **A kept difference names its cases and a rewrite**, listed in `kotlin/cases/main.go`
  with where the spec states it: both results are rewritten and must then be equal, so a
  fault hides only if it changes nothing but the rewritten text in those cases. Not taken:
  a substring of either result, which hid four planted faults; leaving the inputs out,
  which is anticipating. Where a kept `lowercase` difference flips a `contains`, the
  `lowercaseContains` case's boolean is not compared when its text holds `σ`.
- **A bug's message is Kotlin's but for a closed list in spec.md section 12**: an index
  into a list Kotlin built as its empty list, which Kotlin words by the list's class, and
  a `replace` with a bad group, which keeps Kogo's words. The check holds every other
  message to Kotlin's. Not taken: always Kotlin's, which cannot hold for the first; no
  promise, which lets any message drift. Reopen if a program reads a message.
- **A template writes the smallest `Double`s as Java does** (`4.9E-324`): where the
  shortest digits are one, `rt` writes the closest two. `"$d"` is valid in both languages,
  so principle 3 holds it to Kotlin's; `plain` is Kogo's own and keeps its difference.
- **`trim(char)` trims by UTF-16 units**, as Kotlin's does and section 3 says, so a half
  of a pair trims and leaves U+FFFD.

## Kotlin compatibility (DEV-894), agreed with Greg 18 Nov 2026

- **Kogo source does not have to compile under kotlinc, and nothing runs it on the JVM:
  choice (c)** (18 Nov 2026, Greg, in a Zed session over DEV-894's report). When a Kotlin
  original is deleted, its comparison is kept by recorded answers (below). Strict Kotlin
  syntax was there so that an agent who just wrote Kotlin would not find Kogo doing
  something else; but the lists and a way to mark bindings and actions are needed, and some
  were already unlike Kotlin. A Kogo file is 99% Kotlin and is copied as it is read, so
  Kogo's few deviations should look as good as they can. Greg: "The sqlx style literal SQL
  is great and using templating is indeed better than params because it cuts out the
  'getting the order wrong' problem."
  - Not taken: **(a)**, every file valid Kotlin and run on the JVM. Once an original is
    deleted, the JVM run compares Kogo with itself, so it checks the emitter and not the
    port; and it costs a second prelude kept for good (about 800 code / 400 test lines
    now, growing), SQL parameters passed apart (22 of the ports' 67 lose their name
    check), and the lists as ordinary-looking calls.
  - Not taken: **(b)**, tests valid Kotlin, run against the original through shims. Greg:
    "No, we do not want 1k LOC shims."
  - Reopen if a second backend is wanted for its own sake, or if agents are found writing
    a Kotlin habit that Kogo accepts with another meaning.
- **Principle 3 is reworded: Kotlin where it overlaps; Kogo's own forms where it doesn't**
  (18 Nov 2026, Greg; `doc/design.md`). Text valid in both means the same in both; a name
  from Kotlin's standard library, `java.time` or Arrow means what it means there, and
  where Kogo wants another behaviour it takes another name, Kotlin's own where Kotlin has
  one; the corners of spec 12 stay allowed. Taken as proposed, over a spelt-out version.
  - **kotlinc pins down the deviations.** Greg: "I like the idea we use kotlinc to be clear
    on our 'deliberate deviations' from Kotlin." `mise run kotlin` compiles a file for each
    and fails if kotlinc accepts one (DEV-931).
  - **Spec 1 lists the four kinds of deviation**: the `sql` literal, bindings, the lists
    and `action fun`, replacing "three things, each unlike Kotlin".
  - **`java.time` is in scope** because Kotlin has no dates of its own: Kotlin programs call
    it directly, as cusoon2 does in about 30 imports.
- **Decided on the way** (18 Nov 2026, Greg). What changes code is filed (DEV-932,
  DEV-933, DEV-912); these lines are the record.
  - **`@Acts` becomes `action fun`.** Greg: "action fun is good." It closes the entry's
    "Reopen with DEV-894" above, and kotlinc refuses it (run on 2.4.21). Respelt in the
    designs (DEV-931); it is not built, so the spec and the checker take it when it is.
  - **`Uuid.parse` becomes `Uuid.parseOrNull`**: Kotlin's `Uuid.parse` throws on bad
    text, and Kogo's gives null. Spec 12 lists one corner: Kotlin's also reads 32 hex
    digits, and Kogo's does not. `kogo check` already refuses most misuse, the result
    being `Uuid?`; it passes unnoticed only where a null is welcome, as in a template or
    after `?.let`. About 5 / 5.
  - **`LocalDate.atStartOfDay(zone)` returns a `ZonedDateTime`**, as Java's does, and the
    caller adds `.toInstant()`. Both give the end of a skipped midnight (Java run over
    São Paulo, 4 Nov 2018). About 5 / 10. Not taken: a name of Kogo's own,
    `startOfDay`.
  - **`mise run kotlin` extends to `kogo.time` and `Either`**, which it does not cover
    today. About 40 / 40.
  - **Recorded answers in cusoon2.** Before an original is deleted, `run.sh` saves its
    cases, the Kotlin's answers and rows, and compares the port against them from then on;
    a deliberate change regenerates them under Greg's ruling. About 80 / 20, built before
    the first original is deleted (DEV-893 holds the order). Not taken: dropping
    a piece's cases when its original goes.
  - **The cheat-sheet for agents is the examples.** Greg: "we do not want duplication in 3
    places." Each list form gets one example in `examples/`, compiled by the tests; a
    short page points at them and names Kogo's own words. A misfit's error prints the
    signature the role wants, from the checker's table. A role's shape stays in the spec
    and in the checker's table: "It's fine to have it both specced, and then in the
    checker." Not taken: shapes printed from the checker into a sheet (about 30 / 20).
- **The lists keep bare words, with no `::` or colons** (18 Nov 2026, Greg: "pretty happy
  with the bare syntax"; `doc/gates.md`, `doc/work.md`, `doc/lists.md`). A gate of its
  own, a shared gate and `ungated` are kept. These change:
  - **The outside step is spelt `after`**: `updateBooker gatedBy updateBookerGate after
    renameLogins`. Greg: "after helps make clear the order." The step must be an `action
    fun`, so the function says it runs with no transaction (about 5 / 5). It is used once
    in cusoon2 and kept, over a queued effect (loses the at-once refusal), two operations
    (the order sits in the browser) and an `action fun` that writes. Not taken: the chain
    as the list (`gate -> step -> work`): the words carry the checker's rules, and a chain
    of one would be an unmarked ungated row.
  - **The bare `outside` goes.** A gated operation that is an `action fun` runs with no
    transaction, and its gate runs once: `deletePasskey gatedBy person`. It follows spec
    15.3, where a `Db` means no transaction. Greg: "the list is just ... a list labelling
    the gates. I like that."
  - **A list with no check says `unchecked`**: `api hooks unchecked { ... }`, as a row says
    `ungated`. Its Go type cannot be mounted for a browser. Greg: "checkedBy is called for
    each and every listed op. But that is NOT the case for unserved." Not taken:
    `unserved`, `goOnly` (says what it is not), `calledByMain`, `forRoutes` and
    `serverSide` (all name the caller, not what runs); `private` and `internal`, Kotlin
    words with meanings of their own.
  - **A command's name is its function's name**: `commands admin { recordAbn ... }`, run
    as `cusoon2 recordAbn --gstRegistered true`. The string column goes. camelCase is
    kept over CLI convention, so a search for a property finds its flag; cusoon2's scripts
    rename about ten commands, roughly 6 lines. The list's name stays: `main` calls it by
    it.
  - **No commas in any list; one entry per line.** The work list repeats `queue` on each
    line, and spec 15.1's `api ops { me, updateJob }` goes to one per line. *Superseded
    19 Nov 2026 by "A list's entries are separated by a comma, a new line or both"
    (DEV-947, DEV-948) below.*
  - **An effect's and a job's lines are in a fixed order, the order they run**, and the
    checker refuses any other (about 10 / 10): settings `find`, `fair`, `soonest`,
    `attempts`; steps `enter`, `prepare`, `act`, `record`; on failure `failed`,
    `recovery`; a job `next`, `run`, `failed`, `recovery`.
  - **Job, effect and the work list are kept.** A job's example includes a test that
    `failed` stops `next` finding the row; an effect's shows the runner's table of `Act`
    outcomes as a comment above `act`. The work list keeps its `queue` lines and choice D's
    error, so the copy is checked. Not taken: the alert alone with every queue run, the one
    place something would run without being named; the alert passed in Go, which the
    checker could not see.

## Taken recording DEV-894 (DEV-931), not yet discussed

- **A deviation's file names the line kotlinc must refuse**, `// kotlinc refuses line N`
  on its first line, and `mise run kotlin` fails unless kotlinc reports an error on that
  line. That ties the refusal to the deviation's own line, so a typo elsewhere does not
  pass, and survives a reworded message. Not taken: any refusal, which passes a typo;
  kotlinc's message text, which passes any syntax error and which Kotlin does not promise
  to keep; a valid twin of each file that kotlinc must accept, stronger but twice the
  files.
- **The binding's file is an `external class`**: kotlinc on the JVM (2.4.21) accepts a
  bare `external fun` as a native (JNI) function, and refuses `external` on a class or an
  object, though not as a syntax error. So a binding file of bare `external fun`s, such as
  `prelude/text.kg`, passes kotlinc. The Ruling on PR 75 (Question 8, (a)) has principle
  3 and spec 1 say "kotlinc refuses each" and name that exception, which meets no author,
  since spec 13 keeps `external` out of application code. Not taken: `@go` on every
  binding declaration, a language change for no reader; keeping "a syntax error" and
  recording the gap here alone.
- **The printed table writes `action` beside the gate** of an operation that runs wholly
  outside, where it wrote `outside`, to match the list's loss of the bare word. Not
  taken: keeping `outside` in the table alone.
- **`doc/lists-assessment.md` keeps `@Acts` and the old forms**, under a note: it is the
  record of what was weighed. Spec 15.1's commas wait for the lists to be built, since
  the parser reads them today. (Settled by DEV-948, "A list's entries" below.)

## The test form (DEV-929, DEV-938, DEV-940), agreed with Greg 19 Nov 2026

- **A test is a value, `test("sentence") { tx, env -> ... }`, named in a `tests` list**
  (Greg, 19 Nov 2026: S3 of DEV-938, after the trial translation of
  `InvoiceLedgerServiceTest.kt`). The checker takes the environment's type from the list,
  so a test writes no `: Test<Env>`. Every test breaks after `=`, the sentence on the next
  line; ktfmt 0.58 breaks the same Kotlin the same way, and Kogo has no formatter (Greg:
  agents are trusted), so `examples/23_test.kg` is the layout. Not taken: (b), the sentence
  in a comment; S1, S2, S4. Reopen if the layout drifts across the translated tests.
- **`tx.refusal(sql"...")` in place of `assertFails`** (Greg, 19 Nov 2026). A test sees a
  statement Postgres refuses as a value, `Refusal?` with SQLSTATE, constraint and message,
  run in a savepoint, in test files only. Panics stay bugs and are never caught, tests
  included; a refusal a caller can meet inside application code is expected in the
  statement (spec 10.6) and tested as a `Left`. Not taken: `assertFails`, catching a panic
  in tests, since panics as exceptions are not the Go way. Reopen if tests turn out to
  need a refusal buried in application code that the statement cannot expect: Greg asked
  whoever builds on this to watch for it.
- **Test files are `_test.kg`, left out by `kogo emit` and `kogo ts`** (the lead), as Go
  leaves out `_test.go`. Not taken: a test package of its own, which could not reach its
  package's `internal` and `private` helpers.
- **The runner, not each application's helper, gives an in-process call's refusal its
  rollback** (the lead): an operation called from a test that returns `Left` leaves
  nothing behind, as the server's rollback does. Not taken: every application wrapping each
  call in `tx.atomically`. Not built in DEV-940: a test cannot call an operation until
  `kogo.test.request` exists, and that issue builds this with it.
- **The list names its environment: `tests name<Env> { a, b }`** (Greg, 19 Nov 2026, the
  ruling on PR 76's review). A list of bare names holds no type to infer a test's from, so
  the list states it once, read as Kotlin's type argument. Not taken: `tests name: Env { }`;
  the type on the first test only; one environment per program, which rules out a second
  environment for the stand-in servers. The list's separator stays as built: commas in
  lists are revisited across Kogo in an issue of their own (DEV-948, "A list's entries"
  below).
- **A `Db` in a test's environment commits** (Greg, 19 Nov 2026): the rollback covers `tx`,
  and spec 16.2 says so. **Owed with `kogo.test.request`:** whether the checker refuses a
  `tests` list whose environment holds a `Db`, directly or in a field, decided when tests
  can call operations in-process. Not taken now: the refusal, before any test needs a `Db`.
- **The refusals buried in application code and the panics tests expect** (three and two,
  found in cusoon2's tests by DEV-940's handoff) are decided file by file when cusoon2's
  tests are translated (Greg, 19 Nov 2026).

## Taken building the test form (DEV-940), not yet discussed

- **A list names vals of its own file, each in one list.** A cusoon2 test file is one
  class of tests, and a failure is reported with the file the list is in. Not taken: any
  val in scope, as `api` names funs, which needs each val's file carried to the emitter and
  lets one test run twice.
- **A test that no list names is a compile error**, whether it states `Test<Env>` or no
  type: left out of its list it would silently never run, the one failure a list of names
  invites. Not taken: running every `Test` val of a test file whatever the list says, which
  makes the list decoration.
- **`kogo test <dir> <out> <main>` builds and runs the application's Go test main.** The
  environment holds handles only Go can make (a clock, the stand-in servers), so a Go main
  must exist, as the server's does; it calls `test.Run(ctx, pool, out, envs...)` and
  exits 1 on a failure. `kogo test` checks, emits with the tests, builds that main and
  runs it. Not taken: `kogo test` writing a main itself, which could build no environment
  but `Unit`; `kogo test` only emitting, leaving the run to the user, which the issue's
  "runs each test" rules out.
- **The runner finds the lists; the main gives only environments** (the review of PR 76).
  Each emitted list registers itself as its var is initialized, and `test.Run` runs every
  list registered with the first environment of its type. `kogo test` hands the test main
  every list's name and place (`KOGO_TEST_LISTS`), so a list in a package the main does not
  import fails as `not-linked`, and a list with no environment of its type as `no-env`.
  Not taken: the main naming each list, which runs a list it leaves out nowhere and says
  nothing; `kogo test` writing a Go file that imports every package with tests, which the
  main would still have to import.
- **`tx.refusal` panics on a statement Postgres stopped rather than refused** (the review
  of PR 76): SQLSTATE classes 08, 40, 53, 57, 58 and XX. Otherwise a test checking only
  `refusal != null` passes on a timeout. Not taken: a list of the refusals (classes 22, 23,
  42, P0), which would make a new raise in a trigger a panic until added.
- **An unimported assertion, outside a test file, is `test-only`**, not "add import
  kotlin.test.…", which the checker then refuses (the review of PR 76).
- **`kogo check` reads test files; `emit` and `ts` do not.** The checker refuses a use of
  a test file's declaration from another file (`not-visible`) and an import of `kogo.test`
  or `kotlin.test` there (`test-import`), so `check` and `emit` agree about the program.
  Not taken: `check` leaving test files to `kogo test`, which would make the one gate two.
- **A failure's position is the test's val, and `at` is the Kogo line that panicked**, read
  from the Go stack through the emitter's line directives: a test of five assertions
  otherwise says only which sentence failed. It has no column, so it is beside
  file/line/column, not in place of them. Not taken: the position of the panic alone, which
  for a bug deep in application code names no test.
- **`Refusal` is an external class with three vals** (`sqlState`, `constraint`,
  `message`), not a data class: a prelude file holds bindings only. So two refusals are not
  compared with `==`; a test compares the fields it means. Reopen if tests want
  `assertEquals(Refusal(...), ...)`.
- **A binding's parameter with a default is a pointer in Go, null when left out**, as an
  emitted fun's is: `message: String? = null` is `**string`. No binding had a default
  before the assertions; `TestPrelude` now expects what the emitter's call passes. Not
  taken: two overloads of each assertion, with and without a message, 14 bindings for 7.
- **`assertNull` is `<T> assertNull(actual: T?)`** where Kotlin's takes `Any?`, since Kogo
  has no `Any`; and `assertContains` is two overloads, a `List` and a `String`, without
  Kotlin's `ignoreCase`. Each is added to when a test needs more.
- **`tx.refusal` does not see a deferred constraint.** It releases its savepoint, and
  Postgres checks a `deferrable initially deferred` constraint at commit, which a test's
  transaction never reaches. Not taken: `set constraints all immediate` in the savepoint,
  which stays in force for the rest of the test when the statement is accepted. Reopen
  when a schema under test has a deferred constraint.

## Taken writing cusoon2's `main` on paper (DEV-903), not yet discussed

- **`check-config` reads the file and builds every handle, and opens nothing**, as the
  Kotlin's parses only. So a handle's constructor checks its values (an address, an
  encoding, SMTP's security) without the network, and a bad one fails the deploy's check.
- **`main`'s own flag comes before the mode or command**: `cusoon2 --config f recordAbn
  --business acme`. Go's `flag` stops at the first word that is not one, so the command's
  reader is handed only its own flags. Not taken: the Kotlin's `--op <name>`, which mixes
  the two.
- **The webhook answers as `Bounces.kt` does**: 200 and a log line of the keys present
  for a body missing a field, 200 for an event other than `bounce` or `reject` before
  any other field is read, and 500 for a body that is not a JSON object, on purpose, so
  SMTP2GO keeps form-encoded events for 48 hours rather than dropping them. Not taken:
  `doc/gates.md`'s sketch, 400 for both.

## A list's entries (DEV-947, DEV-948), agreed with Greg 19 Nov 2026

- **A list's entries are separated by a comma, a new line or both, with a comma allowed
  after the last**, as Kotlin separates a collection's (Greg, 19 Nov 2026, choice A of
  DEV-947). One helper in `syntax/parse.go` reads the `api` and the `tests` list; spec
  15.1 states it and 16.1 points there. An entry of more than one word (a row with
  `gatedBy`, `after`, `ungated`) stays on one line, since a line end ends it. Supersedes
  DEV-894's "no commas in any list". Not taken: B, no commas and one entry a line, as
  DEV-894 had it, which the built lists and spec 15.1 did not follow; C, commas always, as
  built, which the designs' one-entry-a-line lists did not follow. Reopen if a row of
  words needs to run over a line.
- **The work list names its alert in its head**: `work daemon alertedBy opsAlert { ... }`,
  its queues a list of names, in the shape of `api name checkedBy check { ... }` (Greg, 19
  Nov 2026, DEV-947). Not taken: the `alert` line and a `queue` word on each line, which
  only a list without commas needed. Respelt in the docs alone; no work list is parsed yet.
- **A missing separator says so**: `api ops { a b }` is "expected , or a new line between a
  list's entries" at `b` (DEV-948), where it said "expected }". Not taken: the old message,
  which names one of the three ways on.

## Taken comparing and shifting an Instant (DEV-946), not yet discussed

- **`Instant.ofEpochSecond(epochSecond, nanoAdjustment)` and `Duration.ofSeconds(seconds,
  nanoAdjustment)`, the two-argument forms alone.** The check against Kotlin needs to build
  both from numbers, and `plus` needs a `Duration` that does not come from SQL. One form
  each, since an external overload would need two Go names. Not taken: the one-argument
  `ofSeconds`, `ofMinutes` and `ofDays` of spec 12's list, each a line when first needed.
- **`plus` and `minus` add the seconds and then the nanoseconds, as `java.time`'s do**, so
  a sum that leaves the range part way through panics where Java's throws, with its
  message (`Instant exceeds minimum or maximum instant`, `long overflow`). Not taken: one
  exact sum, which accepts a few results at the range's ends that Java refuses.
- **For kotlinc, `kotlin/time.kt` names `java.time`'s types `kogo.time`'s** by typealias,
  so the shared source imports `kogo.time` in both languages. Not taken: a Kotlin copy of
  each prelude function.
- **`Duration`'s text writes its seconds' digits exactly**, where it went through a
  `float64` and wrote `PT-1.9999999960000001S` for Java's `PT-1.999999996S`; the check
  found it. Not taken: leaving it to an issue of its own, for a three-line fix.

## Spec gaps found by the leakage ports, unresolved

- Outbound calls are handles (above), but their timeouts and retries, and workers, have no
  design. (An application's own binding files stood here until 17 Nov 2026: `doc/work.md` has them,
  as extensions.)
- Operations (spec 15) leave out, on purpose: how one sets a cookie, which a login needs;
  Origin and CSRF checks; a limit on the reruns of a `db.tx` written in a `Db` operation.
