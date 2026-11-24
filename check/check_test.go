package check

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Cloud-Limo/kogo/prelude"
	"github.com/Cloud-Limo/kogo/sqlpass"
	"github.com/Cloud-Limo/kogo/syntax"
)

// A prelude of the tests' own: spec.md section 13 lets the checker be
// tested against any bindings.
const bindings = `package kogo
external class List<out T>
external class Map<K, out V>
external class Pair<out A, out B>
external fun <T> listOf(vararg elements: T): List<T>
external fun <T> emptyList(): List<T>
external fun <K, V> mapOf(vararg pairs: Pair<K, V>): Map<K, V>
external val <T> List<T>.size: Int
external fun <T> List<T>.first(): T
external fun <T, R> List<T>.map(transform: (T) -> R): List<R>
external fun <T> List<T>.forEach(action: (T) -> Unit)
external fun <T> List<T>.sumOf(selector: (T) -> Int): Int
external fun <T> List<T>.sumOf(selector: (T) -> Long): Long
external fun <T> List<T>.plus(element: T): List<T>
external fun <T> List<T>.contains(element: T): Boolean
external fun <T> List<T>.get(index: Int): T
external fun <K, V> Map<K, V>.get(key: K): V?
external fun <A, B> A.to(that: B): Pair<A, B>
external fun <T, R> List<T>.zip(other: List<R>): List<Pair<T, R>>
external val String.length: Int
external fun String.plus(other: String): String
external fun String.compareTo(other: String): Int
external fun String.toIntOrNull(): Int?
external fun Int.toLong(): Long
external fun Int.toDouble(): Double
external fun <T, R> T.let(block: (T) -> R): R
external fun <T> requireNotNull(value: T?, lazyMessage: () -> String): T
external fun require(value: Boolean, lazyMessage: () -> String)
external fun error(message: String): Nothing
external class Uuid
external class ByteArray
external object Text {
    fun of(n: Int): String
}
`

// split gives the files of src: one, or several separated by a line
// of ---. A file with no package line is in package t.
func split(src string) []string {
	files := strings.Split(src, "\n---\n")
	for i, text := range files {
		if !strings.HasPrefix(text, "package ") && !strings.HasPrefix(text, "@file:") {
			files[i] = "package t\n" + text
		}
	}
	return files
}

// fileName names the i'th file: f<i>.kg, or f<i>_test.kg, a test
// file (spec.md 16), where its text holds /*test*/.
func fileName(i int, text string) string {
	if strings.Contains(text, "/*test*/") {
		return fmt.Sprintf("f%d_test.kg", i)
	}
	return fmt.Sprintf("f%d.kg", i)
}

// run checks src with the tests' prelude, which is f0.kg and the real
// prelude's either.kg; the files of src are f1.kg and on. A file of
// src in package kogo, or one under it, joins the prelude, to hold
// bindings of the test's own.
func run(t *testing.T, src string) (*Info, []*syntax.Error) {
	t.Helper()
	text, err := os.ReadFile("../prelude/either.kg")
	if err != nil {
		t.Fatal(err)
	}
	either, err := syntax.Parse("prelude/either.kg", text)
	if err != nil {
		t.Fatal(err)
	}
	pre, program := []*syntax.File{either}, []*syntax.File(nil)
	for i, text := range append([]string{bindings}, split(src)...) {
		f, err := syntax.Parse(fileName(i, text), []byte(text))
		if err != nil {
			t.Fatalf("%s\n%v", text, err)
		}
		if name := f.Package.Name; name == "kogo" || strings.HasPrefix(name, "kogo.") {
			pre = append(pre, f)
		} else {
			program = append(program, f)
		}
	}
	return Check(pre, program, signature)
}

// signature answers for the SQL pass in the tests: a literal's text is
// its own signature, as "$a: Int, ${b.c}: Kind{x y} -> id: Uuid, n:
// List<Long>?", the parameters in the pass's order, a name once,
// Kind{x y} an enum of the constants x and y, and t(a: Int, b: Uuid?)
// the composite type t of fields a and b; a column of a type T?? is
// null only through an outer join. A literal holding « is
// refused there. A word @person:update says the statement needs
// update of the table person, and @person that it reads it;
// @utility:LOCKSTMT that it is the utility statement Postgres tags so.
func signature(lit *syntax.SQL) (*sqlpass.Statement, error) {
	if i := strings.Index(lit.Text, "«"); i >= 0 {
		return nil, &sqlpass.Error{Offset: i, Code: "sql-rejected", Message: "Postgres refused this"}
	}
	// split cuts a list at the commas outside parentheses.
	split := func(s string) (parts []string) {
		depth, start := 0, 0
		for i, r := range s {
			switch {
			case r == '(':
				depth++
			case r == ')':
				depth--
			case r == ',' && depth == 0:
				parts, start = append(parts, s[start:i]), i+1
			}
		}
		return append(parts, s[start:])
	}
	var typ func(string) sqlpass.Type
	var columns func(string) []sqlpass.Column
	typ = func(s string) sqlpass.Type {
		if name, fields, ok := strings.Cut(s, "("); ok {
			return sqlpass.Type{Name: name, Fields: columns(strings.TrimSuffix(fields, ")"))}
		}
		if elem, ok := strings.CutPrefix(s, "List<"); ok {
			t := typ(strings.TrimSuffix(elem, ">"))
			t.List = true
			return t
		}
		if name, labels, ok := strings.Cut(s, "{"); ok {
			return sqlpass.Type{Name: name, Enum: true, Labels: strings.Fields(strings.TrimSuffix(labels, "}"))}
		}
		return sqlpass.Type{Name: s}
	}
	columns = func(s string) []sqlpass.Column {
		cols := []sqlpass.Column{}
		for _, col := range split(s) {
			if name, t, ok := strings.Cut(strings.TrimSpace(col), ": "); ok {
				t, joined := strings.CutSuffix(t, "??")
				base, null := strings.CutSuffix(t, "?")
				cols = append(cols, sqlpass.Column{Name: name, Type: typ(base), Nullable: null || joined, Joined: joined, Outer: joined})
			}
		}
		return cols
	}
	text, tables, utility := lit.Text, []sqlpass.Table(nil), ""
	for _, w := range strings.Fields(text) {
		if name, need, _ := strings.Cut(w, ":"); name == "@utility" {
			utility, text = need, strings.Replace(text, w, "", 1)
		} else if w[0] == '@' {
			tables, text = append(tables, sqlpass.Table{Name: name[1:], Needs: strings.Fields(need)}), strings.Replace(text, w, "", 1)
		}
	}
	params, cols, _ := strings.Cut(text, "->")
	st := &sqlpass.Statement{SQL: lit.Text, Columns: columns(cols), Tables: tables, Utility: utility}
	for _, p := range strings.Split(params, ",") {
		if source, t, ok := strings.Cut(strings.TrimSpace(p), ": "); ok {
			st.Params = append(st.Params, sqlpass.Param{Source: source, Type: typ(t)})
		}
	}
	return st, nil
}

// refused checks that each program is refused once, where ‸ stands,
// with the code and a message that says what to write instead.
func refused(t *testing.T, cases [][3]string) {
	t.Helper()
	for _, c := range cases {
		src, code, fix := c[0], c[1], c[2]
		want := ""
		for i, text := range split(src) {
			if before, _, found := strings.Cut(text, "‸"); found {
				column := len([]rune(before[strings.LastIndexByte(before, '\n')+1:])) + 1
				want = fmt.Sprintf("%s:%d:%d: %s: ", fileName(i+1, text), strings.Count(before, "\n")+1, column, code)
			}
		}
		_, errs := run(t, strings.Replace(src, "‸", "", 1))
		if len(errs) != 1 || !strings.HasPrefix(errs[0].Error(), want) || !strings.Contains(errs[0].Message, fix) {
			t.Errorf("%s\n got %v\nwant %s...%s...", src, errs, want, fix)
		}
	}
}

// passes checks that each program has no error.
func passes(t *testing.T, cases ...string) {
	t.Helper()
	for _, src := range cases {
		if _, errs := run(t, src); errs != nil {
			t.Errorf("%s\n got %v, want no error", src, errs)
		}
	}
}

