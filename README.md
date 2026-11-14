# Kogo

A small language in Kotlin's syntax that compiles to Go, for Postgres
backends that serve web front ends. SQL is written as SQL and checked
against the real schema at build time.

- `spec.md`: the language, v0.
- `examples/`: twenty programs.
- `decisions.md`: what was decided and why.
- `doc/design.md`: principles and how it compiles.

Built so far: the SQL pass (`sqlpass`) and its command.

```
mise exec -- go build -o bin/kogo-sql ./cmd/kogo-sql
DATABASE_URL=postgres://kogo:kogo@127.0.0.1/kogo_test bin/kogo-sql check examples
```

`check` prints each `sql"""` literal's signature, or its error, as one JSON
object a line; `emit <dir> <package>` prints a Go function per literal. It
needs Postgres 18. `mise run testdb` creates the database the tests use.
