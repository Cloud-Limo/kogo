package syntax

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

// Parse reads one source file. It stops at the first fault, which it
// returns as an *Error.
func Parse(name string, src []byte) (f *File, err error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			f, err = nil, e
		}
	}()
	s := newSource(name, string(src))
	p := &parser{src: s, toks: lex(s, 0, len(src)), f: &File{Name: name, Lines: bytes.Count(src, []byte("\n")) + 1}}
	p.tok = p.toks[0]
	p.file()
	return p.f, nil
}

type parser struct {
	src    *source
	toks   []token
	i      int
	tok    token
	parens bool // inside ( ) or [ ], where a line end ends nothing
	f      *File
}

func (p *parser) next() {
	if p.i < len(p.toks)-1 {
		p.i++
	}
	p.tok = p.toks[p.i]
}

func (p *parser) peek(n int) token { return p.toks[min(p.i+n, len(p.toks)-1)] }

// at reports whether the token is the operator or the word s.
func (p *parser) at(s string) bool {
	return (p.tok.kind == tOp || p.tok.kind == tIdent) && p.tok.text == s
}

func (p *parser) got(s string) bool {
	if p.at(s) {
		p.next()
		return true
	}
	return false
}

func (p *parser) want(s string) Pos {
	pos := p.tok.pos
	if !p.got(s) {
		p.fail(pos, "syntax", "expected %s, found %s", s, describe(p.tok))
	}
	return pos
}

// nl reports whether a line ends before the token, where that ends a
// statement: not inside parentheses or brackets.
func (p *parser) nl() bool { return p.tok.nl && !p.parens }

func (p *parser) fail(pos Pos, code, format string, args ...any) {
	fail(p.src, pos, code, format, args...)
}

// notKogo rejects a Kotlin construct of spec.md section 14.
func (p *parser) notKogo(pos Pos, code, what, instead string) {
	p.fail(pos, "kotlin-"+code, "%s is not Kogo: %s", what, instead)
}

func describe(t token) string {
	if t.kind == tEOF {
		return "the end"
	}
	return fmt.Sprintf("%q", t.text)
}

// try runs f and reports whether it parsed; if not, nothing it read
// is consumed.
func (p *parser) try(f func()) (ok bool) {
	i, parens, nsql := p.i, p.parens, len(p.f.SQL)
	defer func() {
		if e := recover(); e != nil {
			if _, ok := e.(*Error); !ok {
				panic(e)
			}
			p.i, p.parens, p.f.SQL = i, parens, p.f.SQL[:nsql]
			p.tok = p.toks[i]
		}
	}()
	f()
	return true
}

// nested parses f with line ends meaning what they do in braces (in
// false) or in parentheses (in true).
func (p *parser) nested(in bool, f func()) {
	saved := p.parens
	p.parens = in
	f()
	p.parens = saved
}

// Kotlin's hard keywords, which are never names.
var keywords = strings.Fields(`as break class continue do else false for fun if in interface is
	null object package return super this throw true try typealias typeof val var when while`)

// Keyword reports whether s is one of Kotlin's hard keywords.
func Keyword(s string) bool { return slices.Contains(keywords, s) }

func (p *parser) ident() (Pos, string) {
	t := p.tok
	if t.kind != tIdent || slices.Contains(keywords, t.text) {
		p.fail(t.pos, "syntax", "expected a name, found %s", describe(t))
	}
	p.next()
	return t.pos, t.text
}

// name reads the name a declaration takes. twice says it is taken
// already, by the declaration's kind (spec.md 1: kotlinc refuses
// both). _ and __ are no names in Kotlin.
func (p *parser) name(twice bool, kind string) string {
	pos, name := p.ident()
	if twice {
		p.fail(pos, "duplicate", "the %s %s is declared twice: rename one", kind, name)
	}
	p.unnamed(pos, name, kind)
	return name
}

func (p *parser) unnamed(pos Pos, name, kind string) {
	if strings.Trim(name, "_") == "" {
		p.fail(pos, "underscore", "%s is no name in Kotlin, which keeps it for what is unused: give the %s a name", name, kind)
	}
}

// path reads a dotted name.
func (p *parser) path() *Ident {
	pos, name := p.ident()
	for p.at(".") && p.peek(1).kind == tIdent {
		p.next()
		_, n := p.ident()
		name += "." + n
	}
	return &Ident{pos, name}
}

// end requires the end of a statement or a declaration: a line end, a
// semicolon, a closing brace or the end of the file.
func (p *parser) end() {
	if !p.tok.nl && !p.at(";") && !p.at("}") && p.tok.kind != tEOF {
		p.fail(p.tok.pos, "syntax", "expected a new line or ; before %s", describe(p.tok))
	}
	for p.got(";") {
	}
}

// list reads items separated by commas up to close, allowing a comma
// after the last, and steps past close.
func (p *parser) list(close string, item func()) {
	for !p.at(close) {
		item()
		if !p.got(",") {
			break
		}
	}
	p.want(close)
}

// entries reads the entries of an api or a tests list up to close,
// each separated from the next by a comma, a line end or both, with
// a comma allowed after the last, and steps past close (spec.md 15.1).
// entry reads one entry, words and all.
func (p *parser) entries(close string, entry func()) {
	for !p.at(close) {
		entry()
		if !p.got(",") && !p.tok.nl && !p.at(close) {
			p.fail(p.tok.pos, "syntax", "expected , or a new line between a list's entries, found %s", describe(p.tok))
		}
	}
	p.want(close)
}

