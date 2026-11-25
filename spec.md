# Kogo v0 specification

Draft of 2026-11-15, fitted to cusoon2. Kogo is a small language in Kotlin's
syntax that compiles to Go. This document is the whole language: a construct
that is not described here does not exist, even when Kotlin has it. Section 14
lists the Kotlin habits that are most often reached for by mistake, each with
its replacement.

Source files end in `.kg`.

## 1. The one rule

Where a construct is valid in both Kotlin and Kogo it means the same thing.
A Kogo file need not compile as Kotlin. A Kogo file imports only from
its own application and from `kogo.` packages, whose names keep
Kotlin's, `java.time`'s and Arrow's spelling and meaning, but for the
corners section 12 lists: `kogo.time.Instant` is `java.time.Instant`,
`kogo.test.assertEquals` is `kotlin.test.assertEquals`. Kogo has four kinds of
deliberate deviation from Kotlin, its own forms, and kotlinc refuses
each (`mise run kotlin` holds this). The one exception is a binding
file of bare `external fun`s, which kotlinc takes for JNI functions;
only bindings write `external` (section 13), so no author meets it:

- the `sql"""..."""` literal (section 10);
- the `external` binding declarations (section 13);
- the lists, `api` (section 15) and `tests` (section 16);
- `action fun`, a function that changes something outside (7.1).

Beside them, what kotlinc refuses Kogo refuses, whatever Go would make
of it: `kotlin/refused/` holds a file of each such form that `kogo
check` once passed, and `mise run kotlin` checks that both refuse each.

## 2. Files, packages, visibility

```kotlin
package cusoon.model

import cusoon.pricing.Cents
import kogo.time.Instant
```

- One `package` line, then imports, then declarations. No wildcard imports.
  A package is the files that share a package line, wherever they lie. An
  import names one declaration of another package; a name of the file's
  own package needs none.
- In a package, a type, a typealias, a marker and a top-level `val` each
  take a name no other of them has, in any file, as in Kotlin: `private`
  limits who may name a declaration, not where its name is taken. Two
  `private val`s in two files do not meet, as two `private fun`s do not
  (5.2). Funs share names (5.2), and a fun may be named for a type or a
  val. Two names of a package that differ only in the case of the first
  letter may be one name in Go; `kogo emit` refuses them. `kogo check`
  refuses an interface's `fun string()` or `fun String()` with no
  parameter, which would be Go's `String` method, so a template would
  write what it returns instead of its data class's text.
- A name is written as it is declared or imported, never with its
  package: `cusoon.model.Booking` in code is an error. An import names a
  top-level name and brings every declaration of it; a nested type comes
  with its outer one, `import shop.Outcome` and then `Outcome.Ok`. An
  extension is imported by its own name.
- `import cusoon.schema.Area as AreaRow` gives the declaration another
  name in this file, as in Kotlin: the file writes `AreaRow`, also for its
  constructor, constants and variants. Any import may take one, a `kogo.`
  one included. The name is the file's alone: `kogo emit` and `kogo ts`
  write the declared one, and an `api` list names a fun by that (15.1). No
  name comes into a file twice: one name imported from two packages is an
  error at the second, two funs included, though Kotlin overloads those.
  Give one `as`.
- An import does not name what the file's own package declares as a
  type, a typealias or a top-level `val`, though in Kotlin the import
  wins: give it another name with `as`, or rename one. A fun of that name
  is one more candidate (5.2).
- Packages do not import each other, directly or through others, though
  Kotlin allows it.
- A file holds any number of top-level declarations: functions, `val`
  constants, types, typealiases, marker annotations, `api` lists
  (section 15) and, in a test file, `tests` lists (section 16).
- `private` means this file. `internal` means this package. No modifier
  means public. There is no `protected`.
- Comments: `//`, `/* */` and `/** */`. KDoc is kept as a comment.
- The declarations of package `kogo` (section 12, and the functions of
  sections 8 and 9) are in scope in every file. The prelude's other
  packages, as `kogo.time`, are imported by name. A program's package is
  not `kogo` or under it. A package's own type or val is meant before one
  of package `kogo` or a built-in type, as in Kotlin.
- A file named `_test.kg` is a test file (section 16). It is in its
  package as any file is, but only a test file may name what one
  declares, or import from `kogo.test`.

## 3. Types

| Type | Meaning |
| --- | --- |
| `Boolean` | true or false |
| `Int` | 32-bit signed, wraps on overflow |
| `Long` | 64-bit signed, wraps on overflow |
| `Double` | IEEE 754 binary64 |
| `String` | immutable UTF-16 text, as in Kotlin |
| `Char` | one UTF-16 code unit |
| `Unit` | the single value `Unit` |
| `Nothing` | no value; the type of `raise`, `error` and `return` |
| `Uuid`, `Instant`, `LocalDate`, `LocalTime`, `ZoneId`, `ZonedDateTime`, `Duration`, `Decimal` | prelude types, section 12 |
| `List<T>`, `Set<T>`, `Map<K, V>`, `Pair<A, B>` | immutable collections, section 12 |
| `ByteArray` | bytes that are carried and never read: below |
| `T?` | `T` or `null` |
| `(A, B) -> R` | function type |
| `Either<E, A>` | a failure `E` or a value `A`, section 9 |

Numeric literals: `1` is `Int`, `1L` is `Long`, `1.0` is `Double`,
`10_000` allowed; there are no hex, binary or exponent forms. There are no implicit numeric conversions; write
`.toLong()`, `.toInt()`, `.toDouble()`. `Int / Int` and `Long / Long`
truncate toward zero. `Long * Int` and the other mixed `Int`/`Long`
operators give `Long`, as in Kotlin, and `< <= > >=` compare an `Int`
with a `Long`; `==` between them is an error, as in Kotlin. A
whole-number literal is a `Long` where a `Long` is expected (a declared
type, a parameter, a return): `val total: Cents = 0`. Everywhere else it
is an `Int`, so `cents == 0L`. A whole-number literal must fit the type
it is read as: `3000000000` read as an `Int` is an error, though Kotlin
reads it as a `Long`; write `3000000000L`. A minus is taken with its
literal, so `-2147483648` is an `Int`. Digits past 9223372036854775807
are an error, as in Kotlin, even after a minus: `-9223372036854775808L`
is refused. `Int` or `Long` mixed with `Double` is an
error in arithmetic and in comparison, though Kotlin allows it: write
`.toDouble()`, so that the place where a whole number becomes a
floating-point one is visible.

Equality `==` is structural for every type. There is no `===`. One of
its two sides must fit where the other's type is wanted (section 5.1),
`?` aside, so `x == null` passes and `Int == Long` and `String == Int`
are errors.
A handle of the prelude's (`Db`, `Tx`, `SerializableTx`, `Http`,
`Console`, `Clock`, `Request`, `Test`, `Raise`) and a `Regex` are the
exception: Kotlin compares each by identity, so `==` on one, or on a
value whose type holds one, is an error where check sees it
(`handle-equality`, `regex-equality`), as for a `ByteArray` below, and
panics where it does not (section 14).

A value that holds a function is not compared, though Kotlin compares
functions by reference. `==` and `!=` are an error where either side's
type holds one: a function type, a data class with a property whose type
holds one, a sealed interface with such a variant, and a `T?` or a type
argument of any of these, as `List<Rule>`, and an interface that
declares a function `val`; so is a `when` branch that compares its
subject with such a value. Comparing with `null` is allowed. Where the
type does not show the function, behind a type parameter or an
interface, the comparison panics once it reaches the function, and
answers `false` where a part before it differs, `contains`, `distinct`
and a `Set`'s or `Map`'s keys included. In a template or `toString()` a
value whose type holds one, through the same types, is an error
(`function-text`):
Kotlin writes a function as its class and an identity that changes
every run, so no program can rest on its text.

A `ByteArray` is bytes, as a `bytea` column holds them (10.4) and a
binding makes or takes them. Kogo code names the type, reads a value
from a row, binds it into a statement, passes it, holds it in a data
class and hands it to a binding, and nothing else: it has no index, no
`size`, no `set` and no constructor, so it is the one array Kogo has
and it cannot change. `==` and `!=` are an error (`bytes-equality`)
where either side's type is or holds one, as for a function above and
through the same types, since Kotlin compares a `ByteArray` by identity
and a data class by its properties': compare the other properties, or
what the bytes were made from. Comparing with `null` is allowed. Where
check cannot see one compared, behind a type parameter or in
`contains`, `in`, `distinct` or a `Set`'s or `Map`'s keys, `==`
compares it as Kotlin does, by identity: an array is equal to itself
and to no other of the same bytes. Two of no bytes are equal, where
Kotlin's two made apart are not. In a template it is an error
(`bytes-text`): Kotlin writes its identity, `[B@` and a number that
changes every run. Inside a data class's text it is written as Kotlin's
`toString` writes it, its bytes signed: `F(name=x, data=[97, 0, -1])`.
It is in no operation's input or answer (15.4).

A `String` counts as Kotlin's does: `length`, `substring`, `take`,
`padStart` and reading it as `Char`s count UTF-16 units, so
`"😀".length` is 2. It never holds half of a surrogate pair, though
Kotlin's can: an operation that would leave one, as
`"😀".substring(0, 1)` or `"😀".take(1)`, leaves U+FFFD in its place,
which also counts as one unit.

### 3.1 Data classes

```kotlin
data class Discount(val description: String, val amount: Cents, val struck: Boolean = false)
```

- Every property is a `val` in the primary constructor. There is no
  `init` and no secondary constructor. The body, when there is one, holds
  only the `override fun`s of the interfaces it implements; an interface's
  `val` is implemented in the constructor (`override val label: String`).
- Default values and named arguments work as in Kotlin.
- `copy(...)`, `==`, `toString()` and destructuring (`val (a, b) = pair`)
  are provided. `copy` builds a value as the constructor does, so it may
  be called only where the constructor may.
- A data class may implement interfaces: `data class Ok<T>(val value: T) : Outcome<T>`.
- A constructor may carry a visibility or a marker (section 11):
  `data class Held internal constructor(val id: ID)`.

### 3.2 Sealed interfaces

```kotlin
sealed interface Outcome<out T> {
    data class Ok<T>(val value: T) : Outcome<T>
    data class Refused(val message: String) : Outcome<Nothing>
}
```

The variants are data classes or `data object`s nested in the body or
declared in the same file. Only a sealed interface holds types, and each
implements it. A nested one is named in full everywhere,
`Outcome.Ok`, also inside the body, where Kotlin allows `Ok`. `out` on a
type parameter of a sealed interface
is the only variance annotation in the language, a binding's external
class aside (section 13).

An `out` type parameter is only given back, as in Kotlin: it may be the
type of a `val` or of what a `fun` returns, never of a `fun`'s parameter,
where a `Box<Int>` used as a `Box<Int?>` could be given `null`. Inside a
type, positions turn as in Kotlin: `fun each(f: (T) -> Unit)` gives a
`T` back, and `Map<T, Int>`, whose key is not `out`, both takes and gives
one, so it is refused anywhere. Write a fun that takes one as an
extension: `fun <T> Box<T>.put(value: T): Box<T>`.

### 3.3 Enums

```kotlin
enum class InviteType { InHouse, Outside }
```

Constants only: no constructor, no body. `.name` gives the constant's
name, `InviteType.entries` the list of constants and
`InviteType.valueOf(name)` the constant of that name; another name
panics, as Kotlin's throws, with Kotlin's message, `No enum constant
shop.InviteType.Other`.

### 3.4 Interfaces

```kotlin
interface Regime<K> {
    val label: String
    fun card(key: K): Card
}
```

An interface declares abstract `val`s and `fun`s. No default bodies. An
interface's `fun` takes no type parameters and has no default values,
though Kotlin allows both: make it a top-level fun that takes the value,
or state the argument at each call. Only
data classes and `data object`s implement interfaces. A `data object` has no
constructor to implement a `val` in, so it implements only interfaces
that declare `fun`s alone. An interface holds no types: declare each at
the top level. There are no other
classes, no inheritance, no `object`, no `companion object`, no anonymous
`object :` expressions. Use a lambda where Kotlin would use an anonymous
object.

### 3.5 Typealias

`typealias Cents = Long`: an alias stands where a type is written; a
value is built, and a constant or a variant named, by the type's own
name.

## 4. Absence

There is one absent value, `null`, and it only inhabits `T?`.

- `x?.f()`, `x ?: y`, `x ?: return y`, `x ?: raise(e)`.
- A test narrows a name (a smart cast): where `x != null` is known to
  hold, `x` of type `T?` is a `T`, and where `x is V` is, `x` of a sealed
  interface type is its variant `V`. Only a name narrows: a local `val`
  or a parameter, a lambda's included. A `var` never narrows, and nor
  does a path such as `booking.price`, though Kotlin narrows some: bind
  it to a `val` first.
- The tests are `x != null`, `x == null` and `x is T`, joined by `&&`,
  `||` and `!`, and the `is` and `null` branches of `when (x)`. In
  `a && b`, `b` is read knowing `a` holds; in `a || b`, knowing it does
  not.
- A narrowing holds in the branch where the test's outcome is known.
  Two statements narrow for the rest of the block: an `if` whose other
  branch cannot finish, and `x ?: return y`. After
  `if (x == null) return y`, or the statement `x ?: return y`, `x` is a
  `T`. Nothing used as a value narrows past itself, though Kotlin's
  does: after `val p = x ?: return y` it is `p` that is a `T`, and `x`
  is still a `T?`. Inside `x?.let { }` the lambda's parameter is a `T`.
- `require`, `check` and `ensure` do not narrow, though Kotlin's do:
  write `val y = requireNotNull(x) { "why" }`, or `ensureNotNull`.
- `requireNotNull(x) { "message" }` and `checkNotNull(x) { "message" }`
  give a `T` or panic (section 8).
- `!!` does not exist.

## 5. Functions

```kotlin
fun shareOf(cents: Cents, percent: Int): Cents = cents * percent / 100

fun Booking.supplierPay(extras: List<BookingExtraCharge>): Cents? { ... }

val InvoiceRow.owingCents: Cents
    get() = totalCents - amountPaidCents - amountCreditedCents

fun <T> firstOr(items: List<T>, fallback: T): T = items.firstOrNull() ?: fallback
```

- Top-level functions, extension functions and extension `val`s with a
  getter. Local functions are allowed inside a function body, without
  type parameters, though Kotlin allows them: declare a generic one at
  the top level. An
  extension's receiver is not nullable, a binding's included (section
  13): `fun String?.f()` does not exist; take the value as a parameter.
