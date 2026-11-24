# Go 1.27's `uuid` package and `kogo.Uuid`

Proposed 25 Nov 2026, not decided, not built.

`go.mod` is on Go 1.27.4, whose standard library has a `uuid` package:
`New`, `NewV4`, `NewV7`, `Parse`, `MustParse`, `Nil`, `Max`, and on
`UUID` the methods `Compare`, `String`, `MarshalText`, `UnmarshalText`
and `AppendText`. `prelude/kogo/kogo.go` still writes `Uuid`'s
`String()` and the hex decoding of `Uuid_ParseOrNull` by hand.

## Proposal

Take the standard type, `type Uuid = uuid.UUID` or a conversion between
the two, in place of the hand-written `String()` and hex decoding.
Leave the id maker as it is. Size: a few lines fewer in
`prelude/kogo`, and no test should need to change. A test that does
change shows a change in what the language promises.

## Before changing

- **Parsing.** `uuid.Parse` also reads braced, `urn:uuid:` and
  32-digit forms. `Uuid.parseOrNull` reads only the 36-character
  8-4-4-4-12 form, in either case, as Kotlin's does (decisions.md,
  DEV-932). Keep the length and hyphen check in front of `uuid.Parse`.
- **Order.** `ownId` promises that the ids one process makes sort in
  the order it made them: RFC 9562's method 1, a 12-bit counter within
  each millisecond, and never earlier than the last id when the clock
  goes back. Nothing seen so far says `uuid.NewV7` promises that.
  Keep `NewId`, `ownId` and `MakeIdsWith` unless it does.

Either way, the outcome goes in `decisions.md`.