func (p *parser) file() {
	f := p.f
	for p.at("@") && p.peek(1).text == "file" {
		pos := p.tok.pos
		p.next()
		p.next()
		p.want(":")
		if !p.at("OptIn") {
			p.notKogo(pos, "annotation", "an annotation other than a marker", "a file's head holds only @file:OptIn(Marker::class)")
		}
		p.next()
		a := &Annotation{Pos: pos, Name: "OptIn"}
		p.want("(")
		p.list(")", func() {
			name := p.path()
			p.want("::")
			p.want("class")
			a.Args = append(a.Args, &ClassRef{name.Pos, name.Name})
		})
		f.OptIn = append(f.OptIn, a)
	}
	if !p.at("package") {
		p.fail(p.tok.pos, "syntax", "a file begins with its package line, as package cusoon.model")
	}
	p.next()
	f.Package = p.path()
	p.end()
	for p.got("import") {
		imp := &Import{Ident: p.path()}
		if p.at(".") {
			p.fail(p.tok.pos, "syntax", "an import names one declaration: no wildcard")
		}
		if p.got("as") {
			pos, name := p.ident()
			imp.As = &Ident{pos, name}
		}
		f.Imports = append(f.Imports, imp)
		p.end()
	}
	for p.tok.kind != tEOF {
		if p.at("api") && p.peek(1).kind == tIdent {
			f.APIs = append(f.APIs, p.api())
		} else if p.at("tests") && p.peek(1).kind == tIdent {
			f.Tests = append(f.Tests, p.tests())
		} else {
			f.Decls = append(f.Decls, p.decl(false))
		}
		p.end()
	}
}

// api reads an api list: its head, api name checkedBy check or api
// name unchecked, and its rows (spec.md 15.1). api and the words of a
// list are names like any other elsewhere.
func (p *parser) api() *API {
	a := &API{Pos: p.tok.pos}
	p.next()
	_, a.Name = p.ident()
	if p.got("checkedBy") {
		pos, name := p.ident()
		a.Check = &Ident{pos, name}
	} else {
		a.Unchecked = p.got("unchecked")
	}
	p.want("{")
	p.entries("}", func() { a.Rows = append(a.Rows, p.row()) })
	return a
}

// row reads one row of an api list: op gatedBy gate, or op ungated,
// on one line.
func (p *parser) row() *Row {
	pos, op := p.ident()
	r := &Row{Op: &Ident{pos, op}}
	// word reports whether the row goes on with w, and steps past it.
	// A line that begins with w goes on the row above, unless it is
	// itself the row of an operation named w.
	word := func(w string) bool {
		next := p.peek(1)
		if !p.at(w) || p.tok.nl && !next.nl && (next.text == "gatedBy" || next.text == "ungated") {
			return false
		}
		p.sameLine(op)
		p.next()
		return true
	}
	name := func() *Ident {
		p.sameLine(op)
		pos, name := p.ident()
		return &Ident{pos, name}
	}
	if word("gatedBy") {
		r.Gate = name()
	} else if !word("ungated") {
		p.fail(pos, "syntax", "%s names no gate: a row is %s gatedBy <gate>, or %s ungated for an operation that finds out who is asking itself (spec.md 15.1)", op, op, op)
	}
	if p.tok.kind != tEOF && !p.tok.nl && !p.at(",") && !p.at("}") {
		p.fail(p.tok.pos, "syntax", "a row is %s gatedBy <gate>, or %s ungated, and nothing more: remove %s, or end the row with a comma or a new line (spec.md 15.1)", op, op, describe(p.tok))
	}
	return r
}

// sameLine requires the token to stand on the line of the row of op.
func (p *parser) sameLine(op string) {
	if p.tok.nl {
		p.fail(p.tok.pos, "syntax", "a row stays on one line: move %s onto %s's line (spec.md 15.1)", p.tok.text, op)
	}
}

// tests reads a tests list, tests name<Env> { a, b }. tests is a name
// like any other elsewhere.
func (p *parser) tests() *Tests {
	t := &Tests{Pos: p.tok.pos}
	p.next()
	_, t.Name = p.ident()
	if !p.at("<") {
		p.fail(p.tok.pos, "syntax", "a tests list names its environment's type: tests %s<Env> { ... }, with <Unit> for none (spec.md 16.1)", t.Name)
	}
	p.next()
	t.Env = p.typ()
	p.want(">")
	p.want("{")
	p.entries("}", func() {
		pos, name := p.ident()
		t.Vals = append(t.Vals, &Ident{pos, name})
	})
	return t
}

// Modifiers that Kotlin has and Kogo does not, with the section 14 row
// that answers each.
var refused = map[string][3]string{
	"open":        {"class", "an open class", "use a data class, a sealed interface and top-level functions"},
	"abstract":    {"class", "an abstract class", "use a data class, a sealed interface and top-level functions"},
	"final":       {"class", "final", "every Kogo type is final; leave it out"},
	"inner":       {"class", "an inner class", "use a data class, a sealed interface and top-level functions"},
	"companion":   {"object", "a companion object", "use top-level vals and functions"},
	"lateinit":    {"lateinit", "lateinit", "pass the value as a parameter"},
	"suspend":     {"suspend", "suspend", "write plain blocking code"},
	"vararg":      {"vararg", "vararg", "take a List"},
	"inline":      {"inline", "inline", "write a plain function"},
	"reified":     {"inline", "reified", "write a plain function"},
	"operator":    {"inline", "operator", "write a plain function"},
	"infix":       {"inline", "infix", "write a plain function"},
	"tailrec":     {"inline", "tailrec", "write a plain function, or a while loop"},
	"crossinline": {"inline", "crossinline", "write a plain function"},
	"noinline":    {"inline", "noinline", "write a plain function"},
	"value":       {"value-class", "a value class", "use a typealias"},
	"protected":   {"visibility", "protected", "Kogo has private (this file) and internal (this package)"},
	"public":      {"visibility", "public", "leave it out: no modifier means public"},
}

// modifier reports whether the word at hand is a modifier: a word
// followed by an annotation, or by a word that is not an infix
// operator (in, is, to, until, as).
func (p *parser) modifier() bool {
	next := p.peek(1)
	return p.tok.kind == tIdent && (next.text == "@" || next.kind == tIdent && precedence[next.text] == 0 && next.text != "as")
}

