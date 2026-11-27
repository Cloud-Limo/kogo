// The Kotlin side of wire/main.go: for each text, java.time's parse
// and then toString, or refused.
package wire

import java.io.File
import java.io.PrintStream
import java.time.LocalDate
import java.time.LocalTime

fun main(args: Array<String>) {
    val out = PrintStream(System.out.buffered(), false, "UTF-8")
    File(args[0]).forEachLine { line ->
        val (kind, text) = line.split("\t", limit = 2)
        val result = try {
            if (kind == "date") LocalDate.parse(text).toString() else LocalTime.parse(text).toString()
        } catch (e: java.time.format.DateTimeParseException) {
            "refused"
        }
        out.println(result)
    }
    out.flush()
}
