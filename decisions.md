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
  (see the gaps below) when a port first needs one.
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
- **The wire carries what cusoon2 sends today, and sealed interfaces** (15 Nov 2026;
  spec 15.4): `Boolean`, `Int`, `Long`, `Double`, `String`, `Uuid`, enums, data classes,
  `List`, `T?` and `Unit`; names as written, nothing optional, and strict reading, a
  missing, unknown or mistyped key being a 400. cusoon2's 20 operations send 35 data
  classes with 107 fields: 53 `String`, 14 ids, 13 data classes or enums, 12 `Int`, 7
  lists, 4 `Boolean`, 3 `Long`, 1 `Double`; 22 may be null. Sealed interfaces were added
  on Greg's question of what they are good for: cusoon2's `AcceptInviteInput` has two
  nullable fields whose three valid combinations are stated in a comment, and a sealed
  input lets TypeScript refuse the fourth and `when` cover the three. The tag is a key
  `type` holding the variant's name, since Kogo has no annotation to name one. Not taken:
  dates and times, which cusoon2 sends as text it has formatted; maps. Each can be added
  and break nothing. Forced, not chosen: a `Double` that is `NaN` or an infinity has no
  JSON, so an answer holding one panics.
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
  may be a sealed type and commits. Not taken: any wire type as the refusal, which lets a
  page branch on a refusal that rolled back, and lets an internal error type onto the
  wire. Widening later breaks no operation, though the answer's object gains a key.
- **`kogo ts` writes the types and the table of operations, and no client** (15 Nov 2026;
  spec 15.6). The types are where a mistake between backend and frontend is caught.
  cusoon2's client is 79 lines of `fetch`, a CSRF header and a React hook, which depend on
  the application. The file is checked in and a stale one is caught by the application's
  own checks with `git diff`, so Kogo has no flag for it. cusoon2's generator is 111 lines
  and works by reflection at run time; this one reads the checker's facts.
- **The server is an emitted table, a Go library and the application's own `main`**
  (15 Nov 2026; `doc/design.md` "The server"). `main` is about fifteen lines of Go that
  read the configuration, open the pool, build the environment and mount the handler. Not
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

## Taken in the v0 spec, not yet discussed

- Markers (`@RequiresOptIn`, `@file:OptIn`) are the one annotation form, so the access walls
  port. Known hole: without inheritance, any file can implement an interface, so a wall is a
  data class with a marked constructor, as example 19's `HeldBusiness` is. `copy` and a sql
  row no longer get round that constructor (see "Agreed with Greg").
- Value classes are out: cusoon2 has none, and binding distinct id types to SQL columns needs
  a design (foreign keys could supply it).
- No `apply`/`run`/`with`, no labelled returns, no `object`, no classes beyond data classes.
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
  may be null. A function is equal to nothing, itself included, where Kotlin compares
  references: Go cannot.
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
  - a local fun or an interface's fun with type parameters (Go has neither), an
    interface's fun with a default, an interface that implements another or has members
    and an `out` parameter, a reference to a fun with a default, a `?.` call of a property
    that holds a function, and a `for` over a `Map` or a `Set`.
- **A marker is written nowhere** (spec 11): the checker has enforced it, and it does
  nothing at run time, so a program with markers is emitted, not refused.
- **Not handled**: a literal outside its type's range; two top-level vals that read each
  other. Two Kogo packages that import each other are now refused by the checker (see
  "Agreed with Greg").
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
  A `Duration` of over about 292,000 years would wrap to a wrong interval. The `//line` directives
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
  the `Name_entries` slice the emitter already writes, and each sealed interface's
  `rpc.Variants[I](V1{}, V2{})`. Not taken: a reader and writer emitted for each type,
  which the design refused; names in a string table, which Go would not check.
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
  key, since one name may stand at many places. An enum's constants and a sealed
  interface's variants are listed; the value never is. The 500's and 503's body is
  `request <id>`, the id the log holds; the 404's and 405's are empty, with `Allow: POST`
  on a 405.
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
- **`NewHandler` panics if the table lacks an enum's entries or a sealed interface's
  variants that some input reaches**, by one walk over each input type, so a table the
  emitter got wrong fails when the server starts, not on the first request to use it.
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

## Spec gaps found by the leakage ports, unresolved

- Outbound calls are handles (above), but their timeouts and retries, workers, and an
  application's own binding files have no design.
- Operations (spec 15) leave out, on purpose: how one sets a cookie, which a login needs;
  Origin and CSRF checks; a limit on the reruns of a `db.tx` written in a `Db` operation.
