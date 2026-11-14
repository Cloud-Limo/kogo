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

## Taken in the v0 spec, not yet discussed

- Both Arrow spellings are in (`Either` values and `Raise<E>` receivers): cusoon2 uses both.
- Markers (`@RequiresOptIn`, `@file:OptIn`) are the one annotation form, so the access walls
  port. Known hole: without inheritance, any file can implement the `HeldBusiness` interface.
- Value classes are out: cusoon2 has none, and binding distinct id types to SQL columns needs
  a design (foreign keys could supply it).
- No `apply`/`run`/`with`, no labelled returns, no `object`, no classes beyond data classes.
- `Regex` is RE2: no lookaround or backreferences.

## Spec gaps found by the leakage ports, unresolved

- A join returning two whole rows needs every column aliased by hand.
- No mapping for composite types (`charge`).
- `Long * Double`, unary minus, smart casts through `&&` and `||`, and destructuring in
  lambda parameters are unspecified.
- `db.tx` cannot finish and roll back (the fees dry run).
- No typed way to turn a named constraint violation into an expected failure.
- `Console`, `Decimal` and the text form of dates and instants have no stated members.