func (p *parser) modifiers() Modifiers {
	var m Modifiers
	seen := map[string]bool{}
	for {
		word := p.tok.text
		switch {
		case p.modifier() && seen[word]:
			p.fail(p.tok.pos, "modifier", "%s is written twice, which kotlinc refuses: write it once", word)
		case p.modifier() && (p.at("private") || p.at("internal")) && m.Visibility != "":
			p.fail(p.tok.pos, "modifier", "%s and %s are both written, which kotlinc refuses: write the one meant", m.Visibility, word)
		}
		if p.modifier() {
			seen[word] = true
		}
		switch {
		case p.at("@"):
			m.Annotations = append(m.Annotations, p.annotation())
		case p.modifier() && refused[p.tok.text][0] != "":
			r := refused[p.tok.text]
			p.notKogo(p.tok.pos, r[0], r[1], r[2])
		case p.modifier() && (p.at("private") || p.at("internal")):
			m.Visibility = p.tok.text
			p.next()
		case p.modifier() && p.at("override"):
			m.Override = true
			p.next()
		case p.modifier() && p.at("const"):
			m.Const = true
			p.next()
		case p.modifier() && p.at("external"):
			m.External = true
			p.next()
		case p.modifier() && p.at("action"):
			m.Calls = p.tok.text
			p.next()
		default:
			return m
		}
	}
}

// annotation reads a marker, @Name, or a binding's @go("package").
func (p *parser) annotation() *Annotation {
	pos := p.want("@")
	if p.peek(1).text == ":" && !p.peek(1).nl {
		p.notKogo(pos, "annotation", "an annotation with a target", "only markers exist, written @Marker, or @property:Marker before a data class's val")
	}
	_, name := p.ident()
	a := &Annotation{Pos: pos, Name: name}
	if p.at("(") && !p.tok.nl {
		if name == "OptIn" && p.peek(1).kind == tIdent {
			p.next()
			if x := p.path(); p.at("::") {
				p.notKogo(pos, "annotation", "@OptIn on a declaration", "opt in on the file: begin this file with @file:OptIn("+x.Name+"::class)")
			}
		}
		if name != "go" {
			p.notKogo(pos, "annotation", "an annotation with arguments", "only markers exist, written @Marker with none")
		}
		p.next()
		a.Args = append(a.Args, p.expr())
		p.want(")")
	}
	return a
}

// decl reads a declaration. A member of an interface or an external
// object is abstract: it has no body.
func (p *parser) decl(abstract bool) Decl {
	pos := p.tok.pos
	m := p.modifiers()
	next := p.peek(1).text
	switch {
	case p.tok.kind == tIdent && !p.at("fun") && !p.at("val") && (next == "external" || next == "fun" || next == "val"):
		p.fail(p.tok.pos, "syntax", "%s is no word of Kogo's before %s: leave it out, or write one that is (private, internal, override, const, external, action)", p.tok.text, next)
	case m.Calls != "" && !p.at("fun"):
		p.fail(pos, "syntax", "%s goes before fun, and before nothing else: %s fun", m.Calls, m.Calls)
	case m.External && (p.at("class") || p.at("object")):
		kind := "external " + p.tok.text
		p.next()
		return p.classDecl(&ClassDecl{Pos: pos, Mods: m, Kind: kind})
	case m.External && !p.at("fun") && !p.at("val"):
		p.fail(p.tok.pos, "syntax", "external goes before class, object, fun or val")
	case p.at("fun"):
		return p.funDecl(m, abstract || m.External)
	case p.at("val") || p.at("var"):
		if p.at("var") { // a local is read by stmt
			p.notKogo(p.tok.pos, "top-level-var", "a var outside a function body", "pass the value as a parameter")
		}
		return p.valDecl(m, abstract || m.External)
	case p.at("typealias"):
		p.next()
		d := &TypeAlias{Pos: pos, Mods: m}
		_, d.Name = p.ident()
		d.TypeParams = p.typeParams(false)
		p.want("=")
		d.Type = p.typ()
		return d
	case p.at("class"):
		p.notKogo(p.tok.pos, "class", "a class", "use a data class, a sealed interface and top-level functions")
	case p.at("object"):
		p.notKogo(p.tok.pos, "object", "an object", "use top-level vals and functions")
	case p.at("sealed") && p.peek(1).text == "class":
		p.notKogo(p.tok.pos, "class", "a sealed class", "use a sealed interface")
	case p.at("context") && p.peek(1).text == "(":
		p.notKogo(p.tok.pos, "context", "a context parameter", contextFix)
	}
	kinds := []string{"data class", "data object", "sealed interface", "interface", "enum class", "annotation class"}
	for _, k := range kinds {
		words := strings.Fields(k)
		if p.at(words[0]) && (len(words) == 1 || p.peek(1).text == words[1]) {
			p.next()
			if len(words) == 2 {
				p.next()
			}
			return p.classDecl(&ClassDecl{Pos: pos, Mods: m, Kind: k})
		}
	}
	p.fail(p.tok.pos, "syntax", "expected a declaration (fun, val, data class, interface, ...), found %s", describe(p.tok))
	return nil
}

// receiverAndName reads Name, or Receiver.name for an extension. No
// extension's receiver is nullable, a binding's included.
func (p *parser) receiverAndName() (Type, string) {
	t := p.typ()
	refuse := func(pos Pos) {
		p.fail(pos, "nullable-receiver", "an extension's receiver is not nullable: take the value as a parameter")
	}
	if p.at("?.") { // Receiver?.name: the ? is the receiver's
		refuse(p.tok.pos)
	}
	if p.got(".") {
		if nullable(t) {
			refuse(t.At())
		}
		_, name := p.ident()
		return t, name
	}
	n, ok := t.(*NamedType)
	if !ok || n.Args != nil || n.Nullable {
		p.fail(t.At(), "syntax", "expected a name, or a receiver type, a dot and a name")
	}
	i := strings.LastIndexByte(n.Name, '.')
	if i < 0 {
		return nil, n.Name
	}
	return &NamedType{Pos: n.Pos, Name: n.Name[:i]}, n.Name[i+1:]
}

func setNullable(t Type) {
	switch t := t.(type) {
	case *NamedType:
		t.Nullable = true
	case *FuncType:
		t.Nullable = true
	}
}

// nullable reports whether t is written with a trailing ?.
func nullable(t Type) bool {
	switch t := t.(type) {
	case *NamedType:
		return t.Nullable
	case *FuncType:
		return t.Nullable
	}
	return false
}

