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
- `doc/porting.md` is how a port of a Kotlin module stays close to it
  and is checked against it: read it before porting.
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
   its fix. A change does not loosen a test or a check in order to pass
   (an assertion, a lint, a CI step, the comparison of a port with its
   Kotlin), unless the handoff's Decided says so and why.

## Workflow

Linear holds the state of every piece of work and GitHub holds its
code. The PR holds each step's comment in full, and the issue the
first line of each, which is all the board shows. An issue goes Todo,
In Progress, In Review, Done, and the first line of its newest comment
says what happens next. Each step below can
run in Zed or as an async run, in any mix. So whatever a later step
needs is on the issue, the PR or the pushed branch, never only in a
session or a worktree.

`lin X` below is short for `mise -C ~/Dev/cusoon2-tasks run linear-X`,
a cusoon2 checkout kept on `renew` for these tasks alone: other
sessions switch branches in `~/Dev/cusoon2`, and the tasks go missing
with them. `$LINEAR_API_KEY` is already in the shell.

- **Lead.** "Lead" starts the session Greg talks to: read
  `doc/lead.md` and follow it. No other session needs that file.
- **File.** `lin file --project Kogo --state Todo "Title" < desc.md`.
  The description has four headings: What, Why, Scope (ending with a
  Budget in code and test lines and a "Done when"), Links. File what
  you find worth doing; nobody screens issues first.
- **Pickup.** "Pickup DEV-NNN" is `lin pickup DEV-NNN kogo-dev-NNN`:
  read its output and start. Use your own worktree, on the branch name
  that output gives: `git -C ~/Dev/kogo fetch origin`, then
  `git worktree add -b <branch> ~/Dev/kogo-dev-NNN origin/main`.
  `~/Dev/kogo` stays on `main` for Greg and the lead: never edit,
  commit or switch branch there, and never work in another issue's
  worktree.
- **Decide.** Make the calls the issue and the spec left open and
  record each in `decisions.md` and under Decided in the handoff. A
  choice that changes what the language is, or how its parts fit, is
  not yours: leave it open and name it in the handoff's Next step.
- **Handoff.** Commit, push with `git push -u origin HEAD` and open a
  PR into `main` with `gh pr create`. Write one comment: the first
  line `Handoff: <PR url> · <branch> · <worktree>`, then Landed /
  Verified / Decided / Next step / Gotcha. Post it on the PR with `gh
  pr comment` and its first line alone on the issue with
  `head -1 handoff.md | lin comment DEV-NNN --state "In Review"`,
  print it, and stop.
- **Code review.** "Code review DEV-NNN": `lin show DEV-NNN` and take
  the PR and branch from the newest Handoff; its text is on the PR.
  Work in `~/Dev/kogo-dev-NNN`: when it is missing, `git -C ~/Dev/kogo
  fetch origin` and create it from `origin/<branch>`; otherwise
  `git pull --ff-only` there. Read the whole diff
  against the issue, `spec.md` and the rules above, and run the tests.
  Change no code. Write one comment: the first line
  `Review: <PR url> at <sha> · N fix, N won't fix, N question`, then
  a numbered list. Each item is a **Fix** (what is wrong, the change,
  its size as code / tests), a **Won't fix** (the finding and why it
  stays) or a **Question** (a choice that is Greg's or the lead's: the
  options and which you lean to). Post it on the PR, and its first
  line alone on the issue with `head -1 review.md | lin comment
  DEV-NNN`, print it, and stop.
- **Fix.** "Fix DEV-NNN": in the same worktree, fast-forwarded or
  made as in Code review when missing, make every Fix of the newest
  Review, read on the PR, as further commits. A `Ruling:` comment
  after it, on the PR too, answers its Questions and overrides the
  Review where they differ, a Fix included. Rewrite the PR description
  if the work moved. Push.
  Write one comment: the first line
  `Fixed: <PR url> at <sha>`, then what was done for each numbered
  item, and under **Open** any Question without a Ruling and any
  choice the fixes raised. Post it on the PR, and its first line
  alone on the issue with `head -1 fixed.md | lin comment DEV-NNN`
  (no `--state`: it stays In Review), print it, and stop. Do not
  merge: the lead reads the fixes and merges.
- **Async runs.** A run has no Linear access and its clone is its
  worktree. Post the step's comment on the PR yourself and write the
  same, in full, as the run's `answer.md`, which the wrapper posts on
  the issue whole: the one place a full comment lands on the issue,
  and what an interrupted run leaves behind. A review or fix run
  leaves its tree clean: whatever is left is pushed to the PR's
  branch. No run merges.
- A comment with no PR to hold it stays on the issue in full: a
  launch comment, a `Lead:` comment, an answer of Greg's given before
  a PR exists.
- Greg does not review Kogo's PRs on GitHub. Review is the Code
  review step, and the lead answers for the quality of what merges
  and is the only one that merges.
  Never rebase or force-push a pushed branch: reviews hold its shas.
- A PR description is three parts and nothing else: one or two
  sentences of what it does, verb first, carrying "Fixes DEV-NNN"; one
  or two of why, in plain words; bullets for the major pieces only.
  What and why are the issue's, updated where the work diverged.
  Verification goes in the handoff comment, not the description. When
  more work lands on the branch, rewrite the description; don't append.
- Commit subject <= 50 chars, body lines <= 72, with a short body saying
  why.
- No AI attribution anywhere: no AI Co-Authored-By trailers, no
  "generated with" lines in commits, PR bodies, PR comments, issues or
  Linear comments.
  `.claude/settings.json` turns off the harness's own attribution.
- Go is pinned in `.mise.toml`; run it as `mise exec -- go ...`. Prefer
  a dependency release at least seven days old.
- Postgres 18 runs in this VM; `sudo -n -u postgres psql` is the
  superuser. Tests use their own database and a schema kept in this repo:
  `mise run testdb` creates it, then `mise exec -- go test ./...`.
  `cusoon` and `cusoon_b` belong to cusoon2's two stacks and are
  re-created all the time. Reading them is fine; for anything that
  writes, cleans or migrates, use a database of your own and pass its
  name explicitly. Writing to one by mistake loses nothing that matters,
  though it can break a test run in flight there: say so in your step's
  comment and carry on.
