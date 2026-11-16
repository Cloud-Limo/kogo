// The Kotlin driver (README.md): reads one case a line, runs it in the
// shared source and prints its result, or the exception's message.
import java.io.File
import java.io.PrintStream

fun longs(field: String): List<Long> = if (field == "") listOf() else field.split(",").map { it.toLong() }

fun text(field: String): String = longs(field).joinToString("") { String(Character.toChars(it.toInt())) }

fun main(args: Array<String>) {
    val out = PrintStream(System.out.buffered(), false, "UTF-8")
    File(args[0]).forEachLine { line ->
        val c = line.split("\t")
        val result = try {
            builtins.call(
                c[0], longs(c[1]), longs(c[2]), text(c[3]), text(c[4]),
                c[5].toInt(), c[6].toInt(), c[7].toLong(), c[8].toLong(), Double.fromBits(c[9].toLong()),
            )
        } catch (e: Throwable) {
            "panic: ${e.message}"
        }
        // UTF-8 holds no half of a surrogate pair: one is written as
        // U+FFFD, which is what Kogo holds in its place (spec.md section 3).
        out.println(result.replace(Regex("[\\uD800-\\uDFFF]"), "\uFFFD").replace("\n", "\\n").replace("\r", "\\r"))
    }
    out.flush()
}