func (p *parser) funDecl(m Modifiers, abstract bool) *FunDecl {
	d := &FunDecl{Pos: p.want("fun"), Mods: m}
	d.TypeParams = p.typeParams(false)
	at := p.tok.pos
	d.Receiver, d.Name = p.receiverAndName()
	p.unnamed(at, d.Name, "fun")
	d.Params = p.params(false, m.External)
	if p.got(":") {
		d.Result = p.typ()
	}
	switch {
	case abstract && (p.at("=") || p.at("{")):
		p.fail(p.tok.pos, "syntax", "a member of an interface or a binding has no body")
	case abstract:
	case p.got("="):
		d.Expr = p.expr()
	case p.at("{"):
		d.Body = p.block()
	default:
		p.fail(p.tok.pos, "syntax", "expected = or { to begin the body of %s, found %s", d.Name, describe(p.tok))
	}
	return d
}

// typeParams reads <A, B>. Only a sealed interface and an external
// class take out.
func (p *parser) typeParams(out bool) []*TypeParam {
	var tps []*TypeParam
	if !p.got("<") {
		return nil
	}
	p.list(">", func() {
		if r := refused["reified"]; p.at("reified") {
			p.notKogo(p.tok.pos, r[0], r[1], r[2])
		}
		pos := p.tok.pos
		tp := &TypeParam{Pos: pos, Out: p.peek(1).kind == tIdent && p.got("out")}
		if tp.Out && !out {
			p.fail(tp.Pos, "syntax", "out goes only on the type parameter of a sealed interface or an external class")
		}
		if p.at("in") {
			p.fail(p.tok.pos, "syntax", "a type parameter is plain, or out on a sealed interface or an external class")
		}
		tp.Name = p.name(slices.IndexFunc(tps, func(o *TypeParam) bool { return o.Name == p.tok.text }) >= 0, "type parameter")
		if p.at(":") {
			p.fail(p.tok.pos, "syntax", "a type parameter has no bound")
		}
		tps = append(tps, tp)
	})
	return tps
}

// params reads a parameter list; a data class's are vals, and a
// binding's may be vararg.
func (p *parser) params(ctor, external bool) []*Param {
	var ps []*Param
	p.want("(")
	p.nested(true, func() {
		p.list(")", func() {
			pos := p.tok.pos
			vararg := external && p.modifier() && p.got("vararg")
			var marks []*Annotation
			for ctor && p.at("@") && p.peek(1).text == "property" && p.peek(2).text == ":" {
				p.i += 2
				p.next()
				at, name := p.ident()
				marks = append(marks, &Annotation{Pos: at, Name: name})
			}
			m := p.modifiers()
			if ctor && m.Annotations != nil {
				p.fail(m.Annotations[0].Pos, "syntax", "a marker on a data class's property is written @property:%s, as Kotlin has it; a bare @%s there marks the constructor's parameter", m.Annotations[0].Name, m.Annotations[0].Name)
			}
			if m.Annotations != nil || m.Visibility != "" || m.Const || m.External || m.Calls != "" {
				p.fail(pos, "syntax", "a parameter takes no modifier")
			}
			par := &Param{Pos: pos, Override: m.Override, Val: p.got("val"), Vararg: vararg, Marks: marks}
			if ctor && (!par.Val || p.at("var")) {
				p.fail(pos, "syntax", "each property of a data class is a val: val name: Type")
			}
			if !ctor && (par.Val || par.Override || p.at("var")) {
				p.fail(pos, "syntax", "a function's parameter is written name: Type")
			}
			par.Name = p.name(ctor && slices.ContainsFunc(ps, func(o *Param) bool { return o.Name == p.tok.text }), map[bool]string{true: "property", false: "parameter"}[ctor])
			p.want(":")
			par.Type = p.typ()
			if p.got("=") {
				par.Default = p.expr()
			}
			ps = append(ps, par)
		})
	})
	return ps
}

func (p *parser) valDecl(m Modifiers, abstract bool) *ValDecl {
	d := &ValDecl{Pos: p.tok.pos, Mods: m, Var: p.at("var")}
	p.next()
	if p.at("<") {
		d.TypeParams = p.typeParams(false)
	}
	if p.at("(") {
		d.Destructure = p.names()
	} else {
		at := p.tok.pos
		d.Receiver, d.Name = p.receiverAndName()
		p.unnamed(at, d.Name, "val")
	}
	if d.TypeParams != nil && d.Receiver == nil {
		p.fail(d.Pos, "type-params", "only an extension val takes type parameters, which kotlinc refuses elsewhere: remove <...>")
	}
	if p.got(":") {
		d.Type = p.typ()
	}
	if p.at("by") {
		p.notKogo(p.tok.pos, "delegation", "by and delegation", "compute the value once and pass it")
	}
	if !abstract && p.got("=") {
		d.Value = p.expr()
	}
	if d.Receiver != nil && p.at("get") && p.peek(1).text == "(" {
		p.next()
		p.next()
		p.want(")")
		p.want("=")
		d.Getter = p.expr()
	}
	return d
}

// names reads (a, b) in a destructuring.
func (p *parser) names() []string {
	var names []string
	p.want("(")
	p.nested(true, func() {
		p.list(")", func() {
			_, n := p.ident()
			names = append(names, n)
		})
	})
	if names == nil {
		p.fail(p.toks[p.i-1].pos, "syntax", "a destructuring names at least one value: (a, b)")
	}
	return names
}

