package emit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"kogo/check"
	"kogo/prelude"
	"kogo/sqlpass"
	"kogo/syntax"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// emit checks the files of a program, t1.kg and on, against the real
// prelude and gives what the emitter makes of them, as written under
// a directory whose import path is root: build's, which lies in this
// package's directory under the last element of root.
func emit(t *testing.T, root string, sources ...string) (*emitter, map[string][]byte, []*syntax.Error) {
	t.Helper()
	pre, err := prelude.Files()
	if err != nil {
		t.Fatal(err)
	}
	var program []*syntax.File
	for i, src := range sources {
		f, err := syntax.Parse(fmt.Sprintf("t%d.kg", i+1), []byte(src))
		if err != nil {
			t.Fatalf("%s\n%v", src, err)
		}
		program = append(program, f)
	}
	// A sql literal is checked against the test database, which the
	// driver then runs it against: $DATABASE_URL names it to both.
	var sigs check.Signatures
	if slices.ContainsFunc(program, func(f *syntax.File) bool { return f.SQL != nil }) {
		url := os.Getenv("KOGO_TEST_DATABASE_URL")
		if url == "" {
			url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
		}
		t.Setenv("DATABASE_URL", url)
		ctx := context.Background()
		db, err := sqlpass.Connect(ctx, url)
		if err != nil {
			t.Fatalf("no test database, create it with `mise run testdb`: %v", err)
		}
		defer db.Close(ctx)
		sigs = check.Signer(ctx, db)
	}
	info, errs := check.Check(pre, program, sigs)
	if errs != nil {
		t.Fatalf("%s\n%v", strings.Join(sources, "\n---\n"), errs)
	}
	e, files, errs := emitAll(info, pre, program, root, filepath.Base(root))
	return e, files, errs
}

// build writes Go files under a new directory of this package, which
// lies in the module that holds rt and the prelude's Go, and runs
// them as a program. In main.go ROOT stands for the directory's
// import path. It gives what the program prints, with its panic's
// trace when it ends in one, then its exit status when not 0, as go
// run gave it; or why it does not build. The build is apart from the
// run, so that what go says about the build, such as the modules it
// downloads, is never read as the program's output.
func build(t *testing.T, files func(root string) map[string][]byte) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", "emitted")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	root := "kogo/emit/" + filepath.Base(dir)
	for name, text := range files(root) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(text), "ROOT", root)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prog := filepath.Join(dir, "prog")
	if out, err := exec.Command("go", "build", "-o", prog, "./"+dir).CombinedOutput(); err != nil {
		return string(out)
	}
	out, err := exec.Command(prog).CombinedOutput()
	if err != nil {
		out = fmt.Appendf(out, "%v\n", err)
	}
	return string(out)
}

