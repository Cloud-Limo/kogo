package syntax

import (
	"errors"
	"strings"
	"testing"
)

// fault parses src, in which ‸ marks where the error must be, and
// gives the error and the marked line and column.
func fault(t *testing.T, src string) (e *Error, line, column int) {
	t.Helper()
	src = "package t\n" + src
	before, _, found := strings.Cut(src, "‸")
	if !found {
		t.Fatalf("no ‸ in %s", src)
	}
	line = strings.Count(before, "\n") + 1
	column = len([]rune(before[strings.LastIndexByte(before, '\n')+1:])) + 1
	_, err := Parse("t.kg", []byte(strings.Replace(src, "‸", "", 1)))
	if !errors.As(err, &e) {
		t.Fatalf("%s: parsed, want an error", src)
	}
	return e, line, column
}

// Each row of spec.md section 14 that syntax alone can see is refused
// where it is written, naming the replacement.
func TestSection14(t *testing.T) {
	for _, c := range []struct{ src, code, instead string }{
		{"fun f(x: Int?) = x‸!!", "kotlin-not-null-assertion", "requireNotNull(x)"},
		{"fun f() = ‸try { 1 } catch (e: E) { 2 }", "kotlin-try", "return an Either"},
		{"fun f() {\n    ‸throw E()\n}", "kotlin-try", "panic with check or error"},
		{"‸class Foo(val x: Int)", "kotlin-class", "data class, a sealed interface"},
		{"‸open class Foo", "kotlin-class", "data class, a sealed interface"},
		{"‸sealed class S", "kotlin-class", "sealed interface"},
		{"data class D(val x: Int) : Base‸()", "kotlin-class", "implements interfaces"},
		{"‸object Config", "kotlin-object", "top-level vals and functions"},
		{"data class D(val x: Int) {\n    ‸companion object\n}", "kotlin-object", "top-level vals and functions"},
		{"val f = ‸object : I {}", "kotlin-object", "a data class implementing the interface, or a lambda"},
		{"fun f() {\n    ‸lateinit var x: String\n}", "kotlin-lateinit", "pass the value as a parameter"},
		{"‸var count = 0", "kotlin-top-level-var", "pass the value as a parameter"},
		{"fun f() = xs.forEach { ‸return@forEach }", "kotlin-label", "if/else or a local function"},
		{"fun f() {\n    ‸outer@ while (true) {}\n}", "kotlin-label", "if/else or a local function"},
		{"‸suspend fun f() {}", "kotlin-suspend", "plain blocking code"},
		{"fun f() = String‸::class", "kotlin-class-literal", "only ::class is inside @file:OptIn"},
		{"‸@Suppress(\"x\") fun f() {}", "kotlin-annotation", "only markers"},
		{"‸@get:JvmName(\"x\") val x = 1", "kotlin-annotation", "only markers"},
		{"val x: Int ‸by lazy { 1 }", "kotlin-delegation", "compute the value once"},
		{"fun f(‸vararg xs: Int) {}", "kotlin-vararg", "take a List"},
		{"fun f() = g(‸*xs)", "kotlin-spread", "pass a List"},
		{"‸inline fun f() {}", "kotlin-inline", "plain function"},
		{"fun <‸reified T> f() {}", "kotlin-inline", "plain function"},
		{"‸operator fun Int.plus(o: Int) = 1", "kotlin-inline", "plain function"},
		{"‸infix fun Int.p(o: Int) = 1", "kotlin-inline", "plain function"},
		{"‸value class Id(val v: Uuid)", "kotlin-value-class", "typealias"},
	} {
		e, line, column := fault(t, c.src)
		if e.Line != line || e.Column != column || e.Code != c.code || !strings.Contains(e.Message, c.instead) {
			t.Errorf("%s\n got %d:%d %s %s\nwant %d:%d %s ...%s...", c.src, e.Line, e.Column, e.Code, e.Message, line, column, c.code, c.instead)
		}
	}
}