func (p *parser) classDecl(d *ClassDecl) *ClassDecl {
	d.Name = p.name(false, "type")
	switch d.Kind {
	case "data class":
		d.TypeParams = p.typeParams(false)
		if !p.at("(") {
			d.Ctor = p.modifiers()
			p.want("constructor")
		}
		d.Params = p.params(true, false)
	case "interface", "sealed interface":
		d.TypeParams = p.typeParams(d.Kind == "sealed interface")
	case "external class":
		d.TypeParams = p.typeParams(true)
		if p.at("{") || p.at("(") {
			p.fail(p.tok.pos, "syntax", "an external class has no constructor and no body: declare each member as an external fun or val with the class as its receiver")
		}
		return d
	case "enum class":
		if p.at("(") {
			p.fail(p.tok.pos, "syntax", "an enum is constants only: no constructor and no body")
		}
		p.want("{")
		p.nested(false, func() {
			for !p.at("}") && !p.at(";") {
				if p.at("name") || p.at("ordinal") || p.at("entries") {
					p.fail(p.tok.pos, "duplicate", "every enum has %s already, and kotlinc refuses a constant of that name: rename it", p.tok.text)
				}
				pos := p.tok.pos
				name := p.name(slices.ContainsFunc(d.Entries, func(e *Ident) bool { return e.Name == p.tok.text }), "constant")
				d.Entries = append(d.Entries, &Ident{pos, name})
				if p.at("(") || p.at("{") {
					p.fail(p.tok.pos, "syntax", "an enum is constants only: no constructor and no body")
				}
				if !p.got(",") {
					break
				}
			}
			if p.got(";") && !p.at("}") { // a ; after the constants starts a body
				p.fail(p.tok.pos, "syntax", "an enum is constants only: no constructor and no body")
			}
			p.want("}")
		})
		return d
	case "annotation class":
		return d
	}
	if d.Kind == "data class" || d.Kind == "data object" {
		if p.got(":") {
			for {
				t := p.typ()
				if p.at("(") {
					p.notKogo(p.tok.pos, "class", "inheritance", "a data class implements interfaces; it extends nothing")
				}
				if p.at("by") {
					p.notKogo(p.tok.pos, "delegation", "by and delegation", "implement each member")
				}
				d.Supers = append(d.Supers, t)
				if !p.got(",") {
					break
				}
			}
		}
	}
	if !p.at("{") {
		return d
	}
	abstract := d.Kind != "data class" && d.Kind != "data object"
	p.want("{")
	p.nested(false, func() {
		for p.got(";"); !p.at("}"); {
			m := p.decl(abstract)
			if f, ok := m.(*FunDecl); !abstract && (!ok || !f.Mods.Override) {
				p.fail(m.At(), "syntax", "a data class body holds only the override funs of its interfaces")
			}
			d.Body = append(d.Body, m)
			p.end()
		}
		p.next()
	})
	return d
}

// typ reads a type: Name, Name.Inner, Name<Args>, (A) -> R,
// Receiver.(A) -> R, and any of them followed by ?.
func (p *parser) typ() Type {
	pos := p.tok.pos
	var t Type
	if p.at("(") {
		var params []Type
		p.next()
		p.nested(true, func() { p.list(")", func() { params = append(params, p.typ()) }) })
		switch {
		case p.got("->"):
			t = &FuncType{Pos: pos, Params: params, Result: p.typ()}
		case len(params) == 1:
			t = params[0]
		default:
			p.fail(p.tok.pos, "syntax", "expected -> after a function type's parameters")
		}
	} else {
		n := &NamedType{Pos: pos}
		_, n.Name = p.ident()
		for p.at(".") && p.peek(1).kind == tIdent {
			p.next()
			_, name := p.ident()
			n.Name += "." + name
		}
		if p.at("<") && !p.tok.nl {
			p.next()
			p.nested(true, func() {
				p.list(">", func() {
					if p.at("*") || p.at("in") || p.at("out") {
						p.fail(p.tok.pos, "syntax", "a type argument is a type: no projection or variance")
					}
					n.Args = append(n.Args, p.typ())
				})
			})
		}
		t = n
		if p.at(".") && p.peek(1).text == "(" {
			p.next()
			f, ok := p.typ().(*FuncType)
			if !ok || f.Receiver != nil {
				p.fail(pos, "syntax", "a function type with a receiver is written Receiver.(A) -> R")
			}
			f.Pos, f.Receiver = pos, n
			return f
		}
	}
	if p.got("?") {
		setNullable(t)
	}
	return t
}

// block reads { statements }.
func (p *parser) block() *Block {
	b := &Block{Pos: p.want("{")}
	p.nested(false, func() { b.Stmts = p.stmts() })
	return b
}

// stmts reads statements up to a closing brace and steps past it.
func (p *parser) stmts() []Stmt {
	var stmts []Stmt
	for p.got(";"); !p.at("}"); {
		if p.tok.kind == tEOF {
			p.fail(p.tok.pos, "syntax", "expected } before the end of the file")
		}
		stmts = append(stmts, p.stmt())
		p.end()
	}
	p.next()
	return stmts
}

// body reads the body of an if, a loop or a when branch: a block, or
// one statement taken as a block.
func (p *parser) body() *Block {
	if p.at("{") {
		return p.block()
	}
	pos := p.tok.pos
	if p.at("fun") {
		p.fail(pos, "local-fun", "kotlinc reads a fun here as an expression, and refuses one with a name: declare it in braces, { fun %s ... }, or before", p.peek(1).text)
	}
	return &Block{pos, []Stmt{p.stmt()}}
}

var assignOps = []string{"=", "+=", "-=", "*=", "/=", "%="}

func (p *parser) stmt() Stmt {
	pos := p.tok.pos
	switch {
	case p.at("val") || p.at("var"):
		return p.valDecl(Modifiers{}, false)
	case p.at("fun"):
		return p.funDecl(Modifiers{}, false)
	case p.at("action") && p.peek(1).text == "fun" && !p.peek(1).nl:
		p.fail(pos, "syntax", "action goes before a top-level fun only: declare the action fun at the top level, or leave the word out and move the action out of this fun")
	case p.at("for"):
		return p.forStmt()
	case p.at("while"):
		p.next()
		w := &While{Pos: pos}
		w.Cond = p.header()
		w.Body = p.body()
		return w
	case p.at("do"):
		p.fail(pos, "syntax", "there is no do-while: write a while loop")
	case p.at("@"):
		p.notKogo(pos, "annotation", "an annotation in a function body", "a marker goes on a top-level declaration or a constructor")
	case p.modifier() && !p.peek(1).nl && refused[p.tok.text][0] != "":
		r := refused[p.tok.text]
		p.notKogo(pos, r[0], r[1], r[2])
	case p.typeDeclAhead():
		p.fail(pos, "syntax", "a type is declared at the top level of a file")
	case p.contextAhead():
		p.notKogo(pos, "context", "a context parameter", contextFix)
	}
	x := p.expr()
	if p.tok.kind == tOp && slices.Contains(assignOps, p.tok.text) && !p.nl() {
		a := &Assign{Pos: p.tok.pos, Op: p.tok.text, Target: x}
		if _, ok := x.(*Ident); !ok {
			p.fail(a.Pos, "syntax", "only a local var is assigned; a collection is changed by building a new one")
		}
		p.next()
		a.Value = p.expr()
		return a
	}
	return x
}