// run emits a program and runs a driver that prints the value of each
// Go expression of calls, in package t, a line each as toString()
// writes it, or the panic it ends in. more is Go that the driver
// declares, and setup Go that it runs first.
func run(t *testing.T, more, setup string, calls []string, sources ...string) []string {
	t.Helper()
	out := build(t, func(root string) map[string][]byte {
		_, files, errs := emit(t, root, sources...)
		if errs != nil {
			t.Fatalf("%v", errs)
		}
		driver := "package main\nimport (\n\"fmt\"\n\"kogo/rt\"\n\"ROOT/t\"\n)\n" + more + `
func show(value func() any) {
defer func() {
if p := recover(); p != nil {
fmt.Println("panics:", p)
}
}()
fmt.Println(rt.ToString(value()))
}
func main() {
` + setup
		for _, call := range calls {
			driver += "show(func() any { return " + call + " })\n"
		}
		files["main.go"] = []byte(driver + "}\n")
		return files
	})
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

// expect runs each case's Go expression against the program and
// compares what it gives, as text, with what the Kogo says it gives.
func expect(t *testing.T, cases [][2]string, sources ...string) {
	t.Helper()
	expectWith(t, "", "", cases, sources...)
}

func expectWith(t *testing.T, more, setup string, cases [][2]string, sources ...string) {
	t.Helper()
	var calls []string
	for _, c := range cases {
		calls = append(calls, c[0])
	}
	got := run(t, more, setup, calls, sources...)
	if len(got) != len(cases) {
		t.Fatalf("%d lines, want %d:\n%s", len(got), len(cases), strings.Join(got, "\n"))
	}
	for i, c := range cases {
		if got[i] != c[1] {
			t.Errorf("%s\n got %s\nwant %s", c[0], got[i], c[1])
		}
	}
}

// Evaluation is left to right where Go does not promise it: Go reads
// a variable when it likes beside a call that changes it, and reads
// arguments in the order of the parameters, not as they are written.
func TestOrder(t *testing.T) {
	expect(t, [][2]string{
		{"t.ReadThenCall()", "11"},
		{"t.Named()", "12 21"},
		{"t.Within()", "babc true"},
		{"t.Defaults()", "12 Span(from=2, to=1, step=9)"},
		{"t.Copied()", "12 Span(from=2, to=1, step=0)"},
	}, `package t

fun readThenCall(): Int {
    var n = 1
    val bump = { n = n * 10; n }
    return n + bump()
}

fun join(a: String, b: String): String = a + b

fun named(): String {
    var log = ""
    val note = { s: String -> log += s; s }
    val r = join(b = note("1"), a = note("2"))
    return log + " " + r
}

fun within(): String {
    var log = ""
    val note = { s: String -> log += s; s }
    val found = note("b") in note("abc")
    return "$log $found"
}

data class Span(val from: Int, val to: Int, val step: Int = 9)

fun defaults(): String {
    var log = ""
    val note = { n: Int -> log += "$n"; n }
    val s = Span(to = note(1), from = note(2))
    return "$log $s"
}

fun copied(): String {
    var log = ""
    val note = { n: Int -> log += "$n"; n }
    val s = Span(0, 0, 0).copy(to = note(1), from = note(2))
    return "$log $s"
}
`)
}

// == is structural for every type: Go would compare a T? by its
// address and refuses to compare a list.
func TestEquality(t *testing.T) {
	expect(t, [][2]string{
		{`t.Same(t.Leg{From: "A", Via: rt.Ptr("B"), Stops: []int32{1, 2}}, t.Leg{From: "A", Via: rt.Ptr("B"), Stops: []int32{1, 2}})`, "true"},
		{`t.Same(t.Leg{From: "A", Via: rt.Ptr("B")}, t.Leg{From: "A"})`, "false"},
		{`t.Same(t.Leg{From: "A", Stops: []int32{1, 2}}, t.Leg{From: "A", Stops: []int32{1, 3}})`, "false"},
		{`t.Differ(t.Leg{From: "A"}, t.Leg{From: "A"})`, "false"},
		{"t.IsFive(rt.Ptr[int32](5))", "true"},
		{"t.IsFive(nil)", "false"},
		{"t.Mixed()", "true true false true"},
		{"t.Kind(nil)", "none"},
		{"t.Kind(rt.Ptr[t.Shape](t.Shape_Line{Length: 3}))", "line 3"},
		{"t.LengthOf(t.Shape_Dot{})", "0"},
	}, `package t

data class Leg(val from: String, val via: String?, val stops: List<Int>)

fun same(a: Leg, b: Leg): Boolean = a == b
fun differ(a: Leg, b: Leg): Boolean = a != b
fun isFive(n: Int?): Boolean = n == 5

sealed interface Shape {
    data object Dot : Shape
    data class Line(val length: Int) : Shape
}

// A test narrows a name: a T? is read through its pointer, and a
// sealed interface as its variant.
fun kind(s: Shape?): String = when (s) {
    null -> "none"
    is Shape.Dot -> "dot"
    is Shape.Line -> "line ${s.length}"
}

fun lengthOf(s: Shape): Int = if (s is Shape.Line) s.length else 0

fun mixed(): String {
    val line: Shape = Shape.Line(2)
    val dot: Shape? = Shape.Dot
    return "${line == Shape.Line(2)} ${dot == Shape.Dot} ${line == dot} ${listOf(1.5, 2.0) == listOf(1.5, 2.0)}"
}
`)
}

// A value is used as a wider type (spec.md 5.1): a list is copied,
// since Go has no wider type for a slice, a T becomes a T?, and a
// function is wrapped.
// A Double product is rounded before it is added, as Kotlin rounds it:
// Go may fuse x*y + z into one instruction, and does on arm64. The
// driver reads x through a function that is not inlined, so the
// compiler cannot work the sum out exactly instead.
func TestFused(t *testing.T) {
	out := build(t, func(root string) map[string][]byte {
		_, files, errs := emit(t, root, `package t

fun inline(x: Double, y: Double, z: Double): Double = x * y + z

fun across(x: Double, y: Double, z: Double): Double {
    val p = x * y
    return p + z
}
`)
		if errs != nil {
			t.Fatalf("%v", errs)
		}
		files["main.go"] = []byte(`package main
import (
"fmt"
"ROOT/t"
)
//go:noinline
func x() float64 { return 1 + 1.0/(1<<30) }
func main() {
fmt.Println(t.Inline(x(), x(), -1) == 1.0/(1<<29), t.Across(x(), x(), -1) == 1.0/(1<<29))
}
`)
		return files
	})
	if out != "true true\n" {
		t.Errorf("x*y + z is rounded once: %s", out)
	}
}

func TestWidening(t *testing.T) {
	expect(t, [][2]string{
		{"t.Oks()", "3"},
		{"t.NoNulls()", "0"},
		{"t.Wrapped()", "Ok(value=4)"},
		{"t.Refusal()", "0"},
		{"t.Maybe(true)", "7"},
		{"t.Maybe(false)", "null"},
	}, `package t

sealed interface Outcome<out T> {
    data class Ok<T>(val value: T) : Outcome<T>
    data class Refused(val message: String) : Outcome<Nothing>
}

// this is narrowed, and value is read on it unwritten.
fun Outcome<Int>.orZero(): Int = when (this) {
    is Outcome.Ok -> value
    is Outcome.Refused -> 0
}

fun total(outcomes: List<Outcome<Int>>): Int = outcomes.sumOf { it.orZero() }

fun oks(): Int {
    val oks = listOf(Outcome.Ok(1), Outcome.Ok(2))
    return total(oks)
}

fun countNulls(xs: List<Int?>): Int = xs.sumOf { if (it == null) 1 else 0 }
fun noNulls(): Int = countNulls(listOf(1, 2))

fun double(o: Outcome<Int>): Outcome.Ok<Int> = Outcome.Ok(o.orZero() * 2)

fun wrapped(): Outcome<Int> {
    val f: (Outcome.Ok<Int>) -> Outcome<Int> = ::double
    return f(Outcome.Ok(2))
}

fun refused(): Outcome<Nothing> = Outcome.Refused("no")
fun refusal(): Int = total(listOf(refused()))

fun maybe(c: Boolean): Int? {
    val n: Int? = if (c) 7 else null
    return n
}
`)
}

// A String counts UTF-16 units, and half of a surrogate pair is
// U+FFFD (spec.md section 3).
func TestStrings(t *testing.T) {
	expect(t, [][2]string{
		{"t.FaceLength()", "2"},
		{"t.HalfFace()", "a\uFFFD"},
		{"t.Chars()", "[true, false, false, true]"},
		{"t.Numbers()", "1.0 5.0 0.3333333333333333 1.0 3 -1 Infinity 1.0E7"},
		{"t.Units()", "ab kotlin.Unit"},
	}, `package t

fun faceLength(): Int = "😀".length
fun halfFace(): String = "a😀b".substring(0, 2)
fun chars(): List<Boolean> = "a😀a".map { it == 'a' }
fun numbers(): String = "${1.0} ${2.5 * 2.0} ${1.0 / 3.0} ${10.0 % 3.0} ${7 / 2} ${-7 % 3} ${1.0 / 0.0} ${10000000.0}"

fun units(): String {
    var out = ""
    "a".let { out += it }
    val u = "b".let { out += it }
    return "$out $u"
}
`)
}

// A function of a kogo. package is imported by name and called as
// any other (spec.md section 12).
func TestPreludeImport(t *testing.T) {
	expect(t, [][2]string{{"t.Percent(12.5)", "12.5%"}, {"t.Percent(5.0)", "5%"}}, `package t

import kogo.text.plain

fun percent(rate: Double): String = "${plain(rate)}%"
`)
}

// The rest of sections 3 to 6, each through the Go it becomes.
func TestLanguage(t *testing.T) {
	expect(t, [][2]string{
		{"t.Fares()", "Fare(base=100, tip=0, note=none) Fare(base=100, tip=5, note=none) 105 true"},
		{"t.Greetings()", "Hello, AnnH Hello, Bob! Yo, CyY"},
		{"t.Band(0) + t.Band(2) + t.Band(9) + t.Band(10)", "zerofewsomemany"},
		{"t.Sign(-5)", "-1"},
		{"t.Sign(0)", "0"},
		{"t.Go(rt.Ptr(t.Light_green))", "true"},
		{"t.Go(nil)", "false"},
		{"t.Lights()", "[red, green] red"},
		{"t.Loops([]int32{1, 2, 3, 4, 5})", "12301x1x3p11 3"},
		{"t.PhoneLength(nil)", "1"},
		{`t.PhoneLength(&t.Rider{Name: "Ann", Phone: rt.Ptr("0400")})`, "5"},
		{`t.Rider_Shout(t.Rider{Name: "Ann"})`, "Ann!"},
		{"t.FlatQuote()", "flat 13"},
		{"t.Boxed()", "7"},
		{"t.ViaHelper()", "3"},
		{"t.Sizes()", "intstring"},
		{"t.Tally_Int(3)", "Tally(n=3)"},
		{"t.Factorial(5)", "120"},
		{"t.Four()", "4"},
		{"t.Limit", "3"},
		{"t.Pick(true)", "1"},
		{"t.Through()", "4"},
		{"t.Next(nil)", "0"},
		{"t.Next(rt.Ptr[int32](4))", "5"},
		{"t.Scoped()", "9"},
	}, `package t

import b.make
import b.take

// A value of package c's passes through this file, which never names c.
fun through(): Int = take(make())

data class Fare(val base: Long, val tip: Long = 0, val note: String = "none")

fun fares(): String {
    val a = Fare(100)
    val b = a.copy(tip = 5)
    val (base, tip) = b
    return "$a $b ${base + tip} ${a == b.copy(tip = 0)}"
}

fun greet(name: String, greeting: String = "Hello", mark: String = greeting.substring(0, 1)): String =
    "$greeting, $name$mark"

fun greetings(): String = greet("Ann") + " " + greet("Bob", mark = "!") + " " + greet(greeting = "Yo", name = "Cy")

fun band(n: Int): String = when (n) {
    0 -> "zero"
    1, 2 -> "few"
    in 3..9 -> "some"
    else -> "many"
}

fun sign(n: Long): Int = when {
    n < 0 -> -1
    n == 0L -> 0
    else -> 1
}

enum class Light { red, green }

fun go(light: Light?): Boolean = when (light) {
    Light.green -> true
    Light.red, null -> false
}

fun lights(): String = "${Light.entries} ${Light.red.name}"

fun loops(xs: List<Int>): String {
    var out = ""
    for (i in 1..3) out += "$i"
    for (i in 0 until 2) out += "$i"
    for (x in xs) {
        when {
            x == 2 -> continue
            x > 3 -> break
            else -> out += "x$x"
        }
    }
    for ((a, b) in xs.zip(xs)) {
        if (a + b > 2) break
        out += "p$a$b"
    }
    var n = 0
    while (n < 3) n += 1
    return "$out $n"
}

data class Rider(val name: String, val phone: String?)

fun phoneLength(r: Rider?): Int = (r?.phone?.length ?: 0) + 1

val Rider.shout: String
    get() = name + "!"

interface Regime<K> {
    val label: String
    fun rate(key: K): Long
}

data class Flat(override val label: String, val cents: Long) : Regime<String> {
    override fun rate(key: String): Long = cents + key.length
}

fun quote(r: Regime<String>, key: String): String = "${r.label} ${r.rate(key)}"
fun flatQuote(): String = quote(Flat("flat", 10), "abc")

interface Holder<T> {
    fun held(): T
}

data class Box<T>(val v: T) : Holder<T> {
    override fun held(): T = v
}

fun <T> unbox(h: Holder<T>): T = h.held()
fun boxed(): Int = unbox(Box(7))

private fun helper(): Int = 1
fun viaHelper(): Int = helper() + other()

fun size(x: Int): String = "int"
fun size(x: String): String = "string"
fun sizes(): String = size(1) + size("a")

data class Tally(val n: Int)
fun tally(n: Int): Tally = Tally(n)

fun factorial(n: Long): Long {
    fun go(k: Long, acc: Long): Long = if (k <= 1) acc else go(k - 1, acc * k)
    return go(n, 1)
}

fun twice(f: (Int) -> Int, x: Int): Int = f(f(x))
fun inc(x: Int): Int = x + 1
fun four(): Int = twice(::inc, 2)

val limit: Int = if (four() > 3) 3 else 0

fun next(x: Int?): Int {
    x ?: return 0
    return x + 1
}

// Names that Go keeps for itself, and a local that hides a parameter.
fun pick(type: Boolean): Int {
    val func = 1
    val nil = 2
    if (type) {
        val type = func
        return type
    }
    return nil
}
`, `package t

private fun helper(): Int = 2
fun other(): Int = helper()
`, "package b\n\nimport c.Thing\n\nfun make(): Thing = Thing(4)\nfun take(x: Thing): Int = x.n\n", "package c\n\ndata class Thing(val n: Int)\n",
		// A marker is checked and then written nowhere (spec.md 11).
		"@file:OptIn(Unscoped::class)\npackage t\n\n@RequiresOptIn\nannotation class Unscoped\n\n@Unscoped\nfun unscoped(): Int = 9\nfun scoped(): Int = unscoped()\n")
}

// A jump may stand where a value goes straight into a val (spec 6),
// and a destructuring val is one.
func TestDestructuringJump(t *testing.T) {
	expect(t, [][2]string{
		{"t.Swap(nil)", "P(a=7, b=8)"},
		{"t.Swap(&t.P{A: 1, B: 2})", "P(a=2, b=1)"},
		{"t.Sums()", "3 7"},
	}, `package t

data class P(val a: Int, val b: Int)

fun none(): P? = null

fun swap(p: P?): P {
    val (a, b) = p ?: return P(7, 8)
    return P(b, a)
}

fun sums(): String {
    val ps: List<P?> = listOf(P(1, 2), null, P(3, 4), P(5, 6))
    var out = ""
    for (q in ps) {
        val (a, b) = q ?: continue
        if (a > 3) {
            val (c, d) = none() ?: break
            out += "$c$d"
        }
        out += "${a + b} "
    }
    return out.trim()
}
`)
}

// A constructor's default is written at the call, with the call's
// type arguments for the class's type parameters.
func TestGenericDefault(t *testing.T) {
	expect(t, [][2]string{
		{"t.Page1()", "Page(items=[], n=2)"},
		{"t.Page2()", "Page(items=[a], n=0)"},
	}, `package t

data class Page<T>(val items: List<T> = listOf(), val n: Int = 0)

fun page1(): Page<String> = Page(n = 2)
fun page2(): Page<String> = Page(listOf("a"))
`)
}

// A type parameter without a Go name in scope is the emitter's own
// mistake: it stops, rather than write a type with no name.
func TestForeignTypeParam(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a type parameter with no Go name gave a type")
		}
	}()
	e := &emitter{fn: &scope{names: map[any]string{}, used: map[string]bool{}}}
	t.Error(e.typ(&check.TypeParam{Name: "T"}))
}