// Section 2: packages, imports and visibility.
func TestNames(t *testing.T) {
	passes(t,
		"package a\nimport b.Money\nimport b.double\nimport b.rate\nfun f(m: Money): Money = double(m) * rate\n---\npackage b\ntypealias Money = Long\nfun double(m: Money): Money = m * 2\nval rate: Long = 3",
		"package a\nfun f(): Int = g()\ninternal fun g(): Int = 1\n---\npackage a\nprivate fun h(): Int = 2\nfun k(): Int = g() + h()\n---\npackage a\nprivate fun h(): Int = 3\nfun m(): Int = h()",
		"package a\nimport kogo.time.ZoneId\nfun f(s: String): List<ZoneId> = listOf(ZoneId.of(\"Zulu\"), ZoneId.of(\"UTC\"), ZoneId.of(s), ZoneId.of(\"$s+1\"))\n---\npackage kogo.time\nexternal class ZoneId\nexternal object ZoneId {\n    fun of(name: String): ZoneId\n}",
		"package a\nimport kogo.time.Stamp\nfun f(s: Stamp, xs: List<Int>): Int = s.seconds() + xs.size\n---\npackage kogo.time\nexternal class Stamp\nexternal fun Stamp.seconds(): Int",
		"package a\nfun f(s: String): List<Regex> = listOf(Regex(\"(?=$s)\"), Regex(s), Regex(\"[a-z]+\"))\n---\npackage kogo\nexternal class Regex\nexternal fun Regex(pattern: String): Regex",
		// One table of names (spec.md 2): a file's private val is its own,
		// and the package's own val or type is meant before the prelude's.
		"private val limit: Int = 50\nfun f(): Int = limit\n---\npackage t\nprivate val limit: String = \"\"\nfun g(): String = limit",
		"data class Either(val n: Int)\nfun f(): Either = Either(1)\nval limit: Int = 50\n---\npackage kogo\nval limit: String = \"\"\n---\npackage t\nfun h(): Int = limit",
		"typealias Money = Long\ndata class P(val n: Int?, val s: String, val m: Money)\nfun f(xs: List<P>): List<P> = xs.sortedBy { it.n }.sortedBy { it.s }.sortedBy { it.m }.sortedBy { it.m < 0 }\n---\npackage kogo\nexternal fun <T, R> List<T>.sortedBy(selector: (T) -> R): List<T>",
		"fun f(s: Set<List<Int>>, xs: List<Int>, ys: Set<Int>): Set<List<Int>> = s - setOf(xs) + listOf(xs)\nfun g(ys: Set<Int>, xs: List<Int>, ns: List<Int?>): Int? = (ys + 1 - xs - ys).toList().maxOrNull() ?: ns.filterNotNull().maxOrNull()\n---\npackage kogo\nexternal class Set<out T>\nexternal fun <T> Set<T>.plus(element: T): Set<T>\nexternal fun <T> Set<T>.plus(elements: List<T>): Set<T>\nexternal fun <T> Set<T>.minus(elements: List<T>): Set<T>\nexternal fun <T> Set<T>.minus(elements: Set<T>): Set<T>\nexternal fun <T> Set<T>.minus(element: T): Set<T>\nexternal fun <T> setOf(vararg elements: T): Set<T>\nexternal fun <T> Set<T>.toList(): List<T>\nexternal fun <T> List<T?>.filterNotNull(): List<T>\nexternal fun <T> List<T>.maxOrNull(): T?",
		"package a\nimport b.twice\nfun f(): Int = 2.twice()\n---\npackage b\nfun Int.twice(): Int = this * 2",
		"package a\nimport b.Shape\nfun f(s: Shape): Int = when (s) {\n    is Shape.Dot -> 0\n    is Shape.Line -> s.length\n}\nfun g(): Shape = Shape.Line(2)\n---\npackage b\nsealed interface Shape {\n    data object Dot : Shape\n    data class Line(val length: Int) : Shape\n}",
	)
	refused(t, [][3]string{
		{"fun f(): Int = ‸g()", "unknown-name", "nothing named g is declared here, in package t or in the prelude"},
		{"fun f(x: ‸Money): Int = 1", "unknown-name", "nothing named Money"},
		{"package a\nfun f(): Int = ‸g()\n---\npackage b\nfun g(): Int = 1", "unknown-name", "add import b.g"},
		{"package a\nfun f(s: ‸Stamp): Int = 1\n---\npackage kogo.time\nexternal class Stamp", "unknown-name", "add import kogo.time.Stamp"},
		{"package a\nimport ‸b.g\n---\npackage c\nfun g(): Int = 1", "unknown-import", "no package b"},
		{"package a\nimport ‸c.h\n---\npackage c\nfun g(): Int = 1", "unknown-import", "package c declares nothing named h"},
		// Kotlin's imports, and the one Kogo has instead.
		{"package a\nimport ‸java.time.Instant\n---\npackage kogo.time\nexternal class Instant", "unknown-import", "no package java.time in Kogo: import kogo.time.Instant instead"},
		{"package a\nimport ‸kotlin.test.assertEquals\n---\npackage kogo.test\nexternal fun assertEquals(a: Int, b: Int)", "unknown-import", "no package kotlin.test in Kogo: import kogo.test.assertEquals instead"},
		{"package a\nimport ‸java.util.UUID", "unknown-import", "Uuid needs no import: remove this one and write Uuid"},
		{"package a\nimport ‸arrow.core.Either", "unknown-import", "Either needs no import: remove this one"},
		{"package a\nimport ‸arrow.core.raise.ensure", "unknown-import", "ensure needs no import: remove this one"},
		{"package a\nimport ‸kotlin.math.abs\n---\npackage kogo\nexternal fun abs(n: Long): Long", "unknown-import", "abs needs no import: remove this one"},
		// Section 12: an offset ID written as a literal is outside the subset.
		{"package a\nimport kogo.time.ZoneId\nfun f(): ZoneId = ZoneId.of(‸\"+05:30\")\n---\npackage kogo.time\nexternal class ZoneId\nexternal object ZoneId {\n    fun of(name: String): ZoneId\n}", "zone-offset", "ZoneId.of takes a region name, such as Australia/Melbourne; offsets are not supported (spec 12)"},
		{"package a\nimport kogo.time.ZoneId\nfun f(): ZoneId = ZoneId.of(‸\"Z\")\n---\npackage kogo.time\nexternal class ZoneId\nexternal object ZoneId {\n    fun of(name: String): ZoneId\n}", "zone-offset", "offsets are not supported"},
		// A literal pattern is compiled by Go's regexp at check (spec.md 12).
		{"package a\nfun f(): Regex = Regex(‸\"a(?=b)\")\n---\npackage kogo\nexternal class Regex\nexternal fun Regex(pattern: String): Regex", "regex-pattern", "Go cannot read this pattern: invalid or unsupported Perl syntax: `(?=`. A Regex's pattern is Go's, not Kotlin's: write it in Go's syntax (spec 12)"},
		{"package a\nfun f(): Regex = Regex(‸\"\"\"(a)\\1\"\"\")\n---\npackage kogo\nexternal class Regex\nexternal fun Regex(pattern: String): Regex", "regex-pattern", "invalid escape sequence: `\\1`"},
		// Section 12: sortedBy orders by a key of these types alone.
		{"data class P(val n: Int)\nfun f(xs: List<P>): List<P> = xs.sortedBy ‸{ it }\n---\npackage kogo\nexternal fun <T, R> List<T>.sortedBy(selector: (T) -> R): List<T>", "sort-key", "this key is a P, and sortedBy orders only by an Int, a Long, a Double, a String, a Char or a Boolean, or one of these that may be null: give it one of these, such as a field of the P (spec 12)"},
		{"enum class E { a }\nfun f(xs: List<Int>): List<Int> = xs.sortedBy(selector = ‸{ E.a })\n---\npackage kogo\nexternal fun <T, R> List<T>.sortedBy(selector: (T) -> R): List<T>", "sort-key", "this key is a E"},
		{"fun f(xs: List<List<Int>>): List<List<Int>> = xs.sortedBy ‸{ it }\n---\npackage kogo\nexternal fun <T, R> List<T>.sortedBy(selector: (T) -> R): List<T>", "sort-key", "this key is a List<Int>"},
		{"data class P(val n: Int)\nfun f(xs: List<P>): List<P> = xs.sortedByDescending ‸{ it }\n---\npackage kogo\nexternal fun <T, R> List<T>.sortedByDescending(selector: (T) -> R): List<T>", "sort-key", "this key is a P, and sortedByDescending orders only by"},
		// maxOrNull takes the largest of these, and none that may be null, as Kotlin's.
		{"fun f(xs: List<Long?>): Long? = xs.‸maxOrNull()\n---\npackage kogo\nexternal fun <T> List<T>.maxOrNull(): T?", "sort-key", "this list's element is a Long?, and maxOrNull finds the largest only of an Int, a Long, a Double, a String, a Char or a Boolean: map the list to one of these first, as mapNotNull does to drop a null (spec 12)"},
		{"enum class E { a }\nfun f(xs: List<E>): E? = xs.‸maxOrNull()\n---\npackage kogo\nexternal fun <T> List<T>.maxOrNull(): T?", "sort-key", "this list's element is a E"},
		// A Set's + or - of one element that is a collection: Kotlin
		// reads it as the elements to add or remove.
		{"fun f(s: Set<List<Int>>, xs: List<Int>): Set<List<Int>> = s ‸- xs\n---\npackage kogo\nexternal class Set<out T>\nexternal fun <T> Set<T>.minus(element: T): Set<T>\nexternal fun <T> Set<T>.minus(elements: List<T>): Set<T>", "set-operand", "this is a List<Int>, and Kotlin takes a List or a Set after a Set's + or - as the elements to add or remove: write setOf(...) around it to add or remove it whole (spec 12)"},
		{"fun f(s: Set<Set<Int>>, xs: Set<Int>): Set<Set<Int>> = s.‸plus(xs)\n---\npackage kogo\nexternal class Set<out T>\nexternal fun <T> Set<T>.plus(element: T): Set<T>", "set-operand", "this is a Set<Int>"},
		{"fun f(s: Set<List<Int>?>, xs: List<Int>): Set<List<Int>?> = s ‸+ xs\n---\npackage kogo\nexternal class Set<out T>\nexternal fun <T> Set<T>.plus(element: T): Set<T>", "set-operand", "this is a List<Int>, and"},
		{"package a\nimport ‸kogo.time.seconds\n---\npackage kogo.time\nexternal class Stamp\nexternal fun Stamp.seconds(): Int", "member-import", "seconds is a member, in scope wherever a value of its receiver's type is: remove this import"},
		// Section 13: only the prelude declares bindings, and an
		// external object holds funs.
		{"external ‸fun String.shout(): String", "external", "only the prelude declares bindings (spec.md 13): give this a body in Kogo"},
		{"package kogo\nexternal object Zone {\n    ‸val utc: Long\n}", "external-object", "an external object holds funs only (spec.md 13): declare fun utc(): ... instead"},
		{"package a\nimport ‸c.g\n---\npackage c\nprivate fun g(): Int = 1", "not-visible", "g is private to f2.kg: remove private"},
		{"package a\nimport ‸c.g\n---\npackage c\ninternal fun g(): Int = 1", "not-visible", "g is internal to package c: remove internal"},
		{"package a\nfun f(): Int = ‸g()\n---\npackage a\nprivate fun g(): Int = 1", "not-visible", "g is private to f2.kg"},
		{"package a\nfun f(x: ‸P): Int = 1\n---\npackage a\nprivate data class P(val n: Int)", "not-visible", "P is private to f2.kg"},
		{"package a\nimport c.P\nfun f(): P = ‸P(1)\n---\npackage c\ndata class P internal constructor(val n: Int)", "not-visible", "P's constructor is internal to package c"},
		// copy builds a value as the constructor does, and needs what it needs.
		{"package a\nimport c.P\nimport c.make\nfun f(): P = make().‸copy(n = 2)\n---\npackage c\ndata class P internal constructor(val n: Int)\nfun make(): P = P(1)", "not-visible", "P's constructor is internal to package c"},
		{"data class P(val n: Int)\n‸typealias P = Int", "duplicate", "package t already declares P: rename one"},
		{"val n: Int = 1\n---\npackage t\n‸val n: Int = 2", "duplicate", "already declares n"},
		{"data class Area(val n: Int)\n---\npackage t\n‸private data class Area(val code: String)", "duplicate", "already declares Area"},
		{"private val n: Int = 1\n---\npackage t\n‸val n: Int = 2", "duplicate", "already declares n"},
		{"val n: Int = 1\n---\npackage t\nprivate ‸val n: Int = 2", "duplicate", "already declares n"},
		{"private val n: Int = 1\nprivate ‸val n: Int = 2", "duplicate", "already declares n"},
		{"private val n: Int = 1\n---\npackage t\nprivate val n: Int = 2\nprivate ‸val n: Int = 3", "duplicate", "already declares n"},
		// A name is written bare, an import names a top-level name, and
		// an extension is imported by its own.
		{"package a\nfun f(): Int = ‸b.g()\n---\npackage b\nfun g(): Int = 1", "unknown-name", "a package is not written in code: import b.g and write g"},
		{"package a\nfun f(): Int = ‸b.O.Ok(1).n\n---\npackage b\nsealed interface O {\n    data class Ok(val n: Int) : O\n}", "unknown-name", "import b.O and write O.Ok"},
		{"package a\nimport ‸b.Shape.Dot\n---\npackage b\nsealed interface Shape {\n    data object Dot : Shape\n}", "unknown-import", "no package b.Shape: import b.Shape and write Shape.Dot"},
		{"package a\nfun f(): Int = 2.‸twice()\n---\npackage b\nfun Int.twice(): Int = this * 2", "unknown-name", "a Int has no function twice: add import b.twice"},
		{"package a\nfun f(): Int = 2.‸twice()\n---\npackage b\nfun String.twice(): String = this\n---\npackage c\nfun Int.twice(): Int = this * 2", "unknown-name", "a Int has no function twice: add import c.twice"},
		{"package a\nimport b.PRow\nfun f(): PRow = ‸PRow(1)\n---\npackage b\ndata class Row(val n: Int)\ntypealias PRow = Row", "unknown-name", "PRow is a typealias, which builds nothing: write Row, the type's own name"},
		{"package a\nfun f(): Int = ‸PRow(1).n\n---\npackage b\ndata class Row(val n: Int)\ntypealias PRow = Row", "unknown-name", "PRow is a typealias, which builds nothing: write Row"},
		{"package a\nfun f(x: ‸b.P): Int = 1\n---\npackage b\ndata class P(val n: Int)", "unknown-name", "a package is not written in code: import b.P and write P"},
		{"package a\nimport b.g\nimport ‸c.g\n---\npackage b\nfun g(): Int = 1\n---\npackage c\nfun g(): Int = 2", "duplicate", "g is already imported from package b"},
		// Section 6: the scope functions that do not exist.
		{"fun f(x: Int): Int = x.‸apply { 1 }", "kotlin-scope-function", "name the value and call it"},
		{"fun f(x: Int): Int? = x.‸takeUnless { it > 1 }", "kotlin-scope-function", "use takeIf"},
		// Section 14: the prelude has no nullable receiver.
		{"fun f(s: String?): Boolean = s.‸isNullOrBlank()", "nullable-receiver", "isNullOrBlank is not Kogo: write x == null || x.isBlank()"},
		{"fun f(xs: List<Int>?): List<Int> = xs.‸orEmpty()", "nullable-receiver", "orEmpty is not Kogo: write x ?: emptyList()"},
	})
	// A hint names only an import Kogo has.
	for src, want := range map[string]string{
		"package a\nimport java.time.OffsetDateTime\n---\npackage kogo.time\nexternal class Instant":          "there is no package java.time",
		"package a\nimport kotlin.test.assertIs\n---\npackage kogo.test\nexternal fun assertTrue(b: Boolean)": "there is no package kotlin.test",
	} {
		if _, errs := run(t, src); len(errs) != 1 || errs[0].Message != want {
			t.Errorf("%s\n got %v, want %s alone", src, errs, want)
		}
	}
}

// Section 2: import ... as gives a declaration another name in the
// file, for its type, constructor, constants and variants; and an
// import does not hide what the file's own package declares.
func TestImportAs(t *testing.T) {
	rows := "\n---\npackage b\ndata class Area(val id: Int, val label: String)\nenum class Tone { warm, cold }\nsealed interface O {\n    data class Ok(val n: Int) : O\n}\nfun g(): Int = 1\nval limit: Int = 2"
	passes(t,
		"package a\nimport b.Area as AreaRow\ndata class Area(val label: String)\nfun f(r: AreaRow): Area = Area(r.label)\nfun h(): AreaRow = AreaRow(1, \"x\")"+rows,
		"package a\nimport b.Tone as T\nimport b.O as Out\nimport b.g as bg\nimport b.limit as cap\nfun f(o: Out): T = when (o) {\n    is Out.Ok -> T.warm\n}\nfun h(): Int = bg() + cap + Out.Ok(1).n"+rows,
		"package a\nimport kogo.Either as KogoEither\ndata class Either(val n: Int)\nfun f(e: Either): KogoEither<String, Int> = e.n.right()",
		// A fun of the package's own name is one more candidate.
		"package a\nimport b.g\nfun g(n: Int): Int = n\nfun f(): Int = g() + g(1)"+rows,
	)
	refused(t, [][3]string{
		{"package a\nimport ‸b.Area\ndata class Area(val label: String)" + rows, "own-import", "package a declares Area, which this import would hide: give the import another name, import b.Area as Other, or rename one"},
		{"package a\nimport ‸b.limit\ntypealias limit = Int" + rows, "own-import", "package a declares limit"},
		{"package a\nimport ‸b.g as Area\nval Area: Int = 1" + rows, "own-import", "package a declares Area"},
		{"package a\nimport ‸a.Area\n---\npackage a\ndata class Area(val n: Int)", "own-import", "Area is package a's own, which needs no import: remove this import"},
		{"package a\nimport c.g\nimport ‸b.g\n---\npackage c\nfun g(n: Int): Int = n" + rows, "duplicate", "g is already imported from package c: give one another name, import b.g as Other"},
		{"package a\nimport b.Area as g\nimport ‸b.g" + rows, "duplicate", "g is already imported from package b"},
		{"package a\nimport b.g\nimport ‸b.Area as g" + rows, "duplicate", "g is already imported from package b"},
		{"package a\nimport b.Area as R\nfun f(): R = ‸Area(1, \"x\")" + rows, "unknown-name", "Area is not in scope"},
		{"package a\nimport b.Area as R\ndata class Area(val label: String)\nfun f(r: R?): Area? = ‸r" + rows, "type-mismatch", "this is a b.Area? where a a.Area? is wanted"},
	})
}

// A program's file is not the prelude's for claiming its package, and
// a program's package is not kogo or under it (spec.md 2).
func TestBindingsOnlyInPrelude(t *testing.T) {
	for src, want := range map[string][]string{
		"package kogo\nexternal class Box\n":  {"kogo-package", "external"},
		"package kogo.time\nval n: Int = 1\n": {"kogo-package"},
		"package shop.kogo\nval n: Int = 1\n": nil,
	} {
		f, err := syntax.Parse("p.kg", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		_, errs := Check(nil, []*syntax.File{f}, nil)
		var got []string
		for _, e := range errs {
			got = append(got, e.Code)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s\n got %v, want %v", src, errs, want)
		}
	}
}

// Section 2: packages do not import each other, directly or through
// others. One error for each group, at its shortest cycle.
func TestImportCycles(t *testing.T) {
	passes(t,
		"package a\nimport b.g\nfun f(): Int = g()\n---\npackage b\nfun g(): Int = 1",
		"package a\nimport a.g\nfun f(): Int = g()\n---\npackage a\nfun g(): Int = 1",
	)
	tie := "package a\nimport ‸c.h\nimport b.g\nfun f(): Int = 1\n---\npackage b\nimport a.f\nfun g(): Int = 1\n---\npackage c\nimport a.f\nfun h(): Int = 1"
	refused(t, [][3]string{
		{"package a\nimport ‸b.g\nimport b.h\nfun f(): Int = g() + h()\n---\npackage b\nimport a.f\nfun g(): Int = 1\nfun h(): Int = 2\nfun k(): Int = f()",
			"import-cycle", "packages import each other: f1.kg:2 in a imports b.g, b.h; f2.kg:2 in b imports a.f; move what one package takes from another into a package both may import"},
		// The cycle runs through an import with as, to package b.
		{"package a\nimport ‸b.g as bg\nfun f(): Int = bg()\n---\npackage b\nimport a.f\nfun g(): Int = 1\nfun k(): Int = f()", "import-cycle", ": f1.kg:2 in a imports b.g;"},
		{"package a\nimport ‸b.g\nfun f(): Int = 1\n---\npackage b\nimport c.h\nfun g(): Int = 1\n---\npackage c\nimport a.f\nfun h(): Int = 1",
			"import-cycle", ": f1.kg:2 in a imports b.g; f2.kg:2 in b imports c.h; f3.kg:2 in c imports a.f;"},
		// a takes from c and b, both of which take from a: of the two
		// cycles, the one whose imports come first is told.
		{tie, "import-cycle", ": f1.kg:2 in a imports c.h; f3.kg:2 in c imports a.f;"},
		// A ring of three that holds a cycle of two: the two is told.
		{"package a\nimport b.g\nfun f(): Int = 1\n---\npackage b\nimport ‸c.h\nfun g(): Int = 1\n---\npackage c\nimport a.f\nimport b.g\nfun h(): Int = 1",
			"import-cycle", ": f2.kg:2 in b imports c.h; f3.kg:3 in c imports b.g;"},
	})
	_, errs := run(t, "package a\nimport b.g\nfun f(): Int = 1\n---\npackage b\nimport a.f\nfun g(): Int = 1\n---\npackage c\nimport d.k\nfun h(): Int = 1\n---\npackage d\nimport c.h\nfun k(): Int = 1")
	if len(errs) != 2 || !strings.HasPrefix(errs[0].Error(), "f1.kg:2:8: import-cycle") || !strings.HasPrefix(errs[1].Error(), "f3.kg:2:8: import-cycle") {
		t.Errorf("two separate cycles: got %v, want an error at each", errs)
	}
}

// Imports rank in the order the files were given, not by name, and a
// program's file in a package of the prelude's is on a cycle like any.
func TestImportCyclesAgainstPrelude(t *testing.T) {
	pre, err := prelude.Files()
	if err != nil {
		t.Fatal(err)
	}
	check := func(files ...string) []*syntax.Error {
		var program []*syntax.File
		for i := 0; i < len(files); i += 2 {
			f, err := syntax.Parse(files[i], []byte(files[i+1]))
			if err != nil {
				t.Fatal(err)
			}
			program = append(program, f)
		}
		_, errs := Check(pre, program, nil)
		return errs
	}
	errs := check("z.kg", "package b\nimport a.f\nfun g(): Int = 1", "a.kg", "package a\nimport b.g\nfun f(): Int = 1")
	if len(errs) != 1 || !strings.HasPrefix(errs[0].Error(), "z.kg:2:8: import-cycle") {
		t.Errorf("given z.kg first: got %v, want the error in z.kg", errs)
	}
	errs = check("a.kg", "package a\nimport kogo.time.Instant\nfun f(): Int = 1", "t.kg", "package kogo.time\nimport a.f\nfun g(): Int = 1")
	if !slices.ContainsFunc(errs, func(e *syntax.Error) bool { return e.Code == "import-cycle" }) {
		t.Errorf("a cycle through kogo.time: got %v, want an import-cycle", errs)
	}
}

// randomString is Kogo's own as newId is: no import, and its String
// goes wherever a String does (spec.md 12).
func TestRandomString(t *testing.T) {
	preludeCases(t, [][2]string{
		{"data class S(val token: String)\nfun f(n: Int): Boolean {\n    val s = S(randomString(n))\n    return \"${randomString(2)}\".length + s.token.length > 0 && randomString(1) != \"a\"\n}\nfun g(): String = randomString(20).let { it + it }", ""},
		{"fun f(): Int = randomString(1)", "t.kg:2:16: type-mismatch: this is a String where a Int"},
		{"fun f(): String = randomString(\"1\")", "t.kg:2:32: type-mismatch: this is a String where a Int"},
	})
}

// toString is Int's, Long's, Double's and Decimal's, as spec.md 12
// lists it, and no other built-in type's.
func TestNumberToString(t *testing.T) {
	preludeCases(t, [][2]string{
		{"fun f(n: Long): String = n.toString()\nfun g(n: Int, d: Double, m: Decimal): String = n.toString() + d.toString() + m.toString()", ""},
		{"data class P(val a: Int)\nfun f(p: P): String = p.toString() + p.a.toString()", ""},
		{"fun f(b: Boolean): String = b.toString()", "t.kg:2:31: no-overload: no toString fits this call with (): the candidates are fun Int.toString()"},
	})
}

// joinToString takes a lambda giving each element's String, which a
// List<String> alone may leave out (spec.md 12).
func TestJoinToString(t *testing.T) {
	preludeCases(t, [][2]string{
		{"fun f(ns: List<Long>): String = ns.joinToString(\",\")", "t.kg:2:36: no-overload: no joinToString fits this call with (String): the candidates are fun List<String>.joinToString(separator: String): String; fun <T> List<T>.joinToString("},
	})
}

// preludeCases checks each case's source, after "package t", against
// the real prelude: it passes when the case wants "", and otherwise
// gives one error that starts with what the case wants.
func preludeCases(t *testing.T, cases [][2]string) {
	t.Helper()
	pre, err := prelude.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		f, err := syntax.Parse("t.kg", []byte("package t\n"+c[0]))
		if err != nil {
			t.Fatal(err)
		}
		_, errs := Check(pre, []*syntax.File{f}, nil)
		if c[1] == "" && errs != nil || c[1] != "" && (len(errs) != 1 || !strings.HasPrefix(errs[0].Error(), c[1])) {
			t.Errorf("%s\n got %v\nwant %q", c[0], errs, c[1])
		}
	}
}

