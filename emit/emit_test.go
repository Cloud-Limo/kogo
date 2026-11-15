package emit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"kogo/check"
	"kogo/prelude"
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
// a directory whose import path is root.
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
	info, errs := check.Check(pre, program)
	if errs != nil {
		t.Fatalf("%s\n%v", strings.Join(sources, "\n---\n"), errs)
	}
	e, files, errs := emitAll(info, pre, program, root)
	return e, files, errs
}

// build writes Go files under a new directory of this package, which
// lies in the module that holds rt and the prelude's Go, and runs
// them as a program. In main.go ROOT stands for the directory's
// import path. It gives what the program prints, with its panic's
// trace when it ends in one.
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
	out, _ := exec.Command("go", "run", "./"+dir).CombinedOutput()
	return string(out)
}

// run emits a program and runs a driver that prints the value of each
// Go expression of calls, in package t, a line each as toString()
// writes it.
func run(t *testing.T, calls []string, sources ...string) []string {
	t.Helper()
	out := build(t, func(root string) map[string][]byte {
		_, files, errs := emit(t, root, sources...)
		if errs != nil {
			t.Fatalf("%v", errs)
		}
		driver := "package main\nimport (\n\"fmt\"\n\"kogo/rt\"\n\"ROOT/t\"\n)\nfunc main() {\n"
		for _, call := range calls {
			driver += "fmt.Println(rt.ToString(" + call + "))\n"
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
	var calls []string
	for _, c := range cases {
		calls = append(calls, c[0])
	}
	got := run(t, calls, sources...)
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
`, "package b\n\nimport c.Thing\n\nfun make(): Thing = Thing(4)\nfun take(x: Thing): Int = x.n\n", "package c\n\ndata class Thing(val n: Int)\n")
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
	for _, line := range []string{"5", "8"} {
		if !regexp.MustCompile(`/t1\.kg:` + line + `\D`).MatchString(out) {
			t.Errorf("no trace names t1.kg:%s:\n%s", line, out)
		}
	}
}

// What is not emitted yet is refused where it stands, never emitted
// wrongly, and so are two declarations that Go cannot tell apart.
func TestRefused(t *testing.T) {
	for _, c := range [][3]string{
		{"fun f(n: Int): Either<String, Int> = n.right()", "1:1: not-yet-emitted", "a fun that can fail (spec.md 9)"},
		{"fun f(xs: List<Int>): Int {\n    xs.filter { return 1 }\n    return 0\n}", "2:17: not-yet-emitted", "end the lambda with its value, and return after the call"},
		{"fun f(x: Int?): Int? = x.takeIf { true }", "1:24: not-yet-emitted", "a nullable type argument for T, which is used as T?"},
		{"fun <T> none(x: T?): T? = x\nfun g(): Int? = none(null)", "2:17: not-yet-emitted", "write the type arguments, none<...>(...)"},
		{"sealed interface S<out T> {\n    data class A<T>(val v: T) : S<T>\n}\nfun f(s: S<S.A<Int>>): S<S<Int>> = s", "4:36: not-yet-emitted", "build the value with the wider type argument"},
		{"fun f(): Int {\n    fun <T> id(x: T): T = x\n    return id(1)\n}", "2:5: not-yet-emitted", "declare it at the top level"},
		{"interface I {\n    fun <T> id(x: T): T\n}", "2:5: not-yet-emitted", "make it a top-level fun"},
		{"fun a(x: Int): Int = x\nfun A(x: Int): Int = x", "2:1: go-name", "are both A_Int in Go"},
		{"data class D(val a: Int, val A: Int)", "1:26: go-name", "two members of D are both A in Go"},
	} {
		_, files, errs := emit(t, "x", "package t\n"+c[0])
		got := ""
		if len(errs) == 1 {
			got = fmt.Sprintf("%d:%d: %s: %s", errs[0].Line-1, errs[0].Column, errs[0].Code, errs[0].Message)
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
		info, _ := check.Check(pre, nil)
		e, _, _ := emitAll(info, pre, nil, "x")
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