// Arithmetic on literals wraps an Int or a Long and gives Infinity for
// a Double, as at run time: Go would fold it as a constant and refuse
// the overflow. A minus on a literal is one literal, so Int.MIN_VALUE
// is written as Kotlin writes it, and -0.0 keeps its sign.
func TestConstants(t *testing.T) {
	e200 := "1" + strings.Repeat("0", 200) + ".0" // Kogo writes no exponent
	expect(t, [][2]string{
		{"t.Big()", "-2147483648"},
		{"t.MsPerYear()", "1471228928"},
		{"t.BigLong()", "-9223372036854775808"},
		{"t.Huge()", "Infinity"},
		{"t.MinInt()", "-2147483648"},
		{"t.Negated()", "-2147483648"},
		{"t.NegZero()", "-0.0"},
	}, `package t

fun big(): Int = 2147483647 + 1
fun msPerYear(): Int = 1000 * 60 * 60 * 24 * 365
fun bigLong(): Long = 9223372036854775807L + 1L
fun huge(): Double = `+e200+` * `+e200+`
fun minInt(): Int = -2147483648
fun negated(): Int = -(-2147483648)
fun negZero(): Double = -0.0
`)
}

// == between two values that are each a Double or a Double? is IEEE's,
// as in Kotlin: NaN is not equal to itself and 0.0 is equal to -0.0.
// Inside a data class it is Kotlin's equals, which says the opposite.
func TestDoubleEquality(t *testing.T) {
	expect(t, [][2]string{
		{"t.Nz(rt.Ptr(0.0))", "true"},
		{"t.Nz(nil)", "false"},
		{"t.NotNaN()", "false"},
		{"t.Both(nil, nil)", "true"},
		{"t.Both(rt.Ptr(1.5), nil)", "false"},
		{"t.Zero(nil) + t.ZeroOfNeg()", "otherzero"},
		{"t.Boxed()", "true false"},
	}, `package t

fun nz(x: Double?): Boolean = x == -0.0

fun notNaN(): Boolean {
    val n: Double? = 0.0 / 0.0
    return n == n
}

fun both(a: Double?, b: Double?): Boolean = a == b

fun zero(x: Double?): String = when (x) {
    0.0 -> "zero"
    else -> "other"
}

fun zeroOfNeg(): String = zero(-0.0)

data class D(val v: Double)

fun boxed(): String = "${D(0.0 / 0.0) == D(0.0 / 0.0)} ${D(0.0) == D(-0.0)}"
`)
}

