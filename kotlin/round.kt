// roundToLong, max and min for kotlinc: Kotlin's, which need an import
// there and none in Kogo, as toLong needs none (spec.md section 12).
package builtins

import kotlin.math.roundToLong as kotlinRoundToLong

fun Double.roundToLong(): Long = kotlinRoundToLong()

fun max(a: Int, b: Int): Int = kotlin.math.max(a, b)
fun max(a: Long, b: Long): Long = kotlin.math.max(a, b)
fun max(a: Double, b: Double): Double = kotlin.math.max(a, b)
fun min(a: Int, b: Int): Int = kotlin.math.min(a, b)
fun min(a: Long, b: Long): Long = kotlin.math.min(a, b)
fun min(a: Double, b: Double): Double = kotlin.math.min(a, b)
