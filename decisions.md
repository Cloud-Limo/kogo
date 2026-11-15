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
- **A value-producing `if`, `when`, `?:` or `?.` is written toward where its value goes,
  and wrapped in a function elsewhere** (15 Nov 2026; spec 6). A `return`, `break` or
  `continue` buried in a larger expression is refused, though Kotlin allows it; the
  examples and both stage-1 ports hold two jumps in a value's place, both
  `val x = y ?: return z`. Not taken: flattening every expression into statements and
  temporaries, about 400 lines and the simplified tree under another name.
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
- **The emitted Go is a shape for the emitter to replace.** One function per literal over a
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
- **Not refused yet**: a `val` in an interface that a data object implements (it has no
  constructor to override it in), an `out` type parameter used as a parameter's type, and
  two functions with one signature, which are an error only when called.

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
  spelling is left to whoever builds the emitter, to record here.

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

## Spec gaps found by the leakage ports, unresolved

- A join returning two whole rows needs every column aliased by hand, and composite types
  (`charge`) have no mapping. Agreed on 15 Nov 2026 and built in DEV-837: a composite type
  fills a data class, and a whole row is selected as `b as business`.
- Outbound calls are handles (above), but their timeouts and retries, workers, and an
  application's own binding files have no design.
- `Console`, `Decimal` and the text form of dates and instants have no stated members; the
  prelude's declarations (spec 13) are where they will be stated.
