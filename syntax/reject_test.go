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
		{"‸@OptIn(Beta::class) fun f() {}", "kotlin-annotation", "opt in on the file: begin this file with @file:OptIn(Beta::class)"},
		{"‸@OptIn(a.Beta::class) fun f() {}", "kotlin-annotation", "begin this file with @file:OptIn(a.Beta::class)"},
		{"‸@OptIn(Beta) fun f() {}", "kotlin-annotation", "only markers"},
		{"‸@get:JvmName(\"x\") val x = 1", "kotlin-annotation", "only markers"},
		{"val x: Int ‸by lazy { 1 }", "kotlin-delegation", "compute the value once"},
		{"fun f(‸vararg xs: Int) {}", "kotlin-vararg", "take a List"},
		{"fun f() = g(‸*xs)", "kotlin-spread", "pass a List"},
		{"‸inline fun f() {}", "kotlin-inline", "plain function"},
		{"fun <‸reified T> f() {}", "kotlin-inline", "plain function"},
		{"‸operator fun Int.plus(o: Int) = 1", "kotlin-inline", "plain function"},
		{"‸infix fun Int.p(o: Int) = 1", "kotlin-inline", "plain function"},
		{"‸value class Id(val v: Uuid)", "kotlin-value-class", "typealias"},
		{"‸context(Raise<String>)\nfun f(): Int = raise(\"no\")", "kotlin-context", "return an Either and bind it"},
		{"fun f() {\n    ‸context(Raise<String>)\n    fun g(): Int = raise(\"no\")\n}", "kotlin-context", "return an Either and bind it"},
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
		{"‸action val x = 1", "syntax", "action goes before fun, and before nothing else"},
		{"‸lookup fun g(): Boolean = true", "syntax", "lookup is no word of Kogo's before fun: leave it out"},
		{"fun g(‸action b: Bell) {}", "syntax", "a parameter takes no modifier"},
		{"fun g() {\n    ‸action fun h() {}\n}", "syntax", "action goes before a top-level fun only"},
		{"fun f() = tx.query<R>(a, ‸sql\"select 1\")", "sql-placement", "only the argument"},
		{"fun f() = tx.run(‸sql\"select 1\")", "sql-placement", "only the argument"},
		{"fun Tx.f() = execute(‸sql\"select 1\")", "sql-placement", "call execute on the Tx: tx.execute(...), or this.execute(...) in a fun on Tx"},
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
		{"fun (‸String?).f(): String = this", "nullable-receiver", "take the value as a parameter"},
		{"fun ‸String? .f(): String = this", "nullable-receiver", "take the value as a parameter"},
		{"fun (‸(Int) -> Int)? .f(): Int = 1", "nullable-receiver", "take the value as a parameter"},
		// What a binding alone may write, and what it may not.
		{"data class D<‸out T>(val x: T)", "syntax", "out goes only on the type parameter of a sealed interface or an external class"},
		{"fun <‸out T> f(x: T) {}", "syntax", "out goes only on the type parameter of a sealed interface or an external class"},
		{"external class C ‸{ fun f(): Int }", "syntax", "declare each member as an external fun or val with the class as its receiver"},
		{"external fun f(): Int ‸= 1", "syntax", "a binding has no body"},
		{"external ‸data class D(val x: Int)", "syntax", "external goes before class, object, fun or val"},
		{"fun f(‸external x: Int) {}", "syntax", "a parameter takes no modifier"},
		{"fun f(‸@M x: Int) {}", "syntax", "a parameter takes no modifier"},
		{"data class D(‸@M val x: Int)", "syntax", "a marker on a data class's property is written @property:M, as Kotlin has it"},
		// A list's entries are separated by a comma or a line end.
		{"api ops unchecked { a ungated ‸b ungated }", "syntax", "expected , or a new line between a list's entries"},
		// A row of an api list names its gate or says ungated, on one
		// line (spec.md 15.1).
		{"api ops unchecked { ‸a }", "syntax", "a names no gate: a row is a gatedBy <gate>, or a ungated"},
		{"api ops unchecked { ‸a, b ungated }", "syntax", "a names no gate"},
		{"api ops unchecked { ‸a after s }", "syntax", "a names no gate"},
		{"api ops unchecked { a ungated ‸after s }", "syntax", "a is ungated, and after names a step between a gate and its work: remove after and its step, or give a a gate"},
		{"api ops unchecked { a gatedBy ‸}", "syntax", "expected a name"},
		{"api ops unchecked { a gatedBy g after ‸}", "syntax", "expected a name"},
		{"api ops unchecked { a gatedBy g ‸gatedBy h }", "syntax", "expected , or a new line between a list's entries"},
		{"api ops unchecked {\n    a\n    ‸gatedBy g\n}", "syntax", "a row stays on one line: move gatedBy onto a's line"},
		{"api ops unchecked {\n    a\n    ‸ungated\n}", "syntax", "a row stays on one line: move ungated onto a's line"},
		{"api ops unchecked {\n    a gatedBy g\n    ‸after s\n}", "syntax", "a row stays on one line: move after onto a's line"},
		{"api ops unchecked {\n    a ungated\n    ‸after s\n}", "syntax", "a row stays on one line: move after onto a's line"},
		{"api ops unchecked {\n    a gatedBy\n    ‸g\n}", "syntax", "a row stays on one line: move g onto a's line"},
		{"api ops unchecked {\n    a gatedBy g after\n    ‸s\n}", "syntax", "a row stays on one line: move s onto a's line"},
		{"api ops checkedBy ‸{ a ungated }", "syntax", "expected a name"},
		{"api ops ‸checked { a ungated }", "syntax", "expected {"},
		{"tests ts<Unit> { a ‸b }", "syntax", "expected , or a new line between a list's entries"},
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

// A tests list names its environment's type (spec.md 16.1).
func TestTestsList(t *testing.T) {
	if f, err := Parse("t_test.kg", []byte("package t\ntests ts<List<Env>> { a, b, }\nval tests: Int = 1")); err != nil ||
		len(f.Tests) != 1 || f.Tests[0].Name != "ts" || len(f.Tests[0].Vals) != 2 || len(f.Decls) != 1 || !f.IsTest() {
		t.Errorf("got %+v, %v: want one tests list of two names, and a val named tests", f, err)
	}
	e, line, column := fault(t, "tests ts ‸{ a }")
	if e.Line != line || e.Column != column || e.Code != "syntax" || !strings.Contains(e.Message, "tests ts<Env> { ... }") {
		t.Errorf("got %v, want a syntax error at %d:%d naming tests ts<Env> { ... }", e, line, column)
	}
}
