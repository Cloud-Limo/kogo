# What `kogo check` accepts of Kotlin's refused test files (DEV-1181)

Research scripts, kept for a later task to take up. They count which of
the diagnostic test files in JetBrains' `kotlin` repository, the ones
written to fail, `kogo check` accepts, and which of those kotlinc
still refuses. The answer is on DEV-1181.

The source is a shallow, sparse clone at tag `v2.4.0`, the compiler
Gradle bundles here, made outside any worktree and only read:

    git clone --depth 1 --branch v2.4.0 --filter=blob:none --sparse \
        https://github.com/JetBrains/kotlin.git /tmp/dev1181/kotlin
    git -C /tmp/dev1181/kotlin sparse-checkout set compiler/testData/diagnostics/tests

Then, with Postgres's `kogo` role and a database of its own (no Kotlin
file holds a `sql` literal, so it is never asked; `kogo_test` is left
to the tests):

    sudo -n -u postgres psql -c "create database kogo_dev1181 owner kogo"

    research/dev-1181/run.sh /tmp/dev1181/kotlin/compiler/testData/diagnostics/tests /tmp/dev1181/w-tests

- `collect.py` skips a file whose directives change the language, the
  compiler's options or the target, several modules, `FILE:` parts that
  are not all `.kt`, a script, a file with no marker, and a file whose
  `RUN_PIPELINE_TILL` is not `FRONTEND` (its markers are warnings only).
  It strips each `<!NAME!>`/`<!>` marker, adds `package kt` to a part
  with no package line, and writes each kept file as a program of its
  own. In 2.4.0 these files are K2's alone: no `.fir.kt`, no
  `FIR_IDENTICAL`.
- `check.sh` runs `kogo check` on each, in parallel.
- `reduce.py` drops, from each refused program, the statement or
  declaration each error lies in (by indentation), until it passes or
  no line that carried a marker is left: a Kotlin test file is mostly
  refused for its first class, which hides the rest.
- `kotlinc.sh` compiles what passed, whole or reduced, with the
  Gradle-bundled kotlinc; a file counts only when kotlinc refuses it.
- `summary.py` prints the counts and each confirmed file with
  kotlinc's first error. Grouping them into forms is by hand.

Other sources run the same way: `compiler/testData/diagnostics/testsWithStdLib`
and `testsWithAnyBackend`, `compiler/fir/analysis-tests/testData/resolve`
and `resolveWithStdlib`, `compiler/tests-spec/testData/diagnostics` (whose
files have no `RUN_PIPELINE_TILL`: their header's `(NEGATIVE)` says an
error is expected). `ANY_DIRECTIVE=1` keeps a file whatever its language,
option or target directives say: kotlinc's own verdict, with no flags,
still decides each one.

`forms/` holds the smallest program of each form found, each a program
of its own (`kogo check research/dev-1181/forms/<one dir with it>`):
`kogo check` passes each but `n13`, which makes it panic, and kotlinc
refuses each.
