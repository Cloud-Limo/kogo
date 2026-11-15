# Kogo

A small language in Kotlin's syntax that compiles to Go, for Postgres
backends that serve web front ends. SQL is written as SQL and checked
against the real schema at build time.

- `spec.md`: the language, v0.
- `examples/`: programs in it.
- `decisions.md`: what was decided and why.
- `doc/design.md`: principles and how it compiles.

Built so far: the parser (`syntax`), the SQL pass (`sqlpass`) with its
command, the prelude's first declarations (`prelude`), and the checker
(`check`), spec sections 2 to 11.

```
mise exec -- go build -o bin/kogo ./cmd/kogo
bin/kogo check <dir>
```

`kogo check` checks the `.kg` files under a directory as one program and
prints one JSON error a line (file, line, column, code, message). It
exits 0 when there are none. A program with a `sql"""` literal is checked
against the database `DATABASE_URL` names, as below.

```
mise exec -- go build -o bin/kogo-sql ./cmd/kogo-sql
DATABASE_URL=postgres://kogo:kogo@127.0.0.1/kogo_test bin/kogo-sql check examples
```

`kogo-sql check` prints each `sql"""` literal's signature, or its error, as one JSON
object a line; `emit <dir> <package>` prints a Go function per literal. It
needs Postgres 18. `mise run testdb` creates the database the tests use.
