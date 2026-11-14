# Kogo v0 specification

Draft of 2026-11-15, fitted to cusoon2. Kogo is a small language in Kotlin's
syntax that compiles to Go. This document is the whole language: a construct
that is not described here does not exist, even when Kotlin has it. Section 14
lists the Kotlin habits that are most often reached for by mistake, each with
its replacement.

Source files end in `.kg`.

## 1. The one rule

Where a construct is valid in both Kotlin and Kogo it means the same thing.
Kogo adds exactly two things that Kotlin lacks, and both look unlike Kotlin:
the `sql"""..."""` literal (section 10) and the `external` binding
declarations (section 13).

## 2. Files, packages, visibility

```kotlin
package cusoon.model

import cusoon.pricing.Cents
import kogo.time.Instant
```

- One `package` line, then imports, then declarations. No wildcard imports,
  no `import ... as`.
- A file holds any number of top-level declarations: functions, `val`
  constants, types, typealiases, marker annotations.
- `private` means this file. `internal` means this package (directory).
  No modifier means public. There is no `protected`.
- Comments: `//`, `/* */` and `/** */`. KDoc is kept as a comment.

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
| `T?` | `T` or `null` |
| `(A, B) -> R` | function type |
| `Either<E, A>` | a failure `E` or a value `A`, section 9 |

Numeric literals: `1` is `Int`, `1L` is `Long`, `1.0` is `Double`,
`10_000` allowed. There are no implicit numeric conversions; write
`.toLong()`, `.toInt()`, `.toDouble()`. `Int / Int` and `Long / Long`
truncate toward zero. `Long * Int` and the other mixed `Int`/`Long`
operators give `Long`, as in Kotlin.

Equality `==` is structural for every type. There is no `===`.

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
  are provided.
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
declared in the same file. `out` on a type parameter of a sealed interface
is the only variance annotation in the language.

### 3.3 Enums

```kotlin
enum class InviteType { InHouse, Outside }
```

Constants only: no constructor, no body. `.name` gives the constant's name
and `InviteType.entries` the list of constants.

### 3.4 Interfaces

```kotlin
interface Regime<K> {
    val label: String
    fun card(key: K): Card
}
```

An interface declares abstract `val`s and `fun`s. No default bodies. Only
data classes and `data object`s implement interfaces. There are no other
classes, no inheritance, no `object`, no `companion object`, no anonymous
`object :` expressions. Use a lambda where Kotlin would use an anonymous
object.

### 3.5 Typealias

`typealias Cents = Long` is transparent, as in Kotlin.

## 4. Absence

There is one absent value, `null`, and it only inhabits `T?`.

- `x?.f()`, `x ?: y`, `x ?: return y`, `x ?: raise(e)`.
- Smart casts: after `if (x != null)`, after `x ?: return`, and inside
  `x?.let { }`, a `val` of type `T?` is a `T`. A `var` is never smart cast.
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
  getter. Local functions are allowed inside a function body.
- Every top-level function states its parameter types and its return type.
  The return type may be omitted only for `Unit`. Inference works inside a
  function body only.
- Default arguments and named arguments as in Kotlin. No `vararg`.
- Generic functions take plain type parameters: no bounds, no `where`, no
  `reified`, no `inline`.
- Overloading by parameter types is allowed.
- No `operator`, `infix`, `tailrec` or `suspend`.
- Lambdas: `{ a, b -> ... }` and `it`. A lambda is a value of a function
  type. Trailing-lambda call syntax as in Kotlin. Function references
  `::name` to top-level functions are allowed.
- Lambdas with receivers (`T.() -> R`) do not exist, with one exception:
  `Raise<E>` (section 9).

## 6. Statements and expressions

- `val` everywhere. `var` only for locals inside a function body.
  Top-level `val` and `const val` hold constants; there is no top-level
  `var` and no other global state.
- `if`/`else` and `when` are expressions.
- `when (x) { ... }` over a sealed interface, an enum or a `Boolean` must
  cover every case or have `else`; the compiler rejects a missing case.
  `when { cond -> ... else -> ... }` requires `else`. Branch conditions:
  constants, `is Type`, `in range`, comma-separated alternatives.