// Section 3: the types, the number rules and what each declaration
// may hold.
func TestTypes(t *testing.T) {
	passes(t,
		"typealias Cents = Long\nval zero: Cents = 0\nfun f(a: Cents, n: Int): Cents = a * n / 100 - -1\nfun g(a: Cents): Boolean = a == 0L && a < 1 && 1 <= a\nfun h(a: Cents?): Cents = a ?: 0\nfun k(): Cents { return 0 }",
		"fun f(a: Double, n: Int): Double = a * n.toDouble() + 1.5",
		"fun f(c: Char, s: String, b: Boolean, u: Unit): String = \"$c $s $b ${f(c, s, !b, Unit)}\" + \"\"\"raw\"\"\"",
		"typealias Pairs<T> = List<Pair<T, T>>\nfun f(ps: Pairs<Int>): Pair<Int, Int> = ps.first()",
		"data class P(val a: Int, val b: String = \"\")\nfun f(p: P): P = p.copy(b = p.toString())\nfun g(p: P): Int {\n    val (a, b) = p\n    return a + b.length\n}\nfun h(): Boolean = P(1) == P(b = \"x\", a = 2)",
		"enum class E { a, b }\nfun f(e: E): String = e.name + E.a.name\nfun g(): List<E> = E.entries\nfun h(s: String): E = E.valueOf(value = s)",
		"interface I<K> {\n    val label: String\n    fun card(key: K): Int\n}\ndata class D(override val label: String, val n: Int) : I<Int> {\n    override fun card(key: Int): Int = key + n + label.length\n}\ninterface J {\n    fun size(): Int\n}\ndata object O : J {\n    override fun size(): Int = 0\n}\nfun f(i: I<Int>, d: D, j: J): Int = i.card(1) + d.card(2) + i.label.length + j.size() + O.size()",
		// An out type parameter is given back: in a val, a result, a list, a lambda's parameter.
		"sealed interface B<out T> {\n    val all: List<T>\n    fun each(f: (T) -> Unit): B<T>\n}",
		"sealed interface S<out T>\ndata class A<T>(val v: T) : S<T>\ndata object B : S<Nothing>\nfun f(c: Boolean): S<Int> = if (c) A(1) else B\nfun g(c: Boolean): Int {\n    val s = if (c) A(1) else B\n    return when (s) {\n        is A -> s.v\n        B -> 0\n    }\n}",
		"fun f(g: (Int, String) -> Long, h: (() -> Unit)?): Long = g(1, \"\")",
		// A value fits a wider type: T? for T, and through an out parameter.
		"fun f(xs: List<Int>, m: Map<String, Int>): Map<String, Int?> = g(xs, m)\nfun g(xs: List<Int?>, m: Map<String, Int?>): Map<String, Int?> = m",
		// A minus is taken with its literal; a Long's digits stand alone.
		"val min: Int = -2147483648\nval big: Long = 3000000000L\nval wide: Long = -3000000000\nval low: Long = -9223372036854775807L",
		"interface I {\n    fun card(key: Int): Int\n}\nfun <T> card(i: I, key: T, n: Int = 0): T = key",
	)
	refused(t, [][3]string{
		// Section 3.2: only a sealed interface holds types, its variants.
		{"interface GoogleMaps {\n    ‸data class Place(val id: String)\n}", "nested-type", "declare Place at the top level"},
		{"sealed interface O {\n    data class Ok(val id: Int) : O\n    ‸data class Note(val text: String)\n}", "nested-type", "O.Note is not a variant of O"},
		{"interface I {\n    ‸typealias N = Int\n}", "nested-type", "a typealias is declared at the top level"},
		{"fun f(n: Int): Long = ‸n", "type-mismatch", "this is a Int where a Long is wanted: write .toLong()"},
		{"fun f(n: Long): Int = ‸n", "type-mismatch", "write .toInt()"},
		{"fun f(): Double = ‸1", "type-mismatch", "this is a Int where a Double is wanted: write .toDouble()"},
		{"fun f(a: Long): Boolean = a ‸== 0", "type-mismatch", "== compares a Long with a Int, which are never equal: write the literal with L"},
		{"fun f(a: Long, d: Double): Double = a ‸* d", "number-mix", "* mixes a Long with a Double: write .toDouble()"},
		{"fun f(n: Int, d: Double): Boolean = d ‸< n", "number-mix", "< mixes a Double with a Int: write .toDouble()"},
		{"fun f(n: Int, s: String): Int = n + ‸s", "type-mismatch", "+ takes a number here, and this is a String"},
		{"fun f(s: String): Int = ‸-s", "type-mismatch", "- takes a number"},
		{"fun f(s: String): String = s + ‸1", "type-mismatch", "this is a Int where a String is wanted"},
		{"fun f(a: Boolean, b: Boolean): Boolean = a ‸< b", "no-overload", "not defined for a Boolean: no binding declares compareTo for it"},
		{"fun f(s: String, n: Int): Boolean = s ‸== n", "type-mismatch", "== compares a String with a Int, which are never equal"},
		{"fun f(n: Int): Boolean = ‸n && true", "type-mismatch", "this is a Int where a Boolean is wanted"},
		{"fun f(xs: ‸List): Int = 1", "type-arguments", "List takes 1 type arguments, and 0 are written"},
		{"‸typealias A = List<A>", "type-arguments", "typealias A names itself"},
		{"data class P(val a: Int)\nfun f(): P = P(‸\"x\")", "type-mismatch", "this is a String where a Int is wanted"},
		{"data class P(val a: Int, val b: Int = 0)\nfun f(): P = ‸P(b = 1)", "arguments", "P needs an argument for a"},
		{"data class P(val a: Int)\nfun f(p: P): P = p.‸copy(c = 1)", "arguments", "copy has no parameter named c"},
		{"data class P(val a: Int)\nfun f(p: P): Int {\n    ‸val (a, b) = p\n    return a\n}", "destructure", "2 names are written for a P, which has 1 parts"},
		{"data class P(val a: Int = ‸\"x\")", "type-mismatch", "this is a String where a Int is wanted"},
		{"data class P(val a: Int)\nfun f(p: P): Int = p.‸b", "unknown-name", "a P has no property b"},
		{"enum class E { a, b }\nfun f(): E = E.‸c", "unknown-name", "E has no constant c: its constants are a, b"},
		{"enum class E { a }\nfun f(): E = ‸E", "not-a-value", "E is a type, not a value"},
		{"enum class E { a }\nfun f(): E = ‸E()", "not-callable", "only a data class is made by calling its name"},
		{"enum class E { a }\nfun f(): E = E.valueOf(‸1)", "type-mismatch", "this is a Int where a String is wanted"},
		{"enum class E { a }\nfun f(): E = E.‸values().first()", "unknown-name", "E has no function values"},
		{"interface I { val label: String }\n‸data class D(val n: Int) : I", "override", "D implements I: give its constructor override val label: String"},
		{"interface I { val label: String }\n‸data object O : I", "override", "a data object has no constructor to give val label: make O a data class, or label a fun in I"},
		{"sealed interface B<out T> {\n    fun put(‸value: T): B<T>\n}", "variance", "T is out in B, so a B gives a T back and takes none (spec.md 3.2): remove out, or make put an extension of B<T>"},
		{"sealed interface B<out T> {\n    ‸fun each(): (T) -> Unit\n}", "variance", "make each an extension of B<T>"},
		{"sealed interface B<out T> {\n    ‸val keys: Map<T, Int>\n}", "variance", "T is out in B"},
		{"interface I { fun card(key: Int): Int }\n‸data class D(val n: Int) : I", "override", "D implements I: give it override fun card(key: Int): Int"},
		{"interface I<K> { fun card(key: K): Int }\ndata class D(val n: Int) : I<Int> {\n    override ‸fun card(key: String): Int = 1\n}", "override", "this does not match I<Int>: write override fun card(key: Int): Int"},
		{"interface I\ndata class D(val n: Int) : I {\n    override ‸fun card(): Int = 1\n}", "override", "no interface of D declares fun card(): Int"},
		{"interface I\ndata class D(‸override val n: Int) : I", "override", "no interface of D declares a val n of this type: remove override"},
		{"data class P(val n: Int)\ndata class D(val n: Int) : ‸P", "override", "P is not an interface"},
		{"sealed interface S\n---\npackage t\ndata object O : ‸S", "override", "S is sealed: declare its variant O in f1.kg"},
		{"fun f(m: Map<String, Int>): Map<String?, Int> = ‸m", "type-mismatch", "this is a Map<String, Int> where a Map<String?, Int> is wanted"},
		{"‸val n = 1", "val-type", "a top-level val states its type: val n: Type"},
		{"data class P(val n: Int)\nfun f(p: P?): Int = p.‸n", "nullable", "this P? may be null: write ?.n, or test it for null first"},
		{"fun big(): Int = ‸3000000000", "number-range", "past an Int's range, where Kotlin reads it as a Long (spec.md 3): write 3000000000L"},
		{"fun big(): Double = ‸3000000000", "number-range", "write 3000000000L"},
		{"fun big(x: Long): Boolean = x == ‸3000000000", "number-range", "write 3000000000L"},
		{"val n: Int = ‸-2147483649", "number-range", "write -2147483649L"},
		{"val n: Long = ‸9223372036854775808L", "number-range", "past a Long's range"},
		{"val n: Long = ‸-9223372036854775808", "number-range", "even after a minus"},
		{"interface I {\n    ‸fun <T> card(key: T): Int\n}", "interface-fun", "make card a top-level fun that takes the value"},
		{"interface I {\n    fun card(‸key: Int = 0): Int\n}", "interface-fun", "state key at each call, or add a top-level fun that does"},
		{"interface Texty {\n    ‸fun string(): String\n}", "go-name", "fun string() with no parameter becomes Go's String method"},
		{"interface I {\n    ‸fun String(): String\n}", "go-name", "fun String() with no parameter becomes Go's String method"},
	})
}

// A value that holds a function is not compared (spec.md 3), where the
// type shows one: Kotlin compares functions by reference, Go not at all.
func TestFunctionEquality(t *testing.T) {
	rule := "data class Rule(val name: String, val test: (Int) -> Boolean)\n"
	passes(t,
		"data class P(val n: Int, val next: P?, val all: List<P>)\nfun f(a: P, b: P?): Boolean = a == b || b != null",
		"fun f(g: (() -> Unit)?): Boolean = g == null",
		"fun f(g: (() -> Unit)?): Int = when (g) {\n    null -> 1\n    else -> 2\n}",
		"interface I { val n: Int }\nfun f(a: I, b: I): Boolean = a == b",
	)
	refused(t, [][3]string{
		{"fun f(g: (Int) -> Boolean): Boolean = g ‸== g", "function-equality", "== compares a (Int) -> Boolean, a function, which is never compared: compare a name kept beside it"},
		{"fun f(a: List<() -> Unit>): Boolean = a ‸== a", "function-equality", "== compares a List<() -> Unit>, which holds a function, which is never compared"},
		{rule + "fun f(r: Rule, o: Rule): Int = when (r) {\n    ‸o -> 1\n    else -> 2\n}", "function-equality", "this when compares a Rule, and Rule's property test holds a function"},
		{"interface I { val f: () -> Unit }\nfun f(a: I, b: I): Boolean = a ‸== b", "function-equality", "I's property f"},
		{rule + "fun f(r: Rule): Boolean = r ‸!= r", "function-equality", "!= compares a Rule, and Rule's property test holds a function, which is never compared: compare the other properties"},
		{rule + "sealed interface S {\n    data object A : S\n    data class B(val r: Rule) : S\n}\nfun f(s: S): Boolean = s ‸== S.A", "function-equality", "compares a S, and Rule's property test"},
		{rule + "fun f(a: List<Rule>, b: List<Rule>): Boolean = a ‸== b", "function-equality", "compares a List<Rule>, and Rule's property test"},
		{rule + "fun f(a: Rule?, b: Rule): Boolean = a ‸== b", "function-equality", "compares a Rule?, and Rule's property test"},
		{rule + "fun f(a: Pair<String, Rule>, b: Pair<String, Rule>): Boolean = a ‸== b", "function-equality", "Rule's property test"},
		{"data class N(val next: N?, val f: () -> Unit)\nfun f(a: N): Boolean = a ‸== a", "function-equality", "N's property f"},
	})
}

