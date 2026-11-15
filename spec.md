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
  no `import ... as`. A package is the files that share a package line,
  wherever they lie. An import names one declaration of another package;
  a name of the file's own package needs none.
- A file holds any number of top-level declarations: functions, `val`
  constants, types, typealiases, marker annotations.
- `private` means this file. `internal` means this package. No modifier
  means public. There is no `protected`.
- Comments: `//`, `/* */` and `/** */`. KDoc is kept as a comment.
- The declarations of package `kogo` (section 12, and the functions of
  sections 8 and 9) are in scope in every file. `kogo.time` is imported
  by name.

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
`10_000` allowed; there are no hex, binary or exponent forms. There are no implicit numeric conversions; write
`.toLong()`, `.toInt()`, `.toDouble()`. `Int / Int` and `Long / Long`
truncate toward zero. `Long * Int` and the other mixed `Int`/`Long`
operators give `Long`, as in Kotlin, and `< <= > >=` compare an `Int`
with a `Long`; `==` between them is an error, as in Kotlin. A
whole-number literal is a `Long` where a `Long` is expected (a declared
type, a parameter, a return): `val total: Cents = 0`. Everywhere else it
is an `Int`, so `cents == 0L`. `Int` or `Long` mixed with `Double` is an
error in arithmetic and in comparison, though Kotlin allows it: write
`.toDouble()`, so that the place where a whole number becomes a
floating-point one is visible.

Equality `==` is structural for every type. There is no `===`. One of
its two sides must fit where the other's type is wanted (section 5.1),
`?` aside, so `x == null` passes and `Int == Long` and `String == Int`
are errors.

A `String` counts as Kotlin's does: `length`, `substring`, `padStart`
and reading it as `Char`s count UTF-16 units, so `"😀".length` is 2. It
never holds half of a surrogate pair, though Kotlin's can: an operation
that would leave one, as `"😀".substring(0, 1)`, leaves U+FFFD in its
place, which also counts as one unit.

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
declared in the same file. A nested one is named in full everywhere,
`Outcome.Ok`, also inside the body, where Kotlin allows `Ok`. `out` on a
type parameter of a sealed interface
is the only variance annotation in the language, a binding's external
class aside (section 13).

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
  getter. Local functions are allowed inside a function body. An
  extension's receiver is not nullable: `fun String?.f()` does not exist;
  take the value as a parameter.
- Every top-level function states its parameter types and its return type.
  The return type may be omitted only for `Unit`. Inference works inside a
  function body only, so a top-level `val` states its type too.
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
- Lambdas with receivers (`T.() -> R`) do not exist, with one exception:
  the block of `either { }` and of `withError`, whose receiver is a
  `Raise<E>` (section 9). A function type with a receiver is not
  written: take the value as a parameter, or return an `Either`.

### 5.1 Type arguments

A call to a generic function may write its type arguments, all of them:
`emptyList<Int>()`. Where it writes none, each type parameter is settled
by the first of these that mentions it:

1. the receiver: `xs` in `xs.map { ... }`;
2. the arguments that are not lambdas; when several mention it, their
   common type;
3. the type the result must have: a declared type, a parameter's type,
   the function's return type;
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

### 5.2 Overloads

A call keeps the candidates of its name whose receiver and arguments
that are not lambdas fit: their count, their names and their types. A
lambda fits any parameter of a function type and takes no part in the
choice. Exactly one candidate must be left. None left is an error that
lists the candidates; several left is an error that names them and says
to rename one. Nothing is ranked as more specific, so `f(x: Int)`
beside `f(x: Int?)` cannot be called with an `Int`, though Kotlin picks
the first.

One exception: when the candidates left differ only in what one lambda
parameter returns, as the prelude's `sumOf` does for `Int`, `Long` and
`Double`, the lambda is read once and what it returns chooses.

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
  not exist; restructure with `if`/`else` or a local function.
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
  that value goes straight into a `val`, an assignment or a `return`,
  or is the result of the function or lambda: the right side of `?:`,
  or a branch of an `if` or a `when` there, as `val p = x ?: return y`.
  Buried in a larger expression, as `f(x ?: return y)`, one is an
  error, though Kotlin allows it: bind the value to a `val` first.

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

- Each form fails with a type that fits the scope's `E`: `raise(e)`
  needs an `e` that is an `E`, `bind` an `Either<E2, A>` whose `E2`
  fits `E`, and a `Raise<E2>` function may be called where `E2` fits
  `E`, so one that raises a variant runs where its sealed interface is
  raised. Convert any other failure with `mapLeft` or `withError`.
- A lambda inside a raise scope is in it, as in Kotlin:
  `either { ids.map { load(it).bind() } }`. A local `fun` is not.
  A lambda that fails into the scope is passed straight to the call
  that runs it: kept as a value, in a `val` or as a result, it could
  run outside the scope, and is refused.
- A `Raise<E>` function is called on the raise scope as on a receiver.
  Overloads that differ only in their `E` are chosen by the scope's,
  and a member of `this` with the same name does not hide one, as it
  hides an extension (section 5.2).
- `Raise<E>` is written only as the receiver of a function, and that
  function has no `this`. `either`'s `E` is settled as section 5.1
  says, before its block is read: by the type wanted, or written,
  `either<String, Int> { ... }`.
