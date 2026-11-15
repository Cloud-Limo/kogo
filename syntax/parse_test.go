package syntax

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/examples.tree from the parser")

// show prints a tree compactly: (Type field ...), leaving out
// positions and empty fields, and a true bool as its field's name.
func show(v any) string {
	var b strings.Builder
	write(&b, reflect.ValueOf(v))
	return b.String()
}

func write(b *strings.Builder, v reflect.Value) {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			b.WriteString("nil")
		} else {
			write(b, v.Elem())
		}
	case reflect.Struct:
		b.WriteString("(" + v.Type().Name())
		for i := range v.NumField() {
			f, name := v.Field(i), v.Type().Field(i).Name
			if f.Type() == reflect.TypeFor[Pos]() || f.IsZero() {
				continue
			}
			b.WriteByte(' ')
			if f.Kind() == reflect.Bool {
				b.WriteString(name)
			} else {
				write(b, f)
			}
		}
		b.WriteString(")")
	case reflect.Slice:
		b.WriteString("[")
		for i := range v.Len() {
			if i > 0 {
				b.WriteByte(' ')
			}
			write(b, v.Index(i))
		}
		b.WriteString("]")
	case reflect.String:
		if s := v.String(); s == "" || strings.ContainsAny(s, " ()[]\"\n\t\\") {
			b.WriteString(strconv.Quote(s))
		} else {
			b.WriteString(s)
		}
	}
}

// Every example parses, to the tree in testdata/examples.tree: one
// line a declaration, then one an api list.
func TestExamples(t *testing.T) {
	files, _ := filepath.Glob("../examples/*.kg")
	if len(files) != 23 {
		t.Fatalf("%d examples, want 23", len(files))
	}
	var got strings.Builder
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		f, err := Parse(file, src)
		if err != nil {
			t.Fatal(err)
		}
		got.WriteString("== " + filepath.Base(file) + " " + show(f.OptIn) + " " + f.Package.Name + " " + show(f.Imports) + "\n")
		for _, d := range f.Decls {
			got.WriteString(show(d) + "\n")
		}
		for _, a := range f.APIs {
			got.WriteString(show(a) + "\n")
		}
	}
	if *update {
		os.WriteFile("testdata/examples.tree", []byte(got.String()), 0o644)
	}
	want, _ := os.ReadFile("testdata/examples.tree")
	if got.String() != string(want) {
		gotLines, wantLines := strings.Split(got.String(), "\n"), strings.Split(string(want), "\n")
		for i := range min(len(gotLines), len(wantLines)) {
			if gotLines[i] != wantLines[i] {
				t.Fatalf("line %d of testdata/examples.tree:\n got %s\nwant %s", i+1, gotLines[i], wantLines[i])
			}
		}
		t.Fatalf("got %d lines, want %d", len(gotLines), len(wantLines))
	}
}

// parse reads src as a file's declarations after a package line.
func parse(t *testing.T, src string) *File {
	t.Helper()
	f, err := Parse("t.kg", []byte("package t\n"+src))
	if err != nil {
		t.Fatalf("%s\n%v", src, err)
	}
	return f
}

// stmts gives the printed statements of src as a function body.
func stmts(t *testing.T, src string) string {
	t.Helper()
	return show(parse(t, "fun f() {\n"+src+"\n}").Decls[0].(*FunDecl).Body.Stmts)
}

func check(t *testing.T, cases [][2]string, print func(*testing.T, string) string) {
	t.Helper()
	for _, c := range cases {
		if got := print(t, c[0]); got != c[1] {
			t.Errorf("%s\n got %s\nwant %s", c[0], got, c[1])
		}
	}
}