// Nor is a ByteArray, which Kotlin compares by identity (spec.md 3):
// it is named, passed and held, and has no operations.
func TestByteArray(t *testing.T) {
	file := "data class File(val name: String, val data: ByteArray)\n"
	passes(t, file+"fun f(a: ByteArray, b: ByteArray?): File? = if (b == null) File(\"a\", a) else null")
	refused(t, [][3]string{
		{"fun f(a: ByteArray, b: ByteArray): Boolean = a ‸== b", "bytes-equality", "== compares a ByteArray, which Kotlin compares by identity, not by its bytes: compare the other properties, or what the bytes were made from"},
		{file + "fun f(a: File, b: File): Boolean = a ‸!= b", "bytes-equality", "!= compares a File, and File's property data holds a ByteArray, which Kotlin compares by identity"},
		{file + "fun f(a: List<File?>, b: List<File?>): Boolean = a ‸== b", "bytes-equality", "File's property data holds a ByteArray"},
		{"fun f(a: ByteArray): Int = a.‸size", "unknown-name", "a ByteArray has no property size"},
		{"fun f(a: ByteArray): Int = a‸[0]", "no-overload", "this operator is not defined for a ByteArray"},
		{"fun f(a: ByteArray?): String = \"x ${‸a}\"", "bytes-text", "a ByteArray? in a template is written as Kotlin writes its identity, [B@ and a number that changes every run: write a name or a size kept beside it"},
	})
}

// Nor is a Regex, which Kotlin compares by identity (spec.md 14).
func TestRegexEquality(t *testing.T) {
	re := "\n---\npackage kogo\nexternal class Regex\nexternal fun Regex(pattern: String): Regex"
	rule := "data class Rule(val name: String, val re: Regex)\n"
	passes(t, rule+"fun f(a: Rule, b: Regex?): Boolean = a.name == \"x\" && b != null && \"$b\" == \"a+\""+re)
	refused(t, [][3]string{
		{"fun f(): Boolean = Regex(\"a+\") ‸== Regex(\"a+\")" + re, "regex-equality", "== compares a Regex, which Kotlin compares by identity, not by its pattern: compare the patterns, as strings"},
		{rule + "fun f(a: Rule, b: Rule): Boolean = a ‸!= b" + re, "regex-equality", "!= compares a Rule, and Rule's property re holds a Regex"},
		{rule + "fun f(a: List<Rule>, b: List<Rule>): Boolean = a ‸== b" + re, "regex-equality", "Rule's property re holds a Regex"},
	})
}

// Nor is a handle, which Kotlin compares by identity (spec.md 3).
func TestHandleEquality(t *testing.T) {
	env := "data class Env(val name: String, val clock: Clock)\n"
	clock := "\n---\npackage kogo.time\nexternal class Clock"
	passes(t, "fun f(db: Db?): Boolean = db == null")
	refused(t, [][3]string{
		{"fun f(a: Db, b: Db): Boolean = a ‸== b", "handle-equality", "== compares a Db, which Kotlin compares by identity, not by what it holds: compare the other properties"},
		{"import kogo.time.Clock\n" + env + "fun f(a: Env, b: Env): Boolean = a ‸!= b" + clock, "handle-equality", "!= compares a Env, and Env's property clock holds a Clock"},
	})
}

// Section 4: absence and narrowing.
func TestNarrowing(t *testing.T) {
	passes(t,
		// Each test narrows a name where its outcome is known.
		"fun f(x: Int?, y: Int?): Int = if (x != null && y != null) x + y else 0",
		"fun f(x: Int?): Int = if (x == null) 0 else x",
		"fun f(x: Int?): Int = if (x == null || x < 0) 0 else x",
		"fun f(x: Int?): Int = if (!(x == null)) x else 0",
		"fun f(x: Int?): Boolean = x != null && x > 0",
		"fun f(x: Int?): Int = when {\n    x == null -> 0\n    else -> x\n}",
		"fun f(x: Int?): Int = when (x) {\n    null -> 0\n    else -> x\n}",
		// And for the rest of the block when the other branch cannot finish.
		"fun f(x: Int?): Int {\n    if (x == null) return 0\n    return x\n}",
		"fun f(x: Int?): Int {\n    if (x != null) {\n    } else {\n        error(\"none\")\n    }\n    return x\n}",
		"fun f(x: Int?): Int {\n    x ?: return 0\n    return x\n}",
		"fun f(xs: List<Int?>): Int {\n    var n = 0\n    for (x in xs) {\n        if (x == null) continue\n        n += x\n    }\n    return n\n}",
		// A lambda's parameter narrows; a safe call's lambda has the value.
		"fun f(xs: List<Int?>): List<Int> = xs.map { if (it != null) it else 0 }",
		"fun f(x: Int?): Int = x?.let { it + 1 } ?: 0",
		"fun f(x: Int?): Int = requireNotNull(x) { \"none\" } + 1",
		"sealed interface S {\n    data class A(val n: Int) : S\n    data object B : S\n}\nfun f(s: S?): Int = if (s is S.A) s.n else 0\nfun g(s: S): Int {\n    if (s is S.B) return 0\n    return 1\n}",
	)
	refused(t, [][3]string{
		{"fun f(): Int = ‸null", "type-mismatch", "this is a Nothing? where a Int is wanted"},
		{"fun f(x: Int?): Int = ‸x", "type-mismatch", "it may be null, so test it first, or give the value to use instead with ?:"},
		{"fun f(x: Int?): Int = ‸x + 1", "nullable", "this Int? may be null: test it for null first, or give the value to use instead with ?:"},
		{"fun f(s: String?): String = ‸s + \"\"", "nullable", "this String? may be null"},
		{"fun f(x: Int?): Int = if (x != null) 0 else ‸x", "type-mismatch", "this is a Int? where a Int is wanted"},
		{"fun f(x: Int?, y: Int?): Int = if (x != null || y != null) ‸x else 0", "type-mismatch", "this is a Int? where a Int is wanted"},
		// A var never narrows, nor a path, nor does require.
		{"fun f(a: Int?): Int {\n    var x = a\n    if (x != null) return ‸x\n    return 0\n}", "type-mismatch", "this is a Int? where a Int is wanted"},
		{"data class P(val n: Int?)\nfun f(p: P): Int = if (p.n != null) ‸p.n else 0", "type-mismatch", "this is a Int? where a Int is wanted"},
		{"fun f(x: Int?): Int {\n    require(x != null) { \"none\" }\n    return ‸x\n}", "type-mismatch", "this is a Int? where a Int is wanted"},
		// The narrowing ends with the block that the test rules.
		{"fun f(x: Int?): Int {\n    if (x != null) {\n        val y = x + 1\n    }\n    return ‸x\n}", "type-mismatch", "this is a Int? where a Int is wanted"},
		{"interface I\ndata class D(val n: Int) : I\nfun f(i: I): Boolean = i is ‸D", "is-variant", "is tests for a variant of a sealed interface, and this value is a I"},
		{"sealed interface S\ndata object A : S\ndata object O\nfun f(s: S): Boolean = s is ‸O", "is-variant", "O is not a variant of S: its variants are A"},
	})
}

// Section 5: functions, type arguments (5.1) and overloads (5.2).
func TestCalls(t *testing.T) {
	passes(t,
		// 5.1: the receiver, the plain arguments, the wanted type, then
		// each lambda, settle a type parameter in that order.
		"fun f(xs: List<Int>): List<String> = xs.map { \"$it\" }",
		"fun f(): List<Int?> = listOf(1, null)",
		"fun f(): List<Long> = emptyList()\nfun g(): List<Long> = listOf<Long>(1, 2)\nfun h(xs: List<Long>?): List<Long> = xs ?: emptyList()",
		"fun f(): Int = emptyList<Int>().size + listOf(1).map { it }.first()",
		// The left of ?: settles a call's on its right.
		"fun f(xs: List<Long>?): Int {\n    val ys = xs ?: emptyList()\n    return ys.size + (xs ?: emptyList()).size\n}",
		// but not one with a lambda argument, whose lambda settles it.
		"sealed interface S {\n    data class A(val v: Long) : S\n    data class B(val v: Long) : S\n}\nfun <R> make(block: () -> R): R = block()\nfun f(a: S.A?, b: S.B): S {\n    val s = a ?: make { b }\n    return s\n}",
		"fun <T> twice(use: (T) -> Int, make: () -> T): Int = use(make())\nfun f(): Int = twice({ x: String -> x.length }, { \"\" })",
		"fun <T> firstOr(items: List<T>, fallback: T): T = if (items.size > 0) items.first() else fallback\nfun f(xs: List<Long>): Long = firstOr(xs, 0L) + firstOr(emptyList<Long>(), 0L)",
		"sealed interface S<out T> {\n    data class A<T>(val v: T) : S<T>\n    data object B : S<Nothing>\n}\nfun f(): List<S<Int>> = listOf(S.A(1), S.B)",
		// 5.2: the count, the names and the types choose; then what one lambda returns.
		"fun g(a: Int): Int = a\nfun g(a: String): Int = 0\nfun g(a: Int, b: Int): Int = a + b\nfun f(): Int = g(1) + g(\"\") + g(1, 2)",
		"fun g(a: Int): Int = a\nfun g(b: String): Int = 0\nfun f(): Int = g(b = \"\")",
		"fun g(a: Long): Long = a\nfun g(a: String): Long = 0\nfun f(): Long = g(1) + g(-1)",
		"fun f(xs: List<String>): Long = xs.sumOf { it.length } + xs.sumOf { 1L }",
		// Types of one name in two packages are not the same type.
		"package a\ndata class Foo(val n: Int)\n---\npackage b\ndata class Foo(val n: Int)\n---\npackage t\nimport a.Foo\nfun g(x: Foo): Int = 1\n---\npackage t\nimport b.Foo\nfun g(x: Foo): Int = 2",
		"fun g(a: Long, b: Int = 2, c: () -> Int): Long = a + b + c()\nfun f(): Long = g(1) { 3 } + g(1, c = { 3 }) + g(a = 1, b = 2, c = { 3 })",
		"fun Int.double(): Int = this * 2\nval Int.half: Int\n    get() = this / 2\nfun Int.f(): Int = double() + half + 1.double() + 2.half",
		"fun f(xs: List<Int>): Int {\n    fun inner(n: Int): Int = if (n > 0) inner(n - 1) else xs.size\n    return inner(3)\n}",
		"fun g(n: Int): Int = n\nfun f(xs: List<Int>): List<Int> {\n    val h: (Int) -> Int = ::g\n    val k = { a: Int, b: Int -> a + b }\n    return xs.map(h).map { k(it, 1) }.zip(xs).map { (a, b) -> a + b }.map { n -> n }\n}",
		"fun f(): String = Text.of(1)",
		"val Int.half: Int\n    get() = this / 2\nval Long.half: Long\n    get() = this / 2\nfun f(a: Int, b: Long): Long = a.half + b.half",
		"data class D(val step: (Int) -> Int)\nval twice: (Int) -> Int = { it * 2 }\nfun D.f(): Int = step(1) + twice(2) + this.step(3)",
		"data class D(val step: (Int) -> Int)\nfun f(d: D?): Int? = d?.step(1)",
		// this's interface fun comes before an extension of its name.
		"interface I {\n    fun cost(n: Long): Long\n}\ndata class D(val k: Long) : I {\n    override fun cost(n: Long): Long = n * k\n}\nfun D.cost(n: Long): Long = n\nfun D.total(): Long = cost(1L) + this.cost(2L)",
		"fun <T> List<T>.second(): T = this[1]\nfun <T> pick(xs: List<T>, n: Int): T = if (n == 1) xs.second() else pick(xs, n - 1)",
	)
	refused(t, [][3]string{
		// The three rows of the table in 5.1.
		{"fun f(): Int {\n    val xs = ‸emptyList()\n    return 1\n}", "type-arguments", "nothing settles emptyList's type parameter T: write its type arguments, emptyList<...>"},
		{"fun f(): Int = ‸listOf(1, \"a\").size", "no-common-type", "the arguments that settle T are Int, String, which have no common type: there is no Any"},
		{"fun <T> twice(use: (T) -> Int, make: () -> T): Int = use(make())\nfun f(): Int = twice({ ‸x -> 1 }, { \"\" })", "lambda-parameters", "nothing settles the type of x before this lambda is read: state it, as { x: Type -> ... }"},
		{"fun <T> twice(use: (T) -> Int, make: () -> T): Int = use(make())\nfun f(): Int = twice(‸{ 1 }, { \"\" })", "lambda-parameters", "nothing settles the type of it"},
		{"fun f(): Int = ‸emptyList<Int, Int>().size", "type-arguments", "emptyList has 1 type parameters: write all of its type arguments, or none"},
		// A literal is an Int unless a Long is expected of it, and the
		// arguments settle T before the type the result must have.
		{"fun f(): List<Long> = ‸listOf(1, 2)", "type-mismatch", "this is a List<Int> where a List<Long> is wanted"},
		{"fun f(n: Long): List<Long> = ‸listOf(n, 2)", "no-common-type", "the arguments that settle T are Long, Int, which have no common type: write each whole number as a Long, 1L"},
		{"fun f(c: Boolean): Int {\n    val x = ‸if (c) 1 else \"a\"\n    return 1\n}", "no-common-type", "the values here are Int, String, which have no common type: state the type the result must have"},
		{"fun f(n: Long?): Long {\n    val x = n ‸?: 0\n    return x\n}", "no-common-type", "the values here are Long, Int, which have no common type: write the whole number as a Long, 0L"},
		// Both errors of 5.2.
		{"fun g(a: Int): Int = a\nfun g(a: String): Int = 0\nfun f(): Int = ‸g(1L)", "no-overload", "no g fits this call with (Long): the candidates are fun g(a: Int): Int; fun g(a: String): Int"},
		{"fun g(a: Int): Int = a\nfun g(a: Int?): Int = 0\nfun f(): Int = ‸g(1)", "ambiguous-call", "2 declarations of g fit this call and nothing ranks them: rename one of fun g(a: Int): Int; fun g(a: Int?): Int"},
		// Two declarations no call could choose between are refused where declared.
		{"fun g(a: Int): Int = a\n‸fun g(b: Int): Long = 0", "duplicate", "fun g(a: Int): Int is already declared in package t with this receiver and these parameter types: rename one"},
		{"fun <T> h(xs: List<T>): Int = 1\n‸fun <U> h(ys: List<U>): Int = 2", "duplicate", "fun <T> h(xs: List<T>): Int is already declared"},
		{"val Int.half: Int\n    get() = this / 2\n---\npackage t\n‸val Int.half: Long\n    get() = 0", "duplicate", "val Int.half: Int is already declared"},
		{"package kogo.x\nexternal fun Int.twice(): Int\n---\npackage kogo.y\nexternal ‸fun Int.twice(): Long", "duplicate", "fun Int.twice(): Int is already declared in package kogo.x"},
		{"private fun g(a: Int): Int = a\n---\n‸fun g(a: Int): Long = 0", "duplicate", "fun g(a: Int): Int is already declared"},
		{"fun <T, U> h(a: T, b: U): Int = 1\n‸fun <U, T> h(a: T, b: U): Int = 2", "duplicate", "fun <T, U> h(a: T, b: U): Int is already declared"},
		{"interface I {\n    fun card(key: Int): Int\n    ‸fun card(k: Int): Long\n}", "duplicate", "fun card(key: Int): Int is already declared in I"},
		{"data class P(val a: Int)\n‸fun P(b: Int): Int = b", "duplicate", "the constructor of P takes these parameter types: rename this fun"},
		// A type that failed to read causes no second error.
		{"typealias A = ‸Foo\nfun g(a: A): Int = 1\nfun g(b: A): Int = 2", "unknown-name", "Foo"},
		{"fun f(xs: List<String>): Int = xs.‸sumOf { \"\" }", "no-overload", "no sumOf fits this call with (a lambda giving String)"},
		{"fun g(a: Int): Int = a\nfun f(): Int = ‸g(1, 2)", "arguments", "g takes 1 arguments, and 2 are written: it is fun g(a: Int): Int"},
		{"fun g(a: Int): Int = a\nfun f(): Int = ‸g(b = 1)", "arguments", "g has no parameter named b"},
		{"fun g(a: Int): Int = a\nfun f(): Int = ‸g(1, a = 2)", "arguments", "g is given a twice"},
		{"fun f(xs: List<Int>): Int = xs.‸first(1)", "arguments", "first takes 0 arguments, and 1 are written"},
		{"fun f(n: Int): Int = n.‸size", "unknown-name", "a Int has no property size"},
		{"fun f(n: Int): Int = n.‸first()", "arguments", "first is not called on a Int"},
		{"fun f(n: Int): Int = n.‸nope()", "unknown-name", "a Int has no function nope"},
		{"fun Int.double(): Int = this * 2\nfun f(): Int = ‸double()", "arguments", "double is called on a value: it is fun Int.double(): Int"},
		{"fun f(s: String?): Int = s.‸compareTo(\"\")", "nullable", "this String? may be null: write ?.compareTo, or test it for null first"},
		{"fun f(n: Int): Int = ‸n(1)", "not-callable", "this is a Int, which is not a function"},
		// Kotlin calls the function job past the local; Kogo's local hides it (spec.md 6).
		{"fun job(): Int = 1\nfun f(): Int {\n    val job = 2\n    return ‸job() + job\n}", "not-callable", "this is a Int, which is not a function"},
		{"fun f(g: (Int) -> Int): Int = ‸g(1, 2)", "arguments", "is called with its 1 arguments in order"},
		{"data class D(val step: () -> Long)\nfun d(n: Long): D = D { n }\nfun f(): Long = d(‸\"s\").step()", "type-mismatch", "this is a String where a Long is wanted"},
		{"fun f(xs: List<Int>): List<Int> = xs.map ‸{ a, b -> a }", "lambda-parameters", "this lambda names 2 parameters where 1 are passed to it"},
		{"fun f(xs: List<Int>): List<Int> = xs.map { x: ‸String -> 1 }", "type-mismatch", "this parameter is passed a Int, which is not a String"},
		{"fun f(xs: List<Int>): List<Int> = xs.map { ‸\"a\" }", "type-mismatch", "this is a String where a Int is wanted"},
		// A fun states its return type unless it is Unit.
		{"‸fun f() = 1", "return-type", "f gives a Int: state its return type, fun f(...): Int"},
		{"‸fun f(c: Boolean): Int {\n    if (c) return 1\n}", "missing-return", "f can reach its end without a value: end it with return and a Int"},
		{"fun f(): Int {\n    ‸return\n}", "type-mismatch", "return needs a value here: a Int"},
		{"fun f() {\n    return ‸1\n}", "type-mismatch", "this is a Int where a Unit is wanted"},
		{"fun g(): Int = 1\nfun g(a: Int): Int = a\nval h: () -> Int = ::‸g", "unknown-name", "::g needs one top-level fun g"},
		{"fun f(): Int = ‸this", "unknown-name", "there is no this here"},
	})
}