// A function behind a type parameter, which check cannot see, is not
// compared (spec.md 3): == panics once it reaches the function, and
// answers false where an earlier part differs.
func TestFunctionEquality(t *testing.T) {
	expect(t, [][2]string{
		{"t.Same()", "panics: a function cannot be compared"},
		{"t.Holds()", "panics: a function cannot be compared"},
		{"t.Differ()", "false"},
		{"t.Plain()", "true"},
	}, `package t

data class Rule(val name: String, val test: (Int) -> Boolean)

fun <T> eq(a: T, b: T): Boolean = a == b

fun same(): Boolean {
    val r = Rule("pos", { n -> n > 0 })
    return eq(r, r)
}

fun holds(): Boolean {
    val r = Rule("pos", { n -> n > 0 })
    return setOf(r).contains(r)
}

fun differ(): Boolean {
    val test = { n: Int -> n > 0 }
    return eq(Rule("a", test), Rule("b", test))
}

fun plain(): Boolean = eq(listOf("a"), listOf("a"))
`)
}

// A panic's trace names the Kogo file and line (principle 5): the
// line of a statement, and the line of a call that is further down
// than the statement begins.
func TestLine(t *testing.T) {
	out := build(t, func(root string) map[string][]byte {
		_, files, errs := emit(t, root, "package t\n\nfun explode(p: String?): String =\n    \"x\" +\n        requireNotNull(p) { \"no p\" }\n\nfun divide(a: Int, b: Int): Int {\n    val q = a / b\n    return q\n}\n")
		if errs != nil {
			t.Fatalf("%v", errs)
		}
		files["main.go"] = []byte(`package main
import (
"os"
"runtime/debug"
"ROOT/t"
)
func trace(f func()) {
defer func() { recover(); os.Stdout.Write(debug.Stack()) }()
f()
}
func main() {
trace(func() { t.Explode(nil) })
trace(func() { t.Divide(1, 0) })
}
`)
		return files
	})
	// The Go is in <out>/t/, two directories below t1.kg, which it
	// names by a relative path that Go resolves from there.
	src := regexp.QuoteMeta(must(filepath.Abs("t1.kg")))
	for _, line := range []string{"5", "8"} {
		if !regexp.MustCompile(src + `:` + line + `\D`).MatchString(out) {
			t.Errorf("no trace names t1.kg:%s:\n%s", line, out)
		}
	}
}