- Every top-level function states its parameter types and its return type.
  The return type may be omitted only for `Unit`. Inference works inside a
  function body only, so a top-level `val` states its type too, but
  for a test, whose list states it (16.1).
- Default arguments and named arguments as in Kotlin. No `vararg`.
- Generic functions take plain type parameters: no bounds, no `where`, no
  `reified`, no `inline`.
- Functions, and extension `val`s, may share a name; section 5.2 says
  how a call chooses among them.
- No `operator`, `infix`, `tailrec` or `suspend`.
- Lambdas: `{ a, b -> ... }` and `it`. A parameter may state its type,
  `{ e: StoreError -> ... }`, and a pair or a data class may be
  destructured, `{ (d, w) -> d * w }`. A lambda is a value of a function
  type. Trailing-lambda call syntax as in Kotlin. Function references
  `::name` to top-level functions are allowed.
- Lambdas with receivers (`T.() -> R`) do not exist. A function type
  with a receiver is not written: take the value as a parameter, or
  return an `Either`.

### 5.1 Type arguments

A call to a generic function may write its type arguments, all of them:
`emptyList<Int>()`. Where it writes none, each type parameter is settled
by the first of these that mentions it:

1. the receiver: `xs` in `xs.map { ... }`;
2. the arguments that are not lambdas; when several mention it, their
   common type;
3. the type the result must have: a declared type, a parameter's type,
   the function's return type; for a call with no lambda argument on
   the right of `?:` where none of these is known, the type of the left
   made non-null, so that `xs ?: emptyList()` is a `List<Long>` for
   `xs: List<Long>?` (a call with a lambda argument there is settled by
   its lambdas, and `?:` takes the common type of its two sides);
4. what a lambda argument returns, lambdas taken left to right.

Every argument and the result are then checked against what was
settled. Nothing is settled twice and there is no search. A lambda's
parameter types must be settled before its body is read; where they are
not, state them: `{ e: StoreError -> ... }`. A type parameter that
nothing settles is an error: write the type arguments.

The common type of two types is the wider one when one of them fits
where the other is wanted (below): the type itself for two the same,
`T?` for `T` and `null`, the other for `Nothing`. Two variants have
their sealed interface. No other two types have one, since there is no
`Any`. The branches of an `if` or a `when` used as a value, and
the two sides of `?:`, need a common type in the same way, unless the
type the result must have is known, when each is checked against that.

A value fits where its own type or a wider one is wanted: `T` fits `T?`,
a variant fits its sealed interface, a data class fits an interface it
implements, `Nothing` fits anything, and for an `out` type parameter
`Outcome<A>` fits `Outcome<B>` when `A` fits `B`.

Kotlin infers more. These are errors in Kogo, each with its fix:

| Kotlin accepts | In Kogo |
| --- | --- |
| `val r = either { ... }`, nothing stating its type | `either<String, Int> { ... }` |
| `listOf(1, "a")`, a `List<Any>` | no such list; there is no `Any` |
| a lambda parameter typed by a later argument | `{ e: StoreError -> ... }` |
| `val xs: List<Long> = listOf(1, 2)`: the arguments settle `T` first, and there a literal is an `Int` | `listOf<Long>(1, 2)`, or `1L` |

A type parameter may be settled as a nullable type, and then a bare
`T` may be null: `?:`, `?.`, `== null` and `when` test it for null as
Kotlin does, so `fun <T> orElse(v: T, fallback: T): T = v ?: fallback`
gives the fallback for a null `v`. Kotlin's `T?` of a nullable `T` holds
one null where Go would hold two, so a nullable type is refused for a
type parameter that its function or type makes a `T?` of: in its
signature, or anywhere in its body: a `T?` it writes (`val r: T? = v`,
`if (c) v else null`, a type whose field is `T?`), or a call that gives
one, as `firstOrNull`, `lastOrNull`, `getOrNull` and `takeIf` do, or a
generic function that does so in turn. `firstOr` above, called on a
`List<Long?>` `holes`, is refused by `kogo emit` (`not-yet-emitted`):
pass a type that is not nullable and keep the null outside,
`firstOr(holes.mapNotNull { it }, 5L)`. A
function that only carries its `T`, as one that runs a block and returns
its answer, takes a nullable type.

### 5.2 Overloads

A call keeps the candidates of its name whose receiver and arguments
that are not lambdas fit: their count, their names and their types. A
lambda fits any parameter of a function type and takes no part in the
choice. Exactly one candidate must be left. None left is an error that
lists the candidates; several left is an error that names them and says
to rename one. Nothing is ranked as more specific, so `f(x: Int)`
beside `f(x: Int?)` cannot be called with an `Int`, though Kotlin picks
the first.

Two `fun`s, or two extension `val`s, of one name in one package whose
receiver and parameter types are the same are an error at the second,
since no call could choose between them. So are two `fun`s of one name
in an interface, and a `fun` named for a data class with its
constructor's parameter types, reported at the `fun`. Parameter names,
results, and the names and order of type parameters do not tell them
apart; two `private` ones in different files do not meet. Nor does a
`?` on a type parameter, so `<A> f(a: A?)` beside `<B> f(b: B)` is an
error. Within one file, nor do type arguments, a `?` but on a number,
a `Boolean` or a `Char`, or the types inside a function type, unless
the results differ the same way: the JVM holds two that differ only so
as one signature, and kotlinc refuses them, so `f(xs: List<Int>): Int`
beside `f(xs: List<Long>): Int` in one file is an error in a program,
and in two files is not. Bindings with
a receiver are one group across packages, whatever their visibility, as
they are in scope everywhere.

One exception: when the candidates left differ only in what one lambda
parameter returns, as the prelude's `sumOf` does for `Int`, `Long` and
`Double`, the lambda is read once and what it returns chooses. That is
the prelude's alone, as kotlinc chooses so only in its library: the
program's own pair is ambiguous at such a call.

Where the receiver, the count and the names leave one candidate, each
argument is read as that candidate's parameter wants it, so a whole-number literal
is a `Long` for a `Long` parameter. Where they leave several, each
argument is read on its own first. A name in a body is a local before
it is anything declared outside, and a value's own property or
interface `fun` before an extension of the same name.

## 6. Statements and expressions

- `val` everywhere. `var` only for locals inside a function body.
  Top-level `val` and `const val` hold constants; there is no top-level
  `var` and no other global state.
- A local hides a name declared outside it, an outer block's local and a
  type's name included, as in Kotlin. It hides a function of its name
  too, where Kotlin calls the function past a local that cannot be
  called: with a local `job` that holds an `Int`, `job()` is
  `not-callable`. Name the local otherwise.
- A top-level `val`'s value may not refer to the `val` itself, or to a
  `val` declared below it in its file, directly or through the `val`s
  and functions it refers to. A reference counts wherever it is
  written, in a lambda or a local function too, whether or not it
  runs. A call of an interface's function on a data class or a data
  object refers to that one's function; a call on a value of the
  interface's type refers to none. Kotlin refuses a direct read of a
  `val` below, and through a function reads it before it holds its
  value: declare the one it reads above it.
- Nor may two files of a package use each other through their `val`s,
  directly or through other files. A file's `val`s use another file
  where their values refer, as above, to a top-level function or `val`
  declared in it. Kotlin sets up a file's `val`s when the file is first
  used, so what such a `val` holds depends on which file a program uses
  first; Go sets them up in one order. Move the `val`s into one file, or
  make one of them a function.
- `if`/`else` and `when` are expressions.
- `when (x) { ... }` over a sealed interface, an enum or a `Boolean` must
  cover every case or have `else`; the compiler rejects a missing case.
  `when { cond -> ... else -> ... }` requires `else`. Over any other type
  `when (x)` needs `else` where it is used as a value. Branch conditions:
  constants, `is Type`, `in range`, comma-separated alternatives.
- `is` checks and smart casts work on sealed interfaces.
- Loops: `for (x in list)`, `for (i in 0 until n)`, `for ((k, v) in map)`,
  `while`. `break` and `continue` without labels.
- `return` leaves the enclosing `fun`. Labelled returns (`return@name`) do
  not exist; restructure with `if`/`else` or a local function. A lambda
  holds no `return`, though Kotlin allows one in a lambda it inlines:
  the lambda's last expression is its value, and the `fun` returns
  after the call that runs it.
- Ranges: `a..b`, `a until b`, `x in a..b`, between two `Int`s or two
  `Long`s. A range is written only after `in`: in a test, a `for` and a
  `when` condition. It is not a value and has no type.
- String templates `"$name"` and `"${expr}"`. Raw strings `"""..."""`.
- Operators: `+ - * / %`, `== !=`, `< <= > >=`, `&& || !`, `+=` and
  friends on a local `var`. On numbers they are section 3's. On any
  other type an operator is a binding (section 13), and the prelude says
  which types have it: `a + b` is `plus`, and so `minus`, `times`, `div`
  and `rem`; `a < b` and the other three are `compareTo`; `x in c` is
  `c.contains(x)`; `c[i]` is `c.get(i)`. Only a binding counts:
  application code defines no operator.
- Scope functions: `let`, `also` and `takeIf` only. `apply`, `run`, `with`
  and `takeUnless` do not exist.
- `to` builds a `Pair`: `a to b`, the binding `to`.
- A statement that is only a value is an error, where Kotlin warns: a
  name, a literal, a member read, a lambda, or an operator's result that
  nothing uses. A call, an assignment, `return` and the like, and
  `x ?: return y` are statements. So `- b` on a line of its own, which
  section 6.1 reads as a new statement, is refused.
- `return`, `break` and `continue` stand in a value's place only where
  that value goes straight into a `val`, an assignment with `=` or a
  `return`, or is the result of the function: the right side
  of `?:`, or a branch of an `if` or a `when` there, as
  `val p = x ?: return y`. Buried in a larger expression, as
  `f(x ?: return y)` or `n += x ?: break`, one is an error, though
  Kotlin allows it: bind the value to a `val` first.

### 6.1 Lines, operators and literals

Where Kotlin's own parser and its published grammar differ, Kogo follows
the parser.

- A statement ends at the end of its line or at `;`. It goes on to the
  next line when that line begins with `.`, `?.`, `?:`, `&&`, `||` or
  `else`, when its own line ends with an operator or a comma, and
  anywhere inside `( )` or `[ ]`. Inside `{ }` (a block or a lambda)
  lines end statements again, even within parentheses. So `a` and `- b`
  on two lines are two statements, and a call's `(`, `[`, `<` or trailing
  lambda starts on the line of what it applies to.
- Precedence, tightest first, as in Kotlin: postfix (`.`, `?.`, a call,
  `[i]`); prefix `-` and `!`; `* / %`; `+ -`; `..`; `until` and `to`;
  `?:`; `in` and `is`; `< <= > >=`; `== !=`; `&&`; `||`. Binary
  operators group to the left; comparisons do not chain (`a < b < c` is
  an error). Unary minus is an operator, so `-1` is `-` applied to `1`.
  There is no unary `+`, `++`, `--`, `!in`, `!is`, `as` or `===`.
- After a name, `<` opens type arguments when what follows reads as
  types closed by `>` and then `(` or `{`, as in `tx.query<Row>(...)`
  and `either<E, A> { }`; otherwise it is less than. As in Kotlin,
  `a < b > (c)` is therefore a call.
- A `{` after a call or a name on the same line is a trailing lambda;
  after the `)` of `if`, `for`, `while` or `when (...)`, or after `else`
  or `->`, it opens a block.
- `"..."` strings and `Char` literals (`'a'`) have Kotlin's escapes:
  `\t \b \n \r \' \" \\ \$` and `\uXXXX`. A raw string has none.
  A surrogate is written as a pair, `"\uD83D\uDE00"`, which only a string
  holds; half of one is refused.
  A `$` that does not begin a template is text. Templates nest:
  `"${if (a) "x" else "y"}"`.
- A list (arguments, parameters, type arguments, `when` conditions) may
  end with a comma. `;` also separates `when` branches. Block comments
  nest.

## 7. Effects

A function can only do what its parameters let it do.

- There are no globals, singletons or ambient state. The clock, the
  database and outbound calls arrive as parameters. `newId()` and
  `randomString(n)` are the two exceptions, because neither's value
  changes what a program does.
- `Tx` is an open database transaction and `Db` is a connection pool
  (section 10). A function that touches the database has one of them,
  or a value that holds a `Tx` (15.7), as a parameter.
- `Clock` gives the time: `clock.now(): Instant`.
- `newId()` gives an id that no other call gives. Kogo's own sort by
  when they were made and increase within a process. An application's
  `main` may hand the runtime a maker of its own, in Go, before any id
  is made (`kogo.MakeIdsWith`); keeping that promise, and that order,
  is then its maker's.

### 7.1 Actions

A transaction is all or nothing and may run again; a call that
changes something outside is neither. So the call says what it is,
and so does every fun that makes it:

```kotlin
action fun announce(console: Console, message: String) {
    console.println(message)        // println is an action of the prelude's
}
```

- A binding that changes something outside says `action` before
  `external` (section 13). Any other binding says nothing and may be
  called anywhere, so a lookup is allowed anywhere; nothing checks
  that a binding is right to say nothing.
- A fun that calls an action, a binding or an `action fun`, is an
  `action fun`; one that is not is a compile error
  (`unmarked-action`), and so is the call in a val's value or getter.
  An `action fun` that calls no action is one too (`idle-action`).
- An `action fun` takes no `Tx`, as a parameter or a receiver
  (`action-tx`): it takes a `Db` and acts between its transactions.
  It calls no action in a block that is handed a `Tx`, as `db.tx`'s
  is (`action-in-tx`).
- An `action fun` that is a row's gated work is handed what its gate
  answered after the gate's transaction has ended (15.3). So that
  gate answers nothing that holds a transaction (`action-gate`,
  15.7).
- An action is not kept in a value (`action-kept`): there is no `::`
  reference to an `action fun`, no member of an interface or a data
  class is one, and a lambda that acts is passed straight to a fun of
  the prelude, which runs it before it returns.
- `action` goes before a top-level `fun` only.

## 8. Bugs are panics

A broken invariant is not an error value.

```kotlin
require(amountCents > 0) { "payment amount must be positive" }
check(updated == rows.size) { "the eligible fees changed" }
val share = checkNotNull(invite.driverSharePercent) { "an outside invite has a share" }
error("unreachable")
```