// Section 6: statements and expressions.
// A val on a cycle still has its own read of a val below told.
func TestValOnCycleReadsBelow(t *testing.T) {
	_, errs := run(t, "val a: Int = x\n---\npackage t\nval x: Int = a + y\nval y: Int = 1")
	if len(errs) != 2 || !strings.Contains(errs[0].Message, "a refers to a itself, through x") || !strings.Contains(errs[1].Message, "declare y above x") {
		t.Errorf("got %v, want a's cycle and x's read of y", errs)
	}
}

func TestStatements(t *testing.T) {
	passes(t,
		"enum class E { a, b }\nfun f(e: E, c: Boolean, n: Int): Int = when (e) {\n    E.a -> when (c) {\n        true -> 1\n        false -> 2\n    }\n    E.b -> when (n) {\n        0, 1 -> 3\n        in 2..9 -> 4\n        else -> 5\n    }\n}",
		"enum class E { a, b }\nfun f(e: E?): Int {\n    when (e) {\n        E.a -> return 1\n        E.b -> return 2\n        null -> return 3\n    }\n}",
		"fun f(n: Int, s: String) {\n    when (n) {\n        1 -> require(n > 0) { s }\n    }\n    if (n > 0) require(n > 1) { s }\n}",
		"fun f(xs: List<Int>, m: Map<String, Int>, n: Long): Long {\n    var total = 0L\n    for (x in xs) total += x\n    for (i in 0 until n) total += i\n    for ((k, v) in m) total += k.length + v\n    for (i in 1..3) {\n        if (i in 2..3 || i in xs) break\n    }\n    while (total > 100) total -= 1\n    return total\n}",
		"fun f(xs: List<Int>, m: Map<String, Int>): Int? {\n    var all = xs + 1\n    all += 2\n    val p = \"a\" to all[0]\n    val (name, n) = p\n    return m[name]\n}",
		"fun f(c: Boolean, s: String?): String {\n    val t = s ?: return \"\"\n    xs(c)\n    return if (c) t else \"no\"\n}\nfun xs(c: Boolean): Int = if (c) 1 else if (!c) 2 else 3",
		"fun f(xs: List<Int>) {\n    var n = 0\n    xs.forEach { n += it }\n    xs.forEach { g(it) }\n    xs.forEach { if (it > 0) n += 1 }\n}\nfun g(n: Int): Int = n",
		// A lambda whose value is a wanted Unit may end with Unit.
		"fun f(xs: List<Int>) {\n    xs.forEach {\n        g(it)\n        Unit\n    }\n}\nfun g(n: Int): Int = n",
		"fun f(a: String, b: String): Boolean = a < b && a >= b",
		"fun f(): Int {\n    fun g(x: Int): Int = x\n    return g(1)\n}",
		// A val reads one above it, one in another file, or a fun.
		"val a: Int = 1\nval b: Int = a + f()\nval c: Int = d\nfun f(): Int = a\n---\npackage t\nval d: Int = 1",
		// A call through an interface is not followed, as in Go; a data
		// class is not its file's, and a file may use one that uses it.
		"interface I {\n    fun card(): Int\n}\ndata class P(val n: Int) : I {\n    override fun card(): Int = c\n}\nfun f(i: I): Int = i.card()\nval c: Int = f(P(1))",
		"val a: Int = P(1).n\n---\npackage t\ndata class P(val n: Int)\nval c: Int = a",
		"val a: Int = f()\n---\npackage t\nfun f(): Int = 1\nfun g(): Int = a",
	)
	refused(t, [][3]string{
		{"enum class E { a, b, c }\nfun f(e: E): Int = ‸when (e) {\n    E.a -> 1\n}", "when-not-exhaustive", "this when over a E does not cover b, c: add a branch for each, or an else"},
		{"sealed interface S {\n    data class A(val n: Int) : S\n    data object B : S\n}\nfun f(s: S): Int = ‸when (s) {\n    is S.A -> 1\n}", "when-not-exhaustive", "does not cover S.B"},
		{"fun f(c: Boolean): Int = ‸when (c) {\n    true -> 1\n}", "when-not-exhaustive", "does not cover false"},
		{"enum class E { a }\nfun f(e: E?): Int = ‸when (e) {\n    E.a -> 1\n}", "when-not-exhaustive", "this when over a E? does not cover null"},
		{"fun f(c: Boolean): Int = ‸when {\n    c -> 1\n}", "when-needs-else", "a when with no subject needs an else branch"},
		{"fun f(n: Int): Int = ‸when (n) {\n    1 -> 1\n}", "when-needs-else", "a when over a Int that is used as a value needs an else branch"},
		{"fun f(n: Int): Int = when (n) {\n    ‸\"a\" -> 1\n    else -> 2\n}", "type-mismatch", "this String is never equal to the when's subject, a Int"},
		{"fun f(n: Int, xs: List<Int>): Int = when (n) {\n    ‸in xs -> 1\n    else -> 2\n}", "range-not-value", "a when's in takes a range: in a..b"},
		{"fun f(c: Boolean): Int = ‸if (c) 1", "if-needs-else", "an if that is used as a value needs an else"},
		{"fun f(c: Int): Int = if (‸c) 1 else 2", "type-mismatch", "this is a Int where a Boolean is wanted"},
		{"fun f(n: Int) {\n    val x = 1\n    ‸x = n\n}", "val-assigned", "x is not a local var: declare it with var"},
		{"fun f(n: Int) {\n    ‸n += 1\n}", "val-assigned", "n is not a local var"},
		{"fun f() {\n    var x = 1\n    x = ‸\"a\"\n}", "type-mismatch", "this is a String where a Int is wanted"},
		{"fun f(n: Long) {\n    var x = 1\n    ‸x += n\n}", "type-mismatch", "this is a Long where a Int is wanted"},
		{"fun f() {\n    val x = 1\n    ‸val x = 2\n}", "duplicate", "x is already declared in this block"},
		{"fun f(): Int {\n    ‸break\n}", "outside-loop", "break is written in a for or a while"},
		{"fun f(xs: List<Int>) {\n    for (x in xs) xs.forEach { ‸continue }\n}", "outside-loop", "and not in a lambda there"},
		{"fun f(n: Int) {\n    for (x in ‸n) {}\n}", "type-mismatch", "for goes over a List, a Set, a Map or a range, and this is a Int"},
		{"fun f(d: Double) {\n    for (x in 0 ‸.. d) {}\n}", "type-mismatch", "a range runs between Ints or Longs, and these are a Int and a Double"},
		{"fun f(): Int {\n    val r = 1 ‸.. 3\n    return 1\n}", "range-not-value", "a range is not a value: write it after in"},
		{"fun f(s: String): Boolean = ‸s in 1..3", "type-mismatch", "a range holds Ints or Longs, and this is a String"},
		{"fun f(n: Int): Boolean = n ‸in 3", "no-overload", "this operator is not defined for a Int: no binding declares contains for it"},
		{"fun f(a: Boolean, b: Int): Boolean = ‸a to b", "type-mismatch", "this is a Pair<Boolean, Int> where a Boolean is wanted"},
		// A statement that is only a value.
		{"fun f(a: Int, b: Int): Int {\n    val y = a\n    ‸- b\n    return y\n}", "value-unused", "this statement is only a value, which nothing uses"},
		{"fun f(a: Int) {\n    ‸a\n}", "value-unused", "assign it, return it or remove it"},
		{"fun f(xs: List<Int>) {\n    ‸xs.size\n}", "value-unused", "only a value"},
		{"fun f(xs: List<Int>) {\n    xs.forEach { ‸it }\n}", "value-unused", "only a value"},
		{"fun f(c: Boolean, a: Int) {\n    if (c) ‸a else g()\n}\nfun g() {}", "value-unused", "only a value"},
		{"fun f(a: Int?, b: Int) {\n    ‸a ?: b\n}", "value-unused", "only a value"},
		{"fun f(): Int {\n    ‸fun <T> id(x: T): T = x\n    return id(1)\n}", "local-fun", "a local fun takes no type parameters (spec.md 5): declare id at the top level"},
		{"‸val a: Int = b\nval b: Int = 1", "val-order", "the value of a refers to b, which is declared below it in this file (spec.md 6): declare b above a"},
		{"‸val a: Int = f()\nfun f(): Int = g { b }\nfun g(h: () -> Int): Int = 1\nval b: Int = 1", "val-order", "refers to b, through f, which"},
		{"‸val c: Int = g()\nfun g(): Int = c", "val-order", "the value of c refers to c itself, through g (spec.md 6): compute it without c, or make one of these a fun"},
		{"‸val a: Int = b\n---\npackage t\nval b: Int = a", "val-order", "refers to a itself, through b"},
		// A cycle in one file is told once, as a cycle.
		{"‸val a: Int = b\nval b: Int = a", "val-order", "the value of a refers to a itself, through b"},
		// A local fun's body is the body of the fun around it.
		{"‸val c: Int = f()\nfun f(): Int {\n    fun g(): Int = c\n    return 1\n}", "val-order", "refers to c itself, through f, g"},
		// An interface's fun called on a data class is its override.
		{"interface I {\n    fun card(): Int\n    fun size(): Int\n}\ndata class P(val n: Int) : I {\n    override fun card(): Int = c\n    override fun size(): Int = card()\n}\n‸val c: Int = P(1).size()", "val-order", "refers to c itself, through size, card"},
		// Files whose vals use each other, by a val or by a fun.
		{"‸val a: Int = b\n---\npackage t\nval b: Int = 1\nval c: Int = a", "val-order", "the vals of f1.kg, f2.kg use each other's files (spec.md 6): a refers to b in f2.kg; c refers to a in f1.kg"},
		{"‸val a: Int = f()\n---\npackage t\nfun f(): Int = 1\nval c: Int = g()\nfun g(): Int = a", "val-order", "a refers to f in f2.kg; c refers to a in f1.kg, through g"},
	})
}

