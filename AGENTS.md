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
- `doc/roadmap.md` is the order of the work that moves cusoon2 onto
  Kogo and then continues operator money, with the reasons.
- `doc/porting.md` is how a port of a Kotlin module stays close to it
  and is checked against it: read it before porting.
- `prototype/` holds stage 1's throwaway scripts; replace, don't extend.

## Rules

1. The eight principles in `doc/design.md` settle design questions. The
   two that bite most: Postgres is the type checker for SQL (never
   re-implement SQL typing), and nothing specific to cusoon2 enters this
   repo's code or tests. Kogo is public: docs may use cusoon2 as the
   worked example, but no secrets, personal data, prices or commercial
   terms go anywhere, PR comments included. `mise run leaks` looks
   for secrets.
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

Kept the same as cusoon2's `AGENTS.md` between the markers below, but
for Where a step works and Who merges; change both in the same day's
PRs.

`lin X` below is short for `mise -C ~/Dev/cusoon2-tasks run linear-X`,
a cusoon2 checkout kept on `renew` for these tasks alone: other
sessions switch branches in `~/Dev/cusoon2`, and the tasks go missing
with them. `$LINEAR_API_KEY` is already in the shell.

<!-- Workflow, shared: begin -->
Linear holds the state of every piece of work and GitHub holds its
code. The PR holds each step's comment in full, and the issue the
first line of each, which is all the board shows, with the PR's
address in Linear's review view under it. An issue goes Todo, In
Progress, In Review, Done, and the first line of its newest event
says what happens next. An event is a comment that begins with a word
of cusoon2's `doc/plans/pipeline-events.md`, section 5, and a colon;
any other is a note, which no reader takes for a step. A session
posts an event with `lin step DEV-NNN < comment.md`, which refuses a
first line that does not fit its word's form, posts the whole where
the word says and its first line on the issue, and does what goes
with the word; a note goes with `lin comment`. Each step
below can run in Zed, as an async run or as a local run, in any mix.
So whatever a later step needs is on the issue, the PR or the pushed
branch, never only in a session or a worktree.

One lead, started in `~/Dev/kogo`, directs the work of both
repositories. Kogo's issues are in the Linear project Kogo and its PRs
go into `main`; cusoon2's are in Cloud Limo Relaunch and go into
`renew`. Both are in team DEV.

- **Where a step works.** Kogo: a worktree per issue,
  `~/Dev/kogo-dev-NNN`, with `<where>` `kogo-dev-NNN`. Pickup makes it,
  or `lin local` before it: `git -C ~/Dev/kogo fetch origin`, then `git
  worktree add -b <branch> ~/Dev/kogo-dev-NNN origin/main`; a later
  step that finds it missing fetches and makes it from
  `origin/<branch>`. `~/Dev/kogo` stays on `main` for Greg, the lead
  and the clerk: never edit, commit or switch branch there, and never
  work in another issue's worktree.
- **Lead.** "Lead", said in `~/Dev/kogo`, starts the session Greg
  talks to: read Kogo's `doc/lead.md` and follow it. It decides, and
  answers what the clerk hands on; it starts an issue's first step,
  waits on no run and merges nothing. No other session needs that
  file.
- **Clerk.** "Clerk DEV-NNN", said in `~/Dev/kogo`, takes one step of
  the pipeline for that issue and exits: read Kogo's `doc/clerk.md`
  and follow it. `lin clerk` says it when an issue's newest event
  becomes `Handoff:`, `Review:`, `Ruling:` or `Fixed:`, from a run or
  a session alike, or its PR merges, so no session reports a step
  back. It starts the Code review and the Fix, checks and tidies.
  What it cannot settle it hands to the lead: a `Clerk:` comment,
  which `lin step` posts with the label `lead`; both wait for Greg's
  next "Lead". It leaves an issue labelled `lead` alone.
- **Guide.** "Guide DEV-NNN", said in the repository the design is
  about, starts a session that takes Greg through a design to his
  rulings: read Kogo's `doc/guide.md` and follow it. It ends with one
  comment on the issue for the lead, and posts no step's comment,
  launches nothing and merges nothing. Greg starts it, since it is a
  conversation; it reads, or takes a worktree of its own, and holds no
  stack. No other session needs that file.
- **File.** `lin file --project <project> --state <state> "Title" <
  desc.md`, into the project of the repository the work lands in, and
  in Todo if it is ready to start, in Backlog if it waits on something
  (its Scope says on what). The description has four headings: What
  (one or two sentences, verb first), Why (one or two sentences in
  plain words; for product work it may end "Without it: <what an op or
  customer does instead>, about N times a year"), Scope (ending with a
  Budget in code and test lines and a "Done when" a reviewer can check
  in one look), Links. Write a file name as a repo path in backticks:
  Linear turns a bare `audit.sh` into a link. File what you find worth
  doing; nobody screens issues first.
- **Pickup.** "Pickup DEV-NNN" is `lin pickup DEV-NNN <where>`, with
  `<where>` as Where a step works says: read its output and start
  there, on the branch that output gives.