func TestDeclarations(t *testing.T) {
	check(t, [][2]string{
		{`data class D<T> internal constructor(val a: T, override val b: Int = 1) : I<T>, J { override fun f(): Int = b }`,
			`[(ClassDecl "data class" D [(TypeParam T)] (Modifiers internal) [(Param Val a (NamedType T)) (Param Override Val b (NamedType Int) (Literal Int 1))] [(NamedType I [(NamedType T)]) (NamedType J)] [(FunDecl (Modifiers Override) f (NamedType Int) (Ident b))])]`},
		{`sealed interface S<out T> { data object N : S<Nothing> }`,
			`[(ClassDecl "sealed interface" S [(TypeParam Out T)] [(ClassDecl "data object" N [(NamedType S [(NamedType Nothing)])])])]`},
		{"enum class E { a, b, }", `[(ClassDecl "enum class" E [(Ident a) (Ident b)])]`},
		{"enum class E { a, b; }", `[(ClassDecl "enum class" E [(Ident a) (Ident b)])]`},
		{"sealed interface S\ndata object N : S", `[(ClassDecl "sealed interface" S) (ClassDecl "data object" N [(NamedType S)])]`},
		{"interface I<K> {\n    val label: String\n    fun card(key: K): Card\n}",
			`[(ClassDecl interface I [(TypeParam K)] [(ValDecl label (NamedType String)) (FunDecl card [(Param key (NamedType K))] (NamedType Card))])]`},
		{"typealias Cents = Long", `[(TypeAlias Cents (NamedType Long))]`},
		{"private const val max: Int = 3\ninternal val x = 1",
			`[(ValDecl (Modifiers private Const) max (NamedType Int) (Literal Int 3)) (ValDecl (Modifiers internal) x (Literal Int 1))]`},
		{"val Row.owing: Long\n    get() = total - paid",
			`[(ValDecl (NamedType Row) owing (NamedType Long) (Binary - (Ident total) (Ident paid)))]`},
		{"fun <T> T.f(g: (T, Int) -> Unit, h: () -> T?, k: Raise<E>.() -> A, m: ((A) -> B)?) {}",
			`[(FunDecl [(TypeParam T)] (NamedType T) f [(Param g (FuncType [(NamedType T) (NamedType Int)] (NamedType Unit))) (Param h (FuncType (NamedType T Nullable))) (Param k (FuncType (NamedType Raise [(NamedType E)]) (NamedType A))) (Param m (FuncType [(NamedType A)] (NamedType B) Nullable))] (Block))]`},
		{"fun a.b.C.f() {}", `[(FunDecl (NamedType a.b.C) f (Block))]`},
		{"@RequiresOptIn\nannotation class M\n@M fun f() = 1\ndata class H @M constructor(val x: Int)",
			`[(ClassDecl (Modifiers [(Annotation RequiresOptIn)]) "annotation class" M) (FunDecl (Modifiers [(Annotation M)]) f (Literal Int 1)) (ClassDecl "data class" H (Modifiers [(Annotation M)]) [(Param Val x (NamedType Int))])]`},
		{"@go(\"httpx\")\nexternal object http {\n    fun get(url: String): Either<E, R>\n}",
			`[(ClassDecl (Modifiers [(Annotation go [(StringLit [(Literal String httpx)])])] External) "external object" http [(FunDecl get [(Param url (NamedType String))] (NamedType Either [(NamedType E) (NamedType R)]))])]`},
		// The binding forms of section 13: a class with no body, and
		// there only, out, a nullable receiver and vararg.
		{"external class List<out T>\nexternal val <T> List<T>.size: Int\nexternal fun <T> List<T>?.orEmpty(): List<T>",
			`[(ClassDecl (Modifiers External) "external class" List [(TypeParam Out T)]) (ValDecl (Modifiers External) [(TypeParam T)] (NamedType List [(NamedType T)]) size (NamedType Int)) (FunDecl (Modifiers External) [(TypeParam T)] (NamedType List [(NamedType T)] Nullable) orEmpty (NamedType List [(NamedType T)]))]`},
		{"external fun <T> listOf(vararg elements: T): List<T>\nexternal fun String?.isNullOrBlank(): Boolean",
			`[(FunDecl (Modifiers External) [(TypeParam T)] listOf [(Param Vararg elements (NamedType T))] (NamedType List [(NamedType T)])) (FunDecl (Modifiers External) (NamedType String Nullable) isNullOrBlank (NamedType Boolean))]`},
	}, func(t *testing.T, src string) string { return show(parse(t, src).Decls) })

	f, err := Parse("t.kg", []byte("@file:OptIn(A::class, b.B::class)\n\npackage x.y\n\nimport a.b.C\nimport d.e\n"))
	if got := show(f); err != nil || got != `(File t.kg [(Annotation OptIn [(ClassRef A) (ClassRef b.B)])] (Ident x.y) [(Ident a.b.C) (Ident d.e)])` {
		t.Errorf("file head: %s %v", got, err)
	}

	// An api list may end with a comma, and api is a name elsewhere.
	f = parse(t, "api ops { me, updateJob, }\napi other {\n    me\n}\nfun api(api: Int): Int = api")
	if got := show(f.APIs); got != `[(API ops [(Ident me) (Ident updateJob)]) (API other [(Ident me)])]` || len(f.Decls) != 1 {
		t.Errorf("api lists: %s, %d declarations", got, len(f.Decls))
	}
}