const contextFix = "take the value as a parameter, or for Raise<E> return an Either and bind it"

// contextAhead reports whether a context(...) before a local fun is
// next, rather than a call of something named context.
func (p *parser) contextAhead() bool {
	if !p.at("context") || p.peek(1).text != "(" {
		return false
	}
	depth := 0
	for n := 1; p.peek(n).kind != tEOF; n++ {
		switch p.peek(n).text {
		case "(":
			depth++
		case ")":
			if depth--; depth == 0 {
				return p.peek(n+1).text == "fun"
			}
		}
	}
	return false
}

// typeDeclAhead reports whether a type's declaration begins here.
// data, enum and sealed are names unless a declaration's word follows.
func (p *parser) typeDeclAhead() bool {
	next := p.peek(1).text
	return p.at("interface") || p.at("typealias") ||
		p.at("data") && (next == "class" || next == "object") ||
		p.at("enum") && next == "class" ||
		p.at("sealed") && (next == "interface" || next == "class")
}

// header reads the ( expr ) of an if, a while or a when.
func (p *parser) header() Expr {
	var x Expr
	p.want("(")
	p.nested(true, func() {
		x = p.expr()
		p.want(")")
	})
	return x
}

func (p *parser) forStmt() *For {
	f := &For{Pos: p.want("for")}
	p.want("(")
	p.nested(true, func() {
		if p.at("(") {
			f.Destructure = p.names()
		} else {
			f.Name = p.name(false, "loop's value")
		}
		p.want("in")
		f.Iter = p.expr()
		p.want(")")
	})
	f.Body = p.body()
	return f
}

// Binary operators by precedence, as Kotlin's: a higher number binds
// tighter. Comparison (4) does not chain.
var precedence = map[string]int{
	"||": 1, "&&": 2, "==": 3, "!=": 3, "<": 4, ">": 4, "<=": 4, ">=": 4, "in": 5, "is": 5,
	"?:": 6, "until": 7, "to": 7, "..": 8, "+": 9, "-": 9, "*": 10, "/": 10, "%": 10,
}

func (p *parser) expr() Expr { return p.binary(1) }

// binary reads operators of precedence min and above. A line end
// before an operator ends the expression, except before ?:, && and
// ||; the member operators . and ?. are read by postfix.
func (p *parser) binary(min int) Expr {
	x := p.prefix()
	for {
		t := p.tok
		if !p.nl() {
			p.refuseOperator()
		}
		level := precedence[t.text]
		if t.kind != tOp && t.kind != tIdent || level == 0 || level < min ||
			p.nl() && t.text != "?:" && t.text != "&&" && t.text != "||" {
			return x
		}
		p.next()
		if t.text == "<" && p.at("(") {
			p.typeArgsRead(t.pos)
		}
		if t.text == "is" {
			x = &Is{t.pos, x, p.typ()}
			continue
		}
		x = &Binary{t.pos, t.text, x, p.binary(level + 1)}
		if level == 4 && precedence[p.tok.text] == 4 && p.tok.kind == tOp && !p.nl() {
			p.fail(p.tok.pos, "syntax", "comparisons do not chain: join two with &&")
		}
	}
}

// typeArgsRead refuses x < (... > ...), which kotlinc reads as type
// arguments: a > (not >=) before the ( after the < closes, other
// than one that closes a < inside, as listOf<Int>( has.
func (p *parser) typeArgsRead(lt Pos) {
	depth, angles := 0, 0
	for n := 0; p.peek(n).kind != tEOF; n++ {
		switch t := p.peek(n); t.text {
		case "(":
			depth++
		case ")":
			if depth--; depth == 0 {
				return
			}
		case "<":
			angles++
		case ">":
			if angles > 0 {
				angles--
			} else if t.kind == tOp {
				p.fail(lt, "less-than", "kotlinc reads this < and the > after it as type arguments: name the right side first, val y = (...), and compare with it")
			}
		}
	}
}

// refuseOperator rejects, where an operator could stand, a Kotlin
// operator that Kogo does not have.
func (p *parser) refuseOperator() {
	t := p.tok
	p.refuseNot()
	switch {
	case t.text == "===" || t.text == "!==":
		p.fail(t.pos, "syntax", "there is no %s: == and != compare by value", t.text)
	case t.text == "as" && t.kind == tIdent:
		p.fail(t.pos, "syntax", "there is no cast: test with is, and the value is smart cast")
	case t.kind == tIdent && precedence[t.text] == 0 && !slices.Contains(keywords, t.text):
		p.fail(t.pos, "syntax", "expected an operator or the end of the statement, found %s; only until and to are written between their operands", describe(t))
	}
}

// refuseNot rejects !in and !is.
func (p *parser) refuseNot() {
	if next := p.peek(1); p.at("!") && (next.text == "in" || next.text == "is") && !next.nl {
		p.fail(p.tok.pos, "syntax", "there is no !%s: write !(x %s y)", next.text, next.text)
	}
}

func (p *parser) prefix() Expr {
	t := p.tok
	switch t.text {
	case "-", "!":
		if t.kind == tOp {
			p.next()
			return &Unary{t.pos, t.text, p.prefix()}
		}
	case "++", "--":
		p.fail(t.pos, "syntax", "there is no %s: write x %c= 1 on a local var", t.text, t.text[0])
	}
	return p.postfix(p.primary())
}

var sqlCalls = map[string]bool{"query": true, "queryOne": true, "querySingle": true, "exists": false, "execute": false, "refusal": false}