- `is` checks and smart casts work on sealed interfaces.
- Loops: `for (x in list)`, `for (i in 0 until n)`, `for ((k, v) in map)`,
  `while`. `break` and `continue` without labels.
- `return` leaves the enclosing `fun`. Labelled returns (`return@name`) do
  not exist; restructure with `if`/`else` or a local function.
- Ranges: `a..b`, `a until b`, `x in a..b`.
- String templates `"$name"` and `"${expr}"`. Raw strings `"""..."""`.
- Operators: `+ - * / %`, `== !=`, `< <= > >=`, `&& || !`, `+=` and
  friends on a local `var`.
- Scope functions: `let`, `also` and `takeIf` only. `apply`, `run`, `with`
  and `takeUnless` do not exist.
- `to` builds a `Pair`: `a to b`.

## 7. Effects

A function can only do what its parameters let it do.

- There are no globals, singletons or ambient state. The clock, the
  database and outbound calls arrive as parameters.
- `Tx` is an open database transaction and `Db` is a connection pool
  (section 10). A function that touches the database has one of them as a
  parameter.
- `Clock` gives the time: `clock.now(): Instant`.

## 8. Bugs are panics

A broken invariant is not an error value.

```kotlin
require(amountCents > 0) { "payment amount must be positive" }
check(updated == rows.size) { "the eligible fees changed" }
val share = checkNotNull(invite.driverSharePercent) { "an outside invite has a share" }
error("unreachable")
```

`require`, `check`, `requireNotNull`, `checkNotNull` and `error` panic, as
do integer division by zero, an index out of range and `single()` on a
list that does not hold exactly one element. Code cannot catch a panic.
The server catches it at the request boundary, rolls the transaction back,
logs it and answers 500.

There is no `try`, `catch`, `finally`, `throw`, `runCatching` or
`Either.catch`, and no exception types.

## 9. Expected failures

An expected failure is a value. There are two spellings of a fallible
function and they are the same thing underneath.

```kotlin
// Spelling 1: return an Either.
fun parsePercent(text: String): Either<String, Int> = either {
    val n = text.toIntOrNull() ?: raise("Enter a number.")
    ensure(n in 0..100) { "Enter a percentage from 0 to 100." }
    n
}

// Spelling 2: a Raise receiver. The return type is the success type.
fun Raise<String>.requirePercent(text: String): Int {
    val n = text.toIntOrNull() ?: raise("Enter a number.")
    ensure(n in 0..100) { "Enter a percentage from 0 to 100." }
    return n
}
```

Inside an `either<E, A> { }` block or a `Raise<E>` function (a *raise
scope* for `E`):

| Form | Meaning |
| --- | --- |
| `raise(e)` | fail now with `e`; type `Nothing` |
| `ensure(cond) { e }` | fail with `e` unless `cond` |
| `ensureNotNull(x) { e }` | `x` as non-null, or fail with `e` |
| `x.bind()` | `x: Either<E, A>` gives its `A`, or fails with its `E` |
| calling a `Raise<E>` function | runs it; its failure is this scope's failure |
| `withError({ e2 -> e }) { ... }` | runs a block that is a raise scope for `E2`, converting its failure |

Outside a raise scope a `Raise<E>` function cannot be called; wrap the
call in `either { }`.

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

The error type `E` is any type. cusoon2 uses `String` (words for the
user) at the RPC boundary and sealed interfaces elsewhere.

Nothing else from Arrow exists: no `Option`, `NonEmptyList`,
`zipOrAccumulate`, `recover`, `nullable { }`, `Ior` or `Validated`.

## 10. SQL

SQL is written as Postgres SQL in a `sql"""..."""` literal and is checked
against the real schema when the program is compiled.

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
- A statement that fits on one line may be written `sql"..."`. Neither
  form has escapes.
- A `sql"""` literal may only appear as the argument of one of the five
  calls below. It is not a `String`, cannot be stored, concatenated or
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

`insert`/`update`/`delete ... returning` go through the query calls.

### 10.3 Result types

`R` is a scalar type when the statement returns one column, otherwise a
data class.

- Columns are matched to the data class's properties **by name**: column
  `legal_name` fills property `legalName` (snake_case to camelCase). Every
  property must be filled and every column used; give a column a name with
  `as`.