func TestExpressions(t *testing.T) {
	check(t, [][2]string{
		// Precedence and associativity, as Kotlin's.
		{"a + b * c - d", `[(Binary - (Binary + (Ident a) (Binary * (Ident b) (Ident c))) (Ident d))]`},
		{"a || b && c == d < e", `[(Binary || (Ident a) (Binary && (Ident b) (Binary == (Ident c) (Binary < (Ident d) (Ident e)))))]`},
		{"x in a..b + 1", `[(Binary in (Ident x) (Binary .. (Ident a) (Binary + (Ident b) (Literal Int 1))))]`},
		{"a ?: b == c", `[(Binary == (Binary ?: (Ident a) (Ident b)) (Ident c))]`},
		{"a to b ?: c", `[(Binary ?: (Binary to (Ident a) (Ident b)) (Ident c))]`},
		{"0 until n - 1", `[(Binary until (Literal Int 0) (Binary - (Ident n) (Literal Int 1)))]`},
		{"x is T && !y", `[(Binary && (Is (Ident x) (NamedType T)) (Unary ! (Ident y)))]`},
		{"-a * - -b", `[(Binary * (Unary - (Ident a)) (Unary - (Unary - (Ident b))))]`},
		{"a?.b.c(d)[e]", `[(Index (Call (Select (Select (Ident a) Safe b) c) [(Arg (Ident d))]) (Ident e))]`},
		{"x ?: return y + 1", `[(Binary ?: (Ident x) (Jump return (Binary + (Ident y) (Literal Int 1))))]`},
		{"f(a, n = 2, ::g, this)", `[(Call (Ident f) [(Arg (Ident a)) (Arg n (Literal Int 2)) (Arg (FuncRef g)) (Arg (Ident this))])]`},
		{"10_000 + 1L + 1.5 + 'c' + '\\n' + true + null", `[(Binary + (Binary + (Binary + (Binary + (Binary + (Binary + (Literal Int 10000) (Literal Long 1)) (Literal Double 1.5)) (Literal Char c)) (Literal Char "\n")) (Literal Boolean true)) (Literal Null null))]`},
		// A line end ends a statement, except before . ?. ?: && || and
		// else, after an operator, and inside parentheses.
		{"a\n.b\n?.c\n?: d\n&& e\n|| f", `[(Binary || (Binary && (Binary ?: (Select (Select (Ident a) b) Safe c) (Ident d)) (Ident e)) (Ident f))]`},
		{"a +\nb", `[(Binary + (Ident a) (Ident b))]`},
		{"(a\n+ b)", `[(Binary + (Ident a) (Ident b))]`},
		{"a\n-b", `[(Ident a) (Unary - (Ident b))]`},
		{"f\n(x)", `[(Ident f) (Ident x)]`},
		{"f\n{ x }", `[(Ident f) (Lambda (Block [(Ident x)]))]`},
		{"return\nx", `[(Jump return) (Ident x)]`},
		{"a /* \n /* \n */ */ - b // c\n- d", `[(Binary - (Ident a) (Ident b)) (Unary - (Ident d))]`},
		{"if (a) b\nelse c", `[(If (Ident a) (Block [(Ident b)]) (Block [(Ident c)]))]`},
		// < opens type arguments only when > and ( or { follow.
		{"q<R>(x)", `[(Call (Ident q) [(NamedType R)] [(Arg (Ident x))])]`},
		{"e<A, B?> { it }", `[(Call (Ident e) [(NamedType A) (NamedType B Nullable)] [(Arg (Lambda (Block [(Ident it)])) Trailing)])]`},
		{"a < b && c > d", `[(Binary && (Binary < (Ident a) (Ident b)) (Binary > (Ident c) (Ident d)))]`},
		{"a < b > (c)", `[(Call (Ident a) [(NamedType b)] [(Arg (Ident c))])]`},
		{"f(a < b, c > d)", `[(Call (Ident f) [(Arg (Binary < (Ident a) (Ident b))) (Arg (Binary > (Ident c) (Ident d)))])]`},
		// A brace after a call is a lambda; after a header, a body.
		{"if (ok) run { x }", `[(If (Ident ok) (Block [(Call (Ident run) [(Arg (Lambda (Block [(Ident x)])) Trailing)])]))]`},
		{"for (x in xs) { y }", `[(For x (Ident xs) (Block [(Ident y)]))]`},
		{"f(a) { b -> c }", `[(Call (Ident f) [(Arg (Ident a)) (Arg (Lambda [(LambdaParam b)] (Block [(Ident c)])) Trailing)])]`},
		{"f({ a }, { b })", `[(Call (Ident f) [(Arg (Lambda (Block [(Ident a)]))) (Arg (Lambda (Block [(Ident b)])))])]`},
		{"{ (a, b), c: Int -> a }", `[(Lambda [(LambdaParam [a b]) (LambdaParam c (NamedType Int))] (Block [(Ident a)]))]`},
		// Templates nest; escapes are Kotlin's.
		{`"${if (a) "x" else "y"}"`, `[(StringLit [(If (Ident a) (Block [(StringLit [(Literal String x)])]) (Block [(StringLit [(Literal String y)])]))])]`},
		{`"$n: ${f { "${it}}" }}$"`, `[(StringLit [(Ident n) (Literal String ": ") (Call (Ident f) [(Arg (Lambda (Block [(StringLit [(Ident it) (Literal String })])])) Trailing)]) (Literal String $)])]`},
		{`"\t\$xA\\"`, `[(StringLit [(Literal String "\t$xA\\")])]`},
		{`"\uD83D\uDE00"`, "[(StringLit [(Literal String \U0001F600)])]"},
		{"\"\"\"a\\n $b\"\"\"\"", `[(StringLit Raw [(Literal String "a\\n ") (Ident b) (Literal String "\"")])]`},
	}, stmts)
}