// The emitted Go names its source by a path relative to itself, and
// starts each line's code at one column, after the line's directive,
// indented a tab for each block or closure it is in.
func TestLayout(t *testing.T) {
	_, files, errs := emit(t, "x", "package t\n\n\n\n\n\n\nfun f(n: Int): Int {\n    val g = { x: Int ->\n        x + n\n    }\n    return g(2)\n}\n")
	if errs != nil {
		t.Fatalf("%v", errs)
	}
	text := string(files["t/kogo.go"])
	if cwd := must(os.Getwd()); strings.Contains(text, cwd) {
		t.Errorf("an absolute path, %s, in:\n%s", cwd, text)
	}
	column, inner := 0, ""
	for _, line := range strings.Split(text, "\n") {
		dir := regexp.MustCompile(`^/\*line \.\./\.\./t1\.kg:\d+\*/ +`).FindString(line)
		if dir == "" {
			continue
		}
		if column == 0 {
			column = len(dir)
		}
		if len(dir) != column {
			t.Errorf("code at column %d, not %d: %q", len(dir), column, line)
		}
		if strings.Contains(line, "x_ + n_") {
			inner = line[len(dir):]
		}
	}
	if column == 0 || !strings.HasPrefix(inner, "\t\treturn ") {
		t.Errorf("no line in a closure in a fun two tabs in:\n%s", text)
	}
}

