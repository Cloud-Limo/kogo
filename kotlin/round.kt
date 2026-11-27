// roundToLong for kotlinc: Kotlin's, which needs an import there and
// none in Kogo, as toLong needs none (spec.md section 12).
package builtins

import kotlin.math.roundToLong as kotlinRoundToLong

fun Double.roundToLong(): Long = kotlinRoundToLong()