// Section 6: a jump stands where a value goes straight to a val, an
// assignment, a return or a result, and nowhere inside a larger
// expression, which the emitter wraps in a Go function.
func TestJumps(t *testing.T) {
	passes(t,
		"fun f(s: String?, c: Boolean): String {\n    val t = s ?: return \"\"\n    var u = t\n    u = if (c) t else return \"x\"\n    return when (c) {\n        true -> s ?: return \"y\"\n        false -> u\n    }\n}",
		"fun f(s: String?): String = s ?: return \"none\"",
		"fun f(xs: List<Int?>, limit: Int?): Int {\n    var n = 0\n    for (x in xs) {\n        val y = x ?: continue\n        n = if (y > 0) n + y else break\n    }\n    return n\n}",
		"fun f(xs: List<Int>): Int = xs.first().let {\n    fun half(n: Int): Int {\n        if (n < 2) return 0\n        return n / 2\n    }\n    half(it)\n}",
		// A loop inside the larger expression holds its own break.
		"fun g(n: Int): Int = n\nfun f(xs: List<Int>, c: Boolean): Int = g(if (c) {\n    var n = 0\n    for (x in xs) {\n        if (x > 2) break\n        n += x\n    }\n    n\n} else 0)",
	)
	fix := "bind the value to a val first"
	refused(t, [][3]string{
		{"fun g(n: Int): Int = n\nfun f(x: Int?): Int = g(x ?: ‸return 0)", "buried-jump", fix},
		{"fun f(x: Int?): Int {\n    val y = 1 + (x ?: ‸return 0)\n    return y\n}", "buried-jump", "this return is buried in a larger expression"},
		{"fun f(x: Int?, c: Boolean): Int {\n    if (c && (x ?: ‸return 0) > 1) return 1\n    return 2\n}", "buried-jump", fix},
		{"fun g(n: Int): Int = n\nfun f(c: Boolean): Int = g(if (c) {\n    ‸return 1\n} else 2)", "buried-jump", fix},
		{"fun g(n: Int): Int = n\nfun f(xs: List<Int?>): Int {\n    var n = 0\n    for (x in xs) n += g(x ?: ‸continue)\n    return n\n}", "buried-jump", "val v = x ?: continue"},
		{"fun f(xs: List<Int?>): Int {\n    for (x in xs) {\n        val y = (x ?: ‸break) + 1\n    }\n    return 1\n}", "buried-jump", "this break is buried"},
		// A lambda holds no return; a local fun inside one does.
		{"fun f(xs: List<Int>): Int {\n    xs.forEach { if (it > 1) ‸return it }\n    return 0\n}", "kotlin-lambda-return", "end the lambda with its value, and return after the call that runs it"},
		{"fun f(x: Int?): Int = x?.let { ‸return it } ?: 0", "kotlin-lambda-return", "a lambda holds no return"},
		// n += v is n = n + v: the value is an operand.
		{"fun f(xs: List<Int?>): Int {\n    var n = 0\n    for (x in xs) n += x ?: ‸break\n    return n\n}", "buried-jump", fix},
	})
}

// Section 9: expected failures. Each row of both tables, then each
// rule broken.
func TestFailures(t *testing.T) {
	passes(t,
		// The forms of a raise scope, either's block.
		"fun f(n: Int): Either<String, Int> = either {\n    if (n < 0) raise(\"negative\")\n    n\n}",
		"fun f(n: Int): Either<String, Int> = either {\n    ensure(n > 0) { \"not positive\" }\n    n\n}",
		"fun f(n: Int?): Either<String, Int> = either { ensureNotNull(n) { \"none\" } + 1 }",
		"fun g(): Either<String, Int> = either { 1 }\nfun f(): Either<String, Int> = either { g().bind() + 1 }",
		// An Either of a variant is bound where its sealed interface is raised.
		"sealed interface S {\n    data object A : S\n}\nfun g(): Either<S.A, Int> = either { raise(S.A) }\nfun f(): Either<S, Int> = either { g().bind() }",
		// The type arguments are written, or settled by the type wanted.
		"fun f(): Int? = either<String, Int> { 1 }.getOrNull()",
		// A lambda inside a raise scope is in it.
		"fun g(n: Int): Either<String, Int> = either { n }\nfun f(xs: List<Int>): Either<String, List<Int>> = either { xs.map { g(it).bind() } }",
		// A fun of the program's takes a lambda that fails into no
		// scope around it, or only into one inside it.
		"fun <T> run(block: () -> T): T = block()\nfun f(n: Int): Either<String, Int> = either { run { either<String, Int> { raise(\"no\") }.getOrNull() } ?: raise(\"none\") }",
		// In a raise scope a member of this does not hide the core's
		// forms: where only one fits, it is the call. Outside one, the
		// member is.
		"interface V {\n    fun raise(r: Int): Int\n}\nfun V.f(): Either<String, Int> = either { raise(\"no\") }",
		"interface V {\n    fun raise(r: Int): Int\n}\nfun V.f(): Either<String, Int> = either { raise(1) }",
		"interface V {\n    fun ensure(n: Int): Boolean\n}\nfun V.f(n: Int): Either<String, Int> = either {\n    ensure(n > 0) { \"negative\" }\n    n\n}",
		"interface V {\n    fun raise(r: String): Int\n}\nfun V.f(): Int = raise(\"no\")",
		// Working with an Either value.
		"fun f(c: Boolean): Either<String, Int> = if (c) 1.right() else \"no\".left()",
		"fun f(x: Either<String, Int>): String = x.fold({ e -> e }, { n -> \"$n\" })",
		"fun f(x: Either<Int, Int>): Either<String, Int> = x.mapLeft { \"$it\" }",
		"fun f(x: Either<String, Int>): Either<String, Long> = x.map { it.toLong() }",
		"fun f(x: Either<String, Int>): Either<String, String> = x.flatMap { if (it > 0) \"$it\".right() else \"no\".left() }",
		"fun f(x: Either<String, Int>): Int = x.getOrElse { 0 } + (x.getOrNull() ?: 1)",
		"fun f(x: Either<String, Int>): String = when (x) {\n    is Either.Left -> x.value\n    is Either.Right -> \"${x.value}\"\n}",
	)
	refused(t, [][3]string{
		// A failure of the wrong type.
		{"fun f(): Either<String, Int> = either { raise(‸1) }", "type-mismatch", "this is a Int where a String is wanted"},
		{"fun f(): Either<String, Int> = either {\n    ensure(true) { ‸1 }\n    1\n}", "type-mismatch", "this is a Int where a String is wanted"},
		{"fun g(): Either<Int, Int> = either { 1 }\nfun f(): Either<String, Int> = either { g().‸bind() }", "raise-type", "bind can fail with a Int, and this raise scope fails with a String: convert the failure, .mapLeft { e -> ... }.bind()"},
		{"interface V {\n    fun raise(r: String): Int\n}\nfun V.f(): Either<String, Int> = either { ‸raise(\"no\") }", "ambiguous-call", "both the raise scope's raise and V's fit this call, and nothing ranks them: rename V's raise"},
		{"interface V {\n    fun raise(r: String): Int\n}\nfun V.f(): Either<String, Int> = either { this.‸raise(\"no\") }", "ambiguous-call", "in Kotlin this.raise inside either { } is the raise scope's raise, not V's: rename V's raise"},
		{"interface V {\n    fun ensure(c: Boolean, f: () -> String)\n}\nfun V.f(): Either<String, Int> = either {\n    this.‸ensure(true) { \"no\" }\n    1\n}", "ambiguous-call", "rename V's ensure"},
		{"interface V {\n    fun ensureNotNull(n: Int?, f: () -> String): Int\n}\nfun V.f(n: Int?): Either<String, Int> = either { this.‸ensureNotNull(n) { \"none\" } }", "ambiguous-call", "rename V's ensureNotNull"},
		// The forms outside a raise scope.
		{"fun g(): Either<String, Int> = either { 1 }\nfun f(): Int = g().‸bind()", "raise-scope", "bind can fail with a String, and this is not a raise scope: wrap it in either<String, ...> { }"},
		{"fun f(): Int = ‸raise(\"no\")", "raise-scope", "raise can fail with a String"},
		// A lambda that fails into the raise scope around it is not
		// kept as a value, which could run outside the scope.
		{"fun f(): Either<String, () -> Int> = either { ‸{ raise(\"no\") } }", "raise-escape", "this lambda fails into the raise scope around it, and is kept as a value, which could run outside the scope: pass it straight to the call that runs it, or make it return an Either<String, A>"},
		{"fun g(): Either<String, Int> = either { 1 }\nfun f(): Either<String, () -> Int> = either { ‸{ g().bind() } }", "raise-escape", "pass it straight to the call"},
		{"fun f(): Either<String, Int> = either {\n    val g: () -> Int = ‸{ raise(\"no\") }\n    g()\n}", "raise-escape", "pass it straight to the call"},
		// Nor is it passed to a fun of the program's, which could keep
		// it: the prelude's funs run it first.
		{"fun <T> later(block: () -> T): () -> T = block\nfun f(): Either<String, () -> Int> = either { later ‸{ raise(\"no\") } }", "raise-escape", "this lambda fails into the raise scope around it, and is passed to later, which could keep it and run it outside the scope: only the prelude's funs are known to run it first; make it return an Either<String, A>, and bind what later gives"},
		{"fun <T> run(block: () -> T): T = block()\nfun f(xs: List<Int>): Either<String, List<Int>> = either { run ‸{ xs.map { if (it < 0) raise(\"no\") else it } } }", "raise-escape", "is passed to run"},
		// A local fun is not in the raise scope around it.
		{"fun f(): Either<String, Int> = either {\n    fun g(): Int = ‸raise(\"no\")\n    g()\n}", "raise-scope", "this is not a raise scope"},
		// A dropped Either.
		{"fun g(): Either<String, Int> = either { 1 }\nfun f() {\n    ‸g()\n}", "either-dropped", "this Either<String, Int> is dropped, and a failure with it: bind it, fold it, return it or assign it"},
		{"fun g(n: Int): Either<String, Int> = either { n }\nfun f(xs: List<Int>) {\n    xs.forEach { ‸g(it) }\n}", "either-dropped", "is dropped"},
		// Nothing settles what either's block raises: spec.md 5.1's first row.
		{"fun f(): Int {\n    val r = ‸either { 1 }\n    return 1\n}", "type-arguments", "nothing settles either's type parameter E before its block is read: write its type arguments, either<...> { ... }"},
		// A program writes no Raise<E>, and a fun with that receiver
		// says no more of what its body raises. Either is built with
		// left and right.
		{"fun ‸Raise<String>.f(): Int = raise(\"no\")", "raise-receiver", "Raise<String> is not written in a program (spec.md 9): return an Either<String, A> from either<String, A> { ... }, and bind it where it is called"},
		{"fun f(r: ‸Raise<String>) {}", "raise-receiver", "return an Either<String, A>"},
		{"fun g(): Either<Int, Int> = either { 1 }\nfun f(): Either<String, Int> = either { ‸withError({ n: Int -> \"$n\" }) { g().bind() } }", "with-error", "withError is not Kogo: convert the failure of a block with either<E2, A> { ... }.mapLeft { e -> ... }.bind()"},
		{"package b\nfun withError(x: Int): Int = x\n---\nfun f(): Int = ‸withError(1)", "unknown-name", "withError is not in scope: add import b.withError"},
		{"fun f(g: ‸Raise<String>.() -> Int) {}", "function-receiver", "a function type has no receiver (spec.md 5): take the value as a parameter, or for Raise<E> return an Either<E, A>"},
		{"fun f(g: ‸Int.() -> Int) {}", "function-receiver", "take the value as a parameter"},
		{"fun f(): Either<String, Int> = Either.‸Left(\"no\")", "either-constructor", "an Either is built with e.left() or a.right()"},
		{"sealed interface S {\n    data class P private constructor(val n: Int) : S\n}\n---\nfun f(): S = S.‸P(1)", "not-visible", "S.P's constructor is private to f1.kg"},
		// A block is a lambda, and a call that fails says no more of it.
		{"fun f(g: () -> Int): Either<String, Int> = either(‸g)", "type-mismatch", "this is a () -> Int where a Raise<String>.() -> Int is wanted"},
		{"fun f(): Either<String, Int> = ‸either(1) { raise(\"no\") }", "arguments", "either is given block twice"},
	})
	// A this that stands as a value inside a raise scope is Kotlin's
	// Raise and Kogo's receiver, so it is refused; a member read through
	// it, which kotlinc refuses, and one named before the block are not.
	acct := "data class Acct(val name: String)\nfun g(a: Acct): String = a.name\n"
	rename := "in Kotlin this inside either { } or db.tx { } is the raise scope, not this Acct: name it in a val before the block, as val acct = this, and use that"
	refused(t, [][3]string{
		{acct + "fun Acct.f(): Either<String, String> = either { \"I am $‸this\" }", "raise-this", rename},
		{acct + "fun Acct.f(): Either<String, String> = either { g(‸this) }", "raise-this", rename},
		{acct + "fun Acct.f(): Either<String, String> = either { ‸this.toString() }", "raise-this", rename},
		{acct + "fun Acct.f(): Either<String, String> = either { ‸this.let { \"I am $it\" } }", "raise-this", rename},
		{acct + "fun Acct.f(): Either<String, String> = either { \"I am ${‸this.right()}\" }", "raise-this", rename},
		{acct + "fun Acct.f(): Either<String, String> = either { ‸toString() }", "raise-this", rename},
		{acct + "fun Acct.f(): Either<String, String> = either { ‸let { \"I am $it\" } }", "raise-this", rename},
		{acct + "fun Acct.f(): Either<String, List<String>> = either { listOf(1).map { g(‸this) } }", "raise-this", rename},
		{acct + "fun Acct.f(db: Db): Either<String, String> = db.tx { tx -> g(‸this) }", "raise-this", rename},
		// A local fun is not in the raise scope, and Kotlin's this there is still the Raise.
		{acct + "fun Acct.f(): Either<String, String> = either {\n    fun h(): String = g(‸this)\n    h()\n}", "raise-this", rename},
		{"interface Shown {\n    fun show(): Either<String, String>\n}\ndata class Box(val n: Int) : Shown {\n    override fun show(): Either<String, String> = either { \"box $‸this\" }\n}", "raise-this", "not this Box: name it in a val before the block, as val box = this"},
	})
	passes(t, acct+"fun Acct.f(): Either<String, String> = either { this.name + name + this.f2() }\nfun Acct.f2(): String = \"I am $this\"\nfun Acct.f3(): Either<String, String> {\n    val acct = this\n    return either { g(acct) }\n}")
	// Only a refused Raise<E> receiver keeps quiet about the body: an
	// unknown type there still lets raise-scope be said.
	if _, errs := run(t, "fun Foo.f(): Int = raise(\"no\")"); len(errs) != 2 || errs[0].Code != "unknown-name" || errs[1].Code != "raise-scope" {
		t.Errorf("an unknown receiver: got %v, want unknown-name then raise-scope", errs)
	}
}