`require`, `check`, `requireNotNull`, `checkNotNull` and `error` panic, as
do integer division by zero, an index out of range and the list and
string calls below. Code cannot catch a panic.
An `Int` or `Long` `/` or `%` by zero panics with Kotlin's message,
`/ by zero`; a `Double` divided by zero is an infinity or NaN. `list[i]`
outside the list panics with `Index <i> out of bounds for length <size>`
(`listOf(1, 2, 3)[5]`: `Index 5 out of bounds for length 3`).
`first()`, `last()` and `single()` of an empty list panic with
`List is empty.`, and `single()` of two or more elements with
`List has more than one element.` (`listOf(1, 2).single()`).
`take(n)` and `drop(n)` of a negative `n` panic with
`Requested element count <n> is less than zero.` (`take(-1)`:
`Requested element count -1 is less than zero.`); an `n` past the end
takes or drops every element. A `String`'s `take(n)`, counted in UTF-16
units, panics with `Requested character count <n> is less than zero.`
`substring` outside the string, counted in UTF-16 units, panics with
`Range [<start>, <end>) out of bounds for length <length>`, where
`substring(start)` ends at the string's length
(`"abc".substring(5, 3)`: `Range [5, 3) out of bounds for length 3`).
A `when` that must cover every case (section 6) and has no `else`
panics, as Kotlin's throws, on a value none of its branches names, with
`when: no branch for <type> <value>` (`when: no branch for InviteType
Partner`), whether it is a value or a statement.
The server catches it at the request boundary, rolls the transaction back,
logs it and answers 500 (section 15).

There is no `try`, `catch`, `finally`, `throw`, `runCatching` or
`Either.catch`, and no exception types.

## 9. Expected failures

An expected failure is a value. A function that can fail returns an
`Either`:

```kotlin
fun parsePercent(text: String): Either<String, Int> = either {
    val n = text.toIntOrNull() ?: raise("Enter a number.")
    ensure(n in 0..100) { "Enter a percentage from 0 to 100." }
    n
}
```

Inside an `either<E, A> { }` block (a *raise scope* for `E`):

| Form | Meaning |
| --- | --- |
| `raise(e)` | fail now with `e`; type `Nothing` |
| `ensure(cond) { e }` | fail with `e` unless `cond` |
| `ensureNotNull(x) { e }` | `x` as non-null, or fail with `e` |
| `x.bind()` | `x: Either<E, A>` gives its `A`, or fails with its `E` |

Outside a raise scope these forms are errors: wrap the code in
`either { }`.

- Every place a body can fail is marked in it: one of these forms, or
  a call with `.bind()`, so it reads without opening its callees. A
  forgotten `bind` leaves an `Either` that is dropped or of the wrong
  type, an error either way.
- Each form fails with a type that fits the scope's `E`: `raise(e)`
  needs an `e` that is an `E`, and `bind` an `Either<E2, A>` whose `E2`
  fits `E`. Convert any other failure with `mapLeft`, of one call,
  `load(id).mapLeft { e -> ... }.bind()`, or of several,
  `either<E2, A> { ... }.mapLeft { e -> ... }.bind()`.
- A lambda inside a raise scope is in it, as in Kotlin:
  `either { ids.map { load(it).bind() } }`. A local `fun` is not.
  A lambda that fails into the scope is passed straight to a prelude
  fun that runs it: kept as a value, in a `val` or as a result, or
  passed to a fun of the program's, which could keep it, it could run
  outside the scope, and is refused.
- Inside a raise scope, a member of `this` named `raise`, `ensure` or
  `ensureNotNull` does not hide the form, as it hides an extension
  (section 5.2): both are candidates, and where both fit, the call is
  an error that says to rename the member. `this.raise(...)` on such a
  member is the same error, since in Kotlin `this` inside `either { }`
  is the raise scope.
- So, in an extension or a data class's fun, inside a raise scope's
  block or a local fun there, a `this` whose text Kotlin reads as the
  raise scope is a compile error (`raise-this`): it would mean the
  receiver here and the raise scope in Kotlin. That is a `this` that
  stands as a value, `"I am $this"`, and a call the raise scope takes
  as well, on `this` or on no receiver: `toString()` and an extension
  on a type parameter, `this.let { "$it" }`, `also`, `takeIf`,
  `right`, `left`. Name the receiver in a `val` before the block,
  `val acct = this`, and use the `val`. A member read through it,
  `this.name`, stays: kotlinc refuses it, so nothing differs.

  ```
  acct.kg:5:60 raise-this: in Kotlin this inside either { } or db.tx { } is the raise scope, not this Acct: name it in a val before the block, as val acct = this, and use that (spec.md 9)
  ```
- `either`'s `E` is settled as section 5.1 says, before its block is
  read: by the type wanted, or written, `either<String, Int> { ... }`.
- A program does not write `Raise<E>`. The forms' signatures are the
  checker's own, in `check/core.kg`, beside `Either` itself.

Working with an `Either` value:

| Form | Meaning |
| --- | --- |
| `a.right()`, `e.left()` | build one |
| `x.fold({ e -> ... }, { a -> ... })` | handle both cases |
| `x.mapLeft { e -> ... }` | convert the failure |
| `x.map { a -> ... }`, `x.flatMap { a -> ... }` | transform the value |
| `x.getOrElse { e -> ... }`, `x.getOrNull()` | leave Either |
| `when (x) { is Either.Left -> x.value; is Either.Right -> x.value }` | match |

An `Either` value that is computed and then dropped is a compile error:
bind it, fold it, return it or assign it.

`Either.Left` and `Either.Right` are the variants of the sealed
interface `Either`, each with a `value`. An `Either` is built only with
`left()` and `right()`: `Either.Left(e)` is refused. It writes itself
as Arrow does, `Either.Left(value)`, not as a data class. The functions of
this table are the prelude's, in `prelude/either.kg`.

The error type `E` is any type. cusoon2 uses `String` (words for the
user) at the RPC boundary and sealed interfaces elsewhere.

Nothing else from Arrow exists: no `Raise<E>` receivers (section 14),
`withError`, `Option`, `NonEmptyList`, `zipOrAccumulate`, `recover`,
`nullable { }`, `Ior` or `Validated`.

## 10. SQL

SQL is written as Postgres SQL in a `sql"""..."""` literal and is checked
against the real schema when the program is compiled.
A program is checked against one schema, and Kogo promises what it
reads only against that schema. After a change to a table, a column or
a type that its SQL names, check, build and start the program again:
what a running program does across such a change is not promised.

```kotlin
data class SupplierRow(val id: ID, val legalName: String, val driverSharePercent: Int)

fun suppliersOf(tx: Tx, operatorId: ID): List<SupplierRow> =
    tx.query<SupplierRow>(sql"""
        select b.id, b.legal_name, sa.driver_share_percent
        from supply_agreement sa
        join business b on b.id = sa.supplier_id
        where sa.operator_id = $operatorId
        order by b.legal_name
    """)
```

### 10.1 The literal

- `$name` and `${expr}` inside `sql"""` are bound parameters, never text.
  The value is sent to Postgres separately from the statement. A name
  written twice is one parameter; an expression is evaluated and bound
  each time it is written. `$1` is an error; any other `$` is SQL text.
  Each `$name` and `${expr}` must be where Postgres reads a parameter:
  one inside a quoted string, a comment or a dollar quote's tag is an
  error, even where the same name is a parameter elsewhere. A `$` as
  text before a name or a digit is written `chr(36)`, as in
  `'in ' || chr(36) || 'AUD'`, or bound as a parameter holding it.
- A statement that fits on one line may be written `sql"..."`. Neither
  form has escapes.
- A `sql"""` literal may only appear as the argument of one of the five
  calls below or, in a test file, of `tx.refusal` (10.8). It is not a `String`, cannot be stored, concatenated or
  built at run time. There are no SQL fragments in v0.
- One statement per literal.

### 10.2 The five calls

| Call | Result | When the row count is wrong |
| --- | --- | --- |
| `tx.query<R>(sql"...")` | `List<R>` | any count is fine |
| `tx.queryOne<R>(sql"...")` | `R?` | more than one row panics |
| `tx.querySingle<R>(sql"...")` | `R` | anything but one row panics |
| `tx.exists(sql"...")` | `Boolean` | true when at least one row |
| `tx.execute(sql"...")` | `Int`, rows affected | |

Each is called on a `Tx` written before the dot: in a fun on `Tx`,
`this.execute(sql"...")`. `insert`/`update`/`delete ... returning` go through the query
calls. `exists` and `execute` discard the result's columns, so those columns are not typed
and need no type of section 10.4: `tx.execute(sql"select pg_advisory_xact_lock($key)")`
passes though `void` has none, and a `returning` under `execute` is ignored.

### 10.3 Result types

`R` is a data class, or a scalar type when the statement returns one
column.

- Columns are matched to the data class's properties **by name**: column
  `legal_name` fills property `legalName` (snake_case to camelCase). Every
  property must be filled and every column used; give a column a name with
  `as`.
- A query call sends its statement, a select, insert, update, delete
  or merge (10.7), as written, wrapped under a select of its columns by
  the names `kogo check` saw, less a trailing `;`: a select as
  `select "a", "b" from (...) _`, any other statement as
  `with _ as (...) select "a", "b" from _`. So a row is read by name
  even when the database served orders a table's columns otherwise than
  the one checked (a column dropped and added again) or has one more,
  and `select *` and `returning *` are safe. An `order by` in the
  statement is kept. Two columns of one name are an error. A statement
  whose own `with` writes is an error under a query call, since Postgres
  takes such a `with` only at the top level: run the write as a
  statement of its own. A column of a composite type
  (`select b as business`) is sent as its fields, each by the name
  `kogo check` saw, so a read does not depend on the order of a table's
  columns or a type's fields.
- A property may itself be a data class, written with a nullable or
  non-null type. It is filled from the columns whose names start with the
  property's name and an underscore:
  `select sa.driver_share_percent, b.id as business_id, b.legal_name as business_legal_name`
  fills `data class Supplier(val driverSharePercent: Int, val business: BusinessName)`.
  A nullable nested data class, on the absent side of an outer join, is
  told by its columns that are null only through the join: a `NOT NULL`
  column of the joined table, or its whole row. Such a column fills a
  property of it that may not be null; any other column that may be
  null fills only a `T?` there, as a field of a whole row does (10.5).
  Those columns share their outer joins, a full join's two sides
  counting as two: the class is told by the one with the fewest, then
  the one whose join the statement writes first, and any other that
  other outer joins can null, or whose joins `kogo check` cannot follow,
  as through `coalesce`, fills only a `T?` there, `"name!"` or not. Over `business b left join tenant t on
  t.business_id = b.id left join tenant_driver d on d.tenant_id = t.id`,
  `t.id as shop_id` tells a `Shop?` and `d.driver_id as shop_driver_id`
  fills only a `Uuid?` of it, as through a subquery on the nullable
  side with an outer join of its own. The class must have at least one
  property that is not nullable filled by such a column, in itself or
  in a data class it holds that is not nullable, and `kogo check`
  refuses one that has none: select one, as `t.id as storefront_id`, or
  the table's whole row. So a class whose columns no outer join can
  null, as `returning *` gives a table's own nullable columns, is
  refused too: read each column as a `T?` property and build the class
  from them in Kogo. The class is `null` when the columns that tell it,
  those filling a property that may not be null, are null, whatever its
  others hold, so `coalesce(n.text, '') as note_text` beside
  `n.id as note_id` reads as no note where there is none. A column
  asserted with `"name!"` that is null where the class is there panics
  (section 8).
- A property that is a data class may instead be filled by the one column
  of its name, of a composite type (10.4). A table's name or alias on its
  own is its whole row, so whole rows are read from a join as
  `select b as business, t as storefront from business b left join tenant t on t.business_id = b.id`
  into `data class Listing(val business: Business, val storefront: Tenant?)`.
  The prefix form is for chosen columns.
- Reading a row builds `R` and each data class in it, so it is allowed
  only where each one's constructor may be called (sections 2 and 11).
- A data class `R` is not written nullable, since a row is never null;
  `queryOne<R>` gives the `R?`. A scalar `R` is a `T?` where its column
  may be null: `queryOne<String?>`.
- `select t.*` is allowed when the data class has exactly the table's
  columns. `kogo schema`, which is not built yet, is to write `schema.kg`
  with one such data class per table and one enum per Postgres enum; it
  is ordinary source, checked in, and written by hand until then.

### 10.4 Type mapping

| Postgres | Kogo |
| --- | --- |
| `boolean` | `Boolean` |
| `integer` | `Int` |
| `bigint` | `Long` |
| `double precision` | `Double` |
| `numeric` | `Decimal` |
| `text`, `citext`, `varchar` | `String` |
| `uuid` | `Uuid` |
| `timestamptz` | `Instant` |
| `date` | `LocalDate` |
| `time` | `LocalTime` |
| `interval` | `Duration` |
| `bytea` | `ByteArray` |
| an enum type | the enum of the same constants in `schema.kg` |
| `T[]` | `List<T>`, or `List<T?>` |
| a domain | its base type's mapping |
| a composite type | a data class |

A composite type is a table's row type or one made with `create type`.
Its fields fill the data class's properties as columns fill a row's
(10.3): field `legal_name` fills property `legalName`, every property
must be filled and every field used, and each field's type maps to its
property's. A parameter cannot be of a composite type, and an array of
one has no Kogo type. The whole row of a subquery or a CTE, like
`row(...)`, has the anonymous type `record`, which has none either:
select from a table, or name the columns.

Each label of an enum type must be a name a constant can have: a letter
or `_`, then letters, digits and `_`, neither a hard keyword such as
`in` nor underscores alone. An enum with any other label, one holding a
space or a hyphen, has no Kogo type, whatever it fills: rename the label
in Postgres, or cast the column to text and read it as a `String`.

An enum is read as the label the column holds, unchecked: a label the
program's enum lacks, one a migration added while an older program
runs, is passed on and compared with `==` as any other, and a `when`
over it panics (section 8).

A parameter's Kogo type must map to the type Postgres infers for it. When
Postgres cannot infer one, cast it: `$active::boolean`. A parameter or a
result column of a type that is not in the table is a compile error: cast
it in the statement.

