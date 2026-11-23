// The prelude's abs for kotlinc: Kotlin's, which a Kotlin file imports
// from kotlin.math and a Kogo file does not (decisions.md).
package builtins

fun abs(n: Long): Long = kotlin.math.abs(n)