// Section 10: the five calls, their result types and their parameters,
// each rule broken. The literals spell their own signatures, as
// signature reads them.
func TestSQL(t *testing.T) {
	passes(t,
		`data class Row(val id: Uuid, val legalName: String, val abn: String?)
fun f(tx: Tx, id: Uuid): List<Row> = tx.query<Row>(sql"$id: Uuid -> id: Uuid, legal_name: String, abn: String?")
fun g(tx: Tx): Row? = tx.queryOne<Row>(sql" -> abn: String, legal_name: String, id: Uuid")
fun h(tx: Tx): String? = tx.queryOne<String?>(sql" -> abn: String?")
fun k(tx: Tx): Long = tx.querySingle<Long>(sql" -> n: Long")
fun m(tx: Tx, id: Uuid): Boolean = tx.exists(sql"$id: Uuid -> v: Void")
fun n(tx: Tx): Int = tx.execute(sql" -> v: Void")`,
		// Nested data classes, one on the absent side of an outer join.
		`data class Tenant(val id: Uuid, val slug: String)
data class Row(val id: Uuid, val tenant: Tenant?, val owner: Tenant)
fun f(tx: Tx): List<Row> = tx.query<Row>(sql" -> id: Uuid, tenant_id: Uuid??, tenant_slug: String??, owner_id: Uuid, owner_slug: String")`,
		// On the absent side, a column never null (a coalesce) or
		// asserted ("name!") fills a property that may not be null,
		// and the class is told there by a column null only through
		// the join, in itself or in a class it holds that is not
		// nullable.
		`data class Tenant(val id: Uuid, val slug: String)
data class Shop(val tenant: Tenant, val note: String?)
data class Row(val tenant: Tenant?, val shop: Shop?)
fun f(tx: Tx): List<Row> = tx.query<Row>(sql" -> tenant_id: Uuid??, tenant_slug: String, shop_tenant_id: Uuid??, shop_tenant_slug: String??, shop_note: String?")`,
		// Whole rows of a join, one on the absent side, a column of a
		// type made with create type, whose fields may be null, and a
		// row holding one.
		`data class Business(val id: Uuid, val legalName: String, val abn: String?)
data class Tenant(val id: Uuid, val slug: String)
data class Amount(val cents: Long?, val currency: String?)
data class Quote(val id: Uuid, val deposit: Amount?)
data class Listing(val business: Business, val storefront: Tenant?, val total: Amount, val quote: Quote)
fun f(tx: Tx): List<Listing> = tx.query<Listing>(sql" -> business: business(id: Uuid, legal_name: String, abn: String?), storefront: tenant(slug: String, id: Uuid)?, total: amount(cents: Long?, currency: String?), quote: quote(id: Uuid, deposit: amount(cents: Long?, currency: String?)?)")`,
		// A T? parameter, an enum by its constants, lists, a name
		// written twice, a typealias and a type of kogo.time.
		`import kogo.time.Instant
enum class Kind { b, a }
typealias Cents = Long
data class P(val ids: List<Uuid>, val kind: Kind, val at: Instant)
fun f(tx: Tx, kind: Kind?, ids: List<Uuid?>, p: P, active: Boolean?, total: Cents): List<P> =
    tx.query<P>(sql"$active: Boolean, $kind: K{a b}, $ids: List<Uuid>, ${p.ids}: List<Uuid>, $total: Long, $active -> ids: List<Uuid>, kind: K{a b}, at: Instant")
---
package kogo.time
external class Instant`,
	)
	refused(t, [][3]string{
		{"data class Row(val id: Uuid)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> id: Uuid, legal_name: String\")", "sql-column-unused",
			"column legal_name fills no property of Row: add a property legalName, or leave the column out"},
		{"data class Row(val id: Uuid, val legalName: String)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> id: Uuid\")", "sql-column-missing",
			"no column fills Row.legalName: select one named legal_name, with as legal_name if need be"},
		{"data class T(val id: Uuid, val slug: String)\ndata class Row(val tenant: T?)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> tenant_id: Uuid??\")", "sql-column-missing",
			"no column fills T.slug: select one named tenant_slug"},
		{"data class Row(val id: Uuid)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> id: Long\")", "sql-column-type",
			"column id is a Long, and fills Row.id, a Uuid: make that a Long, or cast the column in the statement"},
		{"enum class Kind { a, b }\nfun f(tx: Tx): Kind = tx.querySingle<Kind>(‸sql\" -> kind: K{a c}\")", "sql-column-type",
			`column kind is an enum, and fills the result, a Kind: its label "c" is not in Kind, and Kind has constant b that it lacks: make the constants of Kind its labels`},
		{"enum class Kind { a }\nfun f(tx: Tx): List<Kind> = tx.query<Kind>(‸sql\" -> kind: K{a b c}\")", "sql-column-type",
			`its labels "b", "c" are not in Kind: make the constants of Kind its labels`},
		// An enum with a label that is no name has no Kogo type, whatever it fills.
		{"enum class Kind { a, in_house }\nfun f(tx: Tx): Kind? = tx.queryOne<Kind>(‸sql\" -> kind: K{a in-house b}\")", "sql-column-type",
			`column kind is an enum with label "in-house", which cannot name a constant (spec.md 10.4), and so has no Kogo type: rename it in Postgres, or cast the column to text and read it as a String`},
		{"data class Row(val kind: String)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> kind: K{in-house x.y}\")", "sql-column-type",
			`enum with labels "in-house", "x.y", which cannot name a constant (spec.md 10.4), and so has no Kogo type: rename them in Postgres, or cast`},
		{"fun f(tx: Tx): List<String> = tx.querySingle<List<String>>(‸sql\" -> kinds: List<K{a in}>\")", "sql-column-type",
			`enum with label "in", which cannot name a constant (spec.md 10.4), and so has no Kogo type: rename it in Postgres, or cast the column to text[] and read it as a List<String>`},
		{"enum class Kind { a }\nfun f(tx: Tx): Kind = tx.querySingle<Kind>(‸sql\" -> kind: K{a _}\")", "sql-column-type",
			`enum with label "_", which cannot name a constant`},
		{"data class Row(val abn: String)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> abn: String?\")", "sql-nullable",
			`column abn may be null, and fills Row.abn, a String: make that a String?, or if the column is never null, name it as "abn!"`},
		{"fun f(tx: Tx): String = tx.querySingle<String>(‸sql\" -> abn: String?\")", "sql-nullable", "fills the result, a String: make that a String?"},
		// Inside a nullable data class, a column null in its table fills
		// only a T?, and the class needs a column null only through the
		// join in a property that is not nullable.
		{"data class T(val id: Uuid, val slug: String)\ndata class Row(val tenant: T?)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> tenant_id: Uuid??, tenant_slug: String?\")", "sql-nullable",
			`column tenant_slug may be null even where its outer join finds a row, and fills T.slug, a String, in a nullable data class that is null where that join finds none: make that a String?, or if the column is never null, name it as "tenant_slug!"`},
		{"data class T(val id: Uuid?, val abn: String?)\ndata class Row(val tenant: T?)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> tenant_id: Uuid??, tenant_abn: String?\")", "sql-absent",
			"Row.tenant, a T?, has no property that is not nullable filled by a column null only through an outer join, so a row of nulls would read as no T: " +
				"select a NOT NULL column of the joined table for one, as in t.id as tenant_id, or the table's whole row, as in select t as tenant"},
		// A nullable data class whose columns no outer join can null, as
		// returning * gives, is refused whole, in words that name no join.
		{"data class C(val amount: Long, val currency: String?)\ndata class Row(val price: C?)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> price_amount: Long?, price_currency: String?\")", "sql-absent",
			"Row.price, a C?, is filled by columns that may each be null on their own: read each as a T? property of Row in its place, and build the C from them in Kogo"},
		{"data class T(val slug: String)\ndata class S(val t: T?)\ndata class Row(val s: S?)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> s_t_slug: String??\")", "sql-absent",
			"Row.s, a S?, has no property"},
		{"fun f(tx: Tx): Long = tx.querySingle<Long>(‸sql\" -> a: Long, b: Long\")", "sql-columns",
			"this statement gives 2 columns, and a Long is one: read them into a data class with a property for each, or select one column"},
		{"fun f(tx: Tx): List<Long> = tx.query<Long>(‸sql\"\")", "sql-columns", "this statement gives no columns: call execute, or add returning"},
		{"data class Row(val n: Long)\nfun f(tx: Tx): Row? = tx.queryOne<Row?>(‸sql\" -> n: Long\")", "sql-columns", "a row is never null: write Row, not Row?"},
		{"fun f(tx: Tx, id: String): Int = tx.execute(sql\"‸$id: Uuid, $id\")", "sql-parameter-type",
			"$id is a String, and Postgres takes a Uuid there: pass a Uuid, or cast it in the statement, as $id::type"},
		{"fun f(tx: Tx, ids: List<Int>): Int = tx.execute(sql\"‸${ids}: List<Long>\")", "sql-parameter-type", "${ids} is a List<Int>, and Postgres takes a List<Long> there"},
		// A composite type's fields fill a data class as columns do.
		{"data class B(val id: Uuid, val name: String)\ndata class Row(val business: B)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> business: business(id: Uuid)\")",
			"sql-field-missing", "no field of column business fills B.name: business has no field name; name the property after one of its fields, or select columns"},
		{"data class B(val id: Uuid)\ndata class Row(val business: B)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> business: business(id: Uuid, legal_name: String)\")",
			"sql-field-unused", "field legal_name of column business fills no property of B: add a property legalName, or select the columns you want"},
		{"data class B(val id: Long)\ndata class Row(val business: B)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> business: business(id: Uuid)\")",
			"sql-column-type", "field id of column business is a Uuid, and fills B.id, a Long: make that a Uuid"},
		{"data class A(val cents: Long)\ndata class Row(val total: A)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> total: amount(cents: Long?)\")",
			"sql-nullable", "field cents of column total may be null, and fills A.cents, a Long: make that a Long?"},
		{"data class T(val id: Uuid)\ndata class Row(val storefront: T)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> storefront: tenant(id: Uuid)?\")",
			"sql-nullable", `column storefront may be null, and fills Row.storefront, a T: make that a T?, or if the column is never null, name it as "storefront!"`},
		{"data class Row(val business: String)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> business: business(id: Uuid)\")",
			"sql-column-type", "column business is a row of business, and fills Row.business, a String: make that a data class with a property for each of its fields"},
		// select b.*, where the whole row was meant.
		{"data class B(val id: Uuid, val legalName: String)\ndata class Row(val business: B)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> id: Uuid, legal_name: String\")",
			"sql-column-missing", "no column fills Row.business, a B: select a table's whole row as business, as in select b as business"},
		// A sibling's own column, business_id, is not the prefix form.
		{"data class B(val id: Uuid, val legalName: String)\ndata class Row(val id: Uuid, val businessId: Uuid, val business: B)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> id: Uuid, business_id: Uuid, legal_name: String\")",
			"sql-column-missing", "no column fills Row.business, a B: select a table's whole row as business"},
		{"data class B(val legalName: String, val legal_name: String)\ndata class Row(val business: B)\nfun f(tx: Tx): List<Row> = tx.query<Row>(‸sql\" -> business: business(legal_name: String)\")",
			"sql-field-missing", "field legal_name of column business would fill two properties, B.legal_name among them: rename one of them"},
		// Reading a row builds R and each data class in it, as their
		// constructors do.
		{"package a\nimport c.H\nfun f(tx: Tx): List<H> = tx.query<H>(‸sql\" -> b_n: Int\")\n---\npackage c\ndata class B internal constructor(val n: Int)\ndata class H(val b: B)",
			"not-visible", "B's constructor is internal to package c"},
		// The SQL pass's refusal, at its place in the literal.
		{"fun f(tx: Tx): Int = tx.execute(sql\"\"\"\n    select\n    ‸«\"\"\")", "sql-rejected", "Postgres refused this"},
		{"fun f(db: Db): Int = ‸db.execute(sql\"\")", "type-mismatch", "this is a Db where a Tx is wanted"},
		// A Tx runs no utility statement, as commit or set, in a test
		// file too (Postgres's tags, sqlpass.TestTables).
		{"fun f(tx: Tx): Int = tx.execute(‸sql\"@utility:TRANSACTIONSTMT\")", "sql-utility", "(Postgres's TRANSACTIONSTMT), and a Tx runs select, insert, update, delete and merge alone: db.tx begins and ends the transaction"},
		{"fun f(tx: Tx): Boolean = tx.exists(‸sql\"@utility:VARIABLESETSTMT\")", "sql-utility", "a setting is bound with select set_config('name', $value, true)"},
		{"/*test*/\nfun f(tx: Tx): List<Int> = tx.query<Int>(‸sql\"@utility:VARIABLESETSTMT\")", "sql-utility", "(Postgres's VARIABLESETSTMT)"},
	})
	// A data class with no column at all hides only the columns it
	// would take, not one that no property takes.
	src := "data class T(val slug: String)\ndata class Row(val id: Uuid, val owner: T)\nfun f(tx: Tx): List<Row> = tx.query<Row>(sql\" -> id: Uuid, slug: String, typo: Int\")"
	if _, errs := run(t, src); len(errs) != 2 || errs[0].Code != "sql-column-missing" || !strings.HasPrefix(errs[1].Message, "column typo fills no property") {
		t.Errorf("a missing data class: got %v, want sql-column-missing then typo unused", errs)
	}
}

// Section 10.7: db.tx, whose block is a raise scope where the
// enclosing fun returns an Either, and tx.atomically.
func TestTransactions(t *testing.T) {
	passes(t,
		"fun f(db: Db): Int = db.tx { tx -> tx.execute(sql\"\") }",
		"fun f(db: Db): Int = db.tx(serializable = true) { tx -> tx.execute(sql\"\") }",
		"fun f(db: Db, n: Int): Either<String, Int> = db.tx { tx ->\n    if (n < 0) raise(\"negative\")\n    tx.execute(sql\"\")\n}",
		// The enclosing fun settles E, though nothing is wanted.
		"fun f(db: Db): Either<String, Int> {\n    val r = db.tx { tx -> raise(\"no\") }\n    return r\n}",
		"fun f(db: Db, n: Int): Either<String, Int> = db.tx { tx ->\n    tx.atomically {\n        ensure(n > 0) { \"not positive\" }\n        n\n    }\n}",
		"fun f(tx: Tx): Int = tx.atomically { tx.execute(sql\"\") }",
		"fun Db.f(): Either<String, Int> = tx { tx -> raise(\"no\") }",
		// A SerializableTx fits a Tx, and db.tx(serializable = true)
		// hands its block one, in either form.
		"fun g(tx: Tx): Int = tx.execute(sql\"\")\nfun h(tx: SerializableTx): Int = g(tx) + tx.execute(sql\"\")\nfun f(db: Db): Int = db.tx(serializable = true) { tx -> h(tx) }",
		"fun h(tx: SerializableTx): Int = 1\nfun f(db: Db): Either<String, Int> = db.tx(serializable = true) { tx -> ensure(h(tx) > 0) { \"no\" }\n    1\n}",
	)
	refused(t, [][3]string{
		{"fun h(tx: SerializableTx): Int = 1\nfun f(tx: Tx): Int = h(‸tx)", "needs-serializable", "h takes a SerializableTx and this is a Tx, which is not rerun on a conflict: call a function that locks, take a SerializableTx, or open"},
		{"fun h(tx: SerializableTx): Int = 1\nfun f(db: Db): Int = db.tx { tx -> h(‸tx) }", "needs-serializable", "h takes a SerializableTx and this is a Tx, which is not rerun on a conflict: call a function that locks, take a SerializableTx, or open"},
		{"fun f(db: Db): Int = db.tx { tx -> ‸raise(\"no\") }", "raise-scope", "this is not a raise scope"},
		{"fun f(db: Db): Either<String, Int> = db.tx { tx -> raise(‸1) }", "type-mismatch", "this is a Int where a String is wanted"},
		{"fun f(db: Db): Either<String, Int> {\n    ‸db.tx { tx -> 1 }\n    return 1.right()\n}", "either-dropped", "this Either<String, Int> is dropped"},
	})
}

// Section 11: a marked fun, val or constructor is used only in a file
// that opts in to its marker.
func TestMarkers(t *testing.T) {
	marked := "package a\n@RequiresOptIn\nannotation class M\n@M\nfun f(): Int = 1\n@M\nval limit: Int = 2\ndata class H @M constructor(val n: Int)\n---\n"
	passes(t,
		marked+"@file:OptIn(M::class)\npackage b\nimport a.M\nimport a.f\nimport a.limit\nfun g(): Int = f() + limit",
		marked+"package b\nimport a.H\nfun g(h: H): Int = h.n",
	)
	refused(t, [][3]string{
		{marked + "package b\nimport a.f\nfun g(): Int = ‸f()", "opt-in", "f is marked @M: begin this file with @file:OptIn(M::class)"},
		{marked + "package b\nimport a.limit\nfun g(): Int = ‸limit", "opt-in", "limit is marked @M"},
		{marked + "package a\nfun g(): Int = ‸f()", "opt-in", "f is marked @M"},
		{"data class X(val n: Int)\n‸@X\nfun f() {}", "marker", "X is not a marker: a marker is declared @RequiresOptIn annotation class X"},
		{"@file:OptIn(‸X::class)\npackage t\ndata class X(val n: Int)", "marker", "X is not a marker"},
		{"‸@Marker\nfun f() {}", "unknown-name", "nothing named Marker"},
		{"‸annotation class M", "marker", "@RequiresOptIn annotation class M"},
		{"@RequiresOptIn\nannotation class M\n‸@M\ndata class X(val n: Int)", "marker", "a marker goes on a fun, a val or a constructor"},
		{"@RequiresOptIn\nannotation class M\nfun f(m: ‸M) {}", "not-a-type", "M is a marker, not a type"},
	})
}

