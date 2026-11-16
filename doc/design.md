# Kogo design

From the design brief of 15 Nov 2026, trimmed to what still holds after
`decisions.md`.

## Thesis

A language for an LLM author should optimise for checkability and
locality: how fast and precisely a checker says the code is wrong, and
how little context must be loaded to change something safely. Density is
not the goal; redundancy a checker can use (types, exhaustiveness,
constraints) is worth typing.

## Principles

1. **Checkability over terseness.** Any rule the compiler can check is
   worth some verbosity.
2. **Locality.** Meaning comes from what is visible in the code and the
   signatures it calls. No macros, reflection, annotation magic or
   ambient state.
3. **A strict subset where it overlaps Kotlin.** Valid in both means the
   same in both. Additions look unlike Kotlin. In the core (syntax,
   numbers, `null`, `when`, equality, data classes, failures) this is
   strict. In the prelude a Kotlin name means Kotlin's behaviour in
   ordinary use; a difference is allowed only in corners, when matching
   would mean reimplementing a JVM algorithm or table and no ported
   module's side-by-side run depends on it, and each is stated in
   `spec.md` section 12. A function that would differ in ordinary use
   is Kogo's own. Kogo adds no member to a type Kotlin has: what is
   Kogo's own is a function or a type of a `kogo.` package, imported by
   name, so a file's imports show what in it is not Kotlin's.
4. **Postgres is the type checker for SQL.** Never re-implement SQL
   typing; ask the database.
5. **Go is a backend, not a source language.** Nobody reads the emitted
   Go, but line directives map it back to the source, written at the
   start of each line: `/*line ../../src/a.kg:26*/`, padded so that
   the code starts at one column, then indented a tab a level.
6. **Small enough to hold in a prompt.** One way to do each thing; the
   spec replaces training data.
7. **Precise errors.** Errors are machine-readable (file, line,
   column, code) and name the fix. The loop must stay no slower than
   the warm Kotlin build it replaces; speed is a floor, not a claim.
8. **Core apart from prelude.** Nothing specific to one application
   lives in the core.

## Ports

Code moved from Kotlin to Kogo aims to behave exactly as it did;
cusoon2 is the first application to move. Principle 3 does not give
this: a port is not the same text, since its SQL and whatever Kogo
leaves out are rewritten. So each piece is run beside its Kotlin on the
same inputs and compared exactly before it replaces it. A difference
the run finds is fixed, or it is accepted: Greg decides, and it is
recorded with what differs, why matching it costs too much, and the
cases it covers, so that the comparison forgives those cases and
nothing else. `doc/porting.md` has the rules.

## Pipeline

```
Source files ─────────────► 1 Parse
                               │
Binding declarations ─────► 2 Type-check (nulls, when, errors, SQL)
Dev Postgres (migrated) ──►    │
                            3 Emit Go (a /*line*/ map starts each line)
                               │
                            4 go build (links pgx and Go libraries)
```

`kogo check` is the only gate. A program that passes it builds and runs.
A limit that stands is a checker error with a line of the spec; where the
emitter cannot yet write a form the checker accepts, it refuses with
`not-yet-emitted`, which is a debt with an issue.

Only the type-check stage reads bindings and talks to Postgres. Sealed
types become an interface and `when` a switch; `T?` becomes a pointer; a
failure is a panic that its own raise scope recovers; `context.Context`
rides in the `Tx` and `Db` handles and never appears in source. "The
emitter" below has the rest, and "The server" what serves operations.

## The SQL pass

The model is Rust's sqlx: every statement is a constant, checked by
asking Postgres to prepare it.

1. Connect to a development database with all migrations applied.
2. Rewrite `$name` and `${expr}` to `$1`, `$2`, ... and prepare the
   statement.
3. Read the parameter types and result columns from the statement
   description (pgx: `ParamOIDs`, and per field `DataTypeOID`, `TableOID`,
   `TableAttributeNumber`; confirm against the pgx version in use).
4. Map types by `spec.md` section 10.4, resolving domains, enums and
   arrays through the catalog.
5. Decide nullability by section 10.5: non-null only for a plain
   reference to a `NOT NULL` column not on the nullable side of an outer
   join; `as "name!"` and `as "name?"` override.
6. Hand the signature to the checker, whose facts the emitter writes
   the call from ("The emitter" below).

Postgres does not report result nullability; that is the pass's own
work. A later step caches results by statement text and schema hash so
a build can run without a database.

Checked on a real schema with plain `PREPARE` (stage 1): Postgres infers
parameters and columns through domains, citext, enums and arrays, and
rejects a misspelt column, a mistyped comparison and an unknown enum
label, each with a position.

## The checker

The checker reads the parser's trees, the prelude's declarations
(`spec.md` section 13) and the SQL pass's signatures. It reports errors,
or hands the emitter facts beside the syntax tree: tables keyed by tree
node, as Go's own type checker gives.

- The type of every expression, and what every name refers to.
- For each call, the function chosen, its settled type arguments, and
  whether it can fail and with what error type.