- **Decide.** Make the calls the issue and the plan or spec left open
  and record each, with the alternative not taken, under Decided in
  the handoff and where the repository keeps them (Kogo's
  `decisions.md`, the plan a cusoon2 issue links): Decided is what
  Greg reads first. A choice that changes what the product or the
  language is, or how its parts fit, is not yours: leave it open and
  name it in the handoff's Next step.
- **Handoff.** Commit, push with `git push -u origin HEAD` and open
  a PR into the repository's main line with `gh pr create --base
  <main or renew>`. Write one comment: the first line `Handoff: <PR
  url> · <branch>`, then Landed / Verified / Decided / Next step /
  Gotcha. Post it with `lin step DEV-NNN < handoff.md`, which puts it
  on the PR, its first line on the issue, and moves the issue to In
  Review; print it, and stop. A PR that outgrows its Budget, or adds
  what the Scope did not name, says so in the handoff.
- **Code review.** "Code review DEV-NNN": `lin show DEV-NNN` and take
  the PR and branch from the newest Handoff; its text is on the PR.
  Work as Where a step works says, on the branch fast-forwarded with
  `git pull --ff-only`, or made from `origin/<branch>` when missing.
  Read the whole diff against the issue and this file (in Kogo,
  `spec.md` too), and run the tests. Change no code. Write one
  comment: the first line `Review: <PR url> at <sha> · N fix, N won't
  fix, N question`, then a numbered list. Each item is a **Fix** (what
  is wrong, the change, its size as code / tests), a **Won't fix** (the
  finding and why it stays) or a **Question** (a choice that is Greg's
  or the lead's: the options and which you lean to). Post it with
  `lin step DEV-NNN < review.md`, print it, and stop.
- **Fix.** "Fix DEV-NNN": in the same place, fast-forwarded or made as
  in Code review when missing, make every Fix of the newest Review,
  read on the PR, as further commits. A `Ruling:` comment after it, on
  the PR too, answers its Questions and overrides the Review where
  they differ, a Fix included. Rewrite the PR description if the work
  moved. Push. Write one comment: the first line `Fixed: <PR url> at
  <sha>`, then what was done for each numbered item, and under
  **Open** any Question without a Ruling and any choice the fixes
  raised. Post it with `lin step DEV-NNN < fixed.md` (it stays In
  Review), print it, and stop. Do not merge: see Who merges.
- **Who merges.** Kogo: the clerk, and no other session. After Code
  review and Fix it reads the fixes and runs the checks `doc/clerk.md`
  names. It merges when every one passes and nothing is Open, and
  otherwise hands the PR to the lead. Greg does not review Kogo's PRs
  on GitHub: review is the Code review step.
- **Async runs.** The lead or the clerk starts one with `lin launch
  DEV-NNN < brief.md`, the brief's first line `Start: <step> · async ·
  <directive>`: it posts the `Start:` with the brief under it, then
  adds the `agent` label; Greg may add or remove the label too. The
  launcher takes the issue's newest async `Start:` as the brief, so a
  note posted after it changes nothing. A run has
  no Linear access and its clone is its worktree. It clones the
  repository of its issue's project: Kogo for Kogo, cusoon2 for any
  other. Work that lands in another repository than its issue's
  project's runs as a local run.
  Write the step's comment, in full, as the run's `answer.md`, and post
  it with `agent-step < answer.md`: it refuses a first line that does
  not fit its word, as `lin step` does, and posts on the PR the line
  names with the run's marker line last. The wrapper posts on the
  issue the first line and the PR's address in Linear's review view
  when the PR has that text, and `answer.md` whole when not: what an
  interrupted run leaves behind. Its own comment begins `Answer:` for
  a run that is no step, `Failed:` for one that ended with no answer.
  A review or fix run leaves its tree clean: whatever is left is
  pushed to the PR's branch. No run merges.
- **Local runs.** A step that needs this VM, a stack or the other
  repository runs here as a headless session: the lead or the clerk
  starts it with `lin local DEV-NNN <where> "<phrase>"`, which returns
  at once, once it has posted `Start: <step> · local · <where> ·
  <model> <effort> $<cap>` on the issue with the phrase under it: the
  clerk starts each later step the way the newest `Start:` ran. It
  works in `~/Dev/<where>`, made first when that is a missing issue
  worktree, with an async run's model and effort, a $25 cap and no
  time limit, and ends with its step's comment as any
  session does; one that ends without it is handed to the lead, as a
  clerk that fails is. It refuses a `<where>` another issue holds: by
  its newest `Taken` while that issue is In Progress or In Review, by
  its session running there before that Taken is posted, or by the
  lead's `Held: <where>` for a walkthrough until `Released: <where>`.
  `lin local --list` shows each one running, with its minutes and its
  log.
- A comment with no PR to hold it stays on the issue in full: a
  `Start:`, a `Lead:` comment, an answer of Greg's
  given before a PR exists. A `Clerk:` comment does too, PR or none:
  the lead reads the issue.
- Never rebase or force-push a pushed branch: reviews hold its shas.
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
<!-- Workflow, shared: end -->

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