// The guarantee a marked constructor makes (spec.md 11): each way of
// making a value of the data class, refused in a file without the
// opt-in, and with it the ones the opt-in allows. A new way of making
// a data class adds a row here and a clause to section 11.
func TestMarkedConstructor(t *testing.T) {
	head := "package kogo.rpc\nexternal class Request\n---\npackage a\n@RequiresOptIn\nannotation class M\ndata class H @M constructor(val n: Int)\n---\n"
	body := "package b\nimport a.H\nimport a.M\nimport kogo.rpc.Request\ndata class Env(val n: Int)\n"
	opt := "H is marked @M: begin this file with @file:OptIn(M::class)"
	wire := "whose constructor is marked @M, and no request makes one: take the id or the slug in the input and make the value with the function that checks it"
	for _, c := range []struct{ src, code, fix, withOptIn string }{
		{"fun g(): H = ‸H(1)", "opt-in", opt, ""},
		{"fun g(h: H): H = h.‸copy(n = 2)", "opt-in", opt, ""},
		{"fun g(tx: Tx): List<H> = tx.query<H>(‸sql\" -> n: Int\")", "opt-in", opt, ""},
		// The wire stays shut with the opt-in: a request is not trusted.
		{"api ops unchecked { ‸f ungated }\n" + op("H", "Unit"), "wire", "f's input is a H, " + wire, wire},
		{"data class In(val h: H?)\napi ops unchecked { ‸f ungated }\n" + op("In", "Unit"), "wire", "f's input holds In.h: H?, " + wire, wire},
		{"api ops unchecked { ‸f ungated }\n" + op("List<H>", "Unit"), "wire", "f's input holds a H, " + wire, wire},
		// An answer reaches In first; the input is walked still.
		{"data class In(val h: H)\napi ops unchecked { g ungated, ‸f ungated }\n" + op("In", "Unit") + strings.ReplaceAll(op("Unit", "In"), "fun f", "fun g"), "wire", "f's input holds In.h: H, " + wire, wire},
	} {
		refused(t, [][3]string{{head + body + c.src, c.code, c.fix}})
		if src := head + "@file:OptIn(M::class)\n" + body + c.src; c.withOptIn == "" {
			passes(t, strings.Replace(src, "‸", "", 1))
		} else {
			refused(t, [][3]string{{src, c.code, c.withOptIn}})
		}
	}
	// Answering with one makes none.
	passes(t, head+body+"api ops unchecked { f ungated }\n"+op("Unit", "H"))
}

// A constructor that is private or internal is a wall as a marked one
// is (spec.md 15.4): no input holds its class, alone, nested or in a
// List, and an answer may.
func TestWalledConstructorOnTheWire(t *testing.T) {
	head := "package kogo.rpc\nexternal class Request\n---\nimport kogo.rpc.Request\ndata class Env(val n: Int)\n"
	why := ", and no request makes one: take the id or the slug in the input and make the value with the function that checks it"
	for _, wall := range []string{"private", "internal"} {
		h := "data class H " + wall + " constructor(val n: Int)\n"
		ctor := "whose constructor is " + wall + why
		refused(t, [][3]string{
			{head + h + "api ops unchecked { ‸f ungated }\n" + op("H", "Unit"), "wire", "f's input is a H, " + ctor},
			{head + h + "data class In(val h: H?)\napi ops unchecked { ‸f ungated }\n" + op("In", "Unit"), "wire", "f's input holds In.h: H?, " + ctor},
			{head + h + "api ops unchecked { ‸f ungated }\n" + op("List<H>", "Unit"), "wire", "f's input holds a H, " + ctor},
		})
		passes(t, head+h+"api ops unchecked { f ungated }\n"+op("Unit", "H"))
	}
}

// For each literal, what each parameter binds and the property each
// column fills.
func TestSQLFacts(t *testing.T) {
	info, errs := run(t, `data class Tenant(val id: Uuid, val slug: String)
data class Row(val id: Uuid, val tenant: Tenant?)
fun f(tx: Tx, id: Uuid, n: Int): List<Row> = tx.query<Row>(sql"$id: Uuid, ${n + 1}: Int, $id -> tenant_slug: String??, id: Uuid, tenant_id: Uuid??")`)
	if errs != nil {
		t.Fatal(errs)
	}
	for lit, q := range info.SQL {
		if q.Row.String() != "Row" || fmt.Sprint(q.Paths) != "[[1 1] [0] [1 0]]" {
			t.Errorf("row %s filled by %v, want Row by [[1 1] [0] [1 0]]", q.Row, q.Paths)
		}
		if len(q.Args) != 2 || q.Args[0] != lit.Params[0].Expr || q.Args[1] != lit.Params[1].Expr || info.Uses[q.Args[0]].(*Var).Kind != Param {
			t.Errorf("arguments %v, want $id, the parameter, and ${n + 1}", q.Args)
		}
	}
}

// Section 9: what can fail, line by line, leaving out either's block,
// which is a raise scope.
func TestFailsFacts(t *testing.T) {
	src := `fun parsePercent(text: String): Either<String, Int> = either {
    val n = text.toIntOrNull() ?: raise("Enter a number.")
    ensure(n in 0..100) { "Enter a percentage from 0 to 100." }
    n
}
fun twice(text: String): Either<String, Int> = either {
    val n = parsePercent(text).bind()
    listOf(1, 2).map { parsePercent(text).bind() }.size + n
}`
	want := `2 call String
3 call String
7 call String
8 call String
8 lambda String
fun parsePercent String
fun twice String`
	info, errs := run(t, src)
	if errs != nil {
		t.Fatalf("%s\n%v", src, errs)
	}
	var got []string
	for n, e := range info.Fails {
		switch n := n.(type) {
		case *syntax.FunDecl:
			if info.Funcs[n].Pkg == "t" {
				got = append(got, fmt.Sprintf("fun %s %s", n.Name, e))
			}
		case *syntax.Call:
			got = append(got, fmt.Sprintf("%d call %s", n.At().Line-1, e))
		case *syntax.Lambda:
			if info.Types[n].(*FuncType).Receiver == nil {
				got = append(got, fmt.Sprintf("%d lambda %s", n.At().Line-1, e))
			}
		}
	}
	if slices.Sort(got); strings.Join(got, "\n") != want {
		t.Errorf("%s\n got %s\nwant %s", src, strings.Join(got, "\n"), want)
	}
	// A lambda that returns an Either does not fail: its value is the
	// Either, which its type says.
	info, _ = run(t, "fun f(x: Either<String, Int>): Either<String, Int> = x.flatMap { it.right() }")
	for n, e := range info.Fails {
		if _, ok := n.(*syntax.Lambda); ok {
			t.Errorf("the lambda of flatMap: got %v, want it not to fail", e)
		}
	}
	// A call that fails in a lambda of a call that failed leaves the
	// raise scope around it as it was.
	info, _ = run(t, "fun g(): Either<String, Int> = either { 1 }\nfun f(): Either<String, Int> = either<String, Int> { h(1) { g().bind() } }")
	for n, e := range info.Fails {
		if l, ok := n.(*syntax.Lambda); ok && info.Types[l].(*FuncType).Receiver != nil && e.String() != "String" {
			t.Errorf("either's block: got %v, want it to fail with String", e)
		}
	}
}

var update = flag.Bool("update", false, "rewrite testdata/*.facts from the checker")

// The facts of whole examples, as the emitter will read them: one
// line for each node that has any, in source order.
func TestFacts(t *testing.T) {
	pre, err := prelude.Files()
	if err != nil {
		t.Fatal(err)
	}
	var files []*syntax.File
	names := []string{"00_cents", "00_booking", "01_pay", "02_discount", "03_abn", "04_sealed_when", "05_enum_when", "06_nullable",
		"07_either", "09_error_layers", "20_interface_loop_time"}
	for _, name := range names {
		src, err := os.ReadFile("../examples/" + name + ".kg")
		if err != nil {
			t.Fatal(err)
		}
		f, err := syntax.Parse(name+".kg", src)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	info, errs := Check(pre, files, nil)
	if errs != nil {
		t.Fatalf("examples 00 to 07, 09 and 20: %v", errs)
	}
	for _, name := range []string{"01_pay", "04_sealed_when", "06_nullable", "07_either"} {
		i := slices.IndexFunc(files, func(f *syntax.File) bool { return f.Name == name+".kg" })
		got, golden := dump(info, files[i]), "testdata/"+name+".facts"
		if *update {
			os.WriteFile(golden, []byte(got), 0o644)
		}
		want, _ := os.ReadFile(golden)
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := range max(len(gotLines), len(wantLines)) {
			if i >= len(gotLines) || i >= len(wantLines) || gotLines[i] != wantLines[i] {
				t.Fatalf("line %d of %s:\n got %s\nwant %s", i+1, golden, at(gotLines, i), at(wantLines, i))
			}
		}
	}
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "(the end)"
}

// walk visits every node of a tree.
func walk(v reflect.Value, visit func(syntax.Node)) {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return
		}
		if n, ok := v.Interface().(syntax.Node); ok && v.Kind() == reflect.Pointer {
			visit(n)
		}
		walk(v.Elem(), visit)
	case reflect.Struct:
		for i := range v.NumField() {
			walk(v.Field(i), visit)
		}
	case reflect.Slice:
		for i := range v.Len() {
			walk(v.Index(i), visit)
		}
	}
}

// dump prints what info holds for the nodes of one file, a line for
// each fact, in source order.
func dump(info *Info, f *syntax.File) string {
	rows := map[syntax.Node][]string{}
	add := func(n syntax.Node, format string, args ...any) {
		rows[n] = append(rows[n], fmt.Sprintf(format, args...))
	}
	object := func(o Object) string {
		switch o := o.(type) {
		case *Var:
			kind := []string{"local", "parameter", "property", "top-level val", "constant"}[o.Kind]
			if o.Owner != nil {
				return kind + " " + o.Owner.Name + "." + o.Name
			}
			return kind + " " + o.Name
		case *Func:
			return []string{"", "", "method ", "constructor ", "copy ", "toString "}[o.Kind] + describe(o, nil)
		}
		return "type " + o.(*TypeDecl).Name
	}
	mine := map[syntax.Node]bool{}
	walk(reflect.ValueOf(f), func(n syntax.Node) { mine[n] = true })
	for n, t := range info.Types {
		if mine[n] {
			add(n, ": %s", t)
		}
	}
	for n, o := range info.Uses {
		if mine[n] {
			add(n, "is %s", object(o))
		}
	}
	for n := range info.Narrowed {
		if mine[n] {
			add(n, "narrowed from %s", info.Uses[n].(*Var).Type)
		}
	}
	for n, call := range info.Calls {
		if !mine[n] {
			continue
		}
		on := "no receiver"
		if call.Receiver != nil {
			on = "on its receiver"
		} else if call.Func != nil && call.Func.Receiver != nil {
			on = "on this"
			if r, ok := call.Func.Receiver.(*Named); ok && r.Decl.Name == "Raise" {
				on = "on the raise scope"
			}
		}
		var args []string
		for _, as := range call.Args {
			args = append(args, fmt.Sprint(len(as)))
		}
		if call.Func == nil {
			add(n, "calls a value, arguments per parameter [%s]", strings.Join(args, " "))
		} else {
			add(n, "calls %s%s, %s, arguments per parameter [%s]", object(call.Func), list(" with <", call.TypeArgs, ">"), on, strings.Join(args, " "))
		}
	}
	for n, vs := range info.Vars {
		for _, v := range vs {
			if mine[n] {
				add(n, "declares %s: %s", object(v), v.Type)
			}
		}
	}
	for n, fn := range info.Funcs {
		if mine[n] {
			add(n, "declares %s", object(fn))
		}
	}
	for d, td := range info.Classes {
		if mine[d] {
			add(d, "declares type %s", td.Name)
		}
	}
	for n, e := range info.Fails {
		if mine[n] {
			add(n, "fails with %s", e)
		}
	}
	var lines []string
	for n, facts := range rows {
		slices.Sort(facts)
		pos := n.At()
		lines = append(lines, fmt.Sprintf("%3d:%-3d %s", pos.Line, pos.Column, fmt.Sprintf("%T ", n)[len("*syntax."):]+strings.Join(facts, "; ")))
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n") + "\n"
}

// What kotlinc refuses, kogo check refuses (spec.md 1): each form has
// its file in kotlin/refused, which mise run kotlin checks against
// both. These are the cases beside them: what stays accepted, and
// what kotlinc takes and Kogo refuses.
func TestKotlinRefuses(t *testing.T) {
	passes(t,
		"fun f(x: Int, y: Int): Boolean = x < (y + 1)",
		"fun f(x: Int, y: Int): Boolean = x < (if (y >= 115) 1 else 2)",
		"fun g(x: Int): Int = 1\nfun g(x: Int?): Int = 2",
		"const val a: Int = 1\nconst val b: Int = -a + 1\nconst val s: String = \"x$b\"",
		"private data class H(val n: Int)\nprivate fun f(h: H): Int = h.n",
		"val <T> List<T>.two: Int\n    get() = 2",
		"fun f(x: List<Int>): Int = 1\n---\npackage t\nfun f(x: List<String>): Int = 2",
		"fun f(x: List<Int>): Int = 1\nfun f(x: List<String>): String = \"2\"",
		"fun f(x: Int): Boolean = x < (listOf<Int>(1).size)",
		"private sealed interface S {\n    data class A(val next: S?) : S\n}",
	)
	rule := "data class Rule(val name: String, val test: (Long) -> Boolean)\n"
	refused(t, [][3]string{
		{"fun f(): Int = when {\n    ‸is Int -> 33\n    else -> 34\n}", "when-branch", "is tests a when's subject, and this when has none"},
		{"fun f(g: () -> Int): String = \"${‸g}\"", "function-text", "a () -> Int in a template writes a function as Kotlin writes its class and an identity that changes every run: write a name"},
		{"val a: Int = 1\n‸fun f() = a + 1", "return-type", "state its return type, fun f(...): Int"},
		{"fun <A> f(a: A?): Int = 1\n---\npackage t\n‸fun <B> f(b: B): Int = 2", "duplicate", "rename one"},
		{"fun pick(f: () -> Int): Int = f()\nfun pick(f: () -> String): String = f()\nfun g(): Int = ‸pick { 1 }", "ambiguous-call", "rename one of"},
		{rule + "fun f(r: Rule): String = r.‸toString()", "function-text", "a Rule in toString() writes a function as Kotlin writes its class and an identity that changes every run, and Rule's property test holds one"},
		{rule + "fun f(r: Rule?): String = \"rule $‸r\"", "function-text", "a Rule? in a template writes a function as Kotlin writes its class and an identity that changes every run, and Rule's property test holds one"},
	})
}
