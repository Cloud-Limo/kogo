// The prelude's Either for kotlinc: Arrow's, under the names the shared
// source uses without an import, as Kogo's prelude needs none.
package builtins

import arrow.core.flatMap as arrowFlatMap
import arrow.core.getOrElse as arrowGetOrElse
import arrow.core.left as arrowLeft
import arrow.core.right as arrowRight

typealias Either<E, A> = arrow.core.Either<E, A>

fun <A> A.right(): Either<Nothing, A> = arrowRight()
fun <E> E.left(): Either<E, Nothing> = arrowLeft()
fun <E, A, B> Either<E, A>.flatMap(f: (A) -> Either<E, B>): Either<E, B> = arrowFlatMap(f)
fun <E, A> Either<E, A>.getOrElse(default: (E) -> A): A = arrowGetOrElse(default)

// The forms of a raise scope (spec.md section 9), which Arrow has as
// members and extensions of Raise that a Kotlin file imports.
inline fun <E, A> either(block: arrow.core.raise.Raise<E>.() -> A): Either<E, A> = arrow.core.raise.either(block)
fun <E> arrow.core.raise.Raise<E>.ensure(condition: Boolean, raise: () -> E) { if (!condition) raise(raise()) }
fun <E, A> arrow.core.raise.Raise<E>.ensureNotNull(value: A?, raise: () -> E): A = value ?: raise(raise())
