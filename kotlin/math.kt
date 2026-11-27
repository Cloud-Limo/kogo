// kogo.math for kotlinc: kotlin.math's functions under the prelude's
// names, so the shared source means in Kotlin what kotlin.math's do.
package kogo.math

const val PI = kotlin.math.PI

fun sin(x: Double): Double = kotlin.math.sin(x)
fun cos(x: Double): Double = kotlin.math.cos(x)
fun asin(x: Double): Double = kotlin.math.asin(x)
fun sqrt(x: Double): Double = kotlin.math.sqrt(x)