A value goes to Postgres and comes back as it is, with these edges.
Postgres keeps microseconds, so an `Instant`, a `LocalTime` and a
`Duration` are sent cut to the microsecond. A `Duration` is sent as a
count of microseconds, and an `interval` read as one counts a day as 24
hours. What Postgres holds and the Kogo type does not is a panic when
it is read (section 8): `infinity` in a `timestamptz` or a `date`,
`24:00` in a `time`, and an `interval` with months, which has no fixed
length. A `Duration` too long for an `interval`'s microseconds is a
panic when it is sent, and so is an `Instant` or a `LocalDate` past
Postgres's range: -4713-11-24 to +294276-12-31 for a `timestamptz`,
to +5874897-12-31 for a `date`. A `Decimal` holds `NaN` and `Infinity` as well
as numbers, and a zero keeps its scale: `0.00`. An empty `List` is sent as an
empty array, never as `NULL`, and a `ByteArray` of no bytes as an empty
`bytea`.

Kogo reads a subset of what Postgres can send, and outside it refuses:
at `kogo check` where the catalog or the statement tells, else by a
panic that names the limit (section 8). It never gives another value.
`kogo check` refuses a type Postgres does not promise where a truthful
one exists (`String?`), unless the author asserts it with `"name!"`
(10.5), and Kogo panics naming the limit only where no truthful type
exists. An array's elements are the exception: there Postgres promises
nothing either way, so the author's `List<T>` is the assertion. The
limits, with those above:

- A type that is not in the table, an array of a composite type or of a
  domain over an array, a `record`, an enum with a label no constant
  can have: `kogo check`.
- A list of an enum, as a column or a parameter: refused when the
  program is emitted, with the cast to write.
- An array of more than one dimension: a panic when it is read.
  Postgres's `integer[]` does not say how many dimensions a value has,
  and a `List` has one. A lower bound other than 1 is not kept, which
  loses nothing a `List` has.
- A `NULL` element read into a `List<T>`: a panic when it is read, which
  names the column. Postgres says nothing of an array's elements, so the
  author does: `List<T>` asserts that none is null and is held by this
  panic, as `"name!"` is for a column (10.5); `List<T?>` takes them.
  Write `List<T?>` where an element may be null, as in an `array_agg`
  over an outer join, which gives `{NULL}` for a row with none.
- `infinity` in a `timestamptz` or a `date`, `24:00` in a `time`, an
  `interval` with months: a panic when it is read.
- A `String` that holds U+0000, alone or in a `List`: a panic when it is
  sent, since Postgres's `text` holds none.
- A `Duration`, an `Instant` or a `LocalDate` past Postgres's range: a
  panic when it is sent.
- A count of more than 2,147,483,647 rows from `tx.execute`, whose
  result is an `Int`: a panic once the statement has run, which rolls
  the transaction back.

An array of a domain, and a domain over an array, are read as the array
of the base type is.

### 10.5 Nullability

- A result column is non-null only when Postgres can never make it
  null: a reference to a `NOT NULL` column of a table or to a table's
  whole row, `count(...)`, `exists (...)`, a `coalesce(...)` with an
  argument non-null by this rule or a constant other than `null`, or a
  cast of any of these other than from `jsonb` or by a function of the
  schema's; and none of it on the nullable side of an outer
  join. Every other column (other functions and aggregates, as `sum`,
  operators, scalar subqueries, outer-joined columns) is nullable.