// A failure (spec.md 9) is recovered by its own raise scope: raised in
// a lambda, through the prelude's Go that calls the lambda, and not by
// a scope inside it. That a failure passes a scope it is raised inside
// but not on, which no program can write, is prelude/kogo's test.
func TestFailures(t *testing.T) {
	expect(t, [][2]string{
		{`t.Total("12")`, "Either.Right(3)"},
		{`t.Total("1x2")`, "Either.Left(no digit x)"},
		{"t.Nested(1)", "Either.Left(outer)"},
		{"t.Nested(2)", "Either.Right(2 Either.Left(inner))"},
		{"t.Nested(3)", "Either.Right(3 Either.Right(3))"},
		{"t.Lifted(4)", "Either.Right(4)"},
		{`t.Found(rt.Ptr("abc"))`, "Either.Right(3)"},
		{"t.Found(nil)", "Either.Left(none)"},
		{"t.Wider(0)", "Either.Left(A(n=0))"},
		{"t.Wider(1)", "Either.Right(2)"},
	}, `package t

fun digit(c: Char): Either<String, Int> = either {
    ensure(c != 'x') { "no digit $c" }
    c.digitToInt()
}

fun total(s: String): Either<String, Int> = either { s.map { digit(it).bind() }.sumOf { it } }

fun nested(n: Int): Either<String, String> = either {
    val passed = n.let { if (it == 1) raise("outer") else it }
    val own = either<String, Int> { if (n == 2) raise("inner") else n }
    "$passed $own"
}

fun found(p: String?): Either<String, Int> = either {
    val q = p.let { ensureNotNull(it) { "none" } }
    q.length
}

sealed interface S {
    data class A(val n: Int) : S
}

fun a(n: Int): Either<S.A, Int> = if (n > 0) n.right() else S.A(n).left()

fun wider(n: Int): Either<S, Int> = either { a(n).bind() + 1 }

// An Either<Nothing, Int>, bound in a scope of String.
fun lifted(n: Int): Either<String, Int> = either { n.right().bind() }
`)
}

// A panic that is not a failure is a bug (spec.md 8): it passes every
// raise scope untouched, and its trace names the line it began at.
func TestBugInScope(t *testing.T) {
	out := build(t, func(root string) map[string][]byte {
		_, files, errs := emit(t, root, "package t\n\nfun inner(n: Int): Either<String, Int> = either {\n    check(n > 0) { \"broken\" }\n    n\n}\n\nfun outer(): Either<String, Int> = either { inner(0).bind() }\n")
		if errs != nil {
			t.Fatalf("%v", errs)
		}
		files["main.go"] = []byte(`package main
import (
"fmt"
"runtime/debug"
"ROOT/t"
)
func main() {
defer func() { fmt.Printf("recovered %q\n%s", recover(), debug.Stack()) }()
t.Outer()
}
`)
		return files
	})
	if !strings.HasPrefix(out, `recovered "broken"`) || !regexp.MustCompile(`/t1\.kg:4\D`).MatchString(out) {
		t.Errorf("the panic of check does not reach the driver as it was, from t1.kg:4:\n%s", out)
	}
}

