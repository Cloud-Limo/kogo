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
  when a port needs one to read naturally. Amended 22 Nov 2026 (Greg; DEV-1067): a binding's
  receiver is not nullable either, so the rule holds for every extension and a reader
  need not know which prelude names a plain `.` on a nullable value still reaches. The
  prelude's two, `List<T>?.orEmpty()` and `String?.isNullOrBlank()`, are gone: write
  `x ?: emptyList()`, whose `T` the left of `?:` settles (DEV-1069), and
  `x == null || x.isBlank()`, which narrows `x` where `isNullOrBlank` did not. Not taken: keeping the two as a convenience of the prelude;
  cusoon2's port called them at three places, each as clear in the longer form.
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
- **Count, exists, a coalesce with a non-null argument and a cast of a non-null value are
  non-null** (23 Nov 2026, Greg, on cusoon2 PR 1600; DEV-1119; spec 10.5). Half the `"name!"`
  in the port sat on a column Postgres can never make null, and a bang nothing checks makes
  the sum that drops its `coalesce` look like every other. The SQL pass reads each shape in
  Postgres's tree, as it reads a reference: an `AGGREF` that is `count`, an `exists` `SUBLINK`,
  a `COALESCEEXPR` with an argument non-null by the rule or a constant that is not null, and a
  `RELABELTYPE`, `COERCEVIAIO` or cast `FUNCEXPR` (explicit or implicit) of a non-null value,
  where the cast function is Postgres's own (`funcid` below 16384) and not one of its seven
  casts from `jsonb` (lead's Ruling on the Code review, 23 Nov 2026).
  A reference to a subquery's column that is one of these is followed and nulled as any
  reference is, so a `count(*)` on the nullable side of a `left join` is nullable. `count` is
  known by its two oids in `pg_catalog`, 2147 and 2803, which Postgres fixes in its catalogue
  data: a schema may declare an aggregate of its own named `count`, which may return null,
  and a lookup by name would cost a query and still have to pin the schema. A constant counts
  only as a `coalesce` argument, so `select 1` stays nullable. Not taken: any other function
  or aggregate (`sum`, `max`, a schema's), an operator or comparison, a view's or a CTE's
  column, a scalar subquery, a window `count`, an `array(...)` subquery, a cast to a domain;
  Postgres declares none of them non-null and the pass does not reason about strictness. Nor
  a cast from `jsonb` to a number or `boolean`, which gives null for JSON's `null`
  (`'null'::jsonb::int`), known by oid as `count` is; `::text` from `jsonb` goes through I/O
  and stays taken. Nor a cast by a function of the schema's (`create cast ... with
  function`), an extension's included: it is a function of the schema's, and may return
  null. Not taken: every cast function of Postgres's, which promises non-null where Postgres
  gives null. A needless `"name!"` stays allowed. Reopen when one of the four turns out
  nullable in another case Postgres makes null.
- **The module is `github.com/Cloud-Limo/kogo`** (19 Nov 2026), the repository's own
  path, so that a program fetches it as an ordinary Go module pinned by commit. It was
  `module kogo` from 15 Nov, while everything built on one machine where a `replace` to
  a local checkout reached it; cusoon2 committing its emitted Go, built by CI, is what
  reopened that. The emitter keeps the import paths of `rt` and the prelude's Go in two
  constants in `emit/emit.go`. Not taken: a vanity path on a domain of its own, which
  needs a server answering `?go-get=1` and gains only when the repository moves; or
  keeping `kogo` with a `replace` in every program, which no `go build` away from a
  local checkout can follow. Reopen when the repository moves.
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
- **Every prelude name is imported from a `kogo.` package** (19 Nov 2026; DEV-939, DEV-954;
  spec 1, 12 and 14). The test form (PR 76) shipped `kotlin.test`, which a Kogo-only
  codebase would import from a library that is not there. Greg, 19 Nov 2026: option 2,
  every prelude name is imported from a `kogo.` package, and the names inside keep
  Kotlin's, `java.time`'s and Arrow's spelling and meaning. A Kogo file imports only from
  its own application and from `kogo.*`, or needs no import (`Either`, `Uuid`, `listOf`,
  as today). Tests take `kotlin.test`'s names from `kogo.test`: `import
  kogo.test.assertEquals`, not `import kotlin.test.assertEquals`. This extends the 15 Nov
  decision ("`kogo.time` stands for `java.time`") rather than reversing it.
  Not taken. Option 1, Kotlin's and Java's real packages (`java.time.Instant`,
  `arrow.core.Either`, `kotlin.uuid.Uuid`, `kotlin.test`): the most faithful to an agent's
  training, but a Kogo-only codebase would import from libraries that are not there, and
  the import would promise their whole API; it also reverses 15 Nov. Option 3, mixed
  (`kotlin.test` by Kotlin's package, `java.time` as `kogo.time`): the same cost as option
  2 but a second rule to remember, and it keeps `kotlin.` in Kogo files for good. Option 4,
  Kogo's own names throughout: it contradicts principle 3 and DEV-894's reason that agents
  copy the Kotlin they know.
  Accepted cost. The import no longer tells whether a name has exactly Kotlin's meaning:
  `kogo.test.assertEquals` (Kotlin's) and `kogo.text.plain` (Kogo's own) look alike. That
  is already true of `kogo.time`, and principle 3 says a Kotlin name means Kotlin's meaning
  wherever it is imported from.
  An agent writes Kotlin's import out of habit, so `unknown-import` on `java.time.X`,
  `kotlin.test.X`, `java.util.UUID` or `arrow.core.X` (and `arrow.core.raise.X`) names
  the fix: `kogo.time.X`, `kogo.test.X`, or no import for `Uuid` and Arrow's names. It
  does only when Kogo has that X, so a hint never leads to a second error
  (`java.time.OffsetDateTime` gets the plain "there is no package"). Not taken: a hint for every
  package of Kotlin's and Java's, which is a table to keep for imports no port has
  written. Reopen if a port meets another such import more than once.
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
  The other operators are bindings (the entry below). A binding's receiver is not nullable,
  as no extension's is (amended 22 Nov 2026, DEV-1067; it allowed one for `isNullOrBlank`
  and `orEmpty`, both now gone). Reopen if a prelude function's type cannot be written in
  Kogo and is too useful to leave out.
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
  Ordering went the other way: `sortedBy` is declared once, with `kogo check` limiting its
  key (lead, 22 Nov 2026; see the `sortedBy` entry of that date). Reopen if the overloads
  that remain become a burden.
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
  merges up. Reopened 20 Nov 2026: the clerk merges, below, and "the lead" of the next two
  entries is now the clerk.
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
- **A clerk runs the pipeline and merges; the lead decides** (20 Nov 2026, Greg; DEV-999).
  The lead of 19 Nov made 283 calls in 3.5 hours and its context grew from 29k to 640k
  tokens, each call reading all of it again; 55% of the calls were pipeline work set off
  by background waits. So that work goes to a clerk, one `claude -p` on Opus at medium
  effort for each event, started by a poller (`lin clerk`, DEV-1000) and following
  `doc/clerk.md`; the lead lasts one sitting, is woken only by Greg and never waits.
  Greg's: the clerk merges a Kogo PR when every check passes and nothing is Open, and
  otherwise hands it on; a cusoon2 PR stays his to merge, after the clerk says it is
  ready; what the clerk hands on carries the label `lead` and waits for his next "Lead",
  with no notification. Taken here, where the issue was silent or said otherwise:
  - `Ruling:` starts a clerk too, with `Handoff:`, `Review:` and `Fixed:`: the table's
    rows after a Ruling are a Fix or a merge, both the clerk's. Not taken: the lead
    launching the Fix, which leaves a Ruling with nothing to change no one to merge it.
  - The clerk leaves alone an issue labelled `lead`, but for its PR merging, and takes it
    up from its newest comment when the label goes. So the label has a rule: the lead can
    post a Ruling and then a `Lead:` comment keeping the Fix in Zed with no clerk starting
    between the two, and can hold a whole issue. Not taken: the newest comment alone,
    which the poll can read between two posts.
  - The clerk starts in `~/Dev/kogo`, not in the issue's worktree as the issue said: a
    run's handoff has no worktree in this VM, a cusoon2 worktree has no `doc/clerk.md`,
    and a branch's copy may be stale. It makes the worktree when it merges.
  - One clerk at a time: two merges would share `kogo_test`. Not taken: a lock for each
    issue, as DEV-1000 first said.
  - What is handed on takes the place of the step, and is raised once, on the Review: the
    review is always launched, and a Budget outgrown, a `spec.md` change and new work are
    all in one `Clerk:` comment with the Questions, which one Ruling answers. Not taken:
    raising them on the Handoff, which holds the review until Greg next says "Lead".
  - A `Clerk:` comment is on the issue in full, where the lead reads it without the PR.
  - A merge is its own event: the clerk that merges posts `Merged:` and exits, and the
    clerk the merge starts tidies and moves to Todo what waited on it, the same for a PR
    Greg merged. Not taken: tidying when a lead starts, which no longer happens daily.
  - The cap is `--max-budget-usd 5`, a guess at several times a merge step; set it from
    the first days' log.
  - The clerk hands on a Fix it doubts as well as a Won't fix, where the issue named only
    the second: a wrong Fix is made by the next run, a wrong Won't fix only left.
  - No statusline, which the issue left optional: nothing here could show that Zed's
    agent panel draws one, and the poller's log can give each clerk's cost.
  - A step's comment counts only from an owner, member or collaborator, and only when its
    first line is the one on the issue (the Review, 20 Nov): Kogo is public, anyone may
    comment on its PRs, and the clerk quotes a Ruling into a brief and merges unattended.
    Any other is handed on. Not taken: trusting the PR's comments as a lead reading them
    did. A run reading its PR has the same hole, which is cusoon2's launcher's.
  - An issue that fits no row of the clerk's table is handed on, so none is nobody's
    (the Review): the table's last row had taken anything else and exited.
  - The poller pulls `~/Dev/kogo` before it starts a clerk, and its first poll records
    what is there and starts none (Greg's Ruling 10): else a change to `doc/clerk.md` is
    read one event late, and starting the poller starts a clerk for every open issue.
  - The sizes stand (Ruling 8): `doc/lead.md` 246 lines for about 170, `doc/clerk.md`
    221 for about 120, Rulings 11 to 18 included. What is over is written once and repeats nothing. Not taken:
    cutting the cusoon2 paragraph of Merging or Talking to Greg.
  - This PR waits for DEV-1000 and the two merge on one day (Ruling 9), since
    `doc/clerk.md` describes a poller not yet built: DEV-1000 is built to this branch,
    what the build proves wrong comes back here as a second Ruling and Fix, and the
    clerk's own command line, run once by hand, merges this PR. Not taken: merging now
    and saying "Clerk DEV-NNN" by hand, with `main` naming three things that do not
    exist.
  - Building the poller (cusoon2 PR 1543) changed six things, Greg's Rulings 11 to 16 of
    20 Nov. The clerk runs with `--permission-mode auto` and one allowed command, `gh pr
    merge` of a Kogo PR URL with `--merge --match-head-commit`: auto mode refuses a merge
    no person approved, and the rule lets through Kogo's PRs alone. Not taken: `Bash(gh pr
    merge *)`, which would merge a cusoon2 PR; the alias `opus` for the model, which
    changes it with nobody deciding. The merge names the sha its checks ran on, since a
    run's wrapper may push after its `Fixed:`, and a head that moved is handed on. The
    poller sees a merge as the issue turning Done, so a PR without "Fixes DEV-NNN" starts
    no clerk and a Done set by hand is handed on. For a clerk that fails the poller posts
    its own `Clerk:` comment, the exit and the last lines, which the lead reads as a
    failed step and Greg restarts by hand, as the poller starts no step twice. A
    speculative run is labelled with `lin label`. A merge that changes the poller's own
    file is handed to Greg to restart it: a clerk that did would stop itself.
  - The merge takes the full sha, from `git rev-parse HEAD` in the worktree after the
    checks (Greg's Ruling 17, 20 Nov): `gh` matches the head against the whole one, and a
    short sha from a step's comment would be refused and handed on as a head that moved.
  - The clerk writes its comment and brief files under `/tmp`, in a `mktemp -d`
    directory, never in a checkout (Ruling 18): one left in the worktree makes it not
    clean, and the clerk that tidies after the merge would leave it and hand on.
  It changes five entries besides "The lead merges": in "Kogo's async runs" the table is
  now in `doc/clerk.md`; in "The side-by-side run" the clerk runs the comparison, under
  `doc/clerk.md`, "Merging"; in "The lead lists Greg's next moves in two lists" nobody
  says when a Not yet move is ready, a fresh "Lead" shows it; in "The lead does not run
  other sessions' steps" Greg starts only the steps kept in Zed, and the clerk launches
  the review and the fix; in "Greg answers the lead with numbers and letters" `884 back`
  is gone, the clerk seeing a Zed session's comment.
  Reopen if what is handed on waits too long for a "Lead", or the clerk merges what a
  lead would have stopped.
- **The clerk merges `main` into a Kogo PR behind it** (20 Nov 2026, Greg; DEV-1013).
  When the head does not hold `origin/main`, the clerk merges it in the worktree, pushes,
  runs the checks on the merged tree and merges on the new head, fetching just before the
  merge and doing the same once more if `main` moved; only a conflict launches a Fix. A
  clean merge of the base changes nothing the Review read, so it is the one commit the
  clerk makes. Not taken: a Fix run for each base merge (DEV-1005), which cost Kogo PR 111
  two runs, and whose cost grows with the square of the PRs open. A third tip of `main`
  is handed on, not chased. cusoon2 keeps the Fix, its check being cloud CI on the pushed
  head. Reopen if a clean merge breaks what a Review passed.
- **The lead's and the clerk's instructions, revised from a day's use** (20 Nov 2026,
  Greg, item by item; DEV-1023). Each cost a round trip on 20 Nov.
  - A Budget outgrown is handed on only when a Fix or a Question of the Review cuts
    something or doubts a thing added; otherwise its sizes go in `Merged:` or `ready`.
    Six hand-ons asked "accept the size?" and each answer was yes. The Review brief asks
    it to answer each thing that could go as a Fix, a Won't fix or a Question. It
    changes "what is handed on" in the entry on the clerk above. Not taken: handing on
    every overrun.
  - A cusoon2 `ready` line says which of two reviewed PRs merges first when exactly one
    is a port and the other could put it behind `renew`. Not taken: the lead holding small
    cusoon2 launches while a port is in its Fix, which idles work for as long as a Zed
    step waits on Greg.
  - A port's PR behind `renew` is caught up by a run, with no `kogo/run.sh`, when every
    path the merge brings, renames split, begins `doc/`, `infra/`, `frontend/` or
    `site/`. The run reads nothing there (read in `kogo/run.sh` and the Gradle build,
    20 Nov), so the comparison is the one the head passed. Not taken: the issue's list of
    the run's inputs (`backend/`, `jooq/`, `kogo/`), which had missed the Gradle build and
    fails open when an input is added; the four fail closed. Reopen when `run.sh` reads
    under one of them.
  - A run's step comment is under its marker line: the rule on a comment's first line was
    false for every run's as written, and one clerk reported a Handoff missing. It
    changes "a step's comment counts only when its first line is the one on the issue"
    in the entry on the clerk above.
  - A run that cannot start is tried again by the launcher, not the clerk: cusoon2's
    wrapper already retries once after a preemption, and a setup failure joins that
    (DEV-1027). Not taken: the poller starting a clerk to relaunch, a new event and a new
    rule for the same retry.
  - A change of a port's form that later pieces will copy goes on paper first, and a
    brief that proposes a rule asks for its count over every instance. The limit to a
    form others copy was taken here: a paper costs a run, a review, a fix and Greg's
    merge, and "changes the form" alone would be read as every port.
  - A choice about the language is put to Greg from its use: the plain code, the Kotlin
    today, then what each option's author writes.
  - `doc/roadmap.md` loses its State column, a day stale within a day of its writing
    (rows 6a, 6b, 9, 11 and 12 merged and blank). Not taken: the clerk marking a row at
    each merge, which needs a commit of the clerk's to `main`.
  - How a cusoon2 run gets Kogo's documents and tool is in `doc/clerk.md`, "Launching a
    run", where a brief's writer reads; the issue had it in cusoon2's
    `doc/plans/async-agent-runs.md`, under a heading that section does not send to.
  - Not taken: a notice to Greg when an issue is labelled `lead`. "Bump" stays.
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
- **`randomString(n)` is a prelude function, the second exception to "no ambient state"**
  (20 Nov 2026, Greg; DEV-994; spec 7, 12). A session's token is a secret that the page sends
  back, and `newId()` is an id, not one. It passes the test `newId()` was let in by, that its
  value never changes what a program does, so it is no new kind; the application's Kotlin and
  its tokens stay as they are. Not taken: the token made by Postgres in the statement that
  inserts it (`replace(gen_random_uuid()::text, '-', '')`), which needs no change to Kogo but
  changes the application's Kotlin first and the token's form, though it has the precedent of
  ids made in Postgres by a function both sides call (16 Nov). Each character is drawn by
  rejection: a byte of 248 or more, past four times 62, is read again, so each of the 62 is
  as likely as any other; a byte modulo 62 would make `a` to `h` a quarter likelier. Not
  taken: Go's `rand.Int(rand.Reader, 62)` per character, even too but a big integer each
  time. Reopen if a program needs another alphabet, random numbers or bytes, or a test needs
  a fixed token.
- **Kogo's ids are UUID version 7; an application's `main` may hand over its own** (16 Nov
  2026, amended 20 Nov). cusoon2's ids are its own layout, 65.5 µs ticks since 23 Feb 2018
  in the first 48 bits, made for index locality, which version 7 gives; Postgres 18 has
  `uuidv7()`. The two never sort together: a version 7 id made now sorts after cusoon2's ids
  from before about November 2021 and before every later one. So an application whose ids
  are not version 7 and that hands over no maker gets rows that do not sort with its own by
  id: it makes its ids in Postgres, by a function both sides call, or orders those queries
  by a time. Not taken: cusoon2's layout in the prelude, which is specific to one
  application. Amended 20 Nov 2026 (Greg; DEV-995; spec 7, 12): an application keeps its
  own format. Its `main` hands the runtime its id maker, in Go, with
  `kogo.MakeIdsWith`, once and before any id is made, else a panic; `newId()` then calls it,
  and with none handed over gives version 7. cusoon2's ids are on their own clock and never
  go backwards within a process, its Kotlin lists a booking's passengers in id order, and
  Kogo, with no state between calls, cannot write that layout. Kogo's own ids now increase
  within a process too (the lead's, 20 Nov), by RFC 9562's method 1 (6.2): `rand_a` is a
  12-bit counter, set at random below 2048 in each new millisecond, so a millisecond holds
  at least 2048 ids before borrowing the next; the time never goes back from the last id's,
  so a clock that goes back holds the last millisecond and counts on. Not taken: (b) a
  second kind of extension, an application's Go for a function that is not pure and changes
  nothing, the general layer, which waits for a second need; (c) the application moving to
  version 7 and no longer ordering by id; methods 2 and 3 of 6.2, a random increment of the
  74 random bits or a finer fraction of the clock, which hold as much but read less plainly
  in hex. Reopen (b) when a second function an application needs reads the clock,
  randomness or a remembered value.
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
    of one application; it says `action` if it changes something outside (20 Nov 2026,
    below); and it lands with its Go and a test (15 Nov). One that fails a test and is taken anyway
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
- **Only a binding that acts is marked: a binding says `action` or nothing** (20 Nov 2026,
  Greg, in the design session on outside calls; DEV-984; spec 7.1 and 13; `doc/gates.md`,
  "Only a binding that acts is marked"). It reverses `doc/gates.md`'s bullet of 17 Nov
  2026, "A binding says what kind of call it is", which put `lookup` before `external` on
  a binding that asks and changes nothing. Greg: "remove lookup with no test", "And no new words".
  - **Why.** The checker asks one thing of a binding, whether it may run in a transaction
    that can be rolled back and run again, and `action` answers it. Nothing read `lookup`;
    it put the clock, the console and a network call under one word; and the forced
    choice it was added for ("An outbound binding that says neither `lookup` nor `action`
    is an error", `doc/work.md`) cannot be checked without two new words. The idea of a
    lookup stays, and so do `Lookup`, `lookup(...)` and `ask` in `kogo.http`.
  - **The printed table** (`doc/gates.md`, "asks and acts"), for which the word was the
    reserved hook, is given whatever it needs to find an operation's `asks` lines when it
    is built.
  - Not taken: keeping the word, unread, with the spec saying so; a test that lists every
    binding on a handle with its decision, which asks the same author twice and misses a
    new handle class; a forced choice in the language, with a word marking a handle class
    and a third word for a binding that takes a handle and calls nothing (a guess of 20 /
    20).
  - Accepted cost: a binding that changes something outside and says nothing is its
    author's mistake, and nothing finds it. Reopen if one is found in the prelude.
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
    sketches; the name is cusoon2's to choose). The view `find` names carries it
    (*`find` superseded 23 Nov 2026 by "A worker keeps the loop over tenants" below*). The
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
    Built on 19 Nov 2026 (DEV-978) with the body's first 4 KiB, by the lead's Ruling on
    PR 97: see "Taken building `kogo.http`". What is safe to log at all stays open.
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
  keeps `fair tenant_id`. This reverses item 9 of the Ruling on PR 66. *Superseded 23 Nov
  2026 by "A worker keeps the loop over tenants" (DEV-1129) below: within a tenant, and no
  `fair`.*
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
     the port), read by `main`'s own struct, which refuses a key it does not know (23
     Nov 2026, DEV-1125: with `encoding/json`, not one in another case or given twice;
     `doc/main.md`, "Part by part", [1]); it grows with a service and not with a
     setting. Inside `settings` the application nests as it likes.
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
    no line to get wrong, and breaks nothing cusoon2 ships: its calls are `fetch()` POSTs
    in the default mode, and such a POST sends the real Origin whatever the referrer
    policy. (Corrected in place, DEV-964, from DEV-914's Lead comment of 18 Nov 2026 and
    DEV-916: this read "and no page of it sets a referrer policy", which is wrong, since
    nginx sets `Referrer-Policy: no-referrer` on every page. The answer stands, for the
    reason now given.)
    Answered after research Greg ran, which moved the session's recommendation from a to b.
  - **Accepted cost.** A form post, or a `no-cors` fetch, from a page of our own would
    get the 403, since each is served with `Referrer-Policy: no-referrer`; cusoon2 has
    neither. It differs from the Kotlin, so
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

- **Serving a gated row** (19 Nov 2026, DEV-961's run, not Greg; `doc/gates.md`, spec 15.3
  and 15.5). `rpc.Gated(name, gate, step, work)` is a gated row, and the handler runs the
  list's check, then the gate and the work in one SERIALIZABLE transaction.
  - **A step runs between the gate and the work, with no transaction open**, as
    `doc/gates.md`, "The outside step": the gate in a transaction that is rolled back, the
    step once, then the gate again and the work in one transaction. DEV-961's description
    also says the step "runs once the work's transaction has committed", which a step that
    may refuse before the work writes cannot do; the design was followed. Not taken: the
    step after the work's commit. Reopen: Greg meant the sentence as written.
  - **The gate's first transaction is rolled back, not committed.** A gate only reads, and
    until a proof's rules hold it to that (roadmap row 6), a write of a gate whose step then
    refuses is undone. Not taken: commit, one line fewer.
  - **A step is handed a proof whose `Tx` has ended**, and a statement on it is a panic, a
    500, until a proof's rules (roadmap row 6) make it `outside-passes-proof` at compile
    time. Gated work that is an `action fun` is handed one too.
  - **Gated work that is an `action fun` is `rpc.Outside(name, gate, work)`** (Ruling on
    PR 91, item 11; `doc/gates.md`, "An operation that runs wholly outside"): the gate
    once, in a SERIALIZABLE transaction that commits, then the work with none open. The
    emitter reads `Func.Action` and says which, since Go's types do not tell the two
    apart. Not taken: a flag on `Gated`, which leaves a step's argument that such a row
    never has.
  - **The check keeps its own read-only transaction before such a row**, as before every
    row. The Ruling's words are "the check and the gate in a transaction that commits";
    read as the gate's transaction committing, since the check runs before the name is
    looked up (`doc/origin.md`) and Postgres holds it to reading. Not taken: one
    transaction for both, which gives the check a transaction that can write. Reopen: the
    lead meant one transaction.
  - **A row with both `after` and an `action fun` as its work is refused under `step`**
    (Ruling on PR 91, item 10): no one has designed it. Not taken: serving gate, step and
    work all outside.
  - **`examples/25_reply.kg` names a check**, as 22 does, since the driver serves it;
    `examples/26_action.kg` stays `unchecked`, which nothing serves. The gated `action
    fun` that the tests serve is a row of `examples/24_gates.kg`, `testPrinter`. Not
    taken: a gated row in 26, which needs a gate and a check there too.
  - **A gate's SERIALIZABLE `Tx` is not read-only**, as decided on 18 Nov 2026 for a gate on
    a `Tx`; the check's is, by `kogo.ReadOnlyTx`, at READ COMMITTED.
  - **An unchecked list is `rpc.NewUnserved(ops, enums)`**, as `doc/origin.md` has it, and
    nothing takes one until `rpc.NewCalls` (roadmap row 11): its operations are compiled
    and not run. So `examples/22_api.kg`, which the tests serve, names a check, which
    refuses `Sec-Fetch-Site: cross-site`. Not taken: leaving 22 unchecked and unserved,
    which loses the served test of a `Db` operation's emitted Go.
  - **A shared gate is given its row's input by `rpc.Shared[I](gate)`**, so `Gated` has one
    form. Not taken: a second constructor for a gate that reads no input.

- **A test's transaction is SERIALIZABLE, and its block is handed a `SerializableTx`**
  (20 Nov 2026; DEV-969). `kogo.RolledBack` opens it at SERIALIZABLE, as stated and not
  left to the database's default, runs the block once and rolls it back. A test's
  transaction is alone, so nothing reruns it, and a serialization failure in it is a
  panic that fails the test. It follows from Kogo's own rule that a `SerializableTx`
  fits a `Tx` and not the reverse: every test written before still checks, and a test
  can now call a gate or a reader that takes a `SerializableTx`. Greg asked because it
  is language-wide, and he wants the language slightly more general than its first
  application needs where possible. Not taken: (b) a test states its kind
  (`test("...", serializable = true)`), the program's own words and the level the code
  runs at in production, at the cost of the word on nearly every test of a program that
  reads without locks; it can be added later, as a way to state READ COMMITTED, without
  changing a test already written. (c) such functions tested only through a served row,
  at the cost of a server and a row for each such test, and no test of a gate or reader
  alone.
  Reopen: a test that must see what another connection commits meanwhile, which shows
  where the test's `env` holds a `Db`.

## Taken in the v0 spec, not yet discussed

- Markers (`@RequiresOptIn`, `@file:OptIn`) are the one annotation form, so the access walls
  port. A wall is a data class with a marked constructor, as example 19's `HeldBusiness` is.
  `copy` and a sql row no longer get round that constructor (see "Agreed with Greg").
- Value classes are out: cusoon2 has none, and binding distinct id types to SQL columns needs
  a design (foreign keys could supply it).
- No `apply`/`run`/`with`, no labelled returns, no `object`.
- `Regex` is RE2: no lookaround or backreferences. (Widened by DEV-1083: the pattern is Go's.)

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
  also say `List<Long>.sum()`; members in a body would be a
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
  refused where it is declared; with it go `out` and `vararg`, which only a binding may
  write. Reopen if a program needs Go bindings of its own.

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
  use another of the same marker, lets `@OptIn` sit on one declaration (Kogo's error there
  says to opt in on the file), and lets `copy` skip a marked constructor's opt-in. One rule,
  checked at each call, constructor and read of a val. A type carries no marker, and
  `@RequiresOptIn` goes on nothing but a marker.
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
  read from the `go.mod` above it, and the Go imports `github.com/Cloud-Limo/kogo/rt` and
  `.../prelude/...`, so a program's module requires this one. Not taken: copying the
  runtime into `<out>`, which would free a program from this repo and is about 40 lines.
  Reopen when a program is built outside this repo.
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
  call's operands written out of the order of their slots (named arguments, a
  constructor's defaults) are read into temporaries first, as Kotlin reads them.
  `x in c` and `x in a..b` read the right side first, as Kotlin's `c.contains(x)` does
  (DEV-1157): the left first had been chosen as the order written, and DEV-1143 ran
  both and found `i in listOf(bump())` `true` in Kotlin and `false` here, and where
  both sides panic, the message the other side's. In `when (x) { in a..b -> }` the
  subject is read first in both. Only an order `mise run kotlin` compares is known to
  agree with Kotlin's; one nobody wrote a case for is not. Reopens: a case there that
  differs.
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
  has no corner to state. *Superseded for `İ` 20 Nov 2026 by "`lowercase()` follows
  Kotlin for `İ`" (DEV-1009) below.*
- **`lowercase()` follows Kotlin for `İ` (DEV-1009, 20 Nov 2026, the lead's call)**: it
  gives `i` and a combining dot (U+0069 U+0307), as Java's `toLowerCase(Locale.ROOT)`
  does, where Go gives a bare `i`. This reopens the decision above for that letter:
  cusoon2's logins now depend on it, since Go's answer turned an address Kotlin refuses
  into another person's. The rule is context-free, so it is one replacement before
  `strings.ToLower`. A final sigma stays the corner in spec 12: its rule reads the
  letters either side and no ported module depends on it. Not taken: following the sigma
  too, about 20 code / 15 test lines. Reopen the sigma when a port depends on it.

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
- **Not handled**: the request's `Content-Type`, which belongs with the CSRF checks.
  The body's size is the Handler's `MaxBody` (DEV-1056).

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
  `lowercaseContains` case's boolean is not compared when its text holds `σ`. Since
  DEV-1009 `İ` is compared, and the text `aİ` puts it in a case without `σ`.
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
    checker could not see. *The `queue` lines are superseded 19 Nov 2026 by "The work list
    names its alert in its head" (DEV-947, DEV-948) below; choice D's error stands.*

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
  DEV-894's "no commas in any list". A job's or an effect's parts are not a list: they
  stand one a line, with no commas. Not taken: B, commas only between bare names, which
  needs an exception for rows with words; C, commas only within a line, with no trailing
  comma. Reopen if a row of words needs to run over a line.
- **The work list names its alert in its head**: `work daemon alertedBy opsAlert { ... }`,
  its queues a list of names, in the shape of `api name checkedBy check { ... }` (Greg, 19
  Nov 2026, DEV-947). Not taken: the `alert` line and the repeated `queue` lines, which
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

## Taken renaming `Uuid.parse` and retyping `atStartOfDay` (DEV-932), not yet discussed

- **`ZonedDateTime.toInstant()` is bound with it**: spec 12 listed it, the prelude had
  not bound it, and without it a caller of `atStartOfDay` could not get its instant back.
  Not taken: leaving `atStartOfDay` with no way back to an `Instant`.
- **The 32-digit corner is stated in spec 12 and not held by `mise run kotlin`**, which
  does not cover `Uuid` (Kotlin 2.4's `parseOrNull` reads
  `01934f2abcde7f0189abcdef01234567`, run on 19 Nov 2026). Not taken: a `Uuid` case in
  the check against Kotlin here, which would have conflicted with DEV-933's PR 81 in the
  same files. DEV-952 adds it, with the corner as a `kept` row.

## Going public (DEV-941), not yet discussed

- **Apache-2.0, the license text verbatim in `LICENSE`, and the copyright line in
  `README.md`.** Not taken: editing the license's text to carry the name, or a `NOTICE`
  file, which Apache-2.0 asks for only when there are notices to pass on.
- **`mise run leaks` is gitleaks with its default rules, over HEAD's tracked files (`git
  archive HEAD` into a temporary directory, then `gitleaks dir`) and `origin/main..HEAD`.**
  Only what is committed is published, so a key in an ignored file such as
  `.mise.local.toml` does not block a merge. The commit scan fails when the range is empty
  or cannot be resolved, since gitleaks would scan nothing and pass. It needs
  `origin/main` fetched, as the merge step's worktree has. Not taken: `gitleaks dir .`
  over the working tree, which reads ignored and uncommitted files; a `.gitleaks.toml` of
  our own, which nothing found so far needs; a git hook, which a fresh worktree or an
  async clone would not have.
- **The light rule extends rule 1 rather than becoming a rule of its own,** so the rules
  keep their numbers, which reviews cite.

## Taken checking `kogo.time` and `Either` against Kotlin (DEV-933), not yet discussed

- **Kotlin's `Either` is Arrow 2.2.3's `arrow-core-jvm`, fetched by `mise run kotlin`
  from Maven Central into `~/.cache/kogo` and checked by its sha256**, the version
  cusoon2 builds with (`backend/build.gradle.kts` on `renew`). `kotlin/arrow.kt`, in
  the shared source's package, names it and its extension functions as the prelude does, since Kogo
  imports neither and Kotlin has no default import to add. Not taken: a Kotlin `Either`
  written here, which would test Kogo against itself; a Gradle build.
- **The time cases keep to known zones and to dates a million days inside `LocalDate`'s
  range**, as three differences found there are left to a fix of their own: an unknown
  zone's message, `plusDays`' message past the range, and `atZone` past year
  ±999,999,999, where Kogo gives a date and Java throws. Not taken: listing them in
  `kept`, which holds only the differences `spec.md` states. DEV-952 fixes them and
  widens the cases.
- **`Clock.now` has no case**: its value is the time of the run, and the prelude gives no
  way to build a fixed `Clock`. `Uuid.parseOrNull`, `atStartOfDay` and `toInstant` are
  DEV-952's (corrected: this line left them to DEV-932, which added none).

## Taken making `kogo.time` fail and name zones as Java does (DEV-952), not yet discussed

- **`ZoneId.of` refuses the names Go loads and `java.time` 21's tz database lacks by a
  list in the code** (`EST`, `HST`, `MST`, `ROC`, `Factory`, `posixrules`, `localtime`,
  `Local`, and `posix/` and `right/`), found by comparing `ZoneId.getAvailableZoneIds()`
  with the host's `/usr/share/zoneinfo` and Go's `time/tzdata` on 19 Nov 2026. Not
  taken: an allow-list of Java's 604 IDs in the code, which would also refuse names a
  newer tz release adds. Reopen when a tz release adds a name one side lacks.
- **`plusDays` on a date in December of year 999,999,999 panics with `Invalid value for
  Year` for a sum within 59 days**, as Java's short path does. Not taken: the EpochDay
  message alone, which the brief named, as it differs from Java there.
- **The cases add `zoneOf`, `ZoneId.of` of the shared texts**, which reaches the
  invalid-format messages; `zone()` holds `+05:30`, `GMT+1`, `UTC-0`, `-0930` and `Z`
  besides the brief's unknown zone and `EST`, and `Factory`. Not taken: one offset only.
- **`Uuid` reaches the shared source by `import kogo.Uuid`**, which Kogo accepts, and
  `kotlin/kogo.kt`'s typealias for kotlinc. Not taken: a typealias in package `builtins`,
  as `arrow.kt` does, which the brief's `kogo.Uuid` did not ask for.
- **`offsetOf` picks its offset by `n * 10 + m`**, as `n` alone reaches 11 of the
  20 offsets and Kogo has no `Long.toInt()`. **`plusDaysMax` is checked to fail without
  the Year corner** (101 differ), not without the UTF-16 fix, which it cannot reach; the
  Ruling's "each new case fails with item 1's fix taken out" holds for `offsetOf` (64
  differ). Reopen if a case gets a wider index than `n`.

## Kogo is a subset of Kotlin (DEV-957), agreed with Greg 19 Nov 2026

- **Kogo is a subset of Kotlin and `java.time`, never a difference: choice (a)** (19 Nov
  2026, Greg). "Kogo is a subset, never a difference. Where Kogo has a name, it means
  exactly what Kotlin's or `java.time`'s means. Where Kogo stops short, it refuses: at
  `kogo check` where it can, else a panic naming the limit. It never gives a different
  answer. The spec states what Kogo supports in general terms, never in terms of cusoon2's
  data; which parts get built follows the first port (DEV-914, 3a)."
  - Not taken: **(b)**, every name matches Java on every input, error messages included,
    which drew PR 85's 116-line offset parser and DEV-955 for zone IDs cusoon2 never stores
    (it stores Google's region names).
  - Not taken: **(c)**, zone arithmetic moved to Postgres, which turns every zone-touching
    port into a SQL rewrite.
  - So `ZoneId.of` takes region names only and refuses an offset ID with Kogo's own
    message (spec 12); DEV-955 is canceled.

## Taken refusing offset zones (DEV-957), not yet discussed

- **An offset ID is `Z`, one starting `+` or `-`, or `UTC`, `GMT` or `UT` followed by `+`
  or `-`**: exactly the IDs `java.time` hands to `ZoneOffset`. Bare `UTC`, `GMT` and `UT`
  stay, being `java.time` regions; `UT`, which the tz database lacks, is UTC by a line of
  its own. A one-character ID other than `Z` keeps Java's `Invalid ID for ZoneOffset,
  invalid format` message, as `zoneOf` reaches it with ordinary text. Not taken: refusing
  `UT` too, which Java accepts as a region.
- **`mise run kotlin` counts a refusal by a `refused` rule beside `kept`, decided by the
  input**: the cases `zoneId` and `offsetId` give Kotlin's own ID on the same inputs, and a
  case named counts as refused only when Kogo's result is exactly the limit panic and that
  ID is an offset ID by `java.time`'s rule; the limit on any other input differs (Review
  1 and its Ruling). Not taken: deciding by Kotlin's output (refused unless Kotlin's
  result is a panic inside the subset), which let a Kogo refusing a valid region pass.
- **`kogo check` refuses `ZoneId.of` on a string literal that is an offset ID**, code
  `zone-offset`, with the limit's message (the lead's Ruling, Fix 6); a literal is one with
  no template. The rule is the prelude's, written again in `check` so neither package needs
  the other (rule 3). Not taken: sharing one regexp between them.
- **The zone cases index `zone()` by `n * 10 + m`**, as `offsetOf` does: by `m` alone
  they never reached indices 8 to 12, the offsets, which DEV-952's cases therefore never
  compared. Not taken: reordering the list, which `m`'s edges reach only 10 of 15 of.
- **`LocalTime`'s text is tested in Go, not in `mise run kotlin`**: no prelude function
  gives a `LocalTime`, so the shared source cannot make one. Not taken: adding
  `ZonedDateTime.toLocalTime()` for the check alone. Reopen when a port needs a name that
  gives one.

## Taken parsing and checking the `api` list's forms (DEV-950), not yet discussed

- **The old shape is refused, with no grace**: a row with neither `gatedBy` nor `ungated`
  is a syntax error that gives both forms, and a head with neither `checkedBy` nor
  `unchecked` is the checker's `no-check`. Every list of the repo's examples and tests
  moved to `unchecked` and `ungated`. Not taken: reading a bare row as ungated, which
  guesses at what the list must say.
- **The row's forms are the parser's and the missing head the checker's**, as DEV-950's
  scope has them: the tree holds a row only in one of its three forms, and a head with
  neither word. Not taken: both in the parser, which stops at the first list and gives
  the checker's other errors no chance.
- **A list's words are not reserved, an operation's name included.** A line that begins
  with `gatedBy`, `after` or `ungated` goes on the row above and is refused ("a row stays
  on one line: move gatedBy onto a's line"), unless its next word, on that line, is
  `gatedBy` or `ungated`: then it is the row of an operation of that name. A gate's or a
  step's name on a later line is refused with the same message. Not taken: refusing the
  three words as an operation's name, which makes Kogo's lists keep words from Kotlin
  names. Reopen if a list gains a word that may follow `after` on its line.
- **The check's shape is checked here**, `(Tx, Env, Request): Either<String, Unit>` under
  the code `check-shape`, as `doc/origin.md` names it, since a list that names a check
  must name something. Not taken: leaving the shape to the Origin check's issue (roadmap
  row 4), which would let any fun stand there until then. That it only reads is not
  checked.
- **A gate takes a `Tx`** until `SerializableTx` is built (roadmap row 5), and a `Db` is
  refused. **A proof is any type** until a proof's rules are (row 6), and gated work and
  a step take the very type the gate gives, not one it fits.
- **A gate's proof is not a `Tx`, a `Db` or a `Request`** (Review 1 of PR 88): gated work
  takes none of them, and a proof of that type would hand it one. Refused under `gate`, the
  gate's code, since the gate gives it. This is the one proof rule before row 6. Not
  taken: refusing it at the work's second parameter, where the same type is fine for an
  ungated operation's first.
- **A step returns `Either<String, Unit>`** as well as taking the gate's proof and the
  operation's input, since `doc/gates.md` hands its answer to nothing. Not taken: the
  proof and the input alone, which the issue named. That it is an `action fun` is row 7's.
- **A gate that reads an input stands in one row of a list**: the second row to name it
  is refused. Counted for each list, since a fun may stand in more than one. Not taken:
  allowing it where the rows' inputs are one type, which the design does not have.
- **One table of roles drives the shapes** (`check/api.go`, `role`), as `doc/lists.md`
  asks, with one error code a role: `operation`, `gate`, `check-shape`, `step`. An
  ungated operation's messages now say "an ungated operation".
- **`kogo emit` refuses a check and a gated row** (`not-yet-emitted`) until the server
  runs them (row 3), and `kogo ts` writes a gated operation's types as any other's. So
  `examples/24_gates.kg` is parsed and checked by the tests and left out of the emitted
  program, whose test asserts the refusal. Not taken: emitting the table without the
  check or the gate, which would serve the work with nothing before it.

## Taken answering an ungated operation with `Reply` and cookies (DEV-963), not yet discussed

- **`SameSite` is an enum of `kogo.rpc`, `Strict`, `Lax` and `None`**, which
  `doc/gates.md` names and does not declare. Not taken: a `String`, which a typo would
  pass.
- **`maxAgeSeconds` of 0 or less ends the cookie (`Max-Age=0`)**, as HTTP reads it; a
  cookie with no `Max-Age`, one that ends with the browser, cannot be written. Not
  taken: Go's reading, where 0 leaves `Max-Age` out. Reopen when a cookie must last only
  the browser's session.
- **A cookie HTTP cannot carry panics** (500, and a `Tx` operation rolls back, since the
  answer is written inside its transaction). Not taken: Go's `http.SetCookie`, which
  drops it with a log line, so the browser silently goes without it.
- **A cookie a browser refuses panics too** (review of PR 89): `SameSite.None`, or a
  `__Secure-` name, without `secure`, and a `__Host-` name without `secure` or the path
  `/`. The prefixes are read in any case, as browsers read them. Not taken: only
  `http.Cookie.Valid`, which lets these through to a browser that drops them, or a
  case-sensitive prefix. `Domain` is not checked: `Cookie` has none.
- **A `Reply` anywhere else on the wire is one `wire` error** that says only an ungated
  operation's whole answer is one, gated work's answer included. Not taken: an
  `operation` error for gated work, a second rule for the same fact.
- **The server finds a `Reply` by a method of the Go type** (`answer` in
  `prelude/kogo/rpc`), so the emitter writes `rpc.Tx` and `rpc.Db` as before. Not taken:
  `rpc.TxReply` and `rpc.DbReply`, and an emitter that tells them apart.
- **`examples/25_reply.kg` is a list of its own**, served by the driver, so 22 stays as
  it was while DEV-961 and DEV-962 change the same files.

## Taken building `action fun` (DEV-962), not yet discussed

- **`Console.println` and `print` are actions; `Console.readln` and `Clock.now` are
  lookups.** The issue asks that an existing binding carry its kind, and an `action fun`
  must call an action, so an example needs one in the prelude before `kogo.http` (row 9).
  Printing cannot be rolled back and happens twice when run twice. No example or test
  printed before. Not taken: no action until row 9, which leaves no example that checks.
  Reopen if a command that prints inside a transaction is wanted.
- **The kind is a word before `fun`, in either order with `external`**, since the parser
  reads modifiers in any order: `doc/work.md` writes both `action external fun` and
  `external lookup fun`. Nothing reads `lookup` yet; the printed table will. (`lookup`
  was removed on 20 Nov 2026, above.)
- **A binding that says neither kind is not refused**: nothing tells the checker which
  bindings call outside until `kogo.http` lands. Not taken: every binding on an external
  class says one, which would mark `List.map`.
- **"In a transaction" is "in a block handed a `Tx`"** (`action-in-tx`), which is
  `db.tx`'s block and any other the prelude gains. Not taken: knowing `db.tx` by name.
- **A step that is not an `action fun` is refused under `step`**, the role's one code, and
  only where its shape otherwise fits, so one fault gives one error.
- **The word stays true is `idle-action`; a `::` reference, a member and a kept lambda
  are `action-kept`.** A local fun cannot be an `action fun`: a call in one is
  `unmarked-action`, whose message says to move the call into the action fun around it
  (review of PR 90), and `action fun` in a body is the parser's error. Not taken: letting
  a local fun be one, which nothing needs.
- **Left to row 6 (a proof's rules)**: that an `action fun` takes a proof only as a step
  or as gated work that runs wholly outside, and `outside-passes-proof`.
- **Serving a gated `action fun` with no transaction was left to DEV-961**, and so was
  refusing a row with both `after` and an `action fun` as its work (Ruling on PR 90): both
  are under "Serving a gated row" above.
- **Printing is an action** (Greg, 19 Nov 2026, answer (a) to the Ruling on PR 90's
  question): `Console.println` and `print` stay actions, as built. Not taken: (b) printing
  is neither kind. Reopens when logging is designed, with its own handle rather than
  `Console`.
- **Open: a `kogo test` cannot call an `action fun` today**: a test is a `val`, and its
  block is handed a `Tx`. Decided in row 9 with the fake `Http`, and brought to Greg then.

## Taken telling `SerializableTx` from `Tx` (DEV-965), not yet discussed

- **Only the literal `true` makes `db.tx`'s block a `SerializableTx`.**
  `db.tx(serializable = flag)` hands a `Tx`, which is true at either level. Not taken: a
  second fun, `db.serializableTx { }`, which `doc/gates.md` did not write. Reopen if a
  program wants the level from a value.
- **`SerializableTx` is the checker's alone**: core.kg declares it beside `Tx`, the
  checker makes it fit a `Tx` (an external class has no supertypes in the syntax), and
  the emitter writes it as Go's `kogo.Tx`. Not taken: a Go type of its own, which would
  need a conversion wherever one is passed as a `Tx`.
- **A plain `db.tx` is run once at READ COMMITTED, and a deadlock in it is a panic, as
  before; a `Tx` the server opens is rerun on a deadlock** (`doc/gates.md`). Spec 10.7
  now says so of `db.tx`, and a test holds each. Not taken: rerunning a plain `db.tx`.
- **A list's check takes a `Tx`, and a `SerializableTx` there is refused**: the server
  runs it read-only at the database's default and does not rerun it (DEV-964 owns
  `Handler.check`). Not taken: accepting one, which would let it read with no lock where
  nothing reruns it.
- **The rows' constructors take the kind as a bool after the name**:
  `rpc.Tx(name, serializable, f)`, `rpc.Gated(name, serializable, ...)`,
  `rpc.Outside(name, serializable, ...)`. Not taken: a constructor for each kind, six in
  all.
- **The refusal is `needs-serializable`, at the argument**, and names the called fun,
  not the caller as `doc/gates.md`'s sample does: the checker has the call at hand. It
  offers taking a `SerializableTx` too, the change to make in a gate on a `Tx`.
- **Not done here: the printed table's `runs serializable` / `runs plain` line**, which
  waits for the printed table.
- **Open: a `kogo test` block is handed a `Tx`**, so a test cannot call a fun that takes
  a `SerializableTx`. The Review on PR 93 leans to `RolledBack` opening at SERIALIZABLE
  and handing a `SerializableTx`; it is Greg's, brought to him before roadmap row 13. Not
  taken now (the lead, 19 Nov 2026): building that here, which changes what a test may
  call without his word. Settled since (DEV-969, above under Agreed with Greg): a test's
  block is handed a `SerializableTx`.

## Taken serving a list's Origin and CSRF check (DEV-964), not yet discussed

- **The check is an example, served, and no code of the prelude's.** `doc/origin.md`'s
  check and session reader are Kogo an application writes, so they are written as
  `examples/27_origin.kg` and `examples/28_session.kg` in names of their own (a shop's
  domain, an admin's origin) and served by `TestEmittedGoRuns`, one case for each row of
  "Part by part" that shows at use or never. `rpc.go` does not change: `request.header`
  and `request.cookie` already read the four things a check reads, and PR 91 built the
  rest. Not taken: `sameSite` or `tokenMatches` in `kogo.rpc`, which would put one
  application's settings, table and cookie into the prelude. Reopen: a second
  application that writes the same check.
- **The prelude gains `String.endsWith` and `List.contains`**, which `sameSite` and
  `isOurOrigin` call (`origin in settings.extraAllowedOrigins`). Both were in `spec.md`
  section 12 and not yet built; `contains` compares as `==` does. 7 / 6.
- **A cookie's text is read with `Uuid.parseOrNull`, not `toUuidOrNull`.**
  `doc/origin.md` invented `toUuidOrNull` on 18 Nov 2026, before `Uuid.parse` became
  `Uuid.parseOrNull` (DEV-932) and before Kogo was held to a subset of Kotlin (DEV-957):
  Kotlin has the second and not the first. Not taken: adding `toUuidOrNull`, a second
  name for one function.
- **`liveSession`, `session` and `whoAmI` take a `SerializableTx`**, as `doc/origin.md`
  has them (the lead's Ruling on PR 92, 19 Nov 2026, once DEV-965 merged). This replaced
  the entry that had them take a `Tx` until DEV-965 landed. The list's check,
  `fromOurPage`, and `tokenMatches` stay on `Tx`: a check on a `SerializableTx` is
  refused (DEV-965, above). Not taken: merging on `Tx` with a follow-up issue, which
  left the example reading its session at READ COMMITTED meanwhile.
- **The example's cookie has one name, `SID`.** The design's `isSecure(request)` and
  the `__Host-` prefix are the application's and read nothing of the check's.

## Taken showing an operation's five kinds in one table (DEV-970), not yet discussed

- **An operation that must ask outside asks in its transaction and keeps its gate, unless
  many requests would wait at once** (Greg, 19 Nov 2026). Spec 15.1 says so under the
  table of the five kinds, which only gathers what 7.1, 15.2 and 15.3 already hold: a
  lookup is allowed anywhere, and "never inside a transaction" is no rule of Kogo's. The
  cost, a transaction and its connection held while the service answers and the question
  asked again on a rerun, is taken over a gate written by hand. Not taken: the table at the
  end of 15.3, which says 15.1's rows twice; trimming 15.3's bullets to remove the overlap
  with the table's Transactions column, which rewords what the spec promises in a docs
  change. Reopen on a measured wait that is felt on an operation asked rarely.
- **The table's first bullet stays**: it names what serves a row (a worker, in
  `doc/work.md`) without the word, since the spec has no workers yet. Not taken: leaving it
  to the change that brings workers into the spec, which would drop the shape that most
  changes outside should take from the one place that says which shape to pick. Reopen:
  workers come into the spec.
- **The ungated line with one transaction says "taking a `SerializableTx` or a `Tx`"**, where
  the issue said "taking a `Tx`": 15.2 and 15.3 on `main` (DEV-965) allow either. Not
  taken: "taking a `Tx`", which reads as refusing a `SerializableTx` that 15.2 allows.
- **The last line's first cell adds "or asks outside with no transaction open"** (the
  lead's Ruling on PR 94, 19 Nov 2026; Greg is told), so that "take the first that does
  the job" lands a search box on the line the second bullet sends it to. Not taken:
  leaving that case to the bullet alone.
- **15.3's `completeLogin` is an `action fun`**: it calls `redeemCode`, which changes
  something at another service, so written as `fun` it was `unmarked-action` by 7.1 (the
  lead's Ruling on PR 94). Not taken: a docs issue of its own for one word.

## Taken holding a proof's rules (DEV-976), not yet discussed

- **The checker knows a proof by its shape**: a data class whose constructor is marked and
  which has a marked property that is a `Tx` or a `SerializableTx`, or an interface that
  declares such a `val` (`doc/gates.md`'s `OnBooking`). Not taken: by role, whatever type a
  gate gives, which misses the proofs no gate gives (`AtTenant`, a new row's, an interface)
  and the model's files, where the two rules on the transaction are read; a marked `Tx`
  alone, which the design does not call a proof.
- **A gate that gives a transaction gives a proof** (Greg's choice C, 20 Nov 2026, DEV-982):
  a `P` that holds a `Tx` or a `SerializableTx` anywhere but as a proof's own marked one
  is `gate-proof`, said once per gate, at its first row; a `P` of facts alone stays a
  plain data class. Not taken: A, every gate gives a proof, which makes every gated
  program carry a marker and an opt-in for facts that need none; B, as before, where a
  port that forgets the marker gets no rules and no error, PR 91's 500 included.
- **"Holds" has no limit** (the lead's Ruling on PR 99, 20 Nov 2026): `heldTx` walks every
  named type, its type arguments (`List<Plain>`, `Box<Tx>`, `Map<String, Tx>`), a sealed
  interface's variants, a function type's result and nullables, at any depth, and does
  not stop at a proof: it skips only the proof's own marked transaction. So a proof's
  second, unmarked `Tx` (`Two(val other: Tx, @property:M val tx: Tx)`) and a plain class
  inside a proof are refused too, since each hands work a transaction no rule holds,
  the same hole by another door. A `P` that holds a `List` of proofs passes. This
  reverses PR 99's first cut, which stopped at a proof and at type arguments. The error
  names the type that holds the transaction, and says to mark an interface's `val`, a
  data class's constructor and transaction, or take a proof's second one out. Not
  taken: refusing in an `action fun` any `Tx` it did not open, which closes doors a gate
  cannot see but is a rule on actions, not C. Reopen: a port whose gate must hand on a
  transaction in a shape this refuses.
- **`examples/24_gates.kg` moved to C**: `OpenInvoice` is a proof marked `@DeskOnly`, and
  `statusOf` and `unpaid` take a narrower proof, `AtDesk`, of the transaction alone, from
  a gate `atDesk` that calls `clerk` and binds its answer. Not taken: binding
  `${clerk.address}` in their statements, which would scope by nothing the SQL needs.
  `clerk` now gives `Clerk(address)`, facts alone, to `testPrinter`, the action fun that
  reads only facts: C's lighter half, shown.
- **A marked property is built here** (spec 11): a marker on a data class's property was a
  syntax error, and an interface's marked `val` was read and not held. Both are now
  marked, and read only in a file that opts in, by name or by destructuring. The proof
  rules rest on it. Not taken: a proof's transaction known by its name, `tx`.
- **A data class's property is marked `@property:M`** (the Review and Ruling on PR 98, 19
  Nov 2026), and a bare `@M` before its `val` is refused with that to write: kotlinc 2.4.21
  refuses the bare form ("opt-in requirement marker annotation cannot be used on
  parameter") and takes `@property:M` with the meaning wanted, so by principle 3 Kogo
  writes Kotlin's. It is the one target there is. An interface's `@M val` is Kotlin's
  as it stands. Not taken: the bare form as a deviation listed in `kotlin/deviations`.
- **An override of a marked member needs the file's opt-in** (the same Review, Fix 2), a
  `val` or a `fun` of an interface, as kotlinc has it: without it a file that does not opt
  in made a data class of an interface proof from any `Tx`. Not taken: making the
  override carry the marker too, which the read through the interface already holds.
- **A `_` in a destructuring takes no transaction out of a proof, and still needs the
  opt-in of the property in its place.** The Review's Fix 5 skipped both; kotlinc 2.4.21
  refuses `val (id, _) = held` in a file that does not opt in to the marker of the second
  property, so skipping the opt-in would accept what Kotlin refuses. Not taken: the
  Review's whole Fix. Reopen: a later kotlinc accepts it.
- **A statement on the transaction of a proof with no name binds nothing** (Fix 3):
  `xs.first().tx.execute(sql"${id}")` matched the bare `id` of `this`. It is
  `unbound-statement`, with "keep the proof in a val" to do.
- **A block that takes or gives a proof counts as passing one** from a fun that runs
  outside (Fix 4), whatever fun it is handed to, the prelude's included. Not taken:
  following the block into the fun that calls it.
- **Not seen by `action-proof`: a proof held in a list or in a plain data class**, as
  `xs: List<Held>` or `Box(val held: Held)`. Only a file that opts in can make one: a
  step or work that runs outside cannot, since building either passes the proof.
- **The codes** are the design's `proof-tx-escapes` and `outside-passes-proof`, and two it
  did not name: `unbound-statement` and `action-proof`; DEV-982 adds `gate-proof`.
- **"Written directly" is `proof.fact` or a property under it**, `${held.business.id}`,
  spelt on the same name as the receiver. Not taken: `proof.fact` alone, which refuses a
  proof that keeps its row as a data class, as spec 11's `HeldBusiness` does.
- **A statement on a proof's transaction in a fun that runs outside is refused too**, under
  `outside-passes-proof`: the design says SQL in a step is the checker's error, and in a
  file that opts in nothing else refused it. Not taken: leaving it to the marker, which
  holds only the operations' files.
- **Passing counts a receiver, and a proof of any type**, `booking.tenant` included. Only
  an `action fun` that takes a proof is held to it, as the issue says. Not taken: every
  `action fun` outside a `db.tx` block, which no case needs.
- **`copy` fills a proof's transaction as its constructor does** (spec 11 holds a copy to
  the constructor's marker), and destructuring a proof down to its transaction is
  `proof-tx-escapes`.
- **The rules are 15.7, after TypeScript**, so that no section is renumbered under the
  messages that cite them.

## Taken building `kogo.http` (DEV-978), not yet discussed

- **A call that gives no answer gives an `HttpFailure`, `ask` and `perform` alike**
  (Greg, 20 Nov 2026; Ruling on PR 97): `NotSent(why)`, `Unknown(why)`, `Refused(status:
  Int, body: Json?)` and `Unreadable(why)`, by how far the one call got. The failure is
  the handle's, not the kind's: the kind decides which call takes an endpoint, where it
  may be made and whether it may be sent twice, and a network fails one way for both.
  What the kind changes is what a caller makes of `Unknown`, and the caller knows which
  call it made. It replaces `HttpError` (`Refused` or `Failed`), which the run that built
  this gave `ask`, and `SendFailure`, named when `perform` was still written `send`. Not
  taken: the two types as built, renamed to pair (`LookupFailure`, `ActionFailure`), under
  which a client of both kinds writes each helper twice; two that share `Refused` as a
  top-level class.
  - **One rule sorts an answer, whichever call made it**: no connection is `NotSent`; a
    4xx is `Refused`, the body `null` when it is not a JSON object; a 2xx that is not a
    JSON object is `Unreadable`; a 3xx, a 5xx, no answer, and an answer that broke off or
    is past the limit whatever its status, is `Unknown`.
  - **`Unreadable` is its own case**: the service said yes and the answer cannot be read.
    A client raises it for an answer that lacks what the service always sends, in a lookup
    and an action alike, and an `act` that sorts a failure faces it as a branch of its own.
    Not taken: three cases with the client borrowing `Unknown`, which says "may have been
    done" of a call that was answered; a panic, since what the runner makes of a panic in
    `act` is not written.
  - **An `HttpFailure` is one call's.** A function that makes more than one call gives
    what the whole came to, in a type of its own, as an `Act` is: passed on from two
    calls, a failure no longer says which call it was, and a lookup that cannot connect
    after an action was done would read as `NotSent`. Spec 12 says so and nothing checks
    it. Not taken: a check that a function giving an `HttpFailure` makes one call (a
    guess of 15 / 15), left to the runner's build.
  - **An endpoint's kind is its author's word** (spec 12). Not taken: refusing a `lookup`
    by `PUT`, `PATCH` or `DELETE`, which cannot refuse `POST`, the method that charges.
- **Mail does not share HTTP's failure**, which reverses 17 Nov 2026's `Smtp.send` giving
  a `SendFailure` kept with the runner's package (Greg, 20 Nov 2026): "Absolutely a: SMTP
  failures have their own disticnt shape from HTTP failures and it's silly to conflate
  them. These are implementation level types: if we need consistency in higher level
  code, someone can make a type or use and existing domain level type." The runner reads
  an `Act`, which the application makes, and never a transport's failure, so nothing
  holds both. `doc/work.md` shows mail's as `SmtpFailure`, with a `Refused(code, reply)`;
  its name and cases are settled when `kogo.mail` is built. Not taken: one shared type in
  a package of neither transport, whose `Refused` could not hold a `Json?`.
- **A send has left once it has a connection**, so `NotSent` is a failure to make the
  request or to connect, and everything later is `Unknown`. Not taken: once its headers
  are written, which calls a write that failed half-way not sent.
- **`perform` is kept from a second send by a body Go cannot make again**
  (`Request.GetBody` nil), which is what Go's transport asks before it repeats a request,
  a `GET` or one under an `Idempotency-Key` included. An empty `POST`, `PUT` or `PATCH`
  under no key is left alone: Go does not repeat one, and a body of unknown length is
  sent chunked. Not taken: a new connection for every action, a handshake each time. **`ask` may be sent again whatever its method**:
  it carries Go's own mark (an empty `Idempotency-Key`, which is not sent on the wire), so
  the rule is the kind's and not the method's. Not taken: Go's default, under which a
  lookup by `POST` is not sent again and one by `GET` is.
- **HTTP/1.1 only.** The rule for a second send was read in that protocol's transport;
  HTTP/2 has rules of its own for a stream the server refused. Reopen when a service
  needs HTTP/2.
- **No proxy can be stated.** The design's row is "no proxy unless `main` states one",
  and no application has one to state: `Service` has no such field until one does.
- **An answer past the limit is a failure whatever its status**, a 4xx included: it is
  not read, so its status is not trusted either. Not taken: `Refused` with a `null` body.
- **`Json.long` reads digits only**: `1.0` and `1e3` are `null`, though each is a whole
  number, and `double` reads them. Not taken: exact arithmetic on the exponent, which a
  hostile answer can make as large as it likes. Reopen when a service on the roadmap
  writes a whole number another way (Ruling on PR 97).
- **A 2xx answer with no body is an object with no keys**, since a service may answer an
  action with nothing; a 2xx body that is not a JSON object is `Unreadable`. Not taken:
  `Json?` as the answer.
- **An id that is empty, `.` or `..` panics, and so does a method that is not one of
  five, a path that does not begin with `/`, and a written part of a path that holds
  what a path escapes** (a `?`, a space, a `%`, a non-ASCII byte: each piece between
  `/` must be its own `url.PathEscape`). Go sends the path as written only when it is
  a valid escaping; when it is not, it escapes the unescaped path again, and an id's
  `%2F` has by then become a `/`, so an id from a browser could name another endpoint
  (Review of PR 97, Fix 1). Not taken: escaping the written parts for the author, which
  sends a path the declaration does not show. The Scope names two panics, for the
  text around an id. `..` from a browser is escaped to itself and names the path above:
  the same hole the escaping closes. An endpoint is made where the program starts or
  from an id, so a wrong one is found at once.
- **A header is set once**: `withHeader` panics on one that an earlier `withHeader` set,
  as on one of `main`'s. Not taken: the later value wins.
- **A form's and a query's fields keep the order written**, and `main`'s query fields go
  first, by name. A `Json` field in a form is its text.
- **`Service.Timeout` is required, as `MaxAnswer` is** (Ruling on PR 97, the lead, 19 Nov
  2026), until the one deadline of `doc/work.md`, "Time limits", is built: `New` gives
  an error without it, as it does without `MaxAnswer`, and `doc/main.md`'s three handles
  each state a placeholder. Not taken: optional, where a silent service holds a
  goroutine and, in a transaction, a connection, for ever. Reopen when the deadline is
  carried.
- **A refused body is logged up to 4 KiB**, cut with a mark that says it was cut and how
  long it was (Ruling on PR 97, the lead, 19 Nov 2026; it narrows the "status and body"
  of Q9 above), at `Warn`, with the method, the path and the status. Not taken: whole,
  which puts up to `MaxAnswer` of a service's echo of personal data in one log line; a
  choice for each handle, which no application has asked for. What of a body is safe
  to log at all stays open in `doc/work.md`. The query is not logged, nor any header: a
  credential may be in either. A 4xx is logged as `kogo.http: refused` and a 3xx as
  `kogo.http: redirect not followed` (the lead's Ruling on PR 97, 20 Nov 2026; both
  became `kogo.http: failed` with DEV-1050, below), since
  `Refused` is a 4xx and nothing else and a 3xx is `Unknown`. Not taken: `refused` for
  both, the words of Q9, which say of a `302` what its value does not. Each is logged
  before the answer is judged whole, so a refusal past the limit or broken off is
  logged with what was read, and the cut mark's length counts what was read.
- **A 3xx is `Unknown`, to `ask` and to `perform`**, which reverses 17 Nov 2026's "a 3xx
  is reported with its status, as a 4xx is" (Greg, 20 Nov 2026; Ruling on PR 97).
  `Refused` is a 4xx and nothing else. A `303` after a `POST` says the service acted, and
  `doc/charge.md` reads `Refused` as nothing done; one rule sorts both calls; and no
  client can follow a redirect, since an answer's headers are not exposed. It is still
  logged with its status and body. "No redirect is followed" stands. Not taken: as first
  built here, `Refused` for `ask` and `Unknown` for `perform`; `Refused` for both.
- **Three more of Go's defaults are off** (Review of PR 97, Fix 2): no `Accept-Encoding`
  is sent, so an answer is never unpacked on the way to the limit; no `User-Agent`
  unless `main` states one among the headers; and an answer's headers are held to
  `MaxAnswer`, on their own count and not in one sum with the body, where Go reads 10
  MiB. Not taken: one count over headers and body, which needs a reader of our own
  under Go's; a floor for small limits, one more figure nobody stated. A handle whose
  `MaxAnswer` is under a service's few hundred bytes of headers fails every call.
- **`Http` has no `String`, a `DELETE` is not marked against a second send (Go repeats
  none), and the transport copies no timeouts of Go's default one** (Ruling on PR 97,
  13): a kept connection now idles until the service closes it, and a TLS handshake
  is bounded by the handle's `Timeout` alone.
- **`Json` is its text**, read again at each call, so `raw()` is untouched and a value
  of it compares and prints as its text. Not taken: a parsed tree, which is faster on an
  answer read many times and loses the text.
- **Section 13's sample binding is `Http.ask` and `perform`**, in place of an invented
  `Http.get` in a Go package `httpx`, which the real `Http` would contradict. The sample
  no longer shows `@go`; its bullet stays.
- **A binding that says nothing is not refused** (DEV-962's entry; since 20 Nov 2026 a
  binding says `action` or nothing, above): nothing marks which bindings call outside,
  and nothing checks that one that says nothing is right to.
- **A DELETE with no fields sends no body and no `Content-Type`** (the lead, 20 Nov
  2026; DEV-1010), as FusionAuth's Java client and most clients send one, and a service
  may refuse a body on a DELETE. A POST, PUT or PATCH with no fields still sends `{}` or
  the empty form, since an empty object is what such a call expects. Not taken: no body
  for every method with no fields. Reopen when a service refuses `{}` on an empty POST,
  PUT or PATCH, or a port's Kotlin sends one with no body.

## Taken building what `main` needs (DEV-986), not yet discussed

The three entries of 18 Nov 2026 are built as they say. What they left open:

- **The session's four Go names are kept**: `rpc.NewCalls`, `rpc.Calls`,
  `rpc.ReadSettings`, and a step's type as `Class_needsProperty`. The chain starts at
  `Class_()`, a name no declaration can have, since the naming rule ends no name in `_`;
  a step's type and an operation's `_op` value go through the naming rule, so a clash
  with a nested type or an enum's constant is a `go-name` error, not Go that fails to
  build. Not taken: `rpc.NewRoutes` or `rpc.Unmounted` for `Calls`, which say no more.
- **An `_op` value names its operation and carries its types**:
  `rpc.NewOp[Env, I, A]("logout")`, which `rpc.Call` looks up in the list of its `Calls`.
  Where two lists of a package hold one name, each is spelled with its list,
  `Routes_Logout_op`. `A` is what the fun answers, a `Reply<A>` whole, so that a route
  has the cookies. It is written for the operations of a list that names no check, and
  for no other (the lead's ruling on the review, 20 Nov 2026; Greg is told, since it
  narrows "a typed value for each operation" of 18 Nov): a checked list's operation
  has no caller but a browser, its value let hand-written Go run it with its check
  skipped, and without the value a route that names one is a compile error where it
  was a 404 at run time. Not taken: a value for each operation, as first built. Nor an
  `Op` that holds the row's functions, which would need the four row constructors to
  return a second type; the types are the checker's either way.
- **`rpc.Calls` and `rpc.Handler` share an unexported server** (`Handler` is
  `struct{ *server }`, `Calls` is `struct{ *server; _ struct{} }`): one `run` serves a
  browser's call and `rpc.Call`, so the id, the `served` line, the limit and the reruns
  cannot drift apart, and `Limit` is promoted to both. `Calls` is a type of its own and
  not a named `Handler` because a named `Handler` has a `Handler`'s underlying type:
  `(*rpc.Handler[Env])(calls)` would then compile outside `rpc` and mount an unchecked
  list for a browser with no check at all (the review of PR 101 did, and was answered
  200). The blank field makes the two types' structs differ, so Go refuses the
  conversion; `TestHandWrittenMain` holds it. The `served` line is now logged before
  the answer is written, not after. Not taken: a second struct that copies the
  handler's; a named `Handler`, as first built.
- **`rpc.Call`'s error** is an `rpc.Refused` with the refusal's words, or the status and
  text a browser would get: `400 op: input.kind is not one of ...` for an enum that is
  no entry, `404` for an operation not of the list, `500 request <id>` for a panic,
  which is logged as a browser's is, and `503 request <id>` past the limit. Not taken:
  one plain error, where a route could not tell a refusal from a bug; a panic passed
  on to the route, which would then lack the `served` line.
- **The settings' reader is `rpc.ReadSettings[S](settings []byte, report io.Writer)
  (S, error)`, in `prelude/kogo/rpc`**, since the strict reader is that package's and
  unexported; the spec states it in 15.4, as one more thing read as an input is. A
  nested class reports and ignores its unknown keys too. An enum, a class of the
  prelude and a type that crosses no wire are each refused before the file is read,
  by the property's name; a prelude class is told by its Go package. Not taken: a
  package of its own, which would export the reader for one caller. Reopens with the
  first setting that is an enum or a prelude class.
- **A chain is written for a generic data class too**, with its type parameters, and
  for none of the prelude's, whose Go is hand-written. A `data object` has no chain.
- **The clock does not go to the handler here**: none of the three reads it.

## Taken building `ByteArray` and `kogo.mail` (DEV-988), not yet discussed

- **A data class the prelude declares is written in Go by hand, as the emitter would write
  it**: `Address`, `Attachment` and `Mail` in `prelude/kogo/mail`, as `HttpFailure` already is
  in `prelude/kogo/http`. The emitter needed no change: it names a prelude type by its
  package and writes a constructor call as a struct literal. What holds the two together is
  the Go compiler, on any program that makes one (`examples/30_mail.kg`, in
  `TestEmittedGoRuns`): a property renamed on one side alone does not build. Not taken:
  emitting the prelude's data classes into a generated file beside the hand-written Go (a
  build step and a file to keep fresh, about 40 code lines); a binding that takes the
  properties as arguments, which changes what a Kogo program writes. An application's own
  extension needs more, which is open (`doc/work.md`, "Open"): its Go is not in this module,
  so either its author writes the struct by the same rule, which then has to be stated in
  the spec with the tag and the name of each property, or the emitter writes the binding
  file's data classes into the Go package it names. Reopen when extensions are built.
- **`Mail` has `date: Instant`, after `messageId`** (Greg, 20 Nov 2026: a resend carries the
  same date, as the application's Kotlin does by building the whole message when it queues
  it). A message needs a `Date`
  header and `eml` reads no clock. Not taken: the sender adds the date when it sends, so
  the stored bytes have none: then what is sent is not what is stored, and a replay sends
  another date under the same Message-ID.
- **`text` and `html` are quoted-printable, by our own writer; attachments are base64**
  (Ruling on PR 103, the lead, 20 Nov 2026; this replaces every body in base64). A stored
  message reads with `convert_from(eml, 'UTF8')`, is nearer what the Kotlin's library
  writes, and a text part is not sent as base64, which filters mark. The boundaries are
  `=_kogo-mixed` and `=_kogo-alternative`, which neither encoding can write. Not taken:
  every body in base64; Go's `mime/quotedprintable`, whose line breaks could move with a
  release. A soft break comes before any character that would take a line past 75, so a
  line of 76 is broken one short of where it need be: one rule, stated, over the best fit.
- **`Bcc` is written into the bytes.** `Smtp.send(eml)` is given the bytes alone, so they
  must say who gets the message. The sender, when it is built, reads the recipients from
  `To`, `Cc` and `Bcc` and leaves the `Bcc` header out of what it sends. Not taken: no
  `Bcc` header, which loses those recipients; a second column for the envelope, which
  changes the row a port stores.
- **`messageId` is written between `<` and `>` as given**: the application writes the whole
  id, its `@` and domain included. Not taken: Kogo adding a domain, which it does not know.
  A port whose Kotlin gives an id with no `@` has to say what its library wrote.
- **A header's non-ASCII words are B-encoded, never Q**, 39 bytes a word so that one fits
  after `Reply-To: `. An ASCII name or subject is folded at spaces and not encoded (Ruling
  on PR 103), unless it holds `=?`, which a reader decodes even inside quotes, or a run of
  over 60 characters with no space, which no fold fits: then it is encoded words. A header
  is never folded before its first word. Not taken: Q-encoding as `net/mail` writes it.
- **A control character in a name or subject is written as a space**, CR and LF
  included, so that no reader gives one back to put in a header of its own. Not taken:
  refusing it, which a person could then cause.
- **An attachment's name not quoted fits is RFC 2231's pieces**, 60 characters each, a
  line each, so no line passes 998. Not taken: encoded words in `filename=`, which RFC 2047
  forbids though many write it.
- **The address rule is stated as a regex in spec 12 and is a panic in `eml`** (Ruling,
  Question 14 (a)): an ASCII addr-spec of atext and dots, one `@`, at most 254, for every
  `Address.address` and for `messageId`. The application checks a typed address against
  it at its form. Not taken: `eml` giving an `Either`, which every caller unwraps for a
  failure its own check has ruled out. A `mimeType` is printable ASCII, a parameter
  included. Non-ASCII addresses stay refused until a customer has one.
- **`ByteArray`'s `==` is refused by its own code, `bytes-equality`**, through the walk that
  finds a function (`function-equality`), since what to change differs. Where the checker
  cannot see one, behind a type parameter or in `contains`, `in` or a `Set`, `rt.Equal`
  panics on reaching it, as on a function (Review 1 on PR 103: the bytes answered `true`
  where Kotlin answers `false`). Not taken: comparing the bytes there.
- **A `ByteArray` in a template is refused, `bytes-text`**, since Kotlin writes its
  identity; inside a data class's text it is Kotlin's signed bytes, `[97, 0, -1]`, checked
  against kotlinc 2.4.21 by hand. Not taken: a form in `kotlin/`, which cannot make a
  `ByteArray` in Kogo's shared text.
- **The prelude's hand-written Go is held to its `.kg` by `prelude`'s `TestHandWritten`**,
  which compares each data class's properties with its struct's fields and tags in order.
  It compares no types: a program that uses a property holds those, through the Go
  compiler.
- **`kotlin/` gains nothing.** Its checks are that shared text means the same and that
  kotlinc refuses each deviation. `a == b` on a `ByteArray` is valid Kotlin that Kogo
  refuses, as `==` on a function is, and neither has a form there.

## Spec gaps found by the leakage ports, unresolved

- Outbound calls are handles (above), but their timeouts and retries, and workers, have no
  design. (An application's own binding files stood here until 17 Nov 2026: `doc/work.md` has them,
  as extensions.)
- Operations (spec 15) leave out, on purpose: Origin and CSRF checks; a limit on the
  reruns of a `db.tx` written in a `Db` operation.
- **A gate is held to reading (DEV-977, 19 Nov 2026).**
  - **The read-only savepoint goes round a gate on a `SerializableTx` alone.** `doc/gates.md`,
    "What a gate on a `Tx` loses": Postgres's read-only mode refuses a lock with a write,
    and a gate on a `Tx` may lock, so `gate-writes` alone holds it. Not taken: the savepoint
    round every gate, as the issue's "every constructor" could be read, which refuses the
    lock the design allows.
  - **A lock is told from an update by the tree**: Postgres asks update's privilege of a
    table whose rows are locked and names no updated column. Not taken: reading the
    statement's `rowMarks`, which says the same through each query's own range table.
  - **`gate-writes` names the gate**, after the design's words: "(the gate g reaches it;
    spec.md 15.3)", since the statement may be several calls from the gate, and a lock's
    says "a gate on a SerializableTx only reads". A statement is refused once, for the
    first gate that reaches it. Not taken: the design's sample to the letter.
  - **`rpc_test.go`'s gate no longer inserts**, since Postgres now refuses it, so the rows
    that showed which gate's transaction commits are gone. The gate records its
    transaction's id (`pg_current_xact_id`, which read-only mode allows) and the test reads
    `pg_xact_status`. Not taken: a kept setting or a released lock, neither of which tells
    a commit from a rollback.
  - **A gate on a `Tx` is held by `gate-writes` alone** (Ruling on PR 96, the lead). It
    leaves a write inside a database function, and `nextval`, called from a `Tx` gate. Not
    taken now: reading `sum(n_tup_ins + n_tup_upd + n_tup_del)` of
    `pg_stat_xact_all_tables` before and after a `Tx` gate and panicking if it grew (about
    8 code / 4 test lines; on 18.7 it stayed put across a row lock, grew across a function
    that writes, inside a released savepoint too, did not see `nextval`, and took 0.6 ms a
    read; it needs `track_counts`, on by default), which costs two queries on every run of
    a gate no port has, as cusoon2's requests take no locks and its gates are on a
    `SerializableTx`; roles or triggers, which are the application's schema. Reopen, and
    take the read, when a port has a gate on a `Tx` that calls a database function.
  - **A utility statement a gate reaches is refused**, by Postgres's own tag for it
    (`commandType` 6, its `utilityStmt`): its tree lists no table, so `truncate` and `lock
    table` passed, and read-only mode allows `lock table`. Nothing is lost: a gate binds a
    setting with `set_config`. Not taken: leaving it as a named gap.
  - **Gaps named, not closed**: a call through a value of an interface's type is not
    followed to the overrides (the reach walk follows a data class's or object's alone);
    an advisory lock and a write to a temporary table pass both halves. Not taken:
    following every override of an interface's fun from a gate (about 6 code / 3 test
    lines), which refuses a gate when any one implementation writes; refusing the
    advisory-lock functions by name, which re-reads what Postgres allows. Reopen when a
    port's gate calls through an interface or takes an advisory lock.
  - Reopen if a port's gate must write, which the design gives to an ungated operation.

- **The logins' names (DEV-990, 20 Nov 2026).** `error`, `List.none`,
  `Duration.ofDays` (spec 12, not yet built) and, approved by the lead on
  20 Nov 2026 as Kotlin's, `ifBlank`, `substringAfter`, `String.take`,
  `trimEnd`, `Duration.between` and `Duration.seconds`. The dates are the
  day where Greg and the lead work, ahead of a run's clock in UTC. None
  needs a refusal: `mise run kotlin` agrees on every edge. `String.take`
  counts UTF-16 units, as `substring` does, and across half a surrogate
  pair leaves U+FFFD where Kotlin's leaves the half, as spec 3 says
  (`"😀a".take(1)`); the comparison agrees there only because Kotlin's
  output writes the half as U+FFFD. Not taken: a code-point `take`,
  which is not Kotlin's.
  - **A type argument settled as `Nothing` is emitted when the call never
    returns and takes nothing that holds a function**: its result, with
    the arguments put in, is `Nothing`, and no parameter's type is a
    function, or a declared generic type other than `Either` (taken to
    possibly hold one). Such a call gives out no value whose Go type holds
    `rt.Nothing`, so nothing is later widened for free to a type it is not
    (`instantiate`, `emit/types.go`). It is `orThrow(what, left)`. Not
    taken: emitting every `Nothing` argument, which lets a sealed value
    built with `Nothing` fail a later `is` test; using the argument's
    own Go type in place of `Nothing`, which needs the checker's
    unnarrowed type at each call. Reopen when a port passes `Nothing` to
    a call that returns a value.

## Constructs against kotlinc (DEV-1019), 20 Nov 2026

- **`also` is added to the prelude**, `external fun <T> T.also(block:
  (T) -> Unit): T`: spec 6 names `let`, `also` and `takeIf` as the
  scope functions, and the prelude had only two, so the `scope` case
  could not be written. Not taken: leaving `also` out of the case and
  naming the gap for later; the spec already says what it is, and the
  case now holds it to Kotlin. Reopen if `also` is to leave the spec.
- **The constructs' cases are helpers called with the inputs, not one
  `when` per construct inline**: a loop, a jump or a local `fun` needs
  a body, so each case calls a top-level helper in
  `kotlin/builtins/constructs.kg` once or twice with inputs, or a value
  derived from them (`n % 5`), that reach its branches. Not taken:
  fixed inputs per case, which the edges and random cases would not
  vary.

## A statement need not bind its proof (DEV-1029), 20 Nov 2026

- **The rule that a statement binds something of its proof leaves the
  language** (Greg, 20 Nov 2026: the survey's Q1 b): spec 15.7's first
  rule, the checker's `unbound-statement` and its cases go, and nothing
  takes their place. The survey of cusoon2's checks (cusoon2's
  `doc/plans/checks-survey.md`, Q1) ran it against 82 gated
  operations: in 27 the proof has nothing to bind and in 16 more only
  the caller's id, and since the port's one `Model` carries the
  transaction, no proof of the port is held to it. Proofs, their
  marked transaction, `proof-tx-escapes` and `outside-passes-proof`
  stay. The refused cases removed with it are this decision's, not a
  loosening; one turned round holds that a statement on a proof's
  transaction that binds nothing of the proof passes. Not taken: a,
  the rule kept and the port's `Model` exempt. `@BindsTenant`'s
  planned check (`doc/gates.md`, "The gate binds the tenant") was to
  be built on this rule; whether it gets one of its own is open, the
  lead's, and comes up in DEV-945. Reopen with a port whose proofs
  each hold their transaction and their rows.

## The guide (DEV-1036), 21 Nov 2026

- **`doc/guide.md` is the draft Greg read, unchanged** (Greg, 21 Nov
  2026): the form the DEV-1035 session fell into, taken as it stands
  and wrapped as `doc/lead.md` is. Not taken: rewording it on the way
  in. Reopen when it has been used twice more, as it says.
  *Superseded 22 Nov 2026 by "The guide is revised with Greg at the
  end of a sitting that chafed" (DEV-1075) below.*
- **"Guide DEV-NNN" is said in the repository the design is about**
  (the lead, 21 Nov 2026), as the draft and the Workflow line have it:
  a guide checks the design against that repository's code and commits
  the short document to its PR's branch. Not taken: always in
  `~/Dev/kogo`, as the lead and the clerk are.
- **The lead's form for a choice is the guide's**: a piece of code,
  the likely mistake, what catches it and one proposal, with lettered
  options kept for two answers that are truly close. A guide's last
  comment reaches the lead by the board's row for a comment that is no
  step's, on an issue In Review; in any other state Greg, who ends the
  sitting, brings it to his next "Lead". No row or word is added for
  it. Not taken: keeping the lead's lettered options as its default.

## A row has no step (DEV-1039), 21 Nov 2026

- **The `after` form of a row leaves the language** (Greg, 21 Nov
  2026, ruling on the gate design in DEV-1035): `op gatedBy gate
  after step` goes from spec 15.1, 15.2, 15.3 and 15.5, the parser,
  the checker's rules for a step, the emitter and `rpc.Gated`, which
  loses its step. None of the 94 rows cusoon2's survey counted uses
  it, and it has no place for an undo. The one operation that acts
  outside and then writes is `ungated` on a `Db` and calls its gate
  in each of its own transactions. `action fun` work, proofs and
  `outside-passes-proof` stay; that rule's cases written on a step
  are written on work that runs outside. Not taken: keeping the form
  unused. Reopen with a port of an operation that acts outside and
  then writes, which cannot be written `ungated` on a `Db`.
- **A row with a word after its gate or `ungated` is refused by the
  parser, saying what a row is**, `after` among them and with no
  message of its own (the issue's Scope). Before, such a word fell
  to the list's "expected , or a new line between a list's entries",
  which does not say what a row is; that message stays for a tests
  list. Not taken: leaving the list's message, which the Scope asked
  to say what a row is.
- **`examples/24_gates.kg` keeps `sendInvoice` as a plain gated row**
  and loses `tellPrinter`, which no row names now and which takes a
  proof, so `action-proof` would refuse it. The end-to-end run loses
  "refused by the step" and calls the row's success "sent". Not
  taken: keeping `tellPrinter` as a called helper, which is the next
  issue's rewrite of the example.

## Where a port leaves the Kotlin (DEV-1041), 21 Nov 2026

- **What `kogo/run.sh` can see goes Kotlin first; what it cannot is
  written from the design** (Greg, 21 Nov 2026), in `doc/roadmap.md`'s
  "How it moves" and `doc/porting.md`. An answer, a row, or what
  Postgres is asked or holds changes in the Kotlin first, under its
  tests, and the port matches it. The form (the list's rows, a gate,
  `call.kg`, a step's signature, where a mark sits) is written in the
  port from cusoon2's `doc/plans/gates.md`, and the file that departs
  names it in one line; the Kotlin keeps its form until it is deleted.
  Not taken: every change Kotlin first and then translated, which costs
  a Kotlin rewrite for a form the Kotlin has nothing like. Reopen when
  a port whose form has left the Kotlin's meets a Kotlin-first change
  that no longer translates.
- **The roadmap's ops table marks each piece built, with its PR**
  (the issue's Scope), in place of the rule that no table has a column
  for state. Rows 13 to 18 become 13a to 13i, so 19 and 20 keep their
  numbers. The two Open items DEV-972 and DEV-975 went, both merged
  (cusoon2 PR 1536, 1537), and "Settled" says so. Not taken:
  renumbering the rows.

## A gate's answer holds its transaction (DEV-1040), 21 Nov 2026

- **A proof leaves the language** (Greg, 21 Nov 2026, ruling on the
  gate design in DEV-1035): no data class is known as a proof by a
  marked transaction, and `gate-proof`, `proof-tx-escapes` and
  `outside-passes-proof` go, with spec 15.7 as it stood. A gate's
  answer holds its transaction in a plain property, which is kept in
  a name, destructured and passed on. This overrides DEV-1029's note,
  above, that the rules on a proof's transaction stay. They refused a
  stand-in, a transaction kept in a name, and not the mistake, a
  transaction used after it ended; and cusoon2's ops port could not
  be written under them, its gate answering a call with a plain
  transaction. `doc/design.md`, principle 1, gains Greg's clause.
  Reopen with a mistake the two refusals below let through and a rule
  that refuses that mistake itself.
- **Two refusals take their place, with one meaning of "holds a
  transaction"** (the issue's Scope): `action-gate`, a row whose work
  is an `action fun` names a gate whose answer holds none, and
  `tx-escapes`, a `db.tx` block answers nothing that holds one. The
  codes are this issue's. The meaning is the type's alone: a `Tx` or
  a `SerializableTx`, or one in a property or a type argument at any
  depth. It reads a sealed interface's variants and a function's
  result too, as `gate-proof` did, which the Scope did not name. Not
  taken: properties and type arguments alone, which `data class
  Clerk(val f: () -> Tx)` would pass.
- **Not held: a transaction that leaves a block another way than in
  its answer's type.** A lambda that closes over one, answered as a
  `() -> Int`, passes, and so does one assigned to a `var` declared
  outside the block. Spec 15.7 says so. Not taken: following a
  transaction through lambdas and assignments, a walk of where each
  value goes that the ruling just took out.
- **`action-proof` goes, and `action-tx` is not widened.** The rule
  said an `action fun` takes a proof only as a row's gated work;
  there is no proof to name. An `action fun` may declare a parameter
  whose type holds a transaction, and no value reaches it: a row's
  gate is refused by `action-gate`, a `db.tx` block answers none, and
  an action is not called in a block that holds a `Tx`. Not taken:
  `action-tx` refusing any parameter that holds a transaction, about
  5 / 5, which refuses a type and not a use. Reopen if such a value
  is found to reach an `action fun`.
- **A gate still answers no bare `Tx`, `Db` or `Request`** (`gate`,
  reworded): the design's work is never handed the request or a bare
  transaction. Not taken: a gate that answers the `Tx` itself, which
  a call with one property already is.
- **`action-gate` is said on each row, at the gate's name**, where
  `gate-proof` was said once for a gate: the same gate is right for
  one row and wrong for another, by what the work is. `tx-escapes` is
  said at `tx` of `db.tx`, and cites spec 10.7, which gains the rule
  beside what a block is; 7 and 7.1 lose their sentences on a proof.
- **`check.Op.Proof` is `check.Op.Given`**, what the gate answers and
  the work takes. Nothing outside the checker's tests read it.
- **`examples/24_gates.kg` is the design's shape in one file**: a
  call made by private constructors, a step, a held value with a
  marked constructor, model functions that take the transaction and
  the held value, and `mayPrint`, which answers facts for the row
  whose work is an `action fun`. An example is one file, so its
  `private` and its marker hold nothing apart, and its comments name
  the file each part would be in. Its gate takes a `SerializableTx`,
  as the design's does. Not taken: an example of several files, which
  no example is.
- **A gate that reads its operation's input stays in the language**,
  and leaves the example: the design's gates read none, and its list
  of what changes in Kogo does not take the form out. Whether it goes
  is the lead's.
- **`doc/gates.md` is the design alone.** Left out with the proof:
  "The gate binds the tenant", whose check was to rest on a proof and
  is open in DEV-945, and the printed table's `(tenant-wide)` mark,
  which was read from which proof's transaction a statement ran on.
  cusoon2's own lists, the marks' functions and what its storefront's
  port starts from, stay in cusoon2's `doc/plans/gates.md`. The other
  documents that speak of proofs (`doc/charge.md`, `doc/lists.md`,
  `doc/lists-assessment.md`, `doc/remains.md`, `doc/work.md`) are
  dated designs and are not rewritten here; `doc/roadmap.md` is
  DEV-1041's.

## `Json.strings`, four string names, every failed call logged (DEV-1050), 21 Nov 2026

From Greg's rulings of 21 Nov 2026 on DEV-1049; these are the calls they left open.

- **A 5xx's body is not logged**; a 3xx's and a 4xx's still are, their first 4 KiB. A
  4xx's body is logged for what support reads in it, a decline's code and request id
  (Q9); a 5xx's is the service's own trouble, often an error page, which no client acts
  on and which may echo what was sent. Nor is the body of a 2xx that is `Unreadable`,
  which may be a whole answer of personal data. Not taken: a 5xx's body logged as a
  4xx's is, more of what `doc/work.md`'s Open asks whether it is safe to log, for no
  reader. Reopen when support needs something only a 5xx's body says.
- **One message, `kogo.http: failed`, for every failed call**, with `method`, `path`,
  `status` where an answer came, `why`, and `body` for a 3xx or a 4xx. It replaces PR
  97's `kogo.http: refused` and `kogo.http: redirect not followed`: one word true of
  every failure is counted with one search, and `status` tells a 3xx from a 4xx. Not
  taken: the two kept and a third added for the rest. The line is written once the
  call has ended, so a 4xx past the limit or broken off is logged with its status, its
  `why` and what was read of its body.
- **`split` takes one `String` delimiter**, as spec 12 now writes it, `split(delimiter)`.
  Not taken: Kotlin's whole signature, with `vararg`, `ignoreCase`, `limit` and a `Char`
  delimiter, which no port needs yet. `padStart(length, padChar = ' ')` is Kotlin's
  one form on a `String`.
- **The check against Kotlin pads to `n % 9`**: an input is `Int.MAX_VALUE`, which
  neither side can build. A negative length is among the cases, and panics alike.
- **`doc/work.md`'s Open loses its whole bullet on errors a service puts in its body**:
  past the question spec 12 now settles, it said only that each client reads its own.
  The sketch of `timeZone` above it still raises `Unreadable` for a status that is not
  `OK`, which spec 12 now rules out; it is left for the Google client's port, whose
  failure type it needs.
- **Rows 13i to 13k are new in `doc/roadmap.md`**, and the six job operations become
  13l, as rows 13 to 18 became 13a to 13i (DEV-1041). Row 6a is marked built, PR 119.

## A body's limit and a header that is not text (DEV-1056), 21 Nov 2026

From Greg's rulings of 21 Nov 2026 on DEV-1053, and the calls they left open.

- **`MaxBody` is a field of the Handler, beside `Limit`: 64 KiB unless `main` sets it
  where it mounts a list.** A body is built whole into a tree before its type is
  checked, so one request can take up to 35 times its body in memory before it is
  refused; a limit each `main` had to remember left a list open when one forgot it. One
  limit for a mounted list. Not taken: a line in the table, which would be syntax; a
  constant, which no list could raise; a limit for each operation, which no list needs
  yet. `doc/main.md`'s `http.MaxBytesHandler` stays, for the routes `main` reads in Go.
  The field is the Handler's, not the server's that `Calls` shares, since `Call` reads
  no body.
- **A body past it is `413 <name>: the input is past <N> bytes`**: `readBody` lets
  `http.MaxBytesError` through, from the value and from the read after it. Not taken:
  the reader's `400 ... is not JSON`, which tells the caller to fix what is not wrong.
- **No token reader now**, one that checks the type as it reads and so never holds the
  tree. Reopen when a list needs a limit large enough that 35 times it, across the
  requests in flight, is no longer small beside the process's memory.
- **A header read that is not UTF-8 ends the request with a 400** that names the header
  and never its value: a `String` a program holds is text, and such a value, bound in
  SQL, ended as a panic and a logged trace. `Host` is held to the same. A null still
  means absent; a header no program reads is not looked at, nor is a cookie, whose
  parser already drops a value it cannot carry. Not taken: reading it as absent, which
  passes a check written `origin == null || ...`; reading it as ISO-8859-1; refusing
  the whole request for any header, which would refuse a visitor for a cookie another
  site on the domain set.
- **The refusal travels as a panic of its own type, `notText`, which `run` recovers
  into the 400** before it looks at the limit or logs a trace, so the `served` line is
  the request's only line. `Request_Header` gives a `*string` that the emitted code
  uses as it is, and a panic is the one way out of a check and of an operation alike,
  through every transaction's rollback, with no change to the emitter. Not taken: an
  error slot on the `Request` that `run` reads after, which lets the program go on with
  some value it should not have; a check of every header before the list runs, which
  would refuse headers no program reads. A panic that is a real fault stays a 500.
- **The header is named as Go canonicalises it**, `X-Check` for `header("x-check")`,
  so one header reads one way in every 400 whatever case the program wrote.

## Local runs on the dev VM (DEV-1057), 21 Nov 2026

- **A step that needs the dev VM runs there headless** (Greg, 21 Nov). The lead starts an
  issue's first step and the clerk each later one with `lin local DEV-NNN <where>
  "<phrase>"`, in place of a phrase Greg said in Zed; a stack's clone is one issue's from
  Pickup to merge, no time limit stops a session, one that ends without its step's
  comment is handed to the lead as a failed clerk is, a walkthrough holds a clone, a guide
  is still Greg's to start, and cusoon2's PRs are still his to merge. What the issue left
  open, chiefly that a hold is read from `Taken`, `Held` and `Released` comments and the
  $25 cap, is decided in cusoon2's `doc/plans/linear-workflow.md`, "The 21 Nov 2026 local
  runs". It changes "The lead does not run other sessions' steps": Greg starts only a
  step he wants to watch, and a guide. Reopen if two issues contend for a clone often.

## `toString` on numbers (DEV-1061), 21 Nov 2026

- **`toString` on `Int`, `Long`, `Double` and `Decimal` is four prelude externals, each
  calling `rt.ToString`**, the call a string template makes, so `n.toString()` and
  `"$n"` cannot write a number two ways. `Decimal` had none, though spec 12 lists it.
  Not taken: the emitter writing `rt.ToString` for the call itself, as it does for a
  data class's, which would make `toString` a name the checker knows rather than a
  binding (spec 13).
- **`toString` on any other built-in type is now `no-overload`, naming the four
  candidates**, where it was `unknown-name`: the message every overloaded prelude name
  gives (`toLong` on a `Boolean` too). Not taken: special-casing the message back to
  `unknown-name`, which would be code for no gain. A data class's own `toString` is a
  member and is found first, as before. Reopen if a type gains its own `toString`.
- **The built-ins check covers `Int`, `Long` and `Double`, not `Decimal`**: its inputs
  have no `Decimal`, which only a query gives. `Decimal.toString` is the emitter test's
  (`1.50` stays `1.50`). Reopen if the check gains a `Decimal` input.

## A port weighs a write-around (DEV-1065), 21 Nov 2026

- **"Longer" is against the Kotlin's form, in lines**: a write-around is weighed when it
  takes more lines than what the Kotlin wrote in its place, as `eight()`'s ten did for the
  one call to `toUrlValue`. Not taken: a measure in characters, which would weigh
  `requireNotNull` for `!!`, the swap Greg's scope leaves out. Reopen if a one-line
  write-around hides a library's rule.
- **The rule is its own paragraph after "Add what the spec lists"**, not sentences inside it,
  since it applies to both kinds of name that paragraph sorts and reads as a step of its own.
- **It names no write-around but of a name**: matching what a Java client sends on the wire
  (the FusionAuth request defaults in cusoon2's `idp.kg`) is not a missing name and stays
  outside it. The handoff of DEV-1065 asked the lead whether the rule should reach those;
  the lead's ruling of 22 Nov 2026 asks it again when the next such client is ported
  (noted on DEV-1048).
- **The port's Decided accepts a difference only the comparison sees** (22 Nov 2026, Greg;
  `doc/design.md`, "Ports"; `doc/porting.md`). A difference weighed under "Weigh a
  write-around" that nobody but the comparison sees is accepted by the port's Decided, and
  the Code review checks the claim of who sees it. This reopens DEV-884's entry for that
  case alone: every other accepted or owed difference stays Greg's. Not taken: Greg
  accepting each such difference from the handoff, which puts a ruling before him for
  every write-around. Reopen if a difference so accepted turns out to be seen.
- **Spec section 14's `String.format` row adds "or `plain`, or a prelude function"** (22
  Nov 2026, the lead's ruling on DEV-1065): read alone, "string templates, `padStart`" was
  an order to rebuild a format by hand, as `eight()` did. Not taken: leaving the row as it
  was, as the issue's Scope said, and filing it apart. Reopen if a port reads the row as
  licence to skip a prelude function the spec lists.

## A refusal inside a 2xx is logged (DEV-1063), 22 Nov 2026

- **The client logs it with `http.logRefusal(endpoint, why: String)`** (Greg, 22 Nov
  2026), two bindings in `prelude/http.kg`, one for a `Lookup` and one for an `Action`,
  neither an action, so a client's fun that asks and reads the answer stays a lookup;
  spec 7.1 already says a binding that says nothing may be called anywhere, and is
  unchanged. Not taken: `json.logRefusal(why)` on the answer, since a `Json` is its text
  and would have to hide a logger and its call inside a value; a function with no
  endpoint, which cannot say which call was refused; `ask` taking the function that
  reads the answer, so every `Left` is logged, which costs a lambda at each call and a
  generic `ask`; general logging, which stays open under "Printing is an action" (19 Nov
  2026) and is not reopened here.
- **Its line is `kogo.http: refused in a 2xx`, with `method`, `path` and `why`**, at WARN
  to the handle's logger, the one a failed call is written to; `why` is cut at 4 KiB as
  a refused body is. Not taken: `kogo.http: failed`, the failed call's message, which
  would count with the failures a call that `ask` ended as a success, and gives no way
  to tell the two apart; `kogo.http: refused`, the message DEV-1050 retired for a 4xx,
  which a search of old logs would mix with these. It has no `status`: the binding is
  not handed the answer, and every such answer is a 2xx. Reopen if an op needs the exact
  status.
- **The test client is `priceOf` in `examples/29_http.kg`**, run by the emitted-Go test
  against a local server, with the line written to its output. Not taken: a client of
  its own in `cmd/kogo/testdata`, a second place the end-to-end test reads Kogo from.
- **Spec 12's example leaves `NOT_FOUND` silent** (the lead's Ruling, 22 Nov 2026):
  which answers a client logs is the client's to choose, and a stale id is an ordinary
  answer, so a WARN line for each would bury the refused key the line is for. Not
  taken: logging it as the Kotlin did.
- **Its section is spec 12, "An outside service"**, where the `kogo.http` text is: the
  issue names section 14, which is "Not in Kogo" and has no `kogo.http` text.

## `joinToString` (DEV-1070), 21 Nov 2026

- **`joinToString` is two prelude externals: `List<String>.joinToString(separator)` and
  `<T> List<T>.joinToString(separator, transform: (T) -> String)`**, the forms the issue
  scoped. Not taken: Kotlin's one signature with a nullable `transform` defaulting to
  null, which would let a list of anything be joined bare. No use needs that, and a
  `List<ByteArray>` would then be written as its bytes where Kotlin writes `[B@...`: a
  template refuses that (`bytes-text`), but refusing it here would need checker code.
  Calling the bare form on a `List<Long>` is `no-overload`, naming both, and so is
  `List<String?>.joinToString(",")`, which Kotlin accepts (writing `null`): add
  `{ "$it" }`. Reopen when a port needs it.
- **The separator defaults to `", "` in both forms, as Kotlin's does**: the issue counted
  two bare calls among the uses it covers, and `xs.joinToString { ... }` is Kotlin's
  ordinary form. It is a `String`, Kogo having no `CharSequence`. Not built: `prefix`,
  `postfix`, `limit` and `truncated` (one use in cusoon2's backend). Reopen when a port
  needs one.

## `emptyList`'s type from the left of `?:` (DEV-1069), 21 Nov 2026

- **A call with no lambda argument on the right of `?:` takes the left's type, made
  non-null, as the type its result must have, when nothing outside the `?:` gives one**
  (spec 5.1, rule 3; the lead approved it, 22 Nov 2026). Kotlin settles `T` in
  `xs ?: emptyList()` from `xs` with the same meaning. With no lambda, rule 3 fills
  only the type parameters the receiver and the arguments left open, which were
  `type-arguments` errors before, so this grows the subset and changes no program that
  checked before. Not taken: keeping `emptyList<T>()` where nothing else settles `T`,
  which DEV-1067 made the form a reader writes for `orEmpty`.
- **A call with a lambda argument there keeps what it did before** (Greg's ruling on
  the review, 22 Nov 2026): its lambdas settle it, and `?:` takes the common type of its
  two sides. Rule 3 comes before rule 4, so the left's type would override the lambda:
  `a ?: make { b }` for `a: S.A?`, `b: S.B` checked as an `S` and would become a
  `type-mismatch` at `b`. Not taken: handing the left's type to these calls too, which
  would accept `xs ?: make { emptyList() }` but refuse that program, which checked
  before. Reopen when a port needs the first.
- **Only a call takes it**, written directly on the right: a literal there still has
  its own type, so `n ?: 0` for `n: Long?` stays `no-common-type` with its fix `0L`, as
  before. Not taken: handing the left's type to any right side, which would make that
  `0` a Long here and nowhere else a literal stands without a wanted type. Nor a left
  that is `null` or `Nothing`, which settles nothing.
- **`if`/`else` and `when` branches do not settle one another**: `if (c) xs else
  emptyList()` with no wanted type is still `type-arguments`, though Kotlin accepts it.
  The issue scoped `?:` alone. Reopen when a port writes one.

## The guide settles each piece (DEV-1075), 22 Nov 2026

- **A guide's sitting settles each piece of a design as it is shown** (Greg, 22 Nov 2026,
  at the end of DEV-1068's sitting): a piece ends in one proposal, his "next" is
  agreement to it, and the end is the tally and only what is left: what was postponed,
  two rulings that pull against each other, and anything the document asks that no
  piece showed, which catches a question once the choices pass is gone. The first form
  built the whole design and then took its choices one a message; in DEV-1064 and
  DEV-1068 he had judged each piece as it landed, and the second pass asked again what
  was settled. Not taken: the two passes kept for a design whose pieces lean on each
  other. Postponing a piece that cannot be judged yet, and reopening a ruling a later
  piece disturbs, stand in for them. Reopen if a sitting on such a design commits him
  before he has seen the whole.
- **A piece carries what he asked for in DEV-1068**: what Kotlin does, from a run, the
  slip or "I cannot build one", the size, and what was not taken in a clause. A test he
  gives in the sitting is held against every later piece. Not taken: leaving these to
  his questions, which asked for Kotlin's rule four times; holding a test only to the
  piece it was said on.
- **The guide is revised with Greg at the end of a sitting that chafed**, in place of
  "when it has been used twice more", which this revision met. Not taken: a count of
  uses, which would hold a fix back or force one that nobody wants.

## `import ... as`, and an import that hides the package's own (DEV-1076), 21 Nov 2026

- **Any import may take an `as`, a `kogo.` one included, as in Kotlin** (Greg's ruling on
  DEV-1068, `doc/plans/packages-and-names-design.md` section 3). This reverses spec 2's
  "no `import ... as`", which had no entry. The file writes the new name for the type,
  its constructor, constants and variants; `kogo emit` and `kogo ts` write the declared
  name. One name brought into a file twice stays `duplicate`, two funs included. Not
  taken: a package written in code, `a.b.Name`; renaming as the only answer to two
  types of one name; `as` only where a name is taken.
- **An import is refused (`own-import`) where the file's own package declares its name,
  as it writes it, as a type, a typealias or a top-level `val`**, though in Kotlin the
  import wins (design section 4). A fun of that name stays one more candidate.
- **The refusal takes an import from the file's own package of such a name, with its own
  message: "remove this import"**. Not taken: exempting it, which leaves an import that
  does nothing; refusing every import from the file's own package, which also takes one
  of a fun, harmless, and grows the change past this issue. Reopen if a file needs the
  latter.
- **An `api` list names an operation by its fun's declared name, not an alias**: the
  name on the wire is the declared one (15.1), and the list is where a reader finds it;
  `listed` also keys duplicates by the written name. A list whose file imports the fun
  under another name moves to a file that imports it by its own. A check or a gate,
  never on the wire, may be named by an alias. Not taken: accepting the alias, as Kotlin
  would any name in scope. Reopen if a port needs the alias in a list.
- **An import of one declaration twice under the same alias is `duplicate`**, as is an
  alias that meets another import's name; an import without `as` repeated stays
  accepted, as before. Not taken: comparing aliases' targets, a line for nothing.

## A port keeps its Kotlin's shape (DEV-1080), 22 Nov 2026

- **A port is no longer than its Kotlin and reads no worse, and lists where it is not**
  (the lead, 22 Nov 2026, from an audit of cusoon2's 23 ported files): the ports came
  to 1,407 lines against about 1,704 of Kotlin, but 11 places were written longer or
  more awkwardly than the Kotlin where Kogo had the Kotlin's form, and the Reviews of
  the two newest ports raised none of them. Not taken: the list alone, which would have
  named 9 of the 11 and missed a ladder in a function no longer than its Kotlin; a rule
  that only asks for short code, which binds neither author nor reviewer; a script that
  makes the list (about 100 lines, and cusoon2's to hold).
  Reopen if a Review's tries of the Kotlin's form keep finding nothing.
- **Comments stay the Kotlin's, plus marks and what a Ruling asks for**; what the port
  went through goes in the handoff. The audit counted 0.59 comment lines a code line in
  the ports against 0.44 in their Kotlin. Not taken: a ratio to meet, which a reviewer
  would count rather than read.

## The rules on a package's names (DEV-1077), 21 Nov 2026

Greg's rulings on DEV-1068, `doc/plans/packages-and-names-design.md` sections 1, 2, 5, 6
and 7.

- **A package has one table of names for its types, typealiases, markers and top-level
  `val`s, `private` ones included, as in Kotlin; but two `private val`s in two files do
  not meet**, as two `private fun`s do not. A `private val` beside one that is not
  private stays `duplicate`, as in Kotlin. The checker keeps the first `val` of a name
  in the package's table and each file's private one beside it, and a file means its
  own first. Not taken: one table that also holds two `private val`s of two files.
- **A name is written bare, and an import names a top-level name and brings what it
  declares and the types nested in it** (spec 2). The checker held this already; the
  spec gains the words and the tests hold them. Not taken: a package written in code.
- **Only a sealed interface holds types, and only its variants (`nested-type`)**: a type
  or a typealias nested in a plain interface, a typealias nested in any type, and a type
  in a sealed interface that does not implement it, are refused. The parser already
  refuses a type in a data class's body. Not taken: types nested in a plain interface
  as a namespace. One code, `nested-type`, gives all three refusals (not taken: a code
  of its own for "not a variant"; DEV-1078 words them). A refused nested type is still
  declared, so a use of it gives no follow-on error (not taken: dropping it).
- **A program's package is not `kogo` or under it (`kogo-package`)**: a program's
  `package kogo` put its names in every file, and `package kogo.time` had its names
  imported as the prelude's. **A package's own type or val is meant before one of
  package `kogo` or a built-in type**, as the checker already does. Not taken:
  refusing a program's own declaration of a name the prelude or the built-in types
  have.
- **Stated, with no code** (design section 7): `kogo emit` refuses two names that
  differ only in the case of the first letter; an alias stands where a type is written,
  and a value, a constant or a variant is named by the type's own name; a local hides a
  name declared outside it. The emitter's `go-name` refusal stays outside `kogo check`,
  **a named deviation from "`kogo check` is the only gate" (15 Nov 2026)**: Kotlin
  accepts the two names, and moving the refusal into the checker (15 / 4) waits for the
  audit issue DEV-1079, until a file meets one. Reopen when a port meets either.
- **Five messages on names** (design section 8, DEV-1078): a type mismatch names each
  type's package only where the two types themselves print alike, a `?` looked
  through, not where they differ further in, as `List<Area>` against another
  `List<Area>`. Not taken: printing a type with its packages all through (about 15
  lines more). Reopen when a port meets it. An extension not imported is named from
  each package's visible fun of its name that the receiver's type can call, as the call
  would match it once imported (Review, 22 Nov 2026), so the import it names works.
  Not taken: any fun of the name with a receiver, which named imports that only led to
  a second error.
- **An `Instant` or a `LocalDate` past Postgres's range is an error when it is sent**
  (DEV-1087, spec 10.4), at Postgres's own ends, -4713-11-24 to +294276-12-31 for a
  `timestamptz` and to +5874897-12-31 for a `date`, written as Kogo prints a year, not
  only where the driver's int64 of microseconds or int32 of days would wrap: one range
  a reader can look up, and the driver's wrap cancels out inside it. An `Instant` is
  checked on its seconds, as the driver cuts its nanoseconds down to the microsecond.
  Not taken: checking only the wrap, and leaving the rest of the refusal to Postgres's
  `out of range`.
- **A `when` that must cover every case panics on a value none of its branches names**
  (Greg, 22 Nov 2026; spec 8), with `when: no branch for <type> <value>`, in every form.
  Such a value is a row's enum label the program lacks.
  Not taken: checking each enum column against its constants as the row is read (about
  15 / 10), which fails sooner but also fails a request that only passes the value on
  or compares it with `==`, both of which answer truly today.

## A Regex's pattern is Go's (DEV-1083), 22 Nov 2026

Greg's ruling, 22 Nov 2026: a regex is a language of its own.

- **A `Regex`'s pattern is read as Go's `regexp` reads it, and nothing of Kotlin's is
  promised for it**; the name and its functions stay Kotlin's, the stated exception to
  principle 3. Spec 12 lists the differences known, each with an example, and says the
  list is not a promise that it is all; converting a pattern is the port's work
  (`doc/porting.md`). Probes through `mise run kotlin` found seven patterns both engines
  read that differ. Not taken: translating a pattern to Java's meaning in the prelude;
  refusing at check the forms both engines read and mean differently.
- **`kogo check` refuses a string literal Go cannot compile as a pattern
  (`regex-pattern`)**, compiled by Go's `regexp`, as the prelude does when the program
  runs, with Go's reason. A pattern built by a template or held in a variable is not
  read, and panics when the `Regex` is made, as before, with the prelude's text
  unchanged. Compiling the nine patterns cusoon2's ports hold takes about 46
  microseconds. Not taken: leaving every pattern to the panic.
- **`kotlin/builtins` and `kotlin/cases` keep their cases and gain no `kept` entry for a
  pattern**, since nothing is promised to agree; the prelude's table test holds Kogo's
  column of spec 12's table.
- **Spec 1's sentence on the `kogo.` packages' names takes "but for the corners section
  12 lists"** (lead, 22 Nov 2026): the names keep Kotlin's, `java.time`'s and Arrow's
  spelling and meaning but for those corners, which states what section 12 already does
  for `lowercase`, `replace` and now `Regex`. Not taken: leaving the sentence to be read
  as limited by section 12.

## An unread default, and an interface's `string()` (DEV-1085), 22 Nov 2026

- **A defaulted parameter's local is read once (`_ = y_`)**, as a local `val`'s is, so a
  fun that never reads it builds. Not taken: leaving the local out when nothing reads it.
- **`kogo check` refuses an interface's `fun string()` or `fun String()` with no
  parameter (`go-name`)**: either became Go's `String` method, so a template would write
  what it returns instead of its data class's text, `custom` where Kotlin writes
  `Tex(a=3)`. Not taken: having `rt`'s walk tell an emitted data class from a prelude
  type that writes its own text, more code for a name no program has. `kogo check`
  refuses these two names, and `kogo emit` the other three `go-name` cases until
  DEV-1079 moves them.
- **The refusal is the checker's, not the emitter's (Greg's ruling, 22 Nov 2026)**,
  overriding the lead's correction that had first put it beside the emitter's three:
  those compare the Go names the emitter computes, and this one needs only an
  interface's fun, its name and its parameters, which the checker sees itself. Not
  taken: the emitter, beside the other three, until DEV-1079 moves them all, which
  would add a refusal outside the only gate and save nothing.

## The job operations' prelude names (DEV-1094), 22 Nov 2026

- **An enum's `valueOf` is the checker's, as `copy` is, not a binding**: no binding can
  name every enum, so `E.valueOf(name)` resolves to a function the checker makes for `E`,
  and the emitter calls the prelude's `EnumValueOf` with `E`'s entries. Spec 3.3 names it
  beside `.name` and `entries`, and section 12 points there. Not taken: naming it in
  section 12 alone, which holds bindings.
- **`sortedBy` is declared once, as `prelude.diff` declares it, and orders** an `Int`,
  a `Long`, a `Double`, a `String` (by UTF-16 units), a `Char` or a `Boolean`, or a
  nullable one, null first, as Kotlin's `compareValues`. `kogo check` refuses any other
  key, `sort-key` (lead, 22 Nov 2026): Kotlin refuses a key that is not `Comparable`
  when it compiles, so the subset's limit sits in Kotlin's own place, not in a panic
  first seen in production. The run-time panic stays behind the check. Not taken: an
  overload for each key type, as `sumOf` has, about 10 declarations with their Go; or
  the panic alone. Reopen when a port sorts by an enum or a time.
- **`sortedBy` calls its selector as Go's stable sort asks** (lead, 24 Nov 2026,
  DEV-1157): the list it gives is Kotlin's, and the number and order of the calls are
  not (DEV-1143 found 166 of 250 inputs differ, through a selector that logs). Kotlin's
  documentation promises neither, so spec 12 says so and `mise run kotlin` keeps the
  difference (`hSortCalls`). Not taken: matching the JVM's sort call for call. Reopen
  when a port's selector has an effect that adds up.
- **A `Map` compares its entries in any order, and writes itself `{k=v}`**, as Kotlin's,
  on the same `ordered` keys as a `Set`. `for` over a `Map` stays unemitted: no port
  needs it yet.
- **`abs` needs no import**, as section 12 lists it with the numbers, where Kotlin's
  needs `import kotlin.math.abs`, which `kogo check` refuses: a port drops the import.
  So `kotlin/builtins` has no case for it, kotlinc needing the import; prelude/kogo's
  test holds it. Not taken: a `kogo.math` package with an import, a name of Kogo's own
  for one of Kotlin's.
- **A local hides a function of its name** (lead, 22 Nov 2026): Kotlin calls the
  function past a local that cannot be called, and Kogo refuses the call,
  `not-callable`. Spec 6 states it as a limit of the subset; Kogo's behaviour is
  unchanged.

## The clerk's merge checks are one command (DEV-1108), 23 Nov 2026

- **`doc/clerk.md`'s Merging and After a merge call `lin merge-check DEV-NNN`, and
  `--tidy`, and spell out none of its checks** (Greg, 23 Nov 2026: DEV-1090's way A;
  the command is cusoon2's DEV-1107). The prose keeps what is a model's: reading the
  fix commits against the Review, the Open, the order on a `Checked:` line, and the
  Backlog's `Parked:` issues. The command fetches once more after its checks, and the
  clerk merges within seconds of its `pass`, so the second fetch DEV-1013 put just
  before `gh pr merge` goes; `--match-head-commit` still holds the head. Not taken:
  way B, the poller running the checks and putting their result in the clerk's prompt.
  A Handoff that names two PRs gives the one to check after `DEV-NNN`, as the command
  takes it. Reopen if a clerk merges on a `pass` the base had passed by.

## A cusoon2 step that runs `kogo/run.sh` is an ordinary run (DEV-1106), 23 Nov 2026

- **The clerk starts a port's Fix as it starts any other** (Greg, 23 Nov 2026:
  DEV-1091's way A), since the wrapper clones Kogo's `main` beside every cusoon2
  run's clone (cusoon2's DEV-1105). `doc/clerk.md` keeps the clerk's own
  `kogo/run.sh` before `Checked:`, which tests the head it vouches for, and a step
  that lands in the other repository stays a local run. Not taken: keeping the
  local Fix. Reopen if a run's `kogo/run.sh` fails where the dev VM's passes.
- **The `fix:` bullet points to How the next step starts** (the lead's Ruling,
  23 Nov 2026), so it stays true whether the command prints a `lin local` line or,
  after cusoon2's DEV-1116, the phrase alone. Not taken: the Review's wording,
  false once DEV-1116 lands.

## A port's Budget, and the local that hides a fun (DEV-1117), 23 Nov 2026

- **A port's Budget counts code and imports, not comments** (lead, 23 Nov 2026:
  DEV-1096's Ruling). A port keeps the Kotlin's comments, so a Budget that counted
  them would read as overrun on any well-commented file; the handoff gives the
  comment lines on a line of their own, and how many are the Kotlin's. Not taken: a
  Budget that includes the Kotlin's comment lines.
- **Section 14 lists a call to a fun after a local of its name** (lead, 23 Nov 2026:
  DEV-1096's Ruling), the rule section 6 states. `val job = job(1)` alone passes
  `kogo check`, its initializer seeing the fun; a later `job(2)` is `not-callable`,
  so the row names the later call. Not taken: the Scope's row, a local named as a
  fun it calls, which names a form `kogo check` accepts. Not taken either:
  resolving a call past a local that cannot be called, as Kotlin does. Reopen if
  a port meets the row often enough that renaming costs more than the resolution.

## Go's port key, and the settings move as its own PR (DEV-1126), 23 Nov 2026

- **Go listens on `deployment.goPort`, a key of its own** (the lead's name, 23 Nov
  2026; DEV-930's port key). The JVM keeps `deployment.backendPort` while both
  serve, so the two run side by side from one file; Go's `Config` holds
  `backendPort` as a key it ignores. Not taken: `backendPort` shared, which lets
  only one of the two listen. Reopen when the JVM no longer serves: Go may then
  take `backendPort` back.
- **The ten keys' move into `settings`, with `goPort`, is its own cusoon2 PR,
  DEV-1124, before the `main`'s, DEV-1125** (the lead, 23 Nov 2026). The Kotlin
  goes first under its tests, and the `main` then reads a file that already has
  the shape it wants. Not taken: one PR for both, as `doc/main.md` of 18 Nov had
  it. Reopen if the move cannot land without the `main`.

## A worker keeps the loop over tenants (DEV-1129), 23 Nov 2026

- **A worker finds its rows by the loop over tenants, not through a view across them**
  (Greg, 23 Nov 2026: "we'll keep the loop for now"; DEV-1127 explored it). A worker's
  queue names a function that gives the values to serve in turn, and the runner enters
  each value, then asks for that value's next row, one row a value in a pass, as
  cusoon2's Kotlin does for each active tenant. `enter` stays as written, for a job as
  for an effect. The word and its form are not settled here: they are designed with
  `job`, `effect` and `work`, and are Greg's. This supersedes `find` and `fair` (17 Nov
  2026; `doc/work.md`, "Discover, then enter" and "Fair by a column") and, for the
  charge, makes Q12's "waiting payers first across all tenants" within a tenant.
  - **Why.** No schema work and no exception to row-level security; the port matches the
    Kotlin as it is, with no Kotlin-first change; fairness between values is kept for
    every queue, where `find` kept it for mail alone; one word in the language where
    `find` and `fair` were two. Held against all 16 of cusoon2's queues, `find` needed
    nine views that pass row-level security, one of twelve columns, where the design
    counted three.
  - **Accepted cost.** An idle pass grows as queues times values (about 30 queries at two
    tenants, `doc/work.md`). There is no order across values, so `soonest` orders within
    one, and a waiting payer may wait behind one charge of each other busy tenant.
  - Not taken: `find` and `fair` as designed on 17 Nov; `find` held to an id, a tenant and
    one time, with a test pinning each view's columns.
  - Reopen with tenants in the dozens or more, when the idle cost or that wait is felt.

## A request ends at its limit (DEV-1136), 23 Nov 2026

The agent's, on DEV-1133's stress run; the issue gave the three mends and left these.

- **The deadline is on the request's own context, which `run` hands on**: `Limit` from the
  start, on the `Db` and on the `Request` an operation is given, so a check, a gate and
  anything later that reads the request's context share it. Not taken: a deadline on the
  `Db` alone. An outbound call (`khttp`) still has its own `Timeout` and not this one
  (spec 13: "the handle carries the deadline" is not built). Reopen when that is.
- **A client that left is served as 499 and logged by the served line alone**, outcome
  `499`: nginx's status for it, so its log and ours agree. Not taken: a word of its own as
  the outcome (`left`), or a line of its own beside the served line. Call gives it as a
  `Failed` of 499.
- **The three kinds are told apart by the panic's value and the context.** A context error
  or Postgres's `57014` while the request's context is done is the client's leaving where
  the context was cancelled, and the limit where its deadline passed (503, no line beyond
  the served one, as a rerun past the limit was). Otherwise `57014`, class `57P`, class `08`,
  `pgconn.ConnectError`, `pgconn.ErrConnClosed`, a `net.Error` or an EOF is a database
  that is down: 503 with the request's id, and one ERROR line, `database`, with the
  driver's error and no trace. Anything else is the panic it is. Not taken: a WARN for the
  database (it is an operator's to act on); a trace for it (it names no Kogo line worth
  reading). Class `57P` is the whole of it, not `57P01` alone: `57P02` is the crash
  restart every session sees after one backend crashes, and `57P04` a dropped database
  (the Review, Fix 1).
- **`Call`'s error is a `Failed{Status, Text}`, not a plain error** (the lead's Ruling on
  the Review's Question 8): its `Error()` is the same `"503 request …"`, and a route
  answers its `Status`, so a database that is down behind a route reaches nginx as the
  503 it is and not a 500. Not taken: leaving the status in the error's text alone, which
  a route could only parse.
- **A rerun pauses for a random time up to 5 ms after the first conflict and 5 ms more
  after each, to 50 ms**, in `InTx`, so in `db.tx(serializable = true)` too, and the
  request's context ends the pause. Not taken: a fixed pause (two that conflicted stay in
  step); an exponential one (a request waits more than the conflict costs).
- **The served line counts reruns, `reruns`, always present; the cause of each is no
  longer logged.** Postgres logs each serialization failure and deadlock with its detail.
  Not taken: a count for each cause. Reopen if Postgres's log is not kept beside ours.
- **A pool's wait for a connection still has no limit of its own** (Q12 stands): the
  request's deadline ends it.

## A read sends the names it checked (DEV-1167), 24 Nov 2026

Greg's ruling of 24 Nov 2026 on DEV-1161's research; the agent's where marked.

- **Every read is wrapped, not only a star**: `query`, `queryOne` and `querySingle`
  send the author's statement under a select of the columns by the names `kogo
  check` saw. Postgres's tree is the same for `select *` and for its columns named
  in order, so Kogo cannot see a star without reading SQL text. Not taken: wrapping
  only a star. `exists` and `execute` discard their columns and send the statement
  as written.
- **A select is wrapped as a subquery, any other statement as a `with`**, told apart
  by `commandType`. Postgres does not inline a `with` that holds `for update` or a
  volatile function, so one `with` form for all would plan those reads as a `CTE
  Scan` of the whole result. Not taken: one `with` form. Insert, update, delete and
  merge go only in a `with`.
- **Up to about 20 µs of planning on a call Postgres plans afresh is accepted**, the
  wrap's one measured cost (DEV-1161).
- **The order of an `order by` inside the wrap is kept by Postgres 18's planner and
  guarded by a test** (`TestWrapKeepsOrder`, under forced parallel plans), not
  promised by Postgres's documentation, which says it must not be relied on. Reopen
  if that test fails after an upgrade.
- **A read whose own `with` writes is refused at check**, `sql-writing-with`:
  Postgres takes a writing `with` only at the top level, so the wrap cannot hold it.
  Not taken: sending it as written, read by position.
- **Two columns of one name are refused in the SQL pass, `sql-duplicate-column`**
  (the agent's, after DEV-1161's sketch): the wrap's outer select would otherwise
  fail with Postgres's "column reference is ambiguous". Not taken: leaving it to the
  checker's `sql-column-unused`, which never sees it now.
- **A wrap that Postgres refuses, or that describes other names or types than the
  statement, is a fault at the literal, `sql-wrap`** (the agent's, as the sketch had
  it), so `kogo check` names the file and line. Not taken: a failure to check, which
  ends the run without a place.
- **Nullability and tables are still read from the statement as written** (the
  sketch's): the wrap neither renames nor reorders, and its description is compared
  with the statement's.
- **A trailing `;` is cut at Postgres's own end of the statement** (the review's, on
  DEV-1167): the wrap takes the statement up to its tree's `stmt_location` plus
  `stmt_len`, 0 meaning to the end, so a `;` and any comment after it are dropped
  and no SQL text is read. Not taken: refusing a trailing `;` with a code of its
  own, which would refuse a statement that checked before the wrap.

## A Tx runs no utility statement (DEV-1176), 24 Nov 2026

- **Every utility statement is refused, `sql-utility`** (Greg's ruling, 24 Nov 2026): a
  statement whose `commandType` is Postgres's utility, by the tag `sqlpass` records in
  `Statement.Utility`, never by its text. Not taken: a list of the harmful ones (`commit`,
  `rollback`, a `set` without `local`), since a list misses what it forgot, as `discard
  all`, `listen` and `copy ... from stdin`, which never returns. No port or example has a
  utility statement. Reopen when a port needs one, as `lock table` or `set constraints`.
- **The refusal is made in `check`, where the literal's statement comes back from the
  pass**, beside `gate-writes`, not in `sqlpass`: what a `Tx` may run is the language's
  rule, and the pass stays one that types any statement Postgres describes. Not taken:
  refusing in `sqlpass`, which would drop `Statement.Utility` for every other user.
- **A gate's utility statement gets `sql-utility`, not `gate-writes`**: one message serves
  both, since the work may not run it either, so `gate-writes` loses its utility branch
  and says what a gate does to a table alone. A refused statement is left out of what the
  gate check reads, so it is said once. Not taken: keeping `gate-writes` for a gate's,
  which would tell the author to move to the work what the work refuses too.
- **`tx.refusal` is held to it too**, as it goes through the same check as the five calls
  and runs on the same `Tx`. Not taken: the five calls alone, which the issue named.
- **A test cannot make a deferred constraint fire**: spec.md 16 told a test to run `set
  constraints all immediate`, which is now refused; no test did. Named as a gap in 16.
  The `kogo test` fixture that set `statement_timeout` with `set local` uses
  `set_config(..., true)`, as the message says.
- **The gap named, not closed**: a function that sets a session's state,
  `set_config(..., false)` among them, is not seen, since the pass does not look into a
  function. Not taken: refusing a `set_config` whose third argument is `false`, which
  the pass could find in the tree it already reads (`FUNCEXPR`'s `funcid` and `args`):
  that argument may be a parameter or an expression, known only when the statement
  runs, and every other function that sets a session's state stays unseen, so refusing
  the one call closes almost none of the gap.

## A bare `T` that is null, and the `T?` of a nullable `T` (DEV-1179), 24 Nov 2026

Greg's ruling on DEV-1143's finding 1, and the agent's choices beside it.

- **`?:` and `?.` test a bare `T` for null as `== null` does** (Greg): `rt.Equal` of it and
  null, the one test `equals` gives, now named `isNull` in `emit/expr.go`. Not taken: a
  second way of asking (a reflection test of its own, or a type switch).
- **A `T?` whose `T` is itself nullable is refused, not emitted** (Greg): Kotlin holds one
  null where Go's `**T` holds two, and the outer pointer is not nil when the value is.
  Not taken: folding the two nulls into one wherever the value goes, a conversion at each
  call, return and store that crosses a type parameter. Reopen when a port holds a generic
  fun that forms a `T?` and is called with a nullable type.
- **Only a type parameter made `T?` is refused a nullable type** (Greg): in its signature,
  as since DEV-830, or in its body, by a `T?` it writes or a call whose own type parameter
  is made `T?`, through as many generic funs as it is given to. The follow does not pass
  through a generic type's type arguments (Greg, on the review): a value of `Opt<T>` whose
  field is `T?` is built either as `Opt<Long?>`, which the type's own mark refuses, or in a
  generic fun, where building it writes the `T?` and marks that fun's `T`, so following the
  type as well refused only correct programs, as `val o: Opt<T>? = null`. Not taken:
  refusing every nullable type argument to a generic fun, since a wrapper called with a
  nullable result is what a port writes most. Reopen as above.
- **The refusal stays `kogo emit`'s `not-yet-emitted`**, not `kogo check`'s. Which
  parameters are made `T?` is learned as the emitter writes each body's Go types (a `T?`
  local, a safe call's value, a prelude fun's signature), so the checker would need a
  second walk over every body to know it. Not taken: a check in `kogo check`, the better
  place were it as cheap. Reopen when `kogo check` has to answer it alone, as when the
  emitter is not run before a port is called done.
- **`v.takeIf { it != null } ?: fallback` on a nullable `T` is refused with the rest**,
  though it gave Kotlin's answer: its `takeIf` makes a `T?` of the `T`, and its right
  answer came only from the lambda's own test. Not taken: telling such a body apart, which
  would be the folding above in one place.

## A string on the wire is written as Kotlin writes it (DEV-1172), 24 Nov 2026

The lead ruled the encoder changes, not the comparison; the agent's, for the rest.

- **`rpc`'s `quote` is its own loop, not `encoding/json` with `SetEscapeHTML(false)`** as
  `http/json.go` has it: Go escapes U+2028 and U+2029 whatever the setting, and Kotlin
  does not, so the setting alone leaves a difference. It escapes `"`, `\` and each
  character below U+0020, `\b \t \n \f \r` short and the rest `\u00xx` in lower case,
  byte for byte what kotlinx.serialization 1.13.0 wrote for each (checked by hand, the
  probe in the handoff). Not taken: the encoder followed by a replace of `\u2028`, which
  a string holding a backslash then `u2028` breaks.
- **A lone surrogate stays a limit, stated in spec 15.4**: Go reads one as U+FFFD, where
  Kotlin's answer differs. A Kogo string holds no half of a surrogate pair (spec section 3),
  and no row of Postgres holds one.
- **No case in `mise run kotlin`**: its cases call Kogo built-ins from `.kg` source, and
  `quote` is none, so the check has no form for this case.
  The test holds the bytes Kotlin wrote instead. Reopen if the wire gets a check of its own
  against Kotlin.
- **`http/json.go`'s `encode` is left as it is**: it writes to a third party, which
  compares no bytes, and still escapes U+2028 there.

## The SQL pass's five faults from the stress run (DEV-1154), 24 Nov 2026

The lead ruled finding 11; the agent's, for the rest.

- **Each `$name` is checked where it is written**, by the byte offset of each `PARAM`
  in Postgres's tree against where the rewrite put it, and no SQL text is read. Not taken:
  a scan of the literal for quotes, which re-implements Postgres's lexer. A cast moves an
  untyped `PARAM`'s location to the cast's start (`cast(` or `text(`), so each location
  claims the first unclaimed `$name` of its number at or after it, and one left over is
  refused. Not taken: matching offsets exactly, which refused `cast($x as int)`. A `$x`
  in a comment inside a cast, `cast(/* $x */ $x as int)`, is refused at the real `$x`.
- **A syntax error at a parameter gets the hint for a `$` as text**, since a dollar
  quote's tag, `$q$`, reads as one: told by the error's offset being a parameter's. Not
  taken: telling a dollar quote from the text, which is more reading of SQL text. A real
  syntax error there gets the hint too, beside Postgres's own message.
- **A whole row's field under a `NOT NULL ... NO INHERIT` is nullable even after
  `only`**: a composite type is read once per type, not per reference. Not taken: a type
  per reference. A column named after `only` keeps its `NOT NULL`.
- **Only an `instead` rule makes `returning` nullable**: Postgres keeps the statement's
  own `returning` under an `also` rule. Every column of a statement done instead by a rule
  is nullable, whatever the rule's `returning` says. Not taken: reading the rule's tree.
- **42P08 gets 42P18's hint whatever its cause**: the other case, types deduced that
  disagree, is mended by the same cast. Not taken: telling them apart by the message.

## Kogo reads a subset of Postgres's values (DEV-1153), 24 Nov 2026

Greg's ruling at DEV-1148's sitting, piece 1b: outside the subset Kogo refuses, at
`kogo check` where the catalog or the statement tells, else by a panic that names the
limit, and never gives another value (`doc/design.md`, principle 4; spec 10.4 lists the
limits). Where each of DEV-1144's findings 4 to 8 is refused is the agent's.

- **An array of more than one dimension is a panic when it is read** (finding 4). The
  catalog cannot tell: `integer[]` and `integer[][]` are one type, and a value has its
  dimensions, not its column. The statement could only be asked by reading SQL. Not
  taken: a `List<List<T>>`, which the type cannot promise either. A lower bound is
  dropped, as before: a `List` has none.
- **A `NULL` element read into a `List<T>` is a panic that names the column and the
  element** (finding 7), not the driver's `cannot scan NULL into *int32`. The catalog
  never says an array's elements are not null. Not taken: `kogo check` taking only
  `List<T?>` for an array, the strict reading of the sentence, since the catalog does
  tell that the column is an array: it changes what every `List<String>` over a
  `text[]` means, a change to the language, and so Greg's. Reopen with that ruling.
- **Every array is read as its elements' `T?` and a `List<T>` copied from it**
  (`kogo.Array`), an allocation an element more than before. Not taken: reading a
  `List<T>` straight and matching the driver's error text for a null, which the
  driver's next release may reword.
- **`execute`'s count past an `Int` is a panic once the statement has run** (finding 5),
  so the transaction rolls back. Not taken: a `Long`, which changes 10.2 and every
  caller; the largest `Int`, which is another value.
- **A `String` holding U+0000 is a panic when it is bound, alone or in a `List`**
  (finding 6), in Kogo's words and before Postgres is asked. Neither the catalog nor the
  statement knows a value. Not taken: a 400 where the wire reads it (spec 15.4), which
  narrows what a `String` on the wire may hold for an operation that never sends it to
  Postgres: a change to the wire, left to Greg. Until then a caller can still cause the
  500. Not taken: dropping the character, which is another value.
- **The driver is told of every array type Postgres does not ship** (finding 8): one of
  a domain, a domain over an array, `citext[]`. `sqlpass.Type.Array` holds Postgres's
  name for it, set unless the type is in `pg_catalog` with an oid below 16384, Postgres's
  `FirstNormalObjectId`: `information_schema`'s domains, such as `cardinal_number`, are
  made by initdb below it, and their arrays are not the driver's. The emitter hands it
  to `Tx_Known`, which now takes any type's name, for a column and for a parameter
  alike. Not taken: the oid alone, which misses `cardinal_number[]`; asking the catalog
  whether the element is a domain, which misses a domain over an array; telling the driver at
  the read from the row's description, by when the result's format is already asked for.
- **An array of a domain over an array is `sql-unmapped-type`**: it is a list of lists,
  the catalog tells, and it was typed `List<T>` and could not be read. Found by a probe
  while the test's table was built, not by DEV-1144, as was a `List` sent to a parameter
  of a domain's array type, which the driver could not send.
- **The standing test (`emit`'s `TestShapes`) sends each value back** and compares the
  text Postgres makes of what came back with the text it makes of the value: Kogo's
  `toString()` is Kotlin's and not Postgres's, so the two texts cannot be compared
  directly. A fault that reads and sends wrongly alike would pass; `TestValues` holds
  Kogo's text for one value of each type. A composite type is no parameter, so its three
  cells are written by hand and send its fields back; its lists, and an enum's, are
  cells that must be refused. The table's domains are in `testdata/schema.sql`.

## A gated transaction stays in the serialization checks (DEV-1151), 24 Nov 2026

- **`reading` runs `select 1` before the gate's savepoint**, as DEV-1145 tried on a copy:
  the first snapshot is then not read-only, and Postgres keeps the transaction in its
  checks after it writes. The cost is one round trip per gated request on a
  `SerializableTx`. Not taken: dropping the savepoint and holding the gate to reading by
  `gate-writes` alone, DEV-1145's other option, which saves the trip and loses Postgres's
  refusal of a write inside a function and of `nextval`. Reopen if the gate stops being
  run in a read-only savepoint, or if Postgres stops judging a transaction read-only by
  its first snapshot.
- **`heldRace`'s gate reads the table the race counts** (`person`), not the session's
  table, which the test schema has not: the read that matters is any first read.
- **A sentence each in spec 15 and `doc/gates.md`**, though neither said the gate's read
  is the first: each describes the savepoint, and the statement before it is now part of
  how the gate runs.

## U+0000 in a string on the wire is a 400 (DEV-1192), 24 Nov 2026

- **The reader refuses a `String` that holds U+0000**, wherever it stands in the input,
  as the lead ruled on DEV-1153 under Greg's rule that nothing a caller can cause is a
  500. The refusal is at the plain `String` alone: an enum or a `Uuid` that holds one is
  already refused as not one of its names or not a `Uuid`. Not taken: refusing only a
  string an operation sends to Postgres, which the reader cannot know.
- **Settings are refused too**: they are read by the same reader (15.4), so a setting
  holding U+0000 stops the start, naming the key. Not taken: a reader with a flag to let
  them through, a line for a case no one has.
- **Spec 10.4's line stays**: a `String` the program makes that holds U+0000 is still a
  panic when it is sent; an input can no longer bring one.
