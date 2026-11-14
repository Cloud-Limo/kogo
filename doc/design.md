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
7. **A fast loop.** Check and build in seconds; errors are
   machine-readable and name the fix.
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