// A set compares its keys as == does, and two sets regardless of
// order; xs + ys and x in set are their bindings' calls.
func TestCollections(t *testing.T) {
	expect(t, [][2]string{
		{"t.Counted(rt.Ptr[int64](9), []t.Line{{Kind: t.Kind_item, Cents: 5}, {Kind: t.Kind_voucher, Cents: 7}})", "[9, 5]"},
		{"t.Counted(nil, nil)", "[]"},
		{"t.Sets()", "[discount, voucher] true false true"},
		{"t.Capped(-5, 3) + t.Capped(10, 3) + t.Capped(10, 30)", "13"},
		{"t.Taken(5, 2)", "3"},
		{"t.Second([]int64{10, 20}, 15)", "35"},
		{"func() (s any) { defer func() { s = recover() }(); return t.Taken(1, 2) }()", "taking 2 leaves less than none of 1"},
	}, `package t

enum class Kind { item, discount, voucher }

data class Line(val kind: Kind, val cents: Long)

data class Tag(val name: String, val note: String?)

val off: Set<Kind> = setOf(Kind.discount, Kind.voucher, Kind.discount)

fun counted(base: Long?, lines: List<Line>): List<Long> =
    listOfNotNull(base) + lines.filter { !(it.kind in off) }.map { it.cents }

fun sets(): String {
    val note = "B".takeIf { true }
    return "$off ${off == setOf(Kind.voucher, Kind.discount)} ${off == setOf(Kind.voucher)} ${Tag("A", note) in setOf(Tag("A", "B"))}"
}

fun capped(net: Long, fee: Long): Long = fee.coerceAtMost(net.coerceAtLeast(0L))

fun second(nets: List<Long>, n: Int): Long = nets[1] + n.toLong()

fun taken(stock: Long, amount: Long): Long {
    require(amount <= stock) { "taking $amount leaves less than none of $stock" }
    return stock - amount
}
`)
}

// What the stage-1 ports call, through the Go each becomes.
func TestPortCalls(t *testing.T) {
	expect(t, [][2]string{
		{`t.Handle(" Blue Heron Draft ")`, "blue-heron"},
		{`t.Handle("!!")`, "untitled"},
		{`t.Tally([]string{"a", " ", "b"})`, "(2, a b)"},
	}, `package t

fun handle(name: String): String =
    name.trim().lowercase().replace(Regex("""\bdraft$"""), "").replace(Regex("[^a-z0-9]+"), "-").trim('-').ifEmpty { "untitled" }

fun tally(words: List<String>): Pair<Int, String> {
    var n = 0
    var out = ""
    words.filter { it.isNotBlank() }.forEach { w ->
        n += 1
        out = if (out.isNotEmpty()) "$out $w" else w
    }
    return n to out
}
`)
}

// Uuid.parse is the prelude's object fun: a good text gives its id,
// and a bad one null.
func TestUuidParse(t *testing.T) {
	expect(t, [][2]string{
		{`t.Id("01934f2a-bcde-7f01-89ab-CDEF01234567")`, "01934f2a-bcde-7f01-89ab-cdef01234567"},
		{`t.Id("01934f2a-bcde-7f01-89ab")`, "null"},
	}, `package t

fun id(text: String): Uuid? = Uuid.parse(text)
`)
}

// orEmpty and isNullOrBlank take a nullable receiver, and the second
// firstOrNull and contains sit beside the first.
func TestNullableAndOverloads(t *testing.T) {
	expect(t, [][2]string{
		{`t.Names(rt.Ptr("x"))`, "[x, 3]"},
		{`t.Names(nil)`, "[]"},
		{`t.Named(rt.Ptr(" "), "a@b")`, "false true"},
		{`t.Named(nil, "ab")`, "false false"},
		{`t.Named(rt.Ptr("Bo"), "")`, "true false"},
		{`t.Over([]int64{1, 4, 6})`, "4 1 null"},
	}, `package t

fun names(name: String?): List<String> = name?.let { listOf(it, "3") }.orEmpty()

fun named(name: String?, email: String): String = "${!name.isNullOrBlank()} ${email.contains('@')}"

fun over(xs: List<Long>): String = "${xs.firstOrNull { it > 2L }} ${xs.firstOrNull()} ${xs.firstOrNull { it > 9L }}"
`)
}

// What is not emitted yet is refused where it stands, never emitted
// wrongly, and so are two declarations that Go cannot tell apart,
// an enum's entries among them.
func TestRefused(t *testing.T) {
	for _, c := range [][3]string{
		{"enum class Kind { in_house, outside }\nfun f(tx: Tx, kinds: List<Kind>): Int = tx.execute(sql\"delete from invite where kind = any($kinds)\")", "2:93: not-yet-emitted", "a parameter that is a list of an enum"},
		{"enum class Kind { in_house, outside }\nfun f(tx: Tx): List<Kind>? = tx.querySingle<List<Kind>?>(sql\"select array_agg(kind) as kinds from invite\")", "2:30: not-yet-emitted", "column kinds, which holds a list of an enum,"},
		{"fun f(x: Int?): Int? = x.takeIf { true }", "1:24: not-yet-emitted", "a nullable type argument for T, which is used as T?"},
		{"fun <T> none(x: T?): T? = x\nfun g(): Int? = none(null)", "2:17: not-yet-emitted", "write the type arguments, none<...>(...)"},
		{"sealed interface S<out T> {\n    data class A<T>(val v: T) : S<T>\n}\nfun f(s: S<S.A<Int>>): S<S<Int>> = s", "4:36: not-yet-emitted", "build the value with the wider type argument"},
		{"fun a(x: Int): Int = x\nfun A(x: Int): Int = x", "2:1: go-name", "are both A_Int in Go"},
		{"enum class Kind { entries, b }", "1:19: go-name", "are both Kind_entries in Go"},
		{"data class D(val a: Int, val A: Int)", "1:26: go-name", "two members of D are both A in Go"},
		{"interface I {\n    val name: String\n}\ndata class D(override val name: String, val getName: String) : I", "4:14: go-name", "two members of D are both GetName in Go"},
	} {
		_, files, errs := emit(t, "x", "package t\n"+c[0])
		got := ""
		if len(errs) > 0 {
			got = fmt.Sprintf("%d:%d: %s: %s", errs[0].Line-1, errs[0].Column, errs[0].Code, errs[0].Message)
		}
		// One refusal, or with it those of what it holds.
		for _, err := range errs {
			if err.Code != "not-yet-emitted" && len(errs) > 1 {
				got = ""
			}
		}
		if files != nil || !strings.HasPrefix(got, c[1]) || !strings.Contains(got, c[2]) {
			t.Errorf("%s\n got %v\nwant %s ... %s", c[0], errs, c[1], c[2])
		}
	}
}