- Where a name is narrowed, and to what.
- For each `sql` literal, its signature and the column that fills each
  property.
- For each `api` list, its operations in order, and the wire types they
  reach.

Every function, lambda and call is marked as able to fail with an error
type `E`, or not, whichever spelling the author used, so the emitter
knows one notion of a fallible function. The checker asks for a
literal's signature through one function: the SQL pass answers in real
use and a test answers with signatures of its own, so the checker's
tests need no database.

There is no second, simplified tree, because its right shape depends on
the Go to be emitted. The emitter therefore handles every surface form.
A simplifying step can be added between the two later, reading the same
facts.

## The emitter

The emitter writes Go from a checked program and the checker's facts.
Nobody reads the Go (principle 5), so where Go's idiom and a simpler
emitter differ, the simpler emitter wins.

**What it produces first.** A library: one Go package per Kogo package,
called by a hand-written Go driver that compares results with expected
ones. No server, no RPC, no `kogo run`. It is built in three slices,
each proven by running: pure code (examples 01 to 06 and 20), failures
(07 to 09), then SQL and transactions (10 to 19 and 21, against the test
database). Examples 01 and 02 are cusoon2's pay and discount arithmetic,
so the first side-by-side run with the Kotlin original can follow the
first slice. Emitted names must be ones a driver can call.

**Values.**

| Kogo | Go |
| --- | --- |
| `Int`, `Long`, `Double`, `Boolean` | `int32`, `int64`, `float64`, `bool` |
| `T?` | a pointer, nil for `null`, for every `T` |
| data class | a struct, passed by value |
| sealed interface, and `Either` | an interface; `when` is a switch, `is` a type test |
| enum | a named string |
| `List<T>` | a slice, never changed once built |
| lambda | a function value |
| generic function | a generic function, its type arguments written out |

`==` is Go's `==` where the checker knows the type is a number, a
string, a `Boolean` or an enum, and one runtime function that walks both
values everywhere else: Go compares a pointer by address and does not
compare slices. The same walk gives `toString()` its text. Go has no
wider type for a container, so a `List<Ok<Int>>` used as a
`List<Outcome<Int>>` is copied and an `Either<Missing, A>` used as an
`Either<StoreError, A>` is rebuilt; one value widening is free or takes
an address.

**Failures.** `raise` and `bind` are ordinary calls that panic; the
`either { }` block recovers and gives the failure as a value, as Arrow
does with an exception. Each scope has a handle: the block of `either`
is a Go function that takes it as its first parameter, and the lambdas
inside capture it. No function a program declares takes a hidden scope
parameter, since a function that can fail returns an `Either`; the
core's own forms, `raise`, `ensure`, `ensureNotNull` and `bind`, are
given the handle of the innermost scope they are written in, and fail
with its error type. A failure carries
the handle it was raised on, so a scope recovers only its own. A panic
of any other kind is a bug (section 8 of the spec) and passes through
untouched. So a failure works anywhere inside its scope, lambdas
included, and the prelude needs no failure-aware copies. The prelude's
Go must hold no lock and leave nothing half done while it calls a
lambda.

**Strings** are Go strings counted as Kotlin counts: UTF-16 units. An
operation that would leave half a surrogate pair leaves U+FFFD.

**`if`, `when`, `?:` and `?.` as values.** Go's are statements. Where
the value goes straight into a `val`, an assignment or a `return`, or is
the result of a function or lambda, the emitter writes a Go `if` or
`switch` whose branches store or return. Anywhere else it wraps the
expression in a function called on the spot. A wrapper cannot hold a
`return`, `break` or `continue` of the Kogo function, so the checker
refuses one buried in a larger expression. Evaluation stays left to
right, which Go does not promise for every operand: tests must show it.

**The prelude's Go** is hand-written in this repo with its own tests:
`rt` holds what every program needs whatever its prelude (the walk
behind `==` and `toString()`, `Unit`, `Char`, the helpers the emitter
calls), and `prelude/kogo` and `prelude/kogo/time` hold the Go behind
the declarations of Kogo's packages `kogo` and `kogo.time`. One test
emits a file that uses every declaration of `prelude/`
and compiles it. It depends on nothing but the Postgres driver: ids and
times are small types of our own over the standard library, `Regex` is
Go's, and `Decimal` holds what Postgres sent and has no arithmetic.
`Map` and `Set` are one ordered implementation that compares keys by the
runtime walk of `==`, so no key type is refused. Go has no overloading;
one naming rule gives each overload, the prelude's and a program's, its
Go name.

**The request's context** is held by the `Tx` and `Db` handles. Section
7 of the spec puts a handle wherever the database is touched, so no
function takes a hidden context, and a program with no requests carries
none. A driver makes a `Db` from a connection pool and a context.