func (p *parser) postfix(x Expr) Expr {
	for {
		t := p.tok
		switch {
		case p.at(".") || p.at("?."):
			p.next()
			pos, name := p.ident()
			x = &Select{pos, x, t.text == "?.", name}
		case p.nl() && p.at("{"):
			if _, ok := x.(*Call); ok {
				p.fail(t.pos, "lambda-line", "kotlinc takes a lambda on the line after a call as the call's last argument: end the call's line with ;, or name the lambda with a val")
			}
			return x
		case p.nl():
			return x
		case p.at("("):
			x = p.call(x, t.pos, nil)
		case p.at("<") && p.typeArgsAhead():
			var args []Type
			p.next()
			p.nested(true, func() { p.list(">", func() { args = append(args, p.typ()) }) })
			x = p.call(x, t.pos, args)
		case p.at("{"):
			x = p.call(x, t.pos, nil)
		case p.at("["):
			p.next()
			ix := &Index{Pos: t.pos, X: x}
			p.nested(true, func() {
				ix.Index = p.expr()
				p.want("]")
			})
			x = ix
		case p.at("!") && p.peek(1).text == "!" && p.peek(1).pos.Offset == t.pos.Offset+1:
			p.notKogo(t.pos, "not-null-assertion", "x!!", `use requireNotNull(x) { "why" }, or handle the null`)
		case p.at("++") || p.at("--"):
			p.fail(t.pos, "syntax", "there is no %s: write x %c= 1 on a local var", t.text, t.text[0])
		case p.at("::"):
			if p.peek(1).text == "class" {
				p.notKogo(t.pos, "class-literal", "X::class", "the only ::class is inside @file:OptIn(...)")
			}
			p.fail(t.pos, "syntax", "a function reference is ::name, of a top-level function")
		default:
			return x
		}
	}
}

// typeArgsAhead reports whether the < at hand opens type arguments: it
// does when what follows reads as types closed by > and then ( or {.
// Otherwise it is less than. As in Kotlin, a < b > (c) reads as a call.
func (p *parser) typeArgsAhead() bool {
	i, ok := p.i, false
	p.try(func() {
		p.next()
		p.nested(true, func() { p.list(">", func() { p.typ() }) })
		ok = (p.at("(") || p.at("{")) && !p.nl()
	})
	p.i, p.tok = i, p.toks[i]
	return ok
}

// call reads the arguments of a call of fun, and a trailing lambda on
// the same line. A sql literal is read only as the one argument of
// tx.query<R> and the other calls of spec.md section 10.2.
func (p *parser) call(fun Expr, pos Pos, typeArgs []Type) Expr {
	c := &Call{Pos: pos, Fun: fun, TypeArgs: typeArgs}
	sel, _ := fun.(*Select)
	typed, isSQL := false, false
	if sel != nil {
		typed, isSQL = sqlCalls[sel.Name]
	}
	if p.got("(") {
		p.nested(true, func() {
			p.list(")", func() {
				a := &Arg{Pos: p.tok.pos}
				if p.tok.kind == tIdent && p.peek(1).text == "=" {
					_, a.Name = p.ident()
					p.next()
				}
				if p.at("*") {
					p.notKogo(p.tok.pos, "spread", "*spread", "pass a List")
				}
				if id, ok := fun.(*Ident); ok && p.tok.kind == tSQL {
					if _, five := sqlCalls[id.Name]; five {
						p.fail(p.tok.pos, "sql-placement", "call %s on the Tx: tx.%s(...), or this.%s(...) in a fun on Tx", id.Name, id.Name, id.Name)
					}
				}
				if p.tok.kind == tSQL && isSQL && a.Name == "" && len(c.Args) == 0 {
					s := p.sql()
					s.Call, a.Value = sel.Name, s
					if !p.at(")") && !(p.at(",") && p.peek(1).text == ")") {
						p.fail(a.Pos, "sql-placement", "a sql literal is the only argument of %s", sel.Name)
					}
				} else {
					a.Value = p.expr()
				}
				c.Args = append(c.Args, a)
			})
		})
	}
	hasSQL := len(c.Args) > 0
	if hasSQL {
		_, hasSQL = c.Args[0].Value.(*SQL)
	}
	if hasSQL && typed != (len(typeArgs) == 1) || hasSQL && !typed && typeArgs != nil {
		p.fail(pos, "sql-placement", "write tx.query<R>, queryOne<R> and querySingle<R> with the row type R, and exists, execute and a test's refusal without one")
	}
	if p.at("{") && !p.nl() {
		if hasSQL {
			p.fail(p.tok.pos, "sql-placement", "a sql literal is the only argument of %s", sel.Name)
		}
		l := p.lambda()
		c.Args = append(c.Args, &Arg{Pos: l.Pos, Value: l, Trailing: true})
	}
	return c
}

func (p *parser) sql() *SQL {
	t := p.tok
	p.next()
	s := &SQL{Pos: t.pos, Text: t.value, TextPos: t.textPos}
	p.f.SQL = append(p.f.SQL, s) // before any literal in its ${}
	for _, part := range t.parts {
		sp := &SQLParam{Pos: part.pos, Name: part.name}
		if part.expr {
			sp.Expr = p.template(part)
			sp.End = p.src.pos(part.end + 1)
		} else {
			sp.Expr = &Ident{Pos{part.pos.Offset + 1, part.pos.Line, part.pos.Column + 1}, part.name}
			sp.End = Pos{part.pos.Offset + 1 + len(part.name), part.pos.Line, part.pos.Column + 1 + len(part.name)}
		}
		s.Params = append(s.Params, sp)
	}
	return s
}

// template parses the expression of a ${...}.
func (p *parser) template(part part) Expr {
	sub := &parser{src: p.src, toks: lex(p.src, part.start, part.end), f: p.f}
	sub.tok = sub.toks[0]
	x := sub.expr()
	if sub.tok.kind != tEOF {
		sub.fail(sub.tok.pos, "syntax", "expected } to close ${, found %s", describe(sub.tok))
	}
	return x
}