func TestStatements(t *testing.T) {
	check(t, [][2]string{
		{"val (a, b) = p\nvar n: Int = 0\nn += a; n = b",
			`[(ValDecl [a b] (Ident p)) (ValDecl Var n (NamedType Int) (Literal Int 0)) (Assign += (Ident n) (Ident a)) (Assign = (Ident n) (Ident b))]`},
		{"for ((k, v) in m) if (k) break else continue\nwhile (c) n -= 1",
			`[(For [k v] (Ident m) (Block [(If (Ident k) (Block [(Jump break)]) (Block [(Jump continue)]))])) (While (Ident c) (Block [(Assign -= (Ident n) (Literal Int 1))]))]`},
		{"if (a) 1 else if (b) 2 else { 3 }",
			`[(If (Ident a) (Block [(Literal Int 1)]) (Block [(If (Ident b) (Block [(Literal Int 2)]) (Block [(Literal Int 3)]))]))]`},
		{"when (x) {\n    is A.B, in 0..9 -> 1\n    c -> if (d) 2\n    else -> { 3 }\n}",
			`[(When (Ident x) [(Branch [(Cond is (NamedType A.B)) (Cond in (Binary .. (Literal Int 0) (Literal Int 9)))] (Block [(Literal Int 1)])) (Branch [(Cond (Ident c))] (Block [(If (Ident d) (Block [(Literal Int 2)]))])) (Branch (Block [(Literal Int 3)]))])]`},
		{"when { a -> 1; else -> 2 }", `[(When [(Branch [(Cond (Ident a))] (Block [(Literal Int 1)])) (Branch (Block [(Literal Int 2)]))])]`},
		// data, enum, sealed and the refused modifiers are names where
		// no declaration follows.
		{"data.size\nvalue to key\nopen in s\nf { data -> data }\nfinal\ng()",
			`[(Select (Ident data) size) (Binary to (Ident value) (Ident key)) (Binary in (Ident open) (Ident s)) (Call (Ident f) [(Arg (Lambda [(LambdaParam data)] (Block [(Ident data)])) Trailing)]) (Ident final) (Call (Ident g))]`},
		// context is a name where no local fun follows its ( ).
		{"context(a)\ng()", "[(Call (Ident context) [(Arg (Ident a))]) (Call (Ident g))]"},
		{"fun g(x: Int = 1): Int {\n    return x\n}", `[(FunDecl g [(Param x (NamedType Int) (Literal Int 1))] (NamedType Int) (Block [(Jump return (Ident x))]))]`},
	}, stmts)
}