- A property may itself be a data class, written with a nullable or
  non-null type. It is filled from the columns whose names start with the
  property's name and an underscore:
  `select sa.driver_share_percent, b.id as business_id, b.legal_name as business_legal_name`
  fills `data class Supplier(val driverSharePercent: Int, val business: BusinessName)`.
  A nullable nested data class is `null` when all of its columns are null
  (the absent side of an outer join).
- `select t.*` is allowed when the data class has exactly the table's
  columns. `kogo schema` writes `schema.kg` with one such data class per
  table and one enum per Postgres enum; it is ordinary source, checked in.

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
| an enum type | the enum of the same constants in `schema.kg` |
| `T[]` | `List<T>` |
| a domain | its base type's mapping |

A parameter's Kogo type must map to the type Postgres infers for it. When
Postgres cannot infer one, cast it: `$active::boolean`. A parameter or a
result column of a type that is not in the table is a compile error: cast
it in the statement.

### 10.5 Nullability

- A result column is non-null only when it is a plain reference to a
  `NOT NULL` column of a table that is not on the nullable side of an
  outer join. Every other column (expressions, aggregates, outer-joined
  columns) is nullable.
- The reference may pass through a subquery in `from` or a `group by`.
  A column of a CTE, a view or a `union` is nullable, and so is one
  nulled by a grouping set. A `where` that excludes the null rows of an
  outer join does not make its columns non-null.
- Override by naming the column `as "name!"` (assert non-null; a null at
  run time panics) or `as "name?"` (force nullable).
- A non-null column may fill a `T?` property. A nullable column filling a
  `T` property is a compile error.
- A parameter may always be `T?`; it is sent as SQL `NULL`.

### 10.6 Idioms that replace a query builder

- Optional filter: `where ($active::boolean is null or td.active = $active)`.
- A list: `where id = any($ids)`.
- A choice between two statements (with and without `for update`, two
  sort directions): write both literals in an `if`/`else`.

### 10.7 Transactions

```kotlin
fun checkout(db: Db, cart: Cart): Either<OrderError, Order> =
    db.tx { tx ->
        val order = placeOrder(tx, cart).bind()
        chargeCard(tx, order).bind()
        order
    }
```

- `db.tx { tx -> ... }` runs the block in one transaction. The block is a
  raise scope when the enclosing function returns `Either`. It commits
  when the block finishes, and rolls back on a failure or a panic.
- `db.tx(serializable = true) { ... }` runs at SERIALIZABLE and reruns the
  block from the start on a serialization failure or deadlock, so the
  block must have no effect outside the transaction.
- `tx.atomically { ... }` runs a nested block in a savepoint and rolls
  back to it on a failure.

## 11. Markers

A marker is a named permission that a declaration demands of the file
that uses it. It is the one annotation form in the language, and it is
spelled as Kotlin's opt-in so that cusoon2's access walls port unchanged.

```kotlin
@RequiresOptIn
annotation class UnscopedRead

@UnscopedRead
fun business(tx: Tx, id: ID): Business? = ...

data class HeldBusiness @UnscopedRead constructor(val business: Business, val storefront: Tenant?)
```

A file may use a marked function, property or constructor only if it
begins with `@file:OptIn(UnscopedRead::class)`. Anything else is a compile
error. Markers do nothing at run time. No other annotation exists.

## 12. Prelude

The prelude is a library, not part of the core. Names match Kotlin's
standard library and `java.time` where the meaning is the same.

**Collections** (immutable; "changing" one returns a new one).
`listOf`, `emptyList`, `setOf`, `mapOf`, `size`, `isEmpty`, `isNotEmpty`,
`contains`/`in`, `list[i]`, `map[k]` (gives `V?`), `first`, `firstOrNull`,
`last`, `lastOrNull`, `single`, `singleOrNull`, `map`, `mapIndexed`,
`mapNotNull`, `flatMap`, `filter`, `filterNot`, `filterNotNull`, `any`,
`all`, `none`, `count`, `sum`, `sumOf`, `maxOrNull`, `minOrNull`,
`sortedBy`, `sortedByDescending`, `sorted`, `reversed`, `distinct`,
`take`, `drop`, `zip`, `plus` (`+`), `groupBy`, `associateBy`,
`associate`, `toSet`, `toList`, `orEmpty`, `joinToString`, `forEach`,
`keys`, `values`, `listOfNotNull`. A `Map` and a `Set` iterate in
insertion order.