// Other faults: what the spec leaves out, and where a sql literal may not
// stand. Each is refused where it is written.
func TestFaults(t *testing.T) {
	for _, c := range []struct{ src, code, message string }{
		{"val s = ‸sql\"select 1\"", "sql-placement", "only the argument of tx.query<R>"},
		{"fun f() = tx.query<R>(a, ‸sql\"select 1\")", "sql-placement", "only the argument"},
		{"fun f() = tx.run(‸sql\"select 1\")", "sql-placement", "only the argument"},
		{"fun f() = tx.execute(‸sql\"select 1\" + x)", "sql-placement", "the only argument of execute"},
		{"fun f() = tx.query‸(sql\"select 1\")", "sql-placement", "with the row type R"},
		{"fun f() = tx.execute‸<Int>(sql\"select 1\")", "sql-placement", "without one"},
		{"fun f() = tx.execute(sql\"select 1\") ‸{ }", "sql-placement", "the only argument"},
		{"fun f() = tx.execute(‸sql\"select 1)", "sql-unterminated", "no closing quote"},
		{"fun f() = ‸\"abc", "string-unterminated", "no closing quote"},
		{"val s = \"‸\\q\"", "string-escape", `\uXXXX`},
		{"val n = ‸0x1F", "number-syntax", "1, 1L, 1.0 or 10_000"},
		{"val n = ‸1_L", "number-syntax", "1, 1L, 1.0 or 10_000"},
		{"val n = ‸1_.5", "number-syntax", "1, 1L, 1.0 or 10_000"},
		{"val s = \"‸\\uD800\"", "string-escape", "halves of a pair"},
		{"val s = \"‸\\uDE00\\uD83D\"", "string-escape", "halves of a pair"},
		{"val c = ‸'\\uD83D\\uDE00'", "char-syntax", "one character"},
		{"val c = ‸'ab'", "char-syntax", "one character"},
		{"val s = \"‸${a(", "syntax", "no closing }"},
		{"fun f() = a < b ‸< c", "syntax", "comparisons do not chain"},
		{"fun f() = a ‸!in b", "syntax", "!(x in y)"},
		{"fun f() = a ‸=== b", "syntax", "compare by value"},
		{"fun f() = a ‸as B", "syntax", "smart cast"},
		{"fun f() = a ‸shl b", "syntax", "only until and to"},
		{"fun f() {\n    m[k] ‸= v\n}", "syntax", "only a local var is assigned"},
		{"fun f() {\n    n‸++\n}", "syntax", "x += 1"},
		{"fun f() {\n    ‸do { } while (a)\n}", "syntax", "while loop"},
		{"fun f() = a ‸b", "syntax", "only until and to"},
		{"fun f() = 1 ‸fun g() = 2", "syntax", "new line or ;"},
		{"fun <T ‸: Any> f() {}", "syntax", "no bound"},
		{"data class D(‸x: Int)", "syntax", "each property of a data class is a val"},
		{"data class D(val x: Int) {\n    ‸val y = 1\n}", "syntax", "only the override funs"},
		{"interface I {\n    fun f() ‸= 1\n}", "syntax", "has no body"},
		{"enum class E { A‸(1) }", "syntax", "constants only"},
		{"enum class E { A; ‸fun f() = 1 }", "syntax", "constants only"},
		{"fun f() = xs.map { x: List<‸*> -> x }", "syntax", "no projection"},
		{"import a.b‸.*", "syntax", "no wildcard"},
		{"‸protected fun f() {}", "kotlin-visibility", "private (this file) and internal"},
		{"fun f() = when (‸val x = g()) { else -> x }", "syntax", "declare the val before"},
		{"fun String‸?.f(): String = this", "nullable-receiver", "take the value as a parameter"},
		{"val List<Int>‸?.n: Int\n    get() = 1", "nullable-receiver", "take the value as a parameter"},
	} {
		e, line, column := fault(t, c.src)
		if e.Line != line || e.Column != column || e.Code != c.code || !strings.Contains(e.Message, c.message) {
			t.Errorf("%s\n got %d:%d %s %s\nwant %d:%d %s ...%s...", c.src, e.Line, e.Column, e.Code, e.Message, line, column, c.code, c.message)
		}
	}
	if _, err := Parse("t.kg", []byte("fun f() = 1")); err == nil || !strings.Contains(err.Error(), "t.kg:1:1: syntax: a file begins with its package line") {
		t.Errorf("no package line: %v", err)
	}
}
