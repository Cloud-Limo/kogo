# Packages and names: the design

DEV-1068, ruled by Greg on 22 Nov 2026. Build from this.
`packages-and-names.md` beside it is the record of how it was reached:
its rules, its survey of cusoon2 and its probes hold, and where its
proposals differ from this file, this file wins. Sizes are code / tests
/ spec lines, and are estimates. Spec wording here is a draft for the
issue that lands it.

## 1. A package has one table of names

15 / 6 / 5. Spec 2:

> In a package, a type, a typealias, a marker and a top-level `val`
> each take a name no other of them has, in any file, as in Kotlin:
> `private` limits who may name a declaration, not where its name is
> taken. Two `private val`s in two files do not meet, as two `private
> fun`s do not (5.2). Funs share names (5.2), and a fun may be named
> for a type or a val.

```kotlin
// a second file of the package
private data class Area(val code: String)   // error: the package declares Area
private val limit: Int = 50                 // fine: this file's own
```

Held by the checker's `declare`. New: the two `private val`s, which
are `duplicate` today. A `private val` beside one that is not private
stays an error, as in Kotlin. The emitter already numbers private
names by file.

## 2. One import a name, and the name is written bare

0 / 2 / 6. Spec 2:

> A name is written as it is declared or imported, never with its
> package: `cusoon.model.Booking` in code is an error. An import names
> a top-level name and brings every declaration of it; a nested type
> comes with its outer one, `import shop.Outcome` and then
> `Outcome.Ok`. An extension is imported by its own name.

The checker does all of this today; the spec gains the words.

## 3. `import ... as`

45 / 30 / 12. Spec 2, in place of "no `import ... as`":

> `import cusoon.schema.Area as AreaRow` gives the declaration another
> name in this file, as in Kotlin: the file writes `AreaRow`, also for
> its constructor, constants and variants. No name comes into a file
> twice: one name imported from two packages is an error at the
> second, two funs included, though Kotlin overloads those. Give one
> `as`.

```kotlin
package cusoon.rpc.storefront

import cusoon.schema.Area as AreaRow

private fun rpcArea(a: AreaRow): Area =
    Area(a.id, a.label, a.mapLatitude.toDouble(), a.mapLongitude.toDouble(), a.countryCode)
```

Any import may take an `as`, a `kogo.` one included: `import
kogo.Either as KogoEither`. The alias is the file's alone. `kogo emit`
and `kogo ts` write the declared name and read no import. Left to the
build: whether an `api` list may name a fun by its alias; the name on
the wire is the declared one either way (15.1).

## 4. An import does not hide the package's own

8 / 4 / 3. Spec 2:

> An import does not name what the file's own package declares as a
> type, a typealias or a top-level `val`, though in Kotlin the import
> wins: give it another name with `as`, or rename one. A fun of that
> name is one more candidate (5.2).

What it stops, which passes `kogo check` today, and `kogo ts` then
writes the row's columns for the browser as `Area`:

```kotlin
// wire.kg
package shop.front

data class Area(val id: Int, val label: String)
```

```kotlin
// call.kg
package shop.front

import shop.rows.Area          // the row: id, label, costCents

fun myArea(tx: Tx, env: Env, request: Request, input: Unit): Either<String, Area> = either {
    storedArea()
}
```

An import of a name from the file's own package (`import q.b.P` inside
`q.b`) is accepted today and does nothing; the issue says whether this
rule takes it too.

## 5. Only a sealed interface holds types

8 / 4 / 4. Spec 3.2: "Only a sealed interface holds types, and each
implements it." Spec 3.4: "An interface holds no types: declare each
at the top level."

```kotlin
interface GoogleMaps {
    data class Place(val id: String)     // error: declare it at the top level
}

sealed interface Outcome {
    data class Ok(val id: Int) : Outcome
    data class Note(val text: String)    // error: not a variant of Outcome
}
```

The checker and the emitter take both today, and `kogo ts` writes the
first as `Place`. A nested typealias, parsed and dropped today, is
refused with them.

## 6. `kogo` is the prelude's

4 / 2 / 2. Spec 2:

> A program's package is not `kogo` or under it. A package's own type
> or val is meant before one of package `kogo` or a built-in type, as
> in Kotlin.

The first sentence is new: today a program's `package kogo` puts its
names in scope in every file, and its `package kogo.time` has them
imported as the prelude's. The second is what the checker does, now
stated: a package may declare its own `Either`, and a file that wants
both writes `import kogo.Either as KogoEither`. A fun is different, as
today: a program's fun that a call fits as well as the prelude's is
`ambiguous-call` (5.2).

## 7. Stated, with no code

0 / 0 / 6, and one audit issue that holds both until a file meets one.

- Spec 2: "Two names of a package that differ only in the case of the
  first letter may be one name in Go; `kogo emit` refuses them."
  Kotlin accepts them. The audit holds moving the refusal into `kogo
  check`, 15 / 4, since `kogo check` is to be the only gate.
- Spec 3.5, in place of "transparent, as in Kotlin": "An alias stands
  where a type is written; a value is built, and a constant or a
  variant named, by the type's own name." Kotlin takes a constructor
  and a constant through an alias. The audit holds following it there,
  not sized.
- Spec 6: "A local hides a name declared outside it, an outer block's
  local and a type's name included, as in Kotlin."

## 8. Five messages that say what to change

25 / 10 / 0, one issue, no change to the language.

- `q.a.P` in code: "import q.a.P and write P".
- `import q.a.O.Ok`: "import q.a.O and write O.Ok".
- An extension not imported: "add import q.a.show".
- Two types of one name in a mismatch, "this is a Area where a Area is
  wanted": each with its package.
- A constructor through an alias, "PRow is not in scope: add import
  q.a.PRow": the type's own name.

## Rows for spec 14

| Kotlin habit | In Kogo |
| --- | --- |
| two funs of one name imported from two packages | `import ... as` on one |
| an import of a name the file's own package declares | `import ... as`, or rename one |
| `a.b.Name` written in code | import it and write `Name` |
| `import a.b.Outer.Inner` | `import a.b.Outer`, and write `Outer.Inner` |
| a constructor or a constant through a typealias, `PRow(1)` | the type's own name |
| a type nested in an interface as a namespace, `GoogleMaps.Place` | a top-level type, `GoogleMapsPlace` |

## Not taken

A package written in code; renaming as the only answer to two types of
one name; `as` only where a name is taken; types nested in a plain
interface; refusing a program's own declaration of a name the prelude
or the built-in types have; one table that also holds two `private
val`s of two files.

About 105 / 58 / 44 in all.