func (p *parser) primary() Expr {
	t := p.tok
	switch t.kind {
	case tNumber:
		p.next()
		return &Literal{t.pos, t.lit, t.value}
	case tChar:
		p.next()
		return &Literal{t.pos, "Char", t.value}
	case tString:
		p.next()
		s := &StringLit{Pos: t.pos, Raw: t.raw}
		for _, part := range t.parts {
			switch {
			case part.expr:
				s.Parts = append(s.Parts, p.template(part))
			case part.name != "":
				s.Parts = append(s.Parts, &Ident{Pos{part.pos.Offset + 1, part.pos.Line, part.pos.Column + 1}, part.name})
			default:
				s.Parts = append(s.Parts, &Literal{part.pos, "String", part.text})
			}
		}
		return s
	case tSQL:
		p.fail(t.pos, "sql-placement", "a sql literal is only the argument of tx.query<R>, queryOne<R>, querySingle<R>, exists or execute")
	}
	label := func() {
		if next := p.peek(1); next.text == "@" && next.pos.Offset == t.pos.Offset+len(t.text) {
			p.notKogo(t.pos, "label", "a label", "restructure with if/else or a local function")
		}
	}
	switch {
	case p.at("true") || p.at("false"):
		p.next()
		return &Literal{t.pos, "Boolean", t.text}
	case p.at("null"):
		p.next()
		return &Literal{t.pos, "Null", "null"}
	case p.at("("):
		var x Expr
		p.next()
		p.nested(true, func() {
			x = p.expr()
			p.want(")")
		})
		return x
	case p.at("{"):
		return p.lambda()
	case p.at("if"):
		return p.ifExpr()
	case p.at("when"):
		return p.when()
	case p.at("return") || p.at("break") || p.at("continue"):
		label()
		p.next()
		j := &Jump{Pos: t.pos, Op: t.text}
		if t.text == "return" && !p.nl() && p.tok.kind != tEOF && !slices.Contains([]string{"}", ")", "]", ";", ",", "else"}, p.tok.text) {
			j.Value = p.expr()
		}
		return j
	case p.at("::"):
		p.next()
		pos, name := p.ident()
		return &FuncRef{pos, name}
	case p.at("try") || p.at("throw"):
		p.notKogo(t.pos, "try", t.text, "return an Either, or panic with check or error")
	case p.at("object"):
		p.notKogo(t.pos, "object", "an object expression", "write a data class implementing the interface, or a lambda")
	case p.at("this"):
		label()
		p.next()
		return &Ident{t.pos, "this"}
	case t.kind == tIdent && !slices.Contains(keywords, t.text):
		label()
		p.next()
		return &Ident{t.pos, t.text}
	}
	p.fail(t.pos, "syntax", "expected an expression, found %s", describe(t))
	return nil
}

func (p *parser) lambda() *Lambda {
	l := &Lambda{Pos: p.want("{")}
	p.nested(false, func() {
		if p.paramsAhead() {
			p.nested(true, func() {
				for !p.at("->") {
					lp := &LambdaParam{Pos: p.tok.pos}
					if p.at("(") {
						lp.Destructure = p.names()
					} else {
						_, lp.Name = p.ident()
						if p.got(":") {
							lp.Type = p.typ()
						}
					}
					l.Params = append(l.Params, lp)
					if !p.got(",") {
						break
					}
				}
				p.want("->")
			})
		}
		l.Body = &Block{l.Pos, p.stmts()}
	})
	return l
}

// paramsAhead reports whether a lambda's parameters come next: names
// and destructurings then ->, or a name then :, which only a parameter
// begins with. It decides before any type is read, so that a fault in
// a parameter's type is reported as itself.
func (p *parser) paramsAhead() bool {
	i, ok := p.i, false
	p.try(func() {
		p.nested(true, func() {
			for {
				if p.at("(") {
					p.names()
				} else if p.ident(); p.at(":") {
					ok = true
					return
				}
				if !p.got(",") {
					break
				}
			}
			ok = p.at("->")
		})
	})
	p.i, p.tok = i, p.toks[i]
	return ok
}

func (p *parser) ifExpr() *If {
	x := &If{Pos: p.want("if")}
	x.Cond = p.header()
	x.Then = p.body()
	if p.at(";") && p.peek(1).text == "else" {
		p.next()
	}
	if p.at("else") && p.peek(1).text != "->" {
		p.next()
		x.Else = p.body()
	}
	return x
}

func (p *parser) when() *When {
	w := &When{Pos: p.want("when")}
	if p.at("(") {
		if p.peek(1).text == "val" {
			p.fail(p.peek(1).pos, "syntax", "a when subject is an expression; declare the val before the when")
		}
		w.Subject = p.header()
	}
	p.want("{")
	p.nested(false, func() {
		for p.got(";"); !p.at("}"); {
			b := &Branch{Pos: p.tok.pos}
			if n := len(w.Branches); n > 0 && w.Branches[n-1].Conds == nil {
				p.fail(w.Branches[n-1].Pos, "when-branch", "else is a when's last branch, as kotlinc has it: move it to the end")
			}
			if p.got("else") {
				p.want("->")
			} else {
				p.nested(true, func() {
					if p.at("->") {
						p.fail(p.tok.pos, "when-branch", "a branch has a condition before ->, or is else")
					}
					for !p.at("->") {
						c := &Cond{Pos: p.tok.pos}
						p.refuseNot()
						switch {
						case p.got("is"):
							c.Op, c.Type = "is", p.typ()
						case p.got("in"):
							c.Op, c.X = "in", p.expr()
						default:
							c.X = p.expr()
						}
						if x, ok := c.X.(*Binary); ok && w.Subject != nil && (x.Op == "&&" || x.Op == "||") {
							p.fail(x.Pos, "when-branch", "kotlinc refuses %s in a branch of a when with a subject, where it compares the subject with a Boolean: drop the subject and write each test whole, when { ... }", x.Op)
						}
						b.Conds = append(b.Conds, c)
						if !p.got(",") {
							break
						}
					}
					p.want("->")
				})
			}
			b.Body = p.body()
			w.Branches = append(w.Branches, b)
			p.end()
		}
		if w.Branches == nil {
			p.fail(w.Pos, "when-branch", "a when has at least one branch: write one, or remove the when")
		}
		p.next()
	})
	return w
}
