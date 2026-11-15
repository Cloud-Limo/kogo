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
   same in both. Additions look unlike Kotlin.
4. **Postgres is the type checker for SQL.** Never re-implement SQL
   typing; ask the database.
5. **Go is a backend, not a source language.** Nobody reads the emitted
   Go, but `//line` directives map it back to the source.
6. **Small enough to hold in a prompt.** One way to do each thing; the
   spec replaces training data.
7. **Precise errors.** Errors are machine-readable (file, line,
   column, code) and name the fix. The loop must stay no slower than
   the warm Kotlin build it replaces; speed is a floor, not a claim.
8. **Core apart from prelude.** Nothing specific to one application
   lives in the core.

## Pipeline

```
Source files ─────────────► 1 Parse
                               │
Binding declarations ─────► 2 Type-check (nulls, when, errors, SQL)
Dev Postgres (migrated) ──►    │
                            3 Emit Go (with //line maps)
                               │
                            4 go build (links pgx and Go libraries)
```

Only the type-check stage reads bindings and talks to Postgres. Sealed
types become an interface plus a type switch; `T?` becomes a pointer or
an option struct; `Either<E, A>` becomes `(A, error)`; `context.Context`
is threaded by the compiler and never appears in source.

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
6. Emit a pgx call with a scan struct.

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

## MVP steps

1. The SQL pass, as a Go library and a command.
2. Parser and checker for what the first ported module uses.
3. Go emitter.
4. A differential run of a ported module against its Kotlin original.

## Keeping the general language open

- Lower `Either`, `bind`, `raise` and `Raise<E>` to one internal notion
  of a fallible function, so the surface can change later.
- Define Kogo's own semantics for numbers, equality, collection order
  and absence; never inherit Go's by accident.
- Bindings wrap Go libraries; they are declarations and compile to
  nothing.