- The forms' signatures are the checker's own, in `check/core.kg`, beside
  `Either` itself.

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
`left()` and `right()`: `Either.Left(e)` is refused. The functions of
this table are the prelude's, in `prelude/either.kg`.

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
- A property may itself be a data class, written with a nullable or
  non-null type. It is filled from the columns whose names start with the
  property's name and an underscore:
  `select sa.driver_share_percent, b.id as business_id, b.legal_name as business_legal_name`
  fills `data class Supplier(val driverSharePercent: Int, val business: BusinessName)`.
  A nullable nested data class is `null` when all of its columns are null
  (the absent side of an outer join), so a column that may be null fills
  a property of it that may not.
- Reading a row builds `R` and each data class in it, so it is allowed
  only where each one's constructor may be called (sections 2 and 11).
- A data class `R` is not written nullable, since a row is never null;
  `queryOne<R>` gives the `R?`. A scalar `R` is a `T?` where its column
  may be null: `queryOne<String?>`.
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
| `T[]` | `List<T>`, or `List<T?>` |
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

- `db.tx { tx -> ... }` runs the block in one transaction. The block is a
  raise scope when the enclosing function returns `Either<E, ...>`, for
  that `E`, and `db.tx` then gives an `Either<E, A>` of the block's value
  `A`, wherever it is written; elsewhere it gives the block's value. It
  commits when the block finishes, and rolls back on a failure or a
  panic.
- `db.tx(serializable = true) { ... }` runs at SERIALIZABLE and reruns the
  block from the start on a serialization failure or deadlock, so the
  block must have no effect outside the transaction.
- `tx.atomically { ... }` runs a nested block in a savepoint and rolls
  back to it on a failure. The block is in the raise scope around it,
  as any lambda is.

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
begins with `@file:OptIn(UnscopedRead::class)`. Anything else is a
compile error. A `copy` of a data class and a row read into one (10.3)
use its constructor. Only the file's opt-in counts: marking the function
that makes the use does not stand in for it, as it does in Kotlin, and
the file that declares a marked function opts in to use it too. A type
carries no marker; mark its constructor. Markers do nothing at run time.
No other annotation exists.

## 12. Prelude

The prelude is a library, not part of the core. Names match Kotlin's
standard library and `java.time` where the meaning is the same. Its
signatures are bindings (section 13) kept in `prelude/`: the lists below
say what exists, and the declarations say what each takes and gives.

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
insertion order and compare keys as `==` does, by value, so any type may
be a key.

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

**Where the prelude differs from Kotlin.** A name that Kotlin has means
what Kotlin's means in ordinary use. The corners where it does not are
all listed here:

- `Regex` takes RE2 patterns: no lookaround and no backreferences.
- A `String` never holds half of a surrogate pair (section 3).

**Numbers.** `coerceAtLeast`, `coerceAtMost`, `coerceIn`, `abs`, `max`,
`min`, `toInt`, `toLong`, `toDouble`, `toString`. A `Decimal` holds what
Postgres sent, exactly: it has `toDouble`, `toString`, `==` and
`< <= > >=`, and no arithmetic in v0.

**Time** (`kogo.time`). `Instant`, `LocalDate` (`plusDays`, `year`,
`monthValue`, `dayOfMonth`, `atStartOfDay(zone): Instant`), `LocalTime`,
`ZoneId.of(name)`, `ZonedDateTime` (`instant.atZone(zone)`, `hour`,
`minute`, `toLocalDate`, `plusSeconds`, `toInstant`), `Duration`
(`ofDays`, `ofMinutes`, `ofSeconds`), `Clock.now()`. There is no default
zone: every conversion names its zone.

**Ids.** `Uuid`, `Uuid.parse(text): Uuid?`, `newId(): Uuid` (time-ordered
random).

## 13. Bindings

A binding declares what Go code provides. It compiles to nothing. The
prelude is written as bindings, so its signatures are Kogo source: the
checker reads them like any other file, and so can an author.

```kotlin
@go("httpx")
external class Http
external fun Http.get(url: String): Either<HttpError, Response>

external object Uuid {
    fun parse(text: String): Uuid?
}

external class List<out T>
external fun <T> listOf(vararg elements: T): List<T>
external val <T> List<T>.size: Int
external fun <T, R> List<T>.map(transform: (T) -> R): List<R>
external fun <T> List<T>.firstOrNull(): T?
external fun <T> List<T>?.orEmpty(): List<T>
external fun List<Long>.sum(): Long
```

- `external object` holds functions called on its name:
  `Uuid.parse(text)`. They have no effect: an object is reachable from
  anywhere, and section 7 has effects arrive as parameters.
- A binding with an effect, an outbound call, takes a handle: an
  external class such as `Http`, passed to the function that calls out
  as `Tx` and `Db` are, so the signature shows it. The handle carries
  the request's context. Nothing checks that a binding keeps to this;
  it is the rule for whoever writes one.
- `external class Name<T>` names a type that Go implements. It has no
  body, and Kogo code cannot construct or implement it. An external
  object may share its name, which gives `Uuid.parse(text)`.
- `external fun` and `external val` declare a function and a property
  with no body. Every member of an external class is declared this way,
  with the class as the receiver. One with a receiver is a member: it is
  in scope wherever a value of that type is, and is never imported.
- Three things are allowed in a binding and nowhere else: a nullable
  receiver, `out` on an external class's type parameter, and `vararg` on
  a parameter, which then takes any number of arguments.
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
