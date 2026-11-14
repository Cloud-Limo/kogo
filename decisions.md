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
  two values, as a string template would evaluate it. `${...}` may not contain braces until
  the parser reads it. There is no way to write the text `$name`, and so no tagged dollar
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
  pass; `cmd/kogo-sql` is the command and holds the literal finder, a stand-in for the parser
  that reads no Kogo, so it also finds a literal inside a comment or a string.
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

## Spec gaps found by the leakage ports, unresolved

- A `select` of a function returning `void` (`pg_advisory_xact_lock`, cusoon2's `bind_tenant`)
  has a column with no Kogo type, so it is rejected even under `tx.execute`. `void` as `Unit`
  would do it.

- A join returning two whole rows needs every column aliased by hand.
- No mapping for composite types (`charge`).
- `Long * Double`, unary minus, smart casts through `&&` and `||`, and destructuring in
  lambda parameters are unspecified.
- `db.tx` cannot finish and roll back (the fees dry run).
- No typed way to turn a named constraint violation into an expected failure.
- `Console`, `Decimal` and the text form of dates and instants have no stated members.
