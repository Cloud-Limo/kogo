# The check against Kotlin

`mise run kotlin` tests the subset of `doc/design.md`, principle 3, in
each direction. Where both languages accept a program, `builtins/`
checks that it means the same in both; where kotlinc refuses one,
`refused/` checks that `kogo check` refuses it too. `deviations/`
checks the one way out of the subset, Kogo's own forms, which kotlinc
must refuse. It needs a JVM and is not part of `go test ./...`.

- `builtins/*.kg` is the shared source: Kogo, and Kotlin unchanged
  (kotlinc is given the same bytes under a `.kt` name, `time.kt`,
  which names `java.time`'s types `kogo.time`'s, `kogo.kt`, which
  names Kotlin's `Uuid` Kogo's, `math.kt`, which names `kotlin.math`'s
  functions `kogo.math`'s, `round.kt`, which gives `roundToLong` the
  import kotlinc needs and Kogo does not, and `arrow.kt`,
  which gives Arrow's `Either` the names Kogo's prelude needs no
  import for; the task fetches Arrow's jar once). Each branch of
  a `when` is one case, calling prelude functions singly or in a short
  chain, and its value is a template of what must agree.
  `builtins/constructs.kg` does the same for spec 6's constructs:
  each case calls helpers whose `when`, loops and jumps the inputs steer.
- `cases gen` writes the inputs, edges first and then random from a
  fixed seed, one case a line; `main.kt` and `driver/main.go` run each
  and print its result or its panic's message; `cases diff` prints each
  case that differs and exits 1 if any does.
- A difference is fixed in the prelude, or listed in `cases/main.go`'s
  `kept` with where `spec.md` states it: the cases it applies to and a
  rewrite of both results, which must then agree; a rewrite that is a
  verb, as `%.10g`, rounds each number in them.
- An input outside Kogo's subset (principle 3) is listed in
  `cases/main.go`'s `refused`: the cases it applies to, the case that
  gives their input (`zoneId`), the rule an input outside the subset
  matches, and Kogo's limit message, which must be Kogo's whole result
  on such an input. It is counted as refused; the limit on any other
  input differs.

- `deviations/*.kg` are Kogo's deliberate deviations (spec 1), one
  file each. kotlinc must refuse every one with an error on the line
  its first line names, `// kotlinc refuses line N`, so no Kotlin
  habit applies to one unnoticed (principle 3). A new deviation adds
  a file.

- `refused/*.kg` are forms kotlinc refuses that `kogo check` once
  passed (spec 1), one file each, each its own package. Both must
  refuse it on the line its first line names, `// kotlinc refuses
  line N`, or, where the two place the fault apart, `// kotlinc
  refuses line N, kogo check line M`. A form found later adds a file.

To cover a function, add a branch: the generator reads the names from
the source. Everything written is under `kotlin/_work`.