**SQL.** Each of the five calls is a call of the prelude's Go, given the
`Tx`, the statement as Postgres takes it, and what each parameter
binds. A call that reads rows also passes a function, written from the
checker's facts, that scans each column into a variable of its own and
builds `R` from them: Go's compiler checks every row type against its
columns, and nothing is found by reflection. A column that may be null
is read as a pointer and any other as it is, so the driver refuses a
null where the row type has none. The prelude's types take themselves
to and from the driver's. A composite type is read field by field, once
the connection's driver has been told of the type. `db.tx` begins,
commits and rolls back around its block, and `tx.atomically` is a
savepoint; whatever Postgres refuses is a panic, which rolls back on
its way out as a failure does.

## The server

Serving operations (`spec.md` section 15) adds no stage to the
pipeline: the checker reads each `api` list, the emitter writes a table
for it, a Go library serves the table, and the application's own Go
starts it.

**The checker** holds the list's rules: each fun's four parameters and
its `Either<String, A>`, one environment type for a list, and every
input and answer a wire type. It hands the emitter each list with its
operations, and whether each takes a `Tx` or a `Db`.

**The table.** For each list the emitter writes one Go value in the
list's package: each operation's name on the wire, its kind, and its
fun. Nothing outside calls an operation by its Go name, so the naming
rule's neighbours never reach the wire.

**The library** is hand-written Go in this repo, the Go of package
`kogo.rpc`, beside its `Request`. Given a table, a connection pool and
the environment it is an `http.Handler` that does what section 15 says:
it finds the operation, reads the input strictly, runs a `Tx` operation
in a SERIALIZABLE transaction with reruns, commits or rolls back, writes
the answer, and turns a panic into a 500. JSON is read and written by
one walk over the emitted types, as `==` is, by the names the structs
already carry: no reader or writer is emitted for each type. Go's
reflection does not list an enum's constants, so the table carries each
that its inputs reach. Reruns are those of `db.tx(serializable = true)`,
here with a limit, since a person is waiting: 90 seconds unless `main`
says otherwise, and past it the answer is 503, so that a 500 always
means a bug.

**The application's `main`** is Go that it writes once, about fifteen
lines: read the configuration, open the pool, build the environment's
handles, mount the handler at a path, listen. Configuration, ports,
paths and other routes, a webhook say, stay out of the language. There
is no `fun main` and no `kogo serve`. A Go test calls a table without
HTTP.

**Logging.** One id for each HTTP request, given before any rerun and
written on every line about it: an id for each attempt makes a rerun
look like a second request. For every request: the operation, the
outcome (ok, refused, or the status), the time taken, and each rerun
with its cause, since a serialization failure and a deadlock call for
different investigations. For a panic: its message and a trace that
names the Kogo file and line. Never an input, an answer or a refusal's
words: they carry login codes and personal data. The lines go to a
logger that `main` passes.

**TypeScript.** `kogo ts` writes one file for each list from the
checker's facts: the wire types, the answer's type and the table of
operations. Nothing is found by reflection. The file is checked in, and
an application's own checks run `kogo ts` and fail when `git diff`
shows a change, so Kogo needs no flag for a stale file. The client
that calls `fetch` is the application's: it depends on its cookies and
its UI framework.

**Not designed, on purpose:** how an operation sets a cookie (logins);
Origin and CSRF checks, which will be options of the handler; workers;
timeouts and retries of outbound calls.

## MVP steps

The MVP is a real cusoon2 module, with SQL, that matches its Kotlin
original exactly when run side by side and is served over typed RPC.
It is done when all five steps are.

1. The SQL pass, as a Go library and a command.
2. Parser and checker for what the first ported module uses.
3. Go emitter.
4. Differential runs of ported modules against their Kotlin originals:
   the invoice ledger, then suppliers and businesses with SQL.
5. The server, as "The server" above has it: the `api` list, the Go of
   `kogo.rpc`, the emitted table and `kogo ts`.

The run is side by side: one file of cases, hand-picked edges and a few
thousand random ones from a fixed seed, feeds both the Kotlin and the
emitted Go. Every result is compared exactly: whole numbers digit for
digit, a `Double` bit for bit, text character for character, `null`,
lists in order, data classes field by field; a case that throws in
Kotlin must panic in Kogo with the same message. There is no tolerance,
and passing is zero differences. Each difference is a mistake in the
port, a fault in the emitter or the prelude, or a difference Greg
accepts, recorded (Ports). The first module is cusoon2's invoice
ledger and pay arithmetic, which needs only the emitter's first slice;
the second, once SQL runs, is suppliers and businesses, where the same
result also means the same rows changed from identical copies of a
database, with the clock and new ids supplied alike. The port, the
drivers and the cases live in cusoon2, since nothing specific to it
enters this repo, and the port names the cusoon2 commit it matches.

## Keeping the general language open

- Lower `Either`, `bind`, `raise` and `Raise<E>` to one internal notion
  of a fallible function, so the surface can change later.
- Define Kogo's own semantics for numbers, equality, collection order
  and absence; never inherit Go's by accident.
- Bindings wrap Go libraries; they are declarations and compile to
  nothing.
