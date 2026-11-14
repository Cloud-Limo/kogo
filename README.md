# Kogo

A small language in Kotlin's syntax that compiles to Go, for Postgres
backends that serve web front ends. SQL is written as SQL and checked
against the real schema at build time.

- `spec.md`: the language, v0.
- `examples/`: twenty programs.
- `decisions.md`: what was decided and why.
- `doc/design.md`: principles and how it compiles.

Nothing is built yet. The first component is the SQL pass.