func TestSQL(t *testing.T) {
	f := parse(t, "fun f() = tx.query<R>(sql\"\"\"select $a, ${b.c}, ${xs.map { it }}, $1, $ \"\"\")\n// tx.execute(sql\"in a comment\")\nval s = \"sql\\\"\"")
	if len(f.SQL) != 1 {
		t.Fatalf("%d literals, want 1", len(f.SQL))
	}
	s := f.SQL[0]
	if got, want := show(s), `(SQL query "select $a, ${b.c}, ${xs.map { it }}, $1, $ " [(SQLParam a (Ident a)) (SQLParam (Select (Ident b) c)) (SQLParam (Call (Select (Ident xs) map) [(Arg (Lambda (Block [(Ident it)])) Trailing)]))])`; got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
	if s.Line != 2 || s.Column != 23 || s.TextPos.Offset != 38 || s.Params[1].Column != 40 {
		t.Errorf("literal at %d:%d, text at %d, ${ at column %d", s.Line, s.Column, s.TextPos.Offset, s.Params[1].Column)
	}
	for i, want := range []string{"$a", "${b.c}", "${xs.map { it }}"} {
		p, at := s.Params[i], s.TextPos.Offset
		if got := s.Text[p.Offset-at : p.End.Offset-at]; got != want || p.End.Column != p.Column+len(want) {
			t.Errorf("parameter %d spans %q, ends at column %d", i, got, p.End.Column)
		}
	}
	if id := s.Params[0].Expr.(*Ident); id.Offset != s.Params[0].Offset+1 {
		t.Errorf("$a's name at %d, want %d", id.Offset, s.Params[0].Offset+1)
	}

	// A literal inside another's ${} comes after it, in source order.
	f = parse(t, "fun f() = tx.query<R>(sql\"select ${tx.querySingle<Int>(sql\"select 1\")}\")")
	if len(f.SQL) != 2 || f.SQL[0].Text != "select ${tx.querySingle<Int>(sql\"select 1\")}" || f.SQL[1].Text != "select 1" || f.SQL[1].Call != "querySingle" || !f.SQL[1].Typed() {
		t.Errorf("literals out of order: %s", show(f.SQL))
	}
}