// Every declaration of prelude/ has Go behind it, of the name that
// the naming rule gives it and the type that its declaration states:
// a Go file that uses each one compiles.
func TestPrelude(t *testing.T) {
	out := build(t, func(string) map[string][]byte {
		pre, err := prelude.Files()
		if err != nil {
			t.Fatal(err)
		}
		info, _ := check.Check(pre, nil, nil)
		e, _, _ := emitAll(info, pre, nil, "x", "x")
		e.pkg, e.imports, e.fn = "main", map[string]string{}, &scope{names: map[any]string{}, used: map[string]bool{}}
		uses, n := "", 0
		fun := func(f *check.Func) {
			var params []string
			for _, tp := range f.TypeParams {
				e.fn.names[tp] = "any"
			}
			if f.Receiver != nil {
				params = append(params, e.typ(f.Receiver))
			}
			for _, p := range f.Params {
				if d := p.Decl.(*syntax.Param); d.Vararg {
					params = append(params, "..."+e.typ(p.Type))
				} else {
					params = append(params, e.typ(p.Type))
				}
			}
			name := e.qualify(f.Pkg, e.names[f])
			if f.TypeParams != nil {
				name += "[any" + strings.Repeat(", any", len(f.TypeParams)-1) + "]"
			}
			uses += fmt.Sprintf("var _ func(%s) %s = %s\n", strings.Join(params, ", "), e.typ(f.Result), name)
			n++
		}
		for _, f := range pre {
			e.file = f
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *syntax.ClassDecl:
					td := e.info.Classes[d]
					for _, m := range td.Methods {
						fun(m)
					}
					if d.Kind == "external class" {
						args := ""
						if td.Params != nil {
							args = "[any" + strings.Repeat(", any", len(td.Params)-1) + "]"
						}
						uses += fmt.Sprintf("var _ %s%s\n", e.qualify(td.Pkg, e.names[td]), args)
						n++
					}
				default:
					fun(e.info.Funcs[d])
				}
			}
		}
		// Counted apart from the trees: each declaration begins a line.
		declared := 0
		for _, name := range must(filepath.Glob("../prelude/*.kg")) {
			declared += len(regexp.MustCompile(`(?m)^(external (class|fun|val)|    fun) `).FindAll(must(os.ReadFile(name)), -1))
		}
		if n != declared || n == 0 {
			t.Errorf("%d declarations of the prelude are used, and it has %d", n, declared)
		}
		head := "package main\n"
		for path, alias := range e.imports {
			head += fmt.Sprintf("import %s %q\n", alias, path)
		}
		return map[string][]byte{"main.go": []byte(head + uses + "func main() {}\n")}
	})
	if out != "" {
		t.Errorf("the Go that uses every declaration of the prelude does not compile:\n%s", out)
	}
}

// Each api list is a table in its package: its operations in order,
// by their names on the wire, an imported one among them, and the
// entries of each enum they reach. A table whose name a type or a fun
// has takes _api, and a val named as an enum's entries takes _val.
func TestTables(t *testing.T) {
	expect(t, [][2]string{
		{"fmt.Sprint([]string{t.Ops.Operations[0].Name, t.Ops.Operations[1].Name}, t.Ops.Enums)", "[me f] [[Alone Shared] [Open Done]]"},
		{"fmt.Sprint([]string{t.Other_api.Operations[0].Name}, t.Other_api.Enums)", "[g] [[S M]]"},
		{"fmt.Sprint(t.Other_Int(4), t.Kind_entries_val)", "4 3"},
	}, `package t
import kogo.rpc.Request
import u.g
api ops { me, f }
api other { g }
data class Env(val n: Int)
data class Other(val n: Int)
fun other(n: Int): Int = n
val kind_entries: Int = 3
data class Ask(val kind: Kind)
enum class Kind { Alone, Shared }
enum class State { Open, Done }
fun me(tx: Tx, env: Env, request: Request, input: Unit): Either<String, Unit> = either { }
fun f(db: Db, env: Env, request: Request, input: Ask): Either<String, State> = either { raise("no") }
`, `package u
import kogo.rpc.Request
data class Env(val s: String)
enum class Size { S, M }
fun g(tx: Tx, env: Env, request: Request, input: List<Size?>): Either<String, Unit> = either { }
`)
}