There are no mutable collections, sequences or arrays. Build a list with
`map`/`filter`/`+`, or accumulate into a local `var` of an immutable list.

**Strings.** `length`, `isEmpty`, `isBlank`, `isNotBlank`,
`isNullOrBlank`, `ifEmpty`, `trim`, `trim(char)`, `lowercase`,
`uppercase`, `startsWith`, `endsWith`, `contains`, `substring`, `replace`,
`split`, `padStart`, `toIntOrNull`, `toLongOrNull`, `equals(other,
ignoreCase = true)`, `filter`, `filterNot`, `map` (over `Char`), and on
`Char`: `isWhitespace`, `isDigit`, `digitToInt`. `Regex(pattern)` with
`matches`, `containsMatchIn`, `find` and `String.replace(regex, text)`;
the pattern syntax is RE2 (no lookaround, no backreferences). Case
conversion never reads a locale.

**Numbers.** `coerceAtLeast`, `coerceAtMost`, `coerceIn`, `abs`, `max`,
`min`, `toInt`, `toLong`, `toDouble`, `toString`.

**Time** (`kogo.time`). `Instant`, `LocalDate` (`plusDays`, `year`,
`monthValue`, `dayOfMonth`, `atStartOfDay(zone): Instant`), `LocalTime`,
`ZoneId.of(name)`, `ZonedDateTime` (`instant.atZone(zone)`, `hour`,
`minute`, `toLocalDate`, `plusSeconds`, `toInstant`), `Duration`
(`ofDays`, `ofMinutes`, `ofSeconds`), `Clock.now()`. There is no default
zone: every conversion names its zone.

**Ids.** `Uuid`, `Uuid.parse(text): Uuid?`, `newId(): Uuid` (time-ordered
random).

## 13. Bindings

A binding declares the types of a Go package. It compiles to nothing.

```kotlin
@go("httpx")
external object http {
    fun get(url: String): Either<HttpError, Response>
}
```

Bindings are written once per Go package by whoever maintains the prelude.
Application code never contains `external` and never names a Go symbol.
(`@go` is the binding's own syntax, not an annotation in the sense of
section 11.)

## 14. Not in Kogo

Each of these is valid Kotlin and a compile error in Kogo.

| Kotlin habit | In Kogo |
| --- | --- |
| `x!!` | `requireNotNull(x) { "why" }`, or handle the null |
| `try`/`catch`/`throw`, `runCatching` | return `Either`, or panic with `check`/`error` |
| `class Foo { ... }`, `open`, `abstract`, inheritance | data class, sealed interface, top-level functions |
| `object`, `companion object` | top-level `val`s and functions |
| `object : Iface { ... }` | a data class implementing it, or a lambda |
| `lateinit`, top-level `var` | pass the value as a parameter |
| `apply { }`, `run { }`, `with(x) { }` | name the value and call it |
| `return@label` | `if`/`else` or a local function |
| `suspend`, coroutines, `Flow`, threads | plain blocking code |
| `Sequence`, `generateSequence`, `MutableList`, arrays | a `while` loop with a local `var`, or the list functions |
| `java.time.*`, `java.util.UUID`, `BigDecimal` | `kogo.time.*`, `Uuid`, `Decimal` |
| `OffsetDateTime`, `LocalDateTime` | `Instant`, or `ZonedDateTime` with a named zone |
| jOOQ (`ctx.select...`, records, `.store()`, `into(X::class.java)`) | `sql"""` and a data class |
| `X::class`, reflection | nothing; the only `::class` is inside `@file:OptIn(...)` |
| annotations other than markers | nothing |
| `String.format`, `Locale` | string templates, `padStart` |
| `by lazy`, delegation | compute it once and pass it |
| `vararg`, `*spread` | a `List` |
| `inline`, `reified`, `operator`, `infix` | a plain function |
| `println`, `readln` | a `Console` parameter |
| value classes | `typealias`, in v0 |
