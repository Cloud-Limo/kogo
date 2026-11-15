# Kogo

A small language in Kotlin's syntax that compiles to Go, for Postgres
backends that serve web front ends. SQL is written as SQL and checked
against the real schema at build time.

- `spec.md`: the language, v0.
- `examples/`: programs in it.
- `decisions.md`: what was decided and why.
- `doc/design.md`: principles and how it compiles.

Built so far: the parser (`syntax`), the SQL pass (`sqlpass`) with its
command, the prelude's first declarations (`prelude`), the checker
(`check`): names, types, nulls, `when` and expected failures, spec
sections 2 to 6 and 9; and the emitter's first slice (`emit`), which
writes Go for pure code, sections 2 to 6, on the Go of `rt` and
`prelude/kogo`.

```
mise exec -- go build -o bin/kogo ./cmd/kogo
bin/kogo check <dir>
bin/kogo emit <dir> <out>
```

`kogo check` checks the `.kg` files under a directory as one program and
prints one JSON error a line (file, line, column, code, message). It
exits 0 when there are none. `kogo emit` checks them and then writes
one Go package for each Kogo package, `a.b`'s at `<out>/a/b/kogo.go`;
`<out>` lies in a Go module that requires this one.

```
mise exec -- go build -o bin/kogo-sql ./cmd/kogo-sql
DATABASE_URL=postgres://kogo:kogo@127.0.0.1/kogo_test bin/kogo-sql check examples
```

`kogo-sql check` prints each `sql"""` literal's signature, or its error, as one JSON
object a line; `emit <dir> <package>` prints a Go function per literal. It
needs Postgres 18. `mise run testdb` creates the database the tests use.
