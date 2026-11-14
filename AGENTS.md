# Agent instructions

Durable rules for coding agents working on Kogo. Project state (scope,
progress, next steps) lives in Linear, project "Kogo", team DEV.

## What this is

Kogo is a small language in Kotlin's syntax that compiles to Go, for one
shape of program: request in, rules, SQL, response out, on Postgres. Greg
is building it because he wants to; do not argue whether it is practical.

- `spec.md` is the whole language. `examples/` are programs in it.
- `decisions.md` records each choice with its reason and what reopens it.
  Add to it whenever you choose something the spec or the issue did not.
- `doc/design.md` is the design: principles, pipeline, the SQL pass.
- `prototype/` holds stage 1's throwaway scripts; replace, don't extend.

## Rules

1. The eight principles in `doc/design.md` settle design questions. The
   two that bite most: Postgres is the type checker for SQL (never
   re-implement SQL typing), and nothing specific to cusoon2 enters this
   repo's code or tests.
2. Refuse features the task does not need. Every line costs, tests
   included; give sizes (code / tests) with any proposal.
3. Each component (SQL pass, parser, checker, emitter) is a separate Go
   package with its own tests, usable without the others.
4. Errors are machine-readable (file, line, column, code, message) and
   say what to change.
5. A change to the language changes `spec.md` in the same commit.
6. Test what the language promises. Check each new test fails without
   its fix.

## Workflow

- Linear from this repo uses cusoon2's tasks:
  `mise -C ~/Dev/cusoon2 run linear-pickup DEV-NNN` (also `linear-comment`
  and `linear-file`); `$LINEAR_API_KEY` is already in the shell.
  "Pickup DEV-NNN" is that one command: read its output and start.
- One branch per issue, `dev-NNN-short-name`, off `main`. Commit, push
  with `git push -u origin HEAD`, open a PR into `main` with `gh pr
  create`, post the handoff comment (Landed / Verified / Decided / Next
  step / Gotcha) on the issue with the PR's link and on the PR with `gh
  pr comment`, and stop. Greg reviews and merges; answer review with
  further commits. Never rebase or force-push a pushed branch: the
  review holds its shas.
- A PR description is three parts and nothing else: one or two
  sentences of what it does, verb first, carrying "Fixes DEV-NNN"; one
  or two of why, in plain words; bullets for the major pieces only.
  What and why are the issue's, updated where the work diverged.
  Verification goes in the handoff comment, not the description. When
  more work lands on the branch, rewrite the description; don't append.
- Commit subject <= 50 chars, body lines <= 72, with a short body saying
  why. No AI attribution anywhere.
- Go is pinned in `.mise.toml`; run it as `mise exec -- go ...`. Prefer
  a dependency release at least seven days old.
- Postgres 18 runs in this VM; `sudo -n -u postgres psql` is the
  superuser. Tests use their own database and a schema kept in this repo:
  `mise run testdb` creates it, then `mise exec -- go test ./...`.
  cusoon2's databases (`cusoon`, `cusoon_b`) are read-only to you:
  describe statements against them, never write or migrate them.
