# The check against Kotlin

`mise run kotlin` tests two sentences of `doc/design.md`, principle 3:
text valid in both languages means the same in both, and each of
Kogo's own forms is refused by kotlinc. It needs a JVM
and is not part of `go test ./...`.

- `builtins/*.kg` is the shared source: Kogo, and Kotlin unchanged
  (kotlinc is given the same bytes under a `.kt` name, `time.kt`,
  which names `java.time`'s types `kogo.time`'s, `kogo.kt`, which
  names Kotlin's `Uuid` Kogo's, and `arrow.kt`,
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
  rewrite of both results, which must then agree.
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

- `builtins/hunt.kg` is DEV-1143's hunt, a sketch: combinations a port
  leans on (the order of effects, shadowing, a lambda's `var`, data
  classes' text and `==`, chains of `?.` and `?:`, `Either` and raise
  scopes, loops with jumps). Its last five cases differ and are marked;
  while they do, `mise run kotlin` prints them and exits 1. `math.kt`
  names `abs` for kotlinc, which a Kotlin file imports and a Kogo file
  does not, and `arrow.kt` the forms of a raise scope.
- `accepted/*.kg` are what DEV-1143 found `kogo check` to pass and
  kotlinc to refuse, on the line each file's first line names, that is
  no deviation of spec 1. The task does not read them: each is to become
  a refusal of the checker's, with its test there, or a deviation.

To cover a function, add a branch: the generator reads the names from
the source. Everything written is under `kotlin/_work`.
