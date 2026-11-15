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

## Taken in the v0 spec, not yet discussed

- Both Arrow spellings are in (`Either` values and `Raise<E>` receivers): cusoon2 uses both.
- Markers (`@RequiresOptIn`, `@file:OptIn`) are the one annotation form, so the access walls
  port. Known hole: without inheritance, any file can implement the `HeldBusiness` interface.
- Value classes are out: cusoon2 has none, and binding distinct id types to SQL columns needs
  a design (foreign keys could supply it).
- No `apply`/`run`/`with`, no labelled returns, no `object`, no classes beyond data classes.
- `Regex` is RE2: no lookaround or backreferences.

## Taken building the SQL pass (DEV-818), not yet discussed

- **Outer-join nullability is read from Postgres's own parse tree.** The checker's session sets
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
- **A name is one parameter, an expression one per occurrence.** `$active` twice is `$1` twice,
  so Postgres types it once and the cast in 10.6's idiom is optional; `${newId()}` twice is
  two values, as a string template would evaluate it. `${...}` may not contain braces: the
  parser reads them, but the SQL pass scans the literal itself until the checker hands it
  parsed parameters. There is no way to write the text `$name`, and so no tagged dollar
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

## Spec gaps found by the leakage ports, unresolved

- A join returning two whole rows needs every column aliased by hand.
- No mapping for composite types (`charge`).
- `Long * Double` and smart casts through `&&` and `||` are unspecified. (DEV-819 settled
  unary minus and destructuring in lambda parameters.)
- `db.tx` cannot finish and roll back (the fees dry run).
- No typed way to turn a named constraint violation into an expected failure.
- `Console`, `Decimal` and the text form of dates and instants have no stated members.