- A composite value is null as a whole, never field by field: the whole
  row of an outer-joined table is null where the table is absent. Where
  it is there, a field that may be null fills only a `T?` property. A
  field of a table's whole row is non-null where the table's column is
  `NOT NULL`; every field of any other composite value (a column of a
  composite type, a cast, a function's result) may be null, since
  Postgres cannot declare a field of a type `NOT NULL`, and a table's
  constraints do not cover its row type stored elsewhere.
- The reference may pass through a subquery in `from` or a `group by`.
  A column of a CTE, a view or a `union` is nullable, and so is one
  nulled by a grouping set. A `where` that excludes the null rows of an
  outer join does not make its columns non-null.
- Override by naming the column `as "name!"` (assert non-null; a null at
  run time panics) or `as "name?"` (force nullable). A field has no
  override: select it as a column of its own.
- A non-null column may fill a `T?` property. A nullable column filling a
  `T` property is a compile error, but for one null only through an
  outer join inside a nullable nested data class, which the class's
  absence covers (10.3). An array's elements are the author's to
  type (10.4).
- A parameter may always be `T?`; it is sent as SQL `NULL`.

### 10.6 Idioms that replace a query builder

- Optional filter: `where ($active::boolean is null or td.active = $active)`.
- A list: `where id = any($ids)`.
- A choice between two statements (with and without `for update`, two
  sort directions): write both literals in an `if`/`else`.
- A conflict that is an expected outcome: a constraint Postgres
  refuses is a panic (section 8), so say it in the statement.
  `insert ... on conflict do nothing returning *` under `queryOne`
  gives `null` when a unique constraint stood in the way, and where
  there are two, one `tx.exists` says which. An `update` has no
  `on conflict`: test with `tx.exists` first.

### 10.7 Transactions

```kotlin
fun checkout(db: Db, cart: Cart): Either<OrderError, Order> =
    db.tx { tx ->
        val order = placeOrder(tx, cart).bind()
        chargeCard(tx, order).bind()
        order
    }
```

- `db.tx { tx -> ... }` runs the block in one transaction at READ
  COMMITTED, which the handle states and does not leave to the
  database's default. The block is a
  raise scope when the enclosing function returns `Either<E, ...>`, for
  that `E`, and `db.tx` then gives an `Either<E, A>` of the block's value
  `A`, wherever it is written; elsewhere it gives the block's value. It
  commits when the block finishes, and rolls back on a failure or a
  panic. It reruns the block from the start on a deadlock, as a
  server's `Tx` (15.3) is, and on nothing else.
- `db.tx(serializable = true) { ... }` runs at SERIALIZABLE and reruns the
  block from the start on a serialization failure or deadlock.
- A rerun comes after a short random pause, and either form may rerun,
  so the block must have no effect outside the transaction
  (`action-in-tx`, section 7).
- The transaction ends with the block, so the block answers nothing
  that holds one (`tx-escapes`, 15.7): not the `Tx`, and not a value
  with one in a property. It answers the facts the lines after it
  need.
- A transaction's kind is its handle's type. `Tx` is any transaction: a
  function that takes one assumes nothing, and locks what it needs.
  `SerializableTx` is one at SERIALIZABLE, where a conflict is a
  serialization failure and never a wrong read, so a function that
  takes one may read with no lock: the program's is rerun on a
  serialization failure, and a test's fails (16.2). A `SerializableTx` fits where a `Tx`
  is wanted, and not the other way. The block of `db.tx` is handed a
  `Tx`, and that of `db.tx(serializable = true)`, the literal `true`,
  a `SerializableTx`, as is a test's (16.1).

  ```
  invoices.kg:40:19 needs-serializable: invoice takes a SerializableTx and this is a Tx, which is not rerun on a conflict: call a function that locks, take a SerializableTx, or open the transaction with db.tx(serializable = true) (spec.md 10.7)
  ```
- `tx.atomically { ... }` runs a nested block in a savepoint and rolls
  back to it on a failure. The block is in the raise scope around it,
  as any lambda is.
- A `Tx` runs select, insert, update, delete and merge alone, by any
  of the five calls and `tx.refusal`, in a test file too. Any other
  statement is what Postgres calls a utility statement, as `commit`,
  `rollback`, `set`, `set local`, `copy`, `lock table` or `discard`,
  and is a compile error, `sql-utility`, named by Postgres's own tag
  for it. `commit` or `rollback` would end the transaction `db.tx`
  holds, so the rest of the block runs outside one and a panic rolls
  nothing back; a `set` would reach later requests on the pooled
  connection. `db.tx` begins and ends the transaction, and a setting
  that lasts the transaction is bound with `select set_config('name',
  $value, true)`. The pass does not look into a function: one that
  sets a session's state, `set_config(..., false)` among them, is not
  seen.

  ```
  orders.kg:12:16 sql-utility: this is a utility statement (Postgres's TRANSACTIONSTMT), and a Tx runs select, insert, update, delete and merge alone: db.tx begins and ends the transaction, and a setting is bound with select set_config('name', $value, true) (spec.md 10.7)
  ```

### 10.8 A refused statement, in a test

A statement Postgres refuses is a bug (section 8), and where a caller
can meet the refusal the statement says so (10.6). A test of the
schema itself, of a constraint or of what one tenant may not touch,
wants to see the refusal. In a test file, and nowhere else:

```kotlin
val refusal = tx.refusal(sql"insert into invoice (id, number) values (${newId()}, 'INV-1')")
assertEquals("23505", refusal?.sqlState)
assertEquals("invoice_number_key", refusal?.constraint)
```

- `tx.refusal(sql"...")` runs one statement and gives a
  `kogo.test.Refusal?`: `null` when Postgres accepted the statement,
  and otherwise what it said, `sqlState: String` (the SQLSTATE, as
  `23505`), `constraint: String?` (the constraint's name, `null` where
  the refusal names none, as a `not null` does not) and
  `message: String`.
- The statement runs in a savepoint. A refusal rolls back to it, so
  the transaction goes on and holds nothing of the statement; an
  accepted statement's changes stay.
- A statement Postgres stopped, rather than refused, is a panic: a
  timeout, a serialization failure or deadlock, a lost connection, a
  lack of resources or a system error (SQLSTATE classes 08, 40, 53,
  57, 58, XX). A test's transaction is SERIALIZABLE (16.2), and is
  not rerun: a serialization failure in it fails the test.
- Row-level security hides rows rather than refusing a statement: an
  update or delete of another tenant's rows is accepted and touches
  none, so a test asserts that `execute` gives 0. Only a write that
  breaks a policy's `with check` is refused (`42501`).
- The literal is checked when the test is compiled, as `execute`'s is
  and with its columns discarded: a statement that does not parse or
  type is a compile error, not a `Refusal`.
- It is a value, and nothing is caught: a refused statement anywhere
  else in a test is the panic it is in the program, and fails the
  test.

## 11. Markers

A marker is a named permission that a declaration demands of the file
that uses it. It is the one annotation form in the language, and it is
spelled as Kotlin's opt-in so that cusoon2's access walls port unchanged.

```kotlin
@RequiresOptIn
annotation class UnscopedRead

@UnscopedRead
fun business(tx: Tx, id: ID): Business? = ...

data class HeldBusiness @UnscopedRead constructor(val business: Business, val storefront: Tenant?, @property:UnscopedRead val tx: Tx)
```

A file may use a marked function, property or constructor only if it
begins with `@file:OptIn(UnscopedRead::class)`. Anything else is a
compile error. A `val` of an interface takes a marker before `val`. A
property of a data class takes it as `@property:Marker`, as Kotlin has
it: a bare `@Marker` there would mark the constructor's parameter,
which Kotlin refuses, and so does Kogo (`syntax`). Either is read, by
name or by destructuring (a `_` in its place included, as in Kotlin),
and overridden, as is a marked `fun` of an interface, only in such a
file. A value of a data class is made four ways, each held to
a marked constructor: a call of it; a `copy` and a row read into one
(10.3), which use it; and an operation's input, which is refused even
where the file opts in (15.4). Only the file's opt-in counts: marking the function
that makes the use does not stand in for it, as it does in Kotlin, and
the file that declares a marked function opts in to use it too. A type
carries no marker; mark its constructor. Markers do nothing at run time.
No other annotation exists.

## 12. Prelude

The prelude is a library, not part of the core. Names match Kotlin's
standard library and `java.time` where the meaning is the same. Its
signatures are bindings (section 13) kept in `prelude/`: the lists below
are what Kogo will have, and the declarations say what each takes and
gives. A name not built yet is `unknown-name` from `kogo check`; each is
added when a program first needs it.

The prelude adds no function or property to a type that Kotlin has: on
a `Double`, a `String` or a `List`, every member is Kotlin's and means
what Kotlin's means. Where a Kotlin program would reach into Java's
library, for times or for printing a number, Kogo has a function or a
type of its own in a package `kogo.<name>`, imported by name as
`kogo.time` is, and so for input and output, where Kotlin's are
global functions and Kogo's an effect (section 7). So a file's imports
show what in it is not Kotlin's.
`Uuid`, `newId`, `randomString` and `Decimal` are Kogo's own and need
no import;
`ByteArray` (section 3) is Kotlin's name and needs none either.

**Collections** (immutable; "changing" one returns a new one).
`listOf`, `emptyList`, `setOf`, `mapOf`, `size`, `isEmpty`, `isNotEmpty`,
`contains`/`in`, `list[i]`, `getOrNull(i)`, `map[k]` (gives `V?`),
`first`, `firstOrNull`, `last`, `lastOrNull`, `single`, `singleOrNull`,
`map`, `mapIndexed`, `mapNotNull`, `flatMap`, `filter`, `filterNot`,
`filterNotNull`, `any`, `all`, `none`, `count`, `sum`, `sumOf`,
`maxOrNull`, `minOrNull`, `sortedBy`, `sortedByDescending`, `sorted`,
`reversed`, `distinct`, `distinctBy`, `take`, `drop`, `zip`, `plus`
(`+`), a `Set`'s `plus` (`+`) and `minus` (`-`) of an element, a `List`
or a `Set`, `groupBy`, `associateBy`, `associate`, `toSet`, `toList`,
`joinToString`, `forEach`, `keys`, `values`, `listOfNotNull`. A `Map`
and a `Set` iterate in insertion order and compare keys as `==` does,
by value, so any type may be a key. A key that is a `String`, an `Int`,
a `Long`, a `Boolean`, a `Uuid` or an enum is found at once. A key of
any other type, a `T?` among them, is found by walking the keys: a
lookup costs the size of the collection and building one its square,
so think hard before keying a large `Set` or `Map` (or a `distinct`,
`distinctBy`, `groupBy`, `associate` or `associateBy`) by anything
else. `==` of two `Set`s or two `Map`s walks
whatever the kind of key, so comparing two large ones costs the product
of their sizes. This may be mended; nothing promises it.

There are no mutable collections, sequences or arrays. Build a list with
`map`/`filter`/`+`, or accumulate into a local `var` of an immutable list.

**Enums.** `valueOf(name)`, as section 3.3 says.

**Strings.** `length`, `isEmpty`, `isBlank`, `isNotBlank`, `ifEmpty`,
`ifBlank`, `trim`, `trim(char)`, `trimEnd`,
`lowercase`, `uppercase`, `startsWith`, `endsWith`, `contains`,
`substring`, `substringAfter`, `take`, `replace`, `split(delimiter)`,
`padStart`, `toIntOrNull`, `toLongOrNull`,
`equals(other, ignoreCase = true)`, `filter`, `filterNot`, `map` (over
`Char`), and on `Char`: `isWhitespace`, `isDigit`, `digitToInt`.
`Regex(pattern)` with `matches`, `containsMatchIn`, `find` and
`String.replace(regex, text)`; the pattern is read as Go's `regexp`
reads it (RE2), and nothing of Kotlin's is promised for it. Case
conversion never reads a locale.

**Where the prelude differs from Kotlin.** A name that Kotlin has means
what Kotlin's means in ordinary use. The corners where it does not are
all listed here, but for a `Regex`'s pattern, whose list is what we know
and not a promise that it is all:

- A `Regex`'s pattern is Go's, not Kotlin's. One Go cannot read
  (lookaround, a backreference, a possessive repeat) is refused at
  `kogo check` with `regex-pattern` when it is a string literal with no
  template in it, and otherwise panics when the `Regex` is made. One
  both read may mean something else in each:

  | Pattern | Input | Kotlin | Kogo |
  | -- | -- | -- | -- |
  | `a.c` matches | `a\rc` | false | true: `.` stops only at `\n`; Kotlin's also at `\r`, U+0085, U+2028, U+2029 |
  | `\s` matches | U+000B | true | false |
  | `(?i)k` matches | U+212A | false | true: Go folds case by Unicode, Kotlin by ASCII |
  | `[[:alpha:]]` matches | `z` | false | true: a POSIX class in Go, a nested class in Kotlin |
  | `[a-c&&[b]]` matches | `a]` | false | true: Kotlin's `&&` intersects; Go reads the characters |
  | `(?U)\w` matches | `é` | true | false: Kotlin's Unicode classes; Go's ungreedy flag |
  | replace `$` with `!` | `ab\n` | `ab!\n!` | `ab\n!`: Kotlin's `$` also stands before a final line break |
- A `String` never holds half of a surrogate pair (section 3).
- Two `ByteArray`s of no bytes are equal where `==` reaches them
  (section 3), where Kotlin's two made apart are not.
- `lowercase()` does not read the letters either side: a final `Σ`
  becomes `σ` where Kotlin's is `ς`.
- `replace(regex, text)` skips an empty match that starts where a match
  ends: `"abc".replace(Regex("b*"), "-")` is `"-a-c-"` where Kotlin's is
  `"-a--c-"`. The text is read as Kotlin reads it: `$1` and `${name}`
  are groups, and a backslash takes the next character as it is.
- `sortedBy` and `sortedByDescending` order by an `Int`, a `Long`, a
  `Double`, a `String`, a `Char` or a `Boolean`, or one of these that
  may be null, as Kotlin does. A key of any other type, an enum or a
  time among them, is refused at `kogo check` with `sort-key`, and so
  is `maxOrNull` of a list of any other type (and of one that may be
  null, which kotlinc refuses too). The list each sort gives is
  Kotlin's, equal keys in the order they came; how often and in what
  order it calls the selector is not, and Kotlin promises neither.
- A `Set`'s `+` or `-` of one element that is itself a `List` or a
  `Set` is refused at `kogo check` with `set-operand`: Kotlin takes a
  `List` or a `Set` there as the elements to add or remove, and gives
  a `Set<Any>`. Write `setOf(x)` to add or remove `x` whole.
- `trim(char)` takes one `Char`, where Kotlin's takes any number.
- `split(delimiter)` takes one `String`, where Kotlin's also takes
  several, a `Char`, `ignoreCase` and `limit`.
- A bug's message is Kotlin's but for these two. Kotlin words an index
  outside a list by the list's class, so `emptyList<Int>()[0]` is
  `Empty list doesn't contain element at index 0.` there and `Index 0
  out of bounds for length 0` here, as for every list; and a `replace`
  whose text names no group says so in Kogo's words.
- A `Decimal` writes the digits Postgres sent, where Java's `BigDecimal`
  writes an exponent for a small number: `0.0000001` is `1E-7` in
  Kotlin.

**Numbers.** `coerceAtLeast`, `coerceAtMost`, `coerceIn`, `abs`, `max`,
`min`, `toInt`, `toLong`, `toDouble`, `toString`. A `Decimal` holds what
Postgres sent, exactly: it has `toDouble` (the closest `Double`),
`toString` (Postgres's text, as `1.50`), `==` and `< <= > >=`, and no
arithmetic in v0. Its `==` is Java's `equals`, which counts the scale:
`1.50 == 1.5` is false.

**Time** (`kogo.time`). `Instant` (`Instant.ofEpochSecond(seconds,
nanos)`, `isBefore`, `isAfter`, `plus(duration)`, `minus(duration)`),
`LocalDate` (`plusDays`, `year`, `monthValue`, `dayOfMonth`,
`atStartOfDay(zone): ZonedDateTime`), `LocalTime`, `ZoneId.of(name)`,
`ZonedDateTime` (`instant.atZone(zone)`, `hour`, `minute`,
`toLocalDate`, `plusSeconds`, `toInstant`), `Duration` (`ofDays`,
`ofMinutes`, `Duration.ofSeconds(seconds, nanos)`,
`Duration.between(start, end)`, `seconds`), `Clock.now()`.
`ofEpochSecond`, `ofSeconds`, `ofDays`, `plus`, `minus`, `atZone`,
`plusDays` and `ZoneId.of` panic where `java.time`'s throw, with its message:
`Instant exceeds minimum or maximum instant`, `long overflow`, `Invalid
value for EpochDay` past a `LocalDate`'s years ±999,999,999, or
`Unknown time-zone ID: X`. `ZoneId.of` takes a region name of the tz
database (`Australia/Melbourne`, `UTC`), and an unknown or malformed one
panics with `java.time`'s message. An offset ID (`Z`, `+05:30`,
`GMT+1`, `UTC+10`) is outside Kogo's subset of `java.time` and panics
`ZoneId.of takes a region name, such as Australia/Melbourne; offsets
are not supported (spec 12)`; on a string literal, `kogo check` refuses
it with that message and code `zone-offset`. There is no default zone: every
conversion names its zone. `atStartOfDay(zone)` is the day's earliest
time in the zone: where midnight is skipped, the end of the gap.
Each writes itself as `java.time` does, in a template too: an
`Instant` as `2026-11-15T14:30:00.120Z`, with the fraction in groups
of three digits and none when it is zero; a `LocalDate` as
`2026-11-15`; a `ZonedDateTime` as
`2026-11-16T01:30:00.120+11:00[Australia/Sydney]`, without the seconds
when they and the fraction are zero.

**Input and output** (`kogo.io`). A `Console` is a program's standard
input and output, passed to the function that uses it as `Db` is:
`console.println(text)`, `console.print(text)` and `console.readln()`,
which gives the next line without its line break and panics past the
end of the input.

**Text** (`kogo.text`). `plain(value: Double): String` writes a number
as its shortest digits, with no exponent and no trailing `.0`: `5.0` is
`5`, `12.5` is `12.5`, `1.0E7` is `10000000`. Negative zero is `0`. `NaN`
and an infinity have no digits and are a bug: `plain` panics. Every
other `Double` reads as Java's `BigDecimal` writes it but eight below
`1.0E-322`, 1, 2, 10, 12, 14, 16, 18 and 20 times the smallest: where
the shortest digits are one, Java writes the closest two, so the
smallest `Double` ends in `5` here and `49` in Java. The difference is
`plain`'s alone: a template and `toString` write these as Java does.

**Ids.** `Uuid`, `Uuid.parseOrNull(text): Uuid?`, `newId(): Uuid`
(an id no other call gives: section 7). `Uuid.parseOrNull` reads 36
characters, hyphens at 8, 13, 18 and 23 and hex digits of either case
elsewhere, and gives `null` for any other text;
`Uuid.parseOrNull("$id") == id`. Kotlin's also reads 32 hex digits
without hyphens; Kogo's gives `null` for them.

**Secrets.** `randomString(length: Int): String` gives `length`
characters, each drawn evenly from the 62 letters and digits (`a` to
`z`, `A` to `Z`, `0` to `9`) by the operating system's cryptographic
source, so it cannot be guessed: a session's token. Zero gives `""`;
a negative `length` is a bug, and it panics with
`randomString's length <n> is negative`. A token goes in no message
a `require`, `check` or `error` gives, in no operation's refusal and
in no endpoint's path, since each may be logged, and is looked up by
SQL's `=`, never read back to compare with `==`.

**Requests** (`kogo.rpc`). A `Request` is what the server read of one
HTTP request, passed to an operation (section 15):
`request.header(name): String?`, `request.cookie(name): String?` and
`request.clientAddress: String`. A header's name is matched whatever
its case. A `String` a program holds is text, so reading a header
whose value is not UTF-8 ends the request with a 400 that names the
header and never its value, `updateJob: the header Origin is not
UTF-8`, from the list's check as from an operation; a header no
program reads is not looked at. Kogo code does not build or change
one. An ungated operation that sets cookies answers a
`Reply<A>(answer: A, cookies: List<Cookie>)`, each a `Cookie(name,
value, path, maxAgeSeconds: Long, sameSite: SameSite, secure,
httpOnly)`, with `SameSite` one of `Strict`, `Lax` and `None` (15.2).

**An outside service** (`kogo.http`). The prelude's one outbound call.
An `Http` is a handle for one service, made in `main` with its address,
the headers and query fields that go with every call (its credentials:
Kogo code holds no secret), its encoding, the most bytes of an answer
it reads and the longest one call may take. `examples/29_http.kg` has
each form.

- An endpoint is declared once, with its kind: `lookup(method, path):
  Lookup` asks and changes nothing, and `action(method, path): Action`
  changes something outside. `http.ask(endpoint: Lookup, fields:
  List<Field>): Either<HttpFailure, Json>` is a lookup and
  `http.perform(endpoint: Action, fields: List<Field>):
  Either<HttpFailure, Json>` is an action (7.1), so `ask` of an
  `Action` is a type error. The kind is not the method, which is one
  of `GET`, `POST`, `PUT`, `PATCH` and `DELETE`: a lookup may be a
  `POST`, as a search is. Nothing checks an endpoint's kind: it is its
  author's word.
- An id in a path is an argument: `lookup(method, before, id)` and
  `lookup(method, before, id, after)`, and so for `action`. The parts
  are joined as written and the id alone is escaped, so one that holds
  a `/` or a `?` stays one piece of the path. It panics (section 8)
  where `before` does not end in `/` or `after` does not begin with
  one. An id that is empty, `.` or `..`, which would name the path it
  sits in or the one above and may come from a caller or a row, is no
  panic: `ask` and `perform` of that endpoint send nothing and give
  `NotSent`, `an id in a path is not ".."`. A written part
  of a path is written as it is sent: one that holds what a path
  escapes (a `?`, a space, a `%`) panics, with or without an id.
- A field is a name and a value: `field(name, value)` of a `String`, a
  `Long`, a `Double` (written as `toString()` writes it; `NaN` and an
  infinity panic) or a `Boolean`; `field(name, values: List<String>)`;
  `group(name, fields)`; and `field(name, json: Json)`, a JSON object
  sent as it is. Every name is a literal string of the service's. Kogo
  code never says how fields travel: a `GET`'s are in the query, and
  any other call's are the body, in the handle's encoding and the order
  written; a `DELETE` with no fields sends no body and no
  `Content-Type`, and a `POST`, `PUT` or `PATCH` with none sends `{}`
  or the empty form. A form writes a group's field as `group[name]`, a
  list's values each as `name[]` and a `Json` as its text; JSON writes a
  group as an object and a list as an array.
- `http.withHeader(name, value): Http` is a new handle with one more
  header, for a value that is no secret. A header the handle already
  has, from `main` or an earlier `withHeader`, panics.
- An answer is one JSON object, read by key: `json.string(key)`,
  `long`, `double`, `boolean`, `at(key): Json?` (a nested object) and
  `list(key): List<Json>?` (an array of objects) and
  `strings(key): List<String>?` (an array of strings). A read gives
  `null` where the key is absent, is JSON's `null` or holds another
  type, an array with one element of another type among them: a
  string that holds digits is not a number. `json.raw(): String` is the
  object's text untouched, and `Json.parse(text): Json?` is `null` for
  text that is not one JSON object. A 2xx answer with no body reads as
  an object with no keys, and a 2xx answer that is anything else than
  one JSON object is `Unreadable`.
- A call that gives no answer gives an `HttpFailure`, by how far it got
  and its status alone, whichever call it was: `NotSent(why)` when no
  connection was made; `Unknown(why)` when it left and no answer came
  that can be used, any status that is not a 2xx or a 4xx, no answer,
  or one that broke off or is past the limit, so an action may have been done: a `303` says it
  was; `Refused(status: Int, body: Json?)` for a 4xx, the body `null`
  when it is not a JSON object; and `Unreadable(why)` for a 2xx that is
  not one JSON object. What a refusal means is the client's to say, and
  a client raises `Unreadable` itself for an answer that lacks what the
  service always sends.
- An `HttpFailure` says how far a call got and nothing else. It is
  one call's: a function that makes more than one call gives what the
  whole came to, in a type of its own. When a service says no inside
  a 2xx and its client raises that, the client raises it in a failure
  type of its own, and logs it with `http.logRefusal(endpoint, why:
  String)`, for an endpoint of either kind, `why` its own word for the
  no. `logRefusal` is no action, as `ask`'s own logging is not, so a
  lookup that reads its answer stays one:

```kotlin
private fun asked(google: Http, endpoint: Lookup, fields: List<Field>): Either<GoogleFailure, Json> = either {
    val json = google.ask(endpoint, fields).mapLeft { GoogleFailure.Wire(it) }.bind()
    val status = json.string("status")
    when (status) {
        "OK" -> json
        "NOT_FOUND" -> raise(GoogleFailure.NotFound)
        else -> {
            google.logRefusal(endpoint, status ?: "no status")
            raise(GoogleFailure.Refused(status ?: "no status"))
        }
    }
}
```

Where Go's HTTP client would decide, Kogo states it:

| What | Kogo's rule |
| --- | --- |
| A second send | `perform` is never sent twice. `ask` may be, at once, when a kept connection fails before any answer; each second send is logged. |
| A redirect | None is followed: a 3xx is `Unknown`, whichever call it answers. |
| A proxy | None: nothing of the environment says where a call goes. |
| An answer's length | One past the handle's limit is `Unknown`, whatever its status. Its headers are held to the same limit, apart from its body. |
| An answer's form | No `Accept-Encoding` is sent, so an answer is read as the service wrote it, never unpacked. |
| `User-Agent` | None is sent unless the handle has one among its headers. |
| A JSON number | `long` is exact, and `null` for a number written with a fraction or an exponent, or past a `Long`. `double` is the nearest `Double`. |
| A time limit | The handle's own, which `main` must state, from the call's start to the end of its answer; past it a call that left is `Unknown`. |

Every call that `ask` or `perform` ends in an `HttpFailure` is logged
in Go, with its method, its path, its status where an answer came,
why, and the length of the body read; never the body, which may echo
what was sent. So is every `logRefusal`, with its method, its path
and its `why`, its first 4 KiB with a mark where it is cut: no status
and no body. A handle's own headers and query fields are never logged.

**Mail** (`kogo.mail`). A message is data that Kogo builds, and
`eml(mail: Mail): ByteArray` writes it as the bytes of an EML file, for
a row to hold until a sender sends them. `examples/30_mail.kg` builds
one and keeps it.

```kotlin
data class Address(val name: String, val address: String)
data class Attachment(val name: String, val mimeType: String, val data: ByteArray)
data class Mail(val messageId: String, val date: Instant, val from: Address, val replyTo: Address?,
                val to: List<Address>, val cc: List<Address>, val bcc: List<Address>,
                val subject: String, val text: String, val html: String?, val attachments: List<Attachment>)
```

`eml` reads no clock and makes nothing up: the same `Mail` gives the
same bytes, every time, so a message stored twice is the same message.
The application chooses the Message-ID and the date, and keeps each
address once across `to`, `cc` and `bcc`. A name of `""` is no name.
`bcc` is written into the bytes, as a `Bcc` header: they are all a
sender is given, so it reads who the message goes to from them and
leaves that header out of what it sends.

Where Go's `net/mail` and `mime` packages would decide, Kogo writes
the bytes itself and states them:

| What | Kogo's rule |
| --- | --- |
| The headers' order | `Message-ID`, `Date`, `From`, `Reply-To`, `To`, `Cc`, `Bcc`, `Subject`, `MIME-Version: 1.0`, then the content's. One with nothing to say (no `replyTo`, an empty list) is not written. No other header is: no `User-Agent`, no `X-Mailer`. |
| `Message-ID` | `<` `messageId` `>`, as given: the application writes the `@` and what follows it, by the address rule below. |
| `Date` | In UTC, to the second: `Mon, 16 Nov 2026 14:30:00 +0000`. |
| An address | `"name" <address>`, or `<address>` with no name; a list has one address to a line, each but the last ending in `,`. A name is quoted, with `\` before a `"` or a `\`, when it is printable ASCII, holds no `=?` and has no run of over 60 characters without a space; any other is encoded words. |
| The subject | As it is by the same rule as a name, unquoted; if not, encoded words. |
| A control character | In a name or a subject, a space, CR and LF included, so that no reader gives one back. |
| Folding | A header's line is broken, as CR LF before a space, wherever it would pass 78 characters, but never before the header's first word. |
| Encoded words | `=?UTF-8?B?...?=`, each the base64 of at most 39 bytes of UTF-8 and of whole characters, a space between, so that one fits a line after its header's name. |
| `text` and `html` | `text/plain; charset=UTF-8` and `text/html; charset=UTF-8`, quoted-printable, by these rules alone: a byte outside printable ASCII but a tab, and `=`, is `=XX`, in capitals; a space or a tab that ends a line is `=20` or `=09`; a line break is CR LF; when the next character's encoding would take a line past 75 characters, a soft break, `=` and CR LF, comes before it; and a text that does not end in a line break ends in a soft break. |
| An attachment's body | Base64, 76 characters to a line. |
| The parts | `text` alone is the message's body. With `html`, a `multipart/alternative` of `text`, then `html`. With attachments, a `multipart/mixed` of that body, then each attachment in order. |
| A boundary | `boundary="=_kogo-mixed"` and `boundary="=_kogo-alternative"`, always: quoted-printable writes `=` only before two hex digits or a line end, and base64 only at a line's end, so no body holds `=_`. |
| An attachment | `Content-Type:` its `mimeType`, then `Content-Disposition: attachment; filename="name"`, quoted as a name is, when that is printable ASCII and at most 45 characters; any other name is RFC 2231's pieces, a line each, `filename*0*=UTF-8''...;`, `filename*1*=...`, of its UTF-8 percent-encoded but for letters, digits, `.`, `-` and `_`, each at most 60 characters with the first's `UTF-8''`. |
| Line ends | CR LF, every one. |

No line is then over 78 characters but the `Message-ID`'s, an
address's and an attachment's `Content-Type`, each of which the
application chooses.

**The address rule.** `Address.address`, wherever one is written,
`replyTo` included, and `messageId` match

```
^[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+(\.[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+)*@[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+(\.[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+)*$
```

and are at most 254 characters: an ASCII addr-spec of RFC 5322's atext
and dots with one `@`, so that none ends its header or names a second
recipient. One that does not is a bug in the caller, and `eml` panics
(section 8). An address a person typed is checked against this rule,
with `Regex`, where the application takes it, so that the person meets
a refusal there and never the panic; an application's own rule is no
wider than this one. An address outside ASCII (an IDN domain, a UTF-8
local part) is refused: a customer who has one brings it. A `mimeType`
is printable ASCII, a parameter such as `; charset=utf-8` included,
and one that is empty or is not panics likewise.

**Tests** (`kogo.test`; test files only, section 16).
`kogo.test` has `Test<E>`, `test(sentence) { tx, env -> ... }` and
`Refusal` (10.8), and `kotlin.test`'s assertions, each imported by
name from `kogo.test`: `assertEquals(expected, actual)`, `assertNotEquals(illegal,
actual)`, `assertTrue(actual)`, `assertFalse(actual)`,
`assertNull(actual)`, `assertContains(list, element)`,
`assertContains(string, other)` and `fail(message)`. Each takes a last
`message: String?`, which may be left out. One that fails panics
(section 8) with Kotlin's message, and so fails its test:
`assertEquals(3L, 2L)` with `Expected <3>, actual <2>.`, and with a
message, `the sum. Expected <3>, actual <2>.`; `assertTrue` and
`assertFalse` with the message alone where there is one. Values are
compared as `==` compares them and written as `toString()` writes
them. Another of Kotlin's assertions is added when a test first needs
it. There is no `assertFails`: nothing catches a panic.

## 13. Bindings

A binding declares what Go code provides. It compiles to nothing. The
prelude is written as bindings, so its signatures are Kogo source: the
checker reads them like any other file, and so can an author.

```kotlin
external class Http
external fun Http.ask(endpoint: Lookup, fields: List<Field>): Either<HttpFailure, Json>
action external fun Http.perform(endpoint: Action, fields: List<Field>): Either<HttpFailure, Json>

external object Uuid {
    fun parseOrNull(text: String): Uuid?
}

external class List<out T>
external fun <T> listOf(vararg elements: T): List<T>
external val <T> List<T>.size: Int
external fun <T, R> List<T>.map(transform: (T) -> R): List<R>
external fun <T> List<T>.firstOrNull(): T?
external fun List<Long>.sum(): Long
```

- `external object` holds functions called on its name:
  `Uuid.parseOrNull(text)`. They have no effect: an object is reachable from
  anywhere, and section 7 has effects arrive as parameters.
- A binding with an effect, an outbound call, takes a handle: an
  external class such as `Http`, passed to the function that calls out
  as `Tx` and `Db` are, so the signature shows it. The request's
  deadline (15.5) is on its `Db`; `Http` does not carry it yet, and has
  its own `Timeout` alone. Nothing checks that a binding keeps to this;
  it is the rule for whoever writes one.
- `external class Name<T>` names a type that Go implements. It has no
  body, and Kogo code cannot construct or implement it. An external
  object may share its name, which gives `Uuid.parseOrNull(text)`.
- `external fun` and `external val` declare a function and a property
  with no body. Every member of an external class is declared this way,
  with the class as the receiver. One with a receiver is a member: it is
  in scope wherever a value of that type is, and is never imported.
- Two things are allowed in a binding and nowhere else: `out` on an
  external class's type parameter, and `vararg` on a parameter, which
  then takes any number of arguments. A binding's receiver is not
  nullable either (section 5).
- `action external fun` marks a binding that changes something
  outside (7.1): `Console.println` and `Http.perform` are actions.
  Any other binding says nothing, `Console.readln`, `Clock.now` and
  `Http.ask` among them, and nothing checks that it is right to.
- `@go("package")` names the Go package of the declaration it precedes.

Bindings are written by whoever maintains the prelude. Application code
never contains `external` and never names a Go symbol. (`@go` is the
binding's own syntax, not an annotation in the sense of section 11.)

## 14. Not in Kogo

Each of these is valid Kotlin and a compile error in Kogo.

| Kotlin habit | In Kogo |
| --- | --- |
| `x!!` | `requireNotNull(x) { "why" }`, or handle the null |
| `try`/`catch`/`throw`, `runCatching` | return `Either`, or panic with `check`/`error` |
| `fun Raise<E>.f()`, `context(Raise<E>)` | return an `Either` and `bind` it |
| `class Foo { ... }`, `open`, `abstract`, inheritance | data class, sealed interface, top-level functions |
| `object`, `companion object` | top-level `val`s and functions |
| `object : Iface { ... }` | a data class implementing it, or a lambda |
| `lateinit`, top-level `var` | pass the value as a parameter |
| `apply { }`, `run { }`, `with(x) { }` | name the value and call it |
| `return@label` | `if`/`else` or a local function |
| `return` inside a lambda, as `xs.forEach { if (it < 0) return false }` | end the lambda with its value; return after the call |
| `suspend`, coroutines, `Flow`, threads | plain blocking code |
| `Sequence`, `generateSequence`, `MutableList`, arrays | a `while` loop with a local `var`, or the list functions |
| `bytes[i]`, `bytes.size`, `byteArrayOf(...)`, `a == b` on a `ByteArray` | none: a `ByteArray` is carried, and a binding reads it (section 3) |
| `a == b` on a `Regex`, or on a value that holds one (`regex-equality`), which Kotlin compares by identity | compare the patterns, as strings; where check cannot see one, `==` panics (section 3) |
| `a == b` on a handle (`Db`, `Clock`, ...), or on a value that holds one (`handle-equality`), which Kotlin compares by identity | compare the other properties; where check cannot see one, `==` panics (section 3) |
| `java.time.*`, `java.util.UUID`, `BigDecimal` | `kogo.time.*`, `Uuid`, `Decimal` |
| `kotlin.test.*` | `kogo.test.*` |
| `BigDecimal.valueOf(x).stripTrailingZeros().toPlainString()` | `plain(x)`, from `kogo.text`, not `Decimal` |
| `OffsetDateTime`, `LocalDateTime` | `Instant`, or `ZonedDateTime` with a named zone |
| jOOQ (`ctx.select...`, records, `.store()`, `into(X::class.java)`) | `sql"""` and a data class |
| `X::class`, reflection | nothing; the only `::class` is inside `@file:OptIn(...)` |
| annotations other than markers | nothing |
| `String.format`, `Locale` | string templates, `padStart`, or `plain`, or a prelude function |
| `by lazy`, delegation | compute it once and pass it |
| `vararg`, `*spread` | a `List` |
| `fun T?.f()`, `x.orEmpty()`, `x.isNullOrBlank()` | a parameter; `x ?: emptyList()`; `x == null \|\| x.isBlank()` |
| `inline`, `reified`, `operator`, `infix` | a plain function |
| `println`, `readln` | a `Console` parameter, from `kogo.io` |
| value classes | `typealias`, in v0 |
| packages that import each other, directly or through others | move what one takes from the other into a package both may import |
| `fun <T> f(...)` or a default value in an interface's `fun` | a top-level fun that takes the value, or the argument at each call |
| `fun <T> f(...)` inside a function | declare it at the top level |
| `val n = 3000000000`, a `Long` | `3000000000L` |
| a top-level `val` that reads one below it through a function | declare the one it reads above it |
| two files whose top-level `val`s use each other's files | move the `val`s into one file, or make one a fun |
| `==` on a value that holds a function, as `rule == rule` | compare the other properties |
| two funs of one name imported from two packages | `import ... as` on one |
| an import of a name the file's own package declares | `import ... as`, or rename one |
| a call to a fun after a local of its name, as `val job = job(id)` then `job(2)` | name the local otherwise (section 6) |
| `a.b.Name` written in code | import it and write `Name` |
| `import a.b.Outer.Inner` | `import a.b.Outer`, and write `Outer.Inner` |
| a constructor or a constant through a typealias, `PRow(1)` | the type's own name |
| a type nested in an interface as a namespace, `GoogleMaps.Place` | a top-level type, `GoogleMapsPlace` |

## 15. Operations

An operation is a function that a browser calls: JSON in, JSON out. A
program names its operations in an `api` list, and from the list the
compiler writes the server's table and the browser's TypeScript types.

```kotlin
api ops checkedBy fromOurPage {
    me             ungated
    updateJob      ungated
    completeLogin  ungated
}

fun updateJob(tx: Tx, env: OpsEnv, request: Request, input: UpdateJobInput): Either<String, DriverJob> = either {
    val driver = requireDriver(tx, request).bind()
    val job = requireJob(tx, driver, input.number).bind()
    ensure(job.state != JobState.Complete) { "This job can no longer be changed." }
    tx.execute(sql"update job set driver_notes = ${input.notes} where id = ${job.id}")
    DriverJob(job.number, job.state, input.notes)
}
```

### 15.1 The list

```kotlin
api desk checkedBy fromOurPage {
    unpaid       gatedBy atDesk
    markPaid     gatedBy atDesk
    testPrinter  gatedBy mayPrint
    looks        ungated
}

api hooks unchecked {
    forgetLooks  ungated
}
```

- `api name checkedBy check { rows }` is a top-level declaration, the
  third thing Kogo adds to Kotlin. Its words are bare: `api`,
  `checkedBy`, `unchecked`, `gatedBy` and `ungated` are names
  like any other elsewhere, a fun's in a list among them.
- Every fun a list names is a top-level fun in scope where the list
  is written, its own package's or an imported one, and one fun: not
  two funs of that name in scope.
- The head names the list's check, `checkedBy f`: the fun that is to
  run before every row of the list, ungated ones included. A list
  with no check says `unchecked` in its place, and is served to no
  browser: its table is a Go type that the server's handler does not
  take. A head with neither is a compile error.
- A row is an operation and who may call it, in one of two forms.
  With what the operation's fun is (15.2), that is four kinds. Take
  the first that does the job:

  | The operation | Write | Transactions (15.3) | May ask outside | May change outside |
  | -- | -- | -- | -- | -- |
  | reads and writes our database | `op gatedBy gate` | one: the gate, then the work | in the gate or the work; the transaction waits | nowhere |
  | changes something outside and writes nothing of ours | `op gatedBy gate`, with `op` an `action fun` | one for the gate; none for the work | anywhere | in the work |
  | finds out who is asking itself | `op ungated`, taking a `SerializableTx` or a `Tx` | one | in it; the transaction waits | nowhere |
  | finds out who is asking itself, and must keep a write after a refusal or write what an action answered, or asks outside with no transaction open | `op ungated`, taking a `Db` | its own, each a `db.tx` | anywhere | between its transactions |

  - A change outside that can wait, or must not be lost, is not made
    by the request: the operation writes a row, as the first line
    does, and what serves that row makes the change later.
  - An operation that must ask outside asks in its transaction and
    keeps its gate: the first line. The transaction and its
    connection wait for the answer, and a rerun asks again. Where
    many requests would wait so at once, as a search box asking on
    each keystroke would, write the last line: ask between two
    transactions, and find out who is asking by hand.

  A row that neither names a gate nor says `ungated` is a compile
  error, and so is a row with any word after its gate or `ungated`:
  the error says what a row is.
- A list's rows are separated by a comma, a new line or both, as
  Kotlin separates a collection's, with a comma allowed after the
  last. A row stays on one line: a line that begins with `gatedBy` or
  `ungated`, or with a gate's name, is a compile error that says
  which row to move it onto. A line that begins with one of the two
  words and goes on with `gatedBy` or `ungated` is the row of an
  operation of that name.
- An operation's fun's name is its name on the wire, so a list holds
  no operation twice. A fun may stand in more than one list, and a
  gate that reads no input in more than one row (15.2).
- A list names at least one operation. Its name is its TypeScript
  file's (15.6), so no two lists of a program have one name, or names
  that differ only in case, which are one file where case is ignored.
  No list is named `outcome` or `Outcome`, whose interface would take
  the answer's type's name.
- Each of these is an ordinary fun: any code may call it, and nothing
  but the list marks it.
- `kogo check`, `kogo ts` and `kogo emit` read every form.

### 15.2 The shapes

Each fun a list names has the shape of its place in the list. Every
one returns an `Either` whose `Left` is a refusal in words:

| Place | Parameters, in this order | Returns |
| --- | --- | --- |
| the check | a `Tx`, the environment, a `Request` | `Either<String, Unit>` |
| a gate | a `SerializableTx` or a `Tx`, the environment, a `Request`, and the operation's input if it reads it | `Either<String, P>`, with `P` its answer: a call (15.7), or facts |
| gated work, the `op` of a gated row | the environment, what its gate answers, the input | `Either<String, A>` |
| an ungated operation | a `SerializableTx`, a `Tx` or a `Db` (15.3), the environment, a `Request`, the input | `Either<String, A>` or `Either<String, Reply<A>>` |

- The environment is one type of the application's, the same for
  every fun of a list, its check and gates included: a data
  class of what lives as long as the server, the clock and the handles
  of outbound calls (sections 7 and 13).
- A `Request` is `kogo.rpc.Request` (section 12): the caller's
  cookies, headers and address.
- The input is a wire type (15.4), or `Unit` for none, and `A` is a
  wire type, or `Unit` for an operation with nothing to say.
- What one fun answers, the next takes. What gated work takes second
  is the very type its row's gate answers; the input of a gate that
  reads one is the operation's input type. Anything else is a compile
  error.
- A gate reads and decides; it does not write. A statement that a
  gate reaches, in its own body or through the funs it calls or names,
  and that inserts into, updates or deletes from a table or a view is
  a compile error, `gate-writes`, and so is one that locks rows (`for
  update`, `for share`) where the gate takes a `SerializableTx`; a
  gate on a `Tx` may lock. A utility statement, as `truncate` or
  `lock table`, is `sql-utility` wherever it is (10.7), a gate's
  included; a gate binds the tenant, a setting, with `set_config`.
  Postgres
  says what each statement needs of each table, as it types the
  statement (section 10). It does not say what a function the
  statement calls writes, nor `nextval`: 15.3. A call through a value
  of an interface's type is not followed to the classes that override
  the fun.
- A gate that reads no input may stand in many rows, whose operations
  have no gate of their own. A gate that reads an input stands in one
  row of a list.
- Gated work takes no `Request`, `Tx` or `Db`: its gate has read the
  request, and the gate's answer is all it is handed. So a gate
  answers none of the three either (`gate`). It answers a call
  (15.7), a data class of the application's that holds the
  transaction in a plain property beside what the gate found, or the
  facts alone, as `data class Enrolling(val subject: String)`.
- A row whose work is an `action fun` names a gate whose answer holds
  no transaction (`action-gate`, 15.7): the work runs after that
  transaction has ended (15.3), so its gate answers the facts the
  work needs.
- An ungated operation takes no gate's answer.
- The parameters' names are the author's. None of these funs has type
  parameters or default values.
- A `Left` is a refusal: words for the person using the page. Each
  turns any other failure into words, with `mapLeft`, before it
  returns.
- Nothing but the list's check runs before an ungated operation. It
  finds out who is asking in its own first lines, from the `Request`
  it was given.
- An ungated operation that answers a `kogo.rpc.Reply<A>` sets
  cookies: the server sends its `answer` as the operation's answer,
  and each of its `cookies` as one `Set-Cookie` header, in order. A
  refusal sets none. `A` is the operation's answer on the wire and in
  TypeScript (15.4, 15.6), which never see the `Reply`. A `Reply`
  anywhere else on the wire is a compile error: in an input, inside an
  answer, or as gated work's answer, so a search for `Reply` finds
  every operation that sets a cookie.
- A cookie's `maxAgeSeconds` is its `Max-Age`; 0 or less ends it, as
  `Max-Age=0`. A cookie that HTTP cannot carry, such as one whose name
  holds a space, or one a browser refuses, such as `SameSite.None`, a
  name that starts `__Secure-` or one that starts `__Host-` without
  `secure` (`__Host-` needs the path `/` too), is a bug, and panics
  (section 8).

```kotlin
fun signOut(tx: Tx, env: SessionEnv, request: Request, input: Unit): Either<String, Reply<Unit>> = either {
    ensure(request.cookie("session") != null) { "You are not signed in." }
    Reply(Unit, listOf(Cookie("session", "", "/", 0L, SameSite.Lax, env.secure, true)))
}
```

### 15.3 The transaction

- The list's check runs first, once for a request: before the
  operation's name, the method and the body are read, in a transaction
  of its own that Postgres holds to reading and the server rolls back.
  Its refusal is not an operation's (15.5), and nothing else runs.
- A gated row runs in one transaction, opened as an ungated
  operation's `Tx` is: the gate, then the work with what the gate
  answered.
  An answer commits it, a refusal of either rolls it back, and a rerun
  runs the gate again.
- A gated row whose work is an `action fun` (7.1) runs its gate once,
  in a transaction of its own that the server commits, and then the
  work with no transaction open. The work's answer or refusal is the
  request's. The gate's transaction has ended before the work
  begins, so the gate answers facts and nothing that holds a
  transaction (15.7).
- A gate on a `SerializableTx` runs held to reading, each time it
  runs: in a savepoint that Postgres holds read-only and the server
  releases before the work. The server runs a statement before the
  savepoint, so that the transaction's first snapshot is not taken
  read-only: Postgres may then leave the whole transaction out of its
  serialization checks, also after it writes: it does when no other
  writing transaction at SERIALIZABLE is open as the first snapshot is
  taken. There a write, one inside a function of the database's too,
  `nextval` and a row lock are each a bug, and a panic (section 8). A
  setting the gate makes with `set_config` is allowed, and is still
  set in the work, which writes as before: a gate binds the tenant so.
  An advisory lock and a write to a temporary table pass, since
  Postgres's read-only mode allows them, and neither this nor
  `gate-writes` refuses them. A gate on a `Tx` is not run so, since
  Postgres's read-only mode refuses the lock such a gate may take: the
  compile error of 15.2 alone holds it, and a write inside a function
  of the database or `nextval` in such a gate is not refused.
- The first parameter of the gate, or of an ungated operation that
  takes a transaction, names the kind the server opens (10.7): at
  SERIALIZABLE for a `SerializableTx`, at READ COMMITTED for a `Tx`.
  Each transaction a row opens is of that kind: the gate's before an
  `action fun` too. A list's check takes a `Tx`.
- An operation that takes a `SerializableTx` or a `Tx` runs in one
  transaction, which the server opens as above. An answer commits it.
  A refusal rolls it back, so a refused request has changed nothing. A
  panic rolls it back (section 8). On a serialization failure or a
  deadlock, which at READ COMMITTED is a deadlock alone, the server
  rolls back and calls the operation again from the start, so it must
  have no effect outside the transaction, as the block of `db.tx`
  must not (10.7).
- An operation that takes a `Db` runs in no transaction and opens its
  own with `db.tx` (10.7). Each ends with its block, which answers
  facts and nothing that holds a transaction (15.7). Write one where
  something must stay after a refusal, as the count of a failed login
  does, or must happen once, as redeeming a code with another service
  must.

```kotlin
action fun completeLogin(db: Db, env: OpsEnv, request: Request, input: CompleteLoginInput): Either<String, DriverAccount> = either {
    val attempts = db.tx { tx -> countAttempt(tx, request.clientAddress) }.bind()
    ensure(attempts <= 5) { "Too many attempts. Try again later." }
    val user = redeemCode(env.idp, input.oneTimeCode).bind()
    db.tx(serializable = true) { tx ->
        val driver = claimDriver(tx, user) ?: raise("There is no driver account for that address.")
        DriverAccount(driver.name)
    }.bind()
}
```

### 15.4 The wire

Inputs and answers are JSON.

| Kogo | JSON | TypeScript |
| --- | --- | --- |
| `Boolean` | `true` or `false` | `boolean` |
| `Int`, `Long`, `Double` | a number | `number` |
| `String` | a string | `string` |
| `Uuid` | a string, as `toString()` writes it | `string` |
| an enum | its constant's name | `"Offered" \| "Confirmed"` |
| a data class | an object with a key for each property, under its name | an object type |
| `List<T>` | an array | `T[]` |
| `T?` | `null`, or `T` | `T \| null` |
| `Unit` | `{}` | `{[k: string]: never}` |

- Any other type in an input or an answer is a compile error: a `Map`,
  a `Set`, a `Pair`, a `Decimal`, a type of `kogo.time`, a function, an
  external class. Send text that the server has formatted. A
  `ByteArray` is one too: bytes stay in a row, and an operation answers
  what the page shows of them.
- A sealed interface, or a variant of one, is a compile error too: send
  a data class, with an enum that says which case it is.
- `Unit` stands only as the whole input or the whole answer.
- A data class on the wire has no type parameters.
- An input that is or holds a data class whose constructor is marked
  (11), private or internal (2) is a compile error, since a caller
  would make what the program lets only its own code make: take the id
  in the input and make the value with the function that checks it. An
  answer may hold one.
- Two wire types of one name in one list are a compile error, and so
  is one named `Outcome`, the answer's own type in TypeScript, or one
  named as its list's interface (15.6), `Ops` in `api ops`.
- A wire type's name begins with an upper-case letter, so it is none
  of the names TypeScript keeps for its own, as `string` and `never`.
- Nothing is optional: a property that may be `null` is still a key.
- A string is written as Kotlin's `kotlinx.serialization` writes one:
  `"` and `\` escaped, a character below U+0020 as `\b`, `\t`, `\n`,
  `\f`, `\r` or `\u00xx` in lower-case hex, and every other character
  as itself in UTF-8, `&`, `<`, `>` and U+2028 among them.
- A body is UTF-8, and a `String` never holds half of a surrogate pair
  (section 3): a body that is not UTF-8, or an input's `"a\ud800b"`,
  is a 400, `the input is not UTF-8` or `the input holds half of a
  surrogate pair`, and the operation does not run. Read on, either
  would hold U+FFFD in its place, and store another text than was
  sent.
- A `String` on the wire holds no U+0000, which Postgres's `text`
  cannot hold (10.4): an input's `"a\u0000b"` is a 400, `input.name
  holds U+0000`, whether or not the operation would send it to
  Postgres. It is a limit of the subset, where Kotlin reads the string.
- Reading is strict. A key that is missing, a key the type does not
  have, a value of another type, a number that is not whole or does
  not fit where an `Int` or a `Long` is wanted, a string that is not a
  `Uuid` or not one of an enum's names: each is a 400, and the
  operation does not run. A whole number is written as digits, as
  JavaScript writes one: `1.0` and `1e3` are not whole. Where the
  input is `Unit` the body is not read.
- A body past the list's limit, 64 KiB unless `main` sets another
  where it mounts the list, is a 413, `updateJob: the input is past 65536
  bytes`, and the operation does not run.
- A `Long` crosses only from -9007199254740991 to 9007199254740991,
  the whole numbers JavaScript holds exactly. Reading one outside them
  is a 400; an answer that holds one is a bug, and panics. Send a
  larger number as a `String`.
- A `Double` that is `NaN` or an infinity has no JSON: an answer that
  holds one panics.
- An application's settings are read this way too. They are a data
  class the application declares, which its environment holds as one
  property, and one object of its configuration file, a key for each
  property. Nothing defaults: a key that is missing or of another
  type, or a `String` that holds U+0000, stops the start with an
  error that names the key and never its value,
  `settings.opsOrigin is missing`. It differs from an input
  twice: a key the class does not have is reported and ignored, so
  that one file serves the program before and after a deploy; and an
  enum or a data class of the prelude is no setting yet, and an error
  at the start.

The answer is one of two objects:

```json
{"type": "ok", "value": {"number": 41, "state": "Confirmed", "notes": null}}
{"type": "refused", "message": "This job can no longer be changed."}
```

### 15.5 The boundary

Each operation is served by `POST` at its name, under a path the
application chooses.

| What happened | Status | Body |
| --- | --- | --- |
| the operation answered | 200 | the `ok` object, and a `Reply`'s cookies |
| the operation or its gate refused | 200 | the `refused` object |
| the list's check refused, whatever else is wrong | 403 | the check's words, as text |
| the input does not read (15.4) | 400 | the operation and the key, never the value |
| a header read is not UTF-8 (12) | 400 | the operation and the header, never its value |
| the body is past the list's limit (15.4) | 413 | the operation and the limit |
| no operation of that name | 404 | |
| a method other than `POST` | 405 | |
| a panic (section 8) | 500 | the request's id |
| the request did not end within its limit | 503 | the request's id |
| the database cannot be reached, or ended the connection | 503 | the request's id |
| a panic that names a call out that got no answer (12) | 503 | the request's id |
| the client left before the answer | 499, which nobody reads | |

- A request has one limit, from its start to its answer: 90 seconds
  unless `main` sets another. Whatever the request waits on when it
  passes ends there: a connection of the pool, a statement, a lock,
  the pause before a rerun. Its connection goes back to the pool, or
  is closed where the limit cut a statement.
- A rerun (15.3, 10.7) comes after a short random pause, so that two
  requests that conflicted do not meet again in step.
- A client that leaves ends its request as the limit would, and is
  no bug: neither it, nor a database that is down, nor a service that
  does not answer is answered or logged as a panic. A service has not
  answered where a call of the request got no status, refused or past
  its time limit, and the panic's message holds that failure as text,
  as `error("idp: $failure")` does: a later panic of the program's own
  stays a panic, even one whose message holds the bare `why`.

**Nothing a caller can cause by what it sends is a panic.** A panic
is a bug of Kogo's or of the program's: it is answered 500 and logged
with its trace, so one a caller could cause at will would fill the
log and bury the real faults. What a caller can send wrongly is
answered so:

- a body that does not read as the input, one not UTF-8, a `String`
  that holds U+0000 or half of a surrogate pair among it (15.4): the
  reader's 400; one past the limit, 413;
- a header the program reads that is not UTF-8: 400;
- a name no operation has: 404; another method than `POST`: 405;
- no session, a forged or stale cookie, an Origin or a token missing
  or wrong, a header the list needs missing or naming nothing (a
  storefront there is none of): the check's 403 with its words, or
  a gate's refusal;
- anything else wrong in an input that reads, as an id no row has:
  the operation's refusal in words.

An operation, a gate or a check that panics on what a caller sent,
as `querySingle` does on an id the caller chose, has a bug: it
refuses instead, with `ensure` or `?: raise`.

Where Go's `net/http` would decide, the handler (`prelude/kogo/rpc`)
states it, and the test named holds it, by its package:

| What | Go would | Kogo states | Test |
| --- | --- | --- | --- |
| A request's time | set no limit | The request's limit, on all it waits on; past it 503 | `rpc.TestDeadline` |
| A body's length | read any | The list's limit; past it 413 (15.4) | `rpc.TestLimits` |
| A client that leaves | end the context once the body is read, which a Unit operation never reads; the driver gives an error | The body is read whole first: 499 at once, also during the check or a Unit operation, the served line alone | `rpc.TestDeadline` |
| A database that is down, or ended the connection | give the driver's error | 503 with the request's id, one `database` line, no trace | `rpc.TestDeadline` |
| A service that does not answer | panic as the program's `error` does: 500, with a trace | 503 with the request's id, one `service` line with its host and why, no trace | `rpc.TestDeadline` |
| A panic | log it in the server's own words and close the connection unanswered | 500 with the request's id, one `panic` line with its trace; the process lives | `rpc.TestHandler` |
| A method | serve any | `POST` alone; another is 405, with `Allow: POST` | `rpc.TestHandler` |
| An answer's type | guess it from the first bytes | `application/json` for `ok` and `refused`, `text/plain; charset=utf-8` for any other status | `rpc.TestHandler` |
| Reading the input | match a key in any case, take a duplicate, skip an unknown key, read a number as a `float64` | The strict reader of 15.4: 400 | `rpc.TestStrict`, `rpc.TestHandler` |
| Writing a string | escape `<`, `>`, `&`, U+2028 and U+2029 | As Kotlin writes it (15.4) | `rpc.TestQuote` |
| A header's value | give its bytes | One the program reads that is not UTF-8 is 400, naming the header | `rpc.TestLimits` |
| `Host` | keep it out of the headers | `request.header("Host")` gives it | `rpc.TestHandler` |
| A cookie | drop one HTTP cannot carry, with a line in its own log; write no `Max-Age` for 0 | A panic, as for one a browser drops; `maxAgeSeconds` of 0 or less ends the cookie, `Max-Age=0` | `rpc.TestHandler`, `cmd/kogo.TestEmittedGoRuns` |

Where the driver, pgx, or its pool would decide, the database handle
(`prelude/kogo/db.go`) states it, or requires it of the pool, which
the application makes and hands over. `NewHandler`, `NewCalls` and
`test.Run` check the pool, and one that fails a requirement stops
the start with a panic that names the setting. The test named holds
each row.

| What | pgx would | Kogo states | Test |
| --- | --- | --- | --- |
| The isolation | take the database's default | SERIALIZABLE or READ COMMITTED as 15.3 says; SERIALIZABLE for a test (16.2) | `rpc.TestHandler`, `kogo.TestRolledBackIsSerializable` |
| A conflict | give the error | A rerun as 15.3 says; `db.tx` at READ COMMITTED reruns on a deadlock alone | `rpc.TestHandler`, `kogo.TestPlainTxRerunsDeadlock` |
| The pause before a rerun | have none | Random, to 5 ms after the first conflict and 5 ms more after each, to 50; the request's limit ends it | `rpc.TestHandler` |
| A wait for a connection | set no limit | The request's limit | `rpc.TestDeadline` |
| An empty `List` sent | send Go's nil slice as `NULL` | An empty array | `emit.TestValues` |
| A value Postgres sends | read what it can | The subset of 10.4; outside it a refusal or a panic that names the limit | `emit.TestShapes`, `emit.TestLimits` |
| The session's time zone | leave the role's or the database's, which `now()::date`, `date_trunc` or a cast to text reads | UTC, required: the pool sets the run-time parameter `timezone`, in any case, to `UTC` | `kogo.TestCheckPool`, `rpc.TestPool`, `test.TestRunChecksPool` |
| A connection's longest life | close it after an hour, or never for 0 | Required above 0: a connection keeps the types it was told of (10.4) until it closes | `kogo.TestCheckPool` |
| A connection's idle time, the health check | close one idle 30 minutes, check each minute, and ping one idle over a second before handing it out | Kept, nothing required: a connection the pool closes costs a new one; one the database ended is a 503 | `rpc.TestDeadline` |

### 15.6 TypeScript

`kogo ts <dir> <out>` checks the program as `kogo check` does and,
when it passes, writes `<out>/<name>.ts` for each list: every wire
type its operations reach, in the order of their names; the answer's
type; and an interface, named for the list with its first letter in
upper case, that names each operation's input and value in the
list's order. A program with errors writes nothing.

```ts
// Generated by kogo ts from api ops. Do not edit.

export type Outcome<T> = {type: "ok"; value: T} | {type: "refused"; message: string}

export type DriverJob = {
  number: number
  state: JobState
  notes: string | null
}
export type JobState = "Offered" | "Confirmed" | "Complete"

export interface Ops {
  me: {input: {[k: string]: never}; value: DriverAccount | null}
  updateJob: {input: UpdateJobInput; value: DriverJob}
}
```

The code that calls `fetch` is the application's own, typed by this
file.

### 15.7 A value that holds its transaction

A gate's answer may hold the gate's transaction, in a plain property
beside what the gate found. Such an answer is a call:

```kotlin
// One file makes a call: private is the file (section 2), and covers copy (3.1).
data class Clerk private constructor(val address: String)

data class DeskCall private constructor(val tx: SerializableTx, val clerk: Clerk)

fun atDesk(tx: SerializableTx, env: DeskEnv, request: Request): Either<String, DeskCall> = either {
    ensure(mayUseDesk(tx, request.clientAddress)) { "This address may not use the desk." }
    DeskCall(tx, Clerk(request.clientAddress))
}

// The work: a step answers a held value, and the model function takes it.
fun markPaid(env: DeskEnv, call: DeskCall, input: InvoiceRef): Either<String, Long> = either {
    val invoice = requireOpenInvoice(call, input.number).bind()
    ensure(setStatus(call.tx, invoice, true)) { "Invoice ${invoice.number} is gone." }
    invoice.totalCents
}
```

- A call is an ordinary data class, and its transaction an ordinary
  property. The work reads it, keeps it in a name, destructures it
  and passes it to any function that takes a `Tx`. No rule knows a
  class as a call, and none follows the transaction it holds.
- What makes a call mean that its gate ran is its constructor: private,
  so that only the gate's file makes one (section 2), or marked, so
  that only a file that opts in does (section 11). The same makes a
  held value, which a function answers when it has checked an id of
  the input: a function that takes a held value cannot be called
  where that check was left out.

A value that outlives its transaction holds none. There are two such
places, and each is a compile error:

- **Work that runs outside.** A row whose work is an `action fun`
  (7.1) runs its gate in a transaction that the server ends before
  the work begins (15.3). Its gate answers nothing that holds a
  transaction (`action-gate`): it answers the facts the work needs,
  and what needs the transaction goes in the gate, or the work is
  written a `fun`, which runs in it.
- **A `db.tx` block.** The transaction ends with the block (10.7), so
  the block answers nothing that holds one (`tx-escapes`): it answers
  the facts the lines after it need.

```
api.kg:4:25 action-gate: testPrinter is an action fun, and its gate atDesk answers DeskCall, which holds a transaction, tx, a property of DeskCall: the server ends the gate's transaction before work that is an action fun runs, so name a gate that answers the facts testPrinter needs, or make testPrinter a fun, which runs in the transaction (spec.md 15.7)
login.kg:9:19 tx-escapes: this db.tx block answers DeskCall, which holds a transaction, tx, a property of DeskCall: the transaction ends with the block, so answer the facts the lines after it need (spec.md 10.7)
```

"Holds a transaction" is one meaning for both: the type is a `Tx` or
a `SerializableTx`, or has one in a property or a type argument, at
any depth. A nullable, a `List<DeskCall>`, a `Box<Tx>`, a variant of
a sealed interface and a function's result each count. The type of
the answer alone is read. A lambda that closes over a transaction,
answered as a `() -> Int`, is not refused, nor is a transaction that
a block assigns to a `var` declared outside it.

## 16. Tests

A test is a value: a sentence that says what it shows, and a block
that shows it. A test file names its tests in a `tests` list, and
`kogo test` runs each in a transaction of its own.

```kotlin
package cusoon.invoice

import kogo.test.assertEquals
import kogo.test.test

tests ledgerTests<Env> { cardDiscount, noDiscountOnAccount }

val cardDiscount =
    test("applies the 5% card discount and makes a card invoice due at once") { tx, env ->
        val invoice = issue(tx, env.clock, cardBooking(tx))
        assertEquals(9975L, invoice.totalCents)
    }
```

### 16.1 The list

- A test file is one named `_test.kg`. `kogo check` and `kogo test`
  read it with the rest of its package; `kogo emit` and `kogo ts`
  leave it out, as Go leaves out a `_test.go`, so the program holds
  nothing of its tests (section 2).
- `tests name<Env> { a, b, c }` is a top-level declaration of a test
  file, a list as `api` is (15.1). It names top-level vals of its own
  file, each once and in one list, and at least one. Its entries are
  separated as `api`'s are (15.1).
- `Env` is the environment every test of the list takes: the
  application's, as an operation's is (15.2), or `Unit` for none.
- Each val a list names is a `Test<Env>`, and need not say so: this
  is the one top-level val that states no type (section 5), since its
  list states it. So `env` in the block has the list's type.
- A test that no list names would never run, so it is a compile
  error: a top-level val of a test file that states no type, or that
  states `Test<...>`, stands in a list of its file.
- `test(sentence) { tx, env -> ... }` makes a test. The sentence says
  what the test shows, in the words a failure is reported with. The
  block takes the `SerializableTx` of the test's transaction (16.2)
  and the environment, and gives nothing. So a test calls a fun that
  takes a `SerializableTx` as readily as one that takes a `Tx`.

### 16.2 The run

- `kogo test <dir> <out> <main>` checks the program with its test
  files and, when it passes, writes its Go under `<out>` as `kogo
  emit` does, tests included, then builds and runs the Go main
  package in `<main>`. That main is the application's: it makes the
  pool and the environments, and hands over its id maker if it has
  one (section 7), as the server's main does, and hands the pool and
  environments to the runner, `test.Run`. The runner runs every list the main
  links in, each with the environment of its type; a list with no
  environment of its type, or in a package the main does not import,
  fails with code `no-env` or `not-linked`.
- Each test runs in a transaction of its own, which is rolled back
  when the test ends, however it ends. A test sees the rows the
  database began with and those it wrote, never another test's.
  The rollback covers `tx`; a `Db` the environment holds commits as
  it does in the program.
- The transaction is at SERIALIZABLE, which the runner states and
  does not leave to the database's default, and is run once. A
  test's transaction is SERIALIZABLE and alone; a test that must see
  what another connection commits meanwhile is not written in this
  form yet. Alone means in the runner, which runs one test at a time:
  a `Db` the environment holds is another connection, and the test
  sees what that or any other connection committed before the test's
  first statement and nothing committed after it. A write that meets
  such a commit is a serialization failure, as is a cycle with
  another SERIALIZABLE transaction. Nothing reruns it: a
  serialization failure in a test is a panic that fails the test, as
  any statement Postgres stopped is (10.8).
- The transaction never commits, so a deferred constraint never fires
  in a test, and a test cannot make it fire: `set constraints` is a
  utility statement, which a `Tx` does not run (10.7). A value
  taken from a sequence is not rolled back, so a test does not assert
  one.
- A test passes when its block returns. A panic fails it: a failed
  assertion (section 12), a bug in the code it calls, a statement
  Postgres refuses. Nothing catches a panic (section 8) but the
  runner, once, when the test is over. The tests after it run.
- An expected failure is tested as the value it is: call the fun and
  compare its `Left` (section 9). A statement that Postgres must
  refuse is seen with `tx.refusal` (10.8).
- Each test that failed is printed as one JSON object a line: `file`,
  `line` and `column` of the test's val, `code` (`test-failed`, or
  the list's `no-env` or `not-linked`),
  `test` (its sentence), `message` (the panic's) and `at`, the file
  and line the panic came from. The exit status is 0 when every test
  passed, 1 when one failed, 2 when the tests could not be run.

### 16.3 The layout

Every test is laid out one way: a break after `=`, `test("…") { tx,
env ->` on the next line, one level in, and the body one level
further. It is what ktfmt makes of the same Kotlin. Kogo has no
formatter, so `examples/23_test.kg` is the rule.
