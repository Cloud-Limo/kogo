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
(`check`), spec sections 2 to 11, and the emitter (`emit`), which
writes Go for pure code, sections 2 to 6, expected failures, section 9,
and SQL and transactions, section 10, on the Go of `rt` and
`prelude/kogo`. A hand-written Go driver calls the result: there is no
server yet.

```
mise exec -- go build -o bin/kogo ./cmd/kogo
bin/kogo check <dir>
bin/kogo emit <dir> <out>
bin/kogo ts <dir> <out>
```

`kogo check` checks the `.kg` files under a directory as one program and
prints one JSON error a line (file, line, column, code, message). It
exits 0 when there are none. A `<dir>` with no `.kg` file under it is
refused with `no-source`, exit 2, as is a `<dir>` that is not a directory;
a link to one is followed. A program with a `sql"""` literal is checked
against the database `DATABASE_URL` names, as below. `kogo emit` checks
them and then writes one Go package for each Kogo package, `a.b`'s at
`<out>/a/b/kogo.go`; `<out>` lies in a Go module that requires this one.
The Go names its sources by paths relative to itself, so a panic's trace
names the Kogo file and line on any machine. Move `<out>` or the sources
relative to each other and traces name a path that is not there, with
the line still right; emit again.
`kogo emit` refuses, writing nothing, when `<out>` holds a `kogo.go` it
wrote for a package the program no longer has, as after a rename: remove
that file or emit into another directory. Every other file in `<out>`,
such as your own `main.go`, is left alone.
`kogo ts` checks them and then writes `<out>/<name>.ts` for each `api`
list: its wire types and an interface of its operations (spec.md 15.6).

```
mise exec -- go build -o bin/kogo-sql ./cmd/kogo-sql
DATABASE_URL=postgres://kogo:kogo@127.0.0.1/kogo_test bin/kogo-sql check examples
```

`kogo-sql check` prints each `sql"""` literal's signature, or its error, as one JSON
object a line. It needs Postgres 18. `mise run testdb` creates the database the
tests use; a test that runs statements does so in a schema of its own there.

Copyright 2026 Gregory Tod. Licensed under the Apache License,
Version 2.0, as Kotlin is: see `LICENSE`.
