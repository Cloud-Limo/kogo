# The check against Kotlin

`mise run kotlin` tests two sentences of `doc/design.md`, principle 3:
text valid in both languages means the same in both, and each of
Kogo's own forms is refused by kotlinc. It needs a JVM
and is not part of `go test ./...`.

- `builtins/*.kg` is the shared source: Kogo, and Kotlin unchanged
  (kotlinc is given the same bytes under a `.kt` name, `time.kt`,
  which names `java.time`'s types `kogo.time`'s, and `arrow.kt`,
  which gives Arrow's `Either` the names Kogo's prelude needs no
  import for; the task fetches Arrow's jar once). Each branch of
  a `when` is one case, calling prelude functions singly or in a short
  chain, and its value is a template of what must agree.
- `cases gen` writes the inputs, edges first and then random from a
  fixed seed, one case a line; `main.kt` and `driver/main.go` run each
  and print its result or its panic's message; `cases diff` prints each
  case that differs and exits 1 if any does.
- A difference is fixed in the prelude, or listed in `cases/main.go`'s
  `kept` with where `spec.md` states it: the cases it applies to and a
  rewrite of both results, which must then agree.
- The time cases keep to known zones and to dates inside `LocalDate`'s
  range, as `decisions.md` (DEV-933) says, until DEV-952 fixes the
  differences there and removes this line.

- `deviations/*.kg` are Kogo's deliberate deviations (spec 1), one
  file each. kotlinc must refuse every one with an error on the line
  its first line names, `// kotlinc refuses line N`, so no Kotlin
  habit applies to one unnoticed (principle 3). A new deviation adds
  a file.

To cover a function, add a branch: the generator reads the names from
the source. Everything written is under `kotlin/_work`.
