package emit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Cloud-Limo/kogo/check"
	"github.com/Cloud-Limo/kogo/syntax"
)

// value gives the Go for x as a value of the type want.
func (e *emitter) value(x syntax.Expr, want check.Type) string {
	if l, ok := x.(*syntax.Literal); ok && l.Kind == "Null" && want != nil {
		return "(" + e.typ(want) + ")(nil)"
	}
	code := e.expr(x)
	defer func(at syntax.Node) { e.at = at }(e.at)
	e.at = x // where a use that is not emitted yet is reported
	return e.convert(code, e.info.Types[x], want)
}

// expr gives a Go expression for x, of the Go type of x's own type.
// What Go writes as a statement is wrapped in a function called on
// the spot; the checker has refused a jump inside one.
func (e *emitter) expr(x syntax.Expr) string {
	switch x := x.(type) {
	case *syntax.Literal:
		switch x.Kind {
		case "Int", "Long":
			return e.typ(e.info.Types[x]) + "(" + digits(x.Value) + ")"
		case "Double":
			return "float64(" + x.Value + ")"
		case "Char":
			return fmt.Sprintf("%s.Char(%d)", e.rt(), []rune(x.Value)[0])
		case "Boolean":
			return x.Value
		case "Null":
			return "(*" + e.rt() + ".Nothing)(nil)"
		}
		return strconv.Quote(x.Value)
	case *syntax.StringLit:
		var parts []string
		for _, p := range x.Parts {
			if code := e.expr(p); builtin(e.info.Types[p], "String") {
				parts = append(parts, code)
			} else {
				parts = append(parts, e.rt()+".ToString("+code+")")
			}
		}
		if parts == nil {
			return `""`
		}
		return "(" + strings.Join(parts, " + ") + ")"
	case *syntax.Ident:
		return e.ident(x)
	case *syntax.Select:
		if x.Safe {
			return e.wrap(x)
		}
		code, _ := e.sel(x, nil, nil)
		return code
	case *syntax.Unary:
		if l, ok := x.X.(*syntax.Literal); ok && x.Op == "-" && (l.Kind == "Int" || l.Kind == "Long") {
			// One literal, so that -2147483648 is an Int.
			return e.typ(e.info.Types[x]) + "(-" + digits(l.Value) + ")"
		}
		code := e.expr(x.X)
		if x.Op == "-" && constant(x.X) {
			// Go's constants have no -0.0, and -(-2147483648) overflows.
			code = e.rt() + ".Val(" + code + ")"
		}
		return "(" + x.Op + code + ")"
	case *syntax.Binary:
		return e.binary(x)
	case *syntax.Is:
		return e.is(e.expr(x.X), e.info.Types[x.X], e.info.Types[x.Type])
	case *syntax.Call:
		if sel, ok := x.Fun.(*syntax.Select); ok && sel.Safe {
			return e.wrap(x)
		}
		code, _ := e.callExpr(x, nil, nil)
		return code
	case *syntax.Index:
		code, _ := e.call(x, e.info.Calls[x], nil, nil)
		return code
	case *syntax.Lambda:
		return e.lambda(x)
	case *syntax.If, *syntax.When:
		return e.wrap(x)
	case *syntax.FuncRef:
		f := e.info.Uses[x].(*check.Func)
		if d, ok := f.Decl.(*syntax.FunDecl); ok && slices.ContainsFunc(d.Params, func(p *syntax.Param) bool { return p.Default != nil }) {
			e.notYet(x, "a reference to a fun with a default value", "pass a lambda that calls it")
		}
		return e.qualify(f.Pkg, e.names[f])
	}
	e.notYet(x, "this expression", "bind it to a val first")
	return "nil"
}

// digits gives a whole number's digits as Go reads them: Go reads
// 010 as eight.
func digits(v string) string {
	if n, err := strconv.ParseUint(v, 10, 64); err == nil {
		return strconv.FormatUint(n, 10)
	}
	return v
}

// constant reports whether Go reads the Go of x as a constant: a
// number literal, or a whole one with a minus. Go works out arithmetic
// on two constants exactly and refuses a result outside the type,
// where Kotlin wraps an Int or a Long and gives a Double Infinity.
func constant(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Literal:
		return x.Kind == "Int" || x.Kind == "Long" || x.Kind == "Double"
	case *syntax.Unary:
		l, ok := x.X.(*syntax.Literal)
		return ok && x.Op == "-" && (l.Kind == "Int" || l.Kind == "Long")
	}
	return false
}

// wrap gives x, which Go writes as statements, as an expression: a
// function called on the spot, whose branches return.
func (e *emitter) wrap(x syntax.Expr) string {
	t := e.info.Types[x]
	body := e.inner(func() {
		e.into(x, dest{ret, "", t})
		e.w(`panic("unreachable")`)
	})
	return "func() " + e.typ(t) + " {\n" + body + e.dir() + "}()"
}

// ident reads a name: a local, a member of this, a top-level val, a
// data object or Unit.
func (e *emitter) ident(x *syntax.Ident) string {
	if c := e.info.Calls[x]; c != nil { // an extension val of this
		code, _ := e.call(x, c, nil, nil)
		return code
	}
	switch o := e.info.Uses[x].(type) {
	case *check.Var:
		switch o.Kind {
		case check.Property:
			code, _ := e.thisAt(x)
			return e.prop(code, o)
		case check.Global:
			return e.qualify(o.Pkg, e.names[o])
		}
		code := e.fn.names[o]
		if code == "" {
			code = "this_"
		}
		if o.Mutable {
			// Go orders calls left to right, and not the read of a
			// variable beside a call that changes it.
			code = e.rt() + ".Val(" + code + ")"
		}
		if t := e.info.Narrowed[x]; t != nil {
			code = e.narrow(code, o.Type, t)
		}
		return code
	case *check.TypeDecl:
		return e.typ(&check.Named{Decl: o}) + "{}"
	}
	panic("emit: " + x.Name + " refers to nothing")
}

// thisAt gives this, and its type, where the name id reads a member
// of it unwritten.
func (e *emitter) thisAt(id *syntax.Ident) (string, check.Type) {
	if t := e.info.Narrowed[id]; t != nil {
		return e.narrow("this_", e.this, t), t
	}
	return "this_", e.this
}

// prop reads a property of the Go value code.
func (e *emitter) prop(code string, v *check.Var) string {
	switch v.Owner.Kind {
	case "enum class": // its name
		return "string(" + code + ")"
	case "interface", "sealed interface":
		return code + ".Get" + export(v.Name) + "()"
	}
	return code + "." + export(v.Name)
}

// sel reads x.name, and gives its type. recv, when set, stands for
// x: the receiver of a ?. once it is known not to be null.
func (e *emitter) sel(x *syntax.Select, recv *operand, rt check.Type) (string, check.Type) {
	if c := e.info.Calls[x]; c != nil { // an extension val
		return e.call(x, c, recv, rt)
	}
	switch o := e.info.Uses[x].(type) {
	case *check.TypeDecl: // a data object
		return e.typ(&check.Named{Decl: o}) + "{}", e.info.Types[x]
	case *check.Var:
		switch {
		case o.Kind == check.Entry:
			return e.qualify(o.Owner.Pkg, e.names[o]), o.Type
		case o.Decl == nil && o.Name == "entries":
			return e.entries(o.Owner), o.Type
		}
		if recv == nil {
			recv, rt = &operand{code: e.expr(x.X)}, e.info.Types[x.X]
		}
		return e.prop(recv.code, o), check.Subst(o.Type, o.Owner.Params, rt.(*check.Named).Args)
	}
	panic("emit: ." + x.Name + " refers to nothing")
}

// lambda writes a lambda as a Go function value. The block of a raise
// scope, whose receiver is Raise<E>, takes the scope's handle first;
// the lambdas inside it capture the handle.
func (e *emitter) lambda(l *syntax.Lambda) string {
	ft := e.info.Types[l].(*check.FuncType)
	var params []string
	defer func(r *raise) { e.raise = r }(e.raise)
	if ft.Receiver != nil {
		e.raise = &raise{e.local(ft, "raise"), ft.Receiver.(*check.Named)}
		params = append(params, e.raise.handle+" "+e.typ(ft.Receiver))
	}
	var destructured []func()
	if len(l.Params) == 0 && len(ft.Params) == 1 {
		params = append(params, e.local(e.info.Vars[l][0], "it")+" "+e.typ(ft.Params[0]))
	}
	for i, p := range l.Params {
		if vars := e.info.Vars[p]; p.Destructure == nil {
			params = append(params, e.local(vars[0], p.Name)+" "+e.typ(ft.Params[i]))
		} else {
			t := e.temp()
			params = append(params, t+" "+e.typ(ft.Params[i]))
			destructured = append(destructured, func() { e.parts(vars, t, ft.Params[i]) })
		}
	}
	body := e.inner(func() {
		for _, declare := range destructured {
			declare()
		}
		e.block(l.Body, dest{ret, "", ft.Result})
		e.end(ft.Result)
	})
	return "func(" + strings.Join(params, ", ") + ") " + e.typ(ft.Result) + " {\n" + body + e.dir() + "}"
}

// plain reports whether Go's == compares values of the type t as
// Kogo's does: a number, a String, a Boolean, a Char or an enum.
func plain(t check.Type) bool {
	n, ok := t.(*check.Named)
	return ok && (n.Decl.Kind == "enum class" || n.Decl.Kind == "builtin" && n.Decl.Name != "Unit" && n.Decl.Name != "Nothing")
}

// double gives t's Double when t is Double or Double?, and nil when
// it is neither.
func double(t check.Type) check.Type {
	if n, ok := t.(*check.Nullable); ok {
		t = n.Elem
	}
	if builtin(t, "Double") {
		return t
	}
	return nil
}

// equals gives a == y, a being Go of the type at. Two values that are
// each a Double or a Double? are compared by IEEE's ==, as Kotlin
// compares them, once neither is null. Anything else but two plain
// values is compared by rt's walk: Go compares a pointer by its
// address, and slices not at all.
func (e *emitter) equals(a string, at check.Type, y syntax.Expr) string {
	_, null := at.(*check.Nullable)
	if l, ok := y.(*syntax.Literal); ok && l.Kind == "Null" && null {
		return "(" + a + " == nil)"
	}
	b, bt := e.expr(y), e.info.Types[y]
	switch {
	case plain(at) && e.same(at, bt):
		return "(" + a + " == " + b + ")"
	case double(at) != nil && double(bt) != nil:
		opt := &check.Nullable{Elem: double(at)}
		return e.rt() + ".EqualDouble(" + e.convert(a, at, opt) + ", " + e.convert(b, bt, opt) + ")"
	}
	return e.rt() + ".Equal(" + a + ", " + b + ")"
}

// is gives the test that code, of the type t, is the variant v.
func (e *emitter) is(code string, t, v check.Type) string {
	if n, ok := t.(*check.Nullable); ok {
		return fmt.Sprintf("%s.IsNullable[%s, %s](%s)", e.rt(), e.typ(v), e.typ(n.Elem), code)
	}
	return fmt.Sprintf("%s.Is[%s](%s)", e.rt(), e.typ(v), code)
}

// inRange gives code in a..b or code in a until b. It is a call, so
// that both ends are read, in order, as Kotlin reads them.
func (e *emitter) inRange(code string, r *syntax.Binary) string {
	fn := map[string]string{"..": "In", "until": "InUntil"}[r.Op]
	return fmt.Sprintf("%s.%s(int64(%s), int64(%s), int64(%s))", e.rt(), fn, code, e.expr(r.X), e.expr(r.Y))
}

// num gives a whole number as the type to: an Int beside a Long is
// a Long.
func (e *emitter) num(code string, from, to check.Type) string {
	if builtin(from, "Int") && builtin(to, "Long") {
		return "int64(" + code + ")"
	}
	return code
}

// arith gives a op b for two numbers (spec.md section 3). Division is
// a call, so that a division by zero panics in source order. A Double
// product is converted, which stops Go fusing it with an addition into
// one instruction that rounds once: Kotlin rounds each.
func (e *emitter) arith(op, a string, at check.Type, b string, bt check.Type) string {
	t := at
	if builtin(bt, "Long") {
		t = bt
	}
	a, b = e.num(a, at, t), e.num(b, bt, t)
	switch {
	case op == "/":
		return e.rt() + ".Div(" + a + ", " + b + ")"
	case op == "%" && builtin(t, "Double"):
		return e.rt() + ".Mod(" + a + ", " + b + ")"
	case op == "%":
		return e.rt() + ".Rem(" + a + ", " + b + ")"
	case op == "*" && builtin(t, "Double"):
		return "float64(" + a + " * " + b + ")"
	}
	return "(" + a + " " + op + " " + b + ")"
}

func (e *emitter) binary(x *syntax.Binary) string {
	switch x.Op {
	case "&&", "||":
		return "(" + e.expr(x.X) + " " + x.Op + " " + e.expr(x.Y) + ")"
	case "?:":
		return e.wrap(x)
	case "==", "!=":
		a, b := x.X, x.Y
		if l, ok := a.(*syntax.Literal); ok && l.Kind == "Null" {
			a, b = b, a
		}
		code := e.equals(e.expr(a), e.info.Types[a], b)
		if x.Op == "!=" {
			code = "(!" + code + ")"
		}
		return code
	case "in":
		if r, ok := x.Y.(*syntax.Binary); ok && (r.Op == ".." || r.Op == "until") {
			return e.inRange(e.expr(x.X), r)
		}
	}
	if c := e.info.Calls[x]; c != nil { // an operator that is a binding
		code, _ := e.call(x, c, nil, nil)
		if strings.ContainsAny(x.Op, "<>") {
			code = "(" + code + " " + x.Op + " 0)"
		}
		return code
	}
	a := e.expr(x.X)
	if constant(x.X) && constant(x.Y) { // then Go would work it out
		a = e.rt() + ".Val(" + a + ")"
	}
	return e.arith(x.Op, a, e.info.Types[x.X], e.expr(x.Y), e.info.Types[x.Y])
}

// An operand of a call: its receiver or an argument.
type operand struct {
	x    syntax.Expr // nil for a receiver already read, and for a default
	code string
	late bool // a constructor's default: read after the arguments written
}

// ordered gives the Go that build makes of the operands, read in the
// order they are written. Go reads a call's operands in the order of
// their slots, which named arguments and x in c do not follow: then
// each is read into a temporary first.
func (e *emitter) ordered(ops []operand, result check.Type, build func(codes []string) string) string {
	var codes []string
	var written []int
	last, late, inOrder := -1, false, true
	for i, o := range ops {
		codes = append(codes, o.code)
		if _, lambda := o.x.(*syntax.Lambda); o.late {
			late = true
		} else if o.x != nil && !lambda {
			at := first(o.x).Offset
			inOrder = inOrder && !late && at > last
			last = max(last, at)
			written = append(written, i)
		}
	}
	if inOrder {
		return build(codes)
	}
	slices.SortFunc(written, func(i, j int) int { return first(ops[i].x).Offset - first(ops[j].x).Offset })
	body := e.inner(func() {
		for _, i := range written {
			t := e.temp()
			e.w("%s := %s", t, codes[i])
			codes[i] = t
		}
		e.w("return %s", build(codes))
	})
	return "func() " + e.typ(result) + " {\n" + body + e.dir() + "}()"
}

// callExpr writes a call, traced to its own line when a statement
// runs over several.
func (e *emitter) callExpr(x *syntax.Call, recv *operand, rt check.Type) (string, check.Type) {
	code, t := e.call(x, e.info.Calls[x], recv, rt)
	if line := x.At().Line; line != e.line {
		code = fmt.Sprintf("/*line %s:%d*/%s", e.path, line, code)
	}
	return code, t
}

// call writes a call as the checker settled it, and gives the type
// of its value: at is the call, or the name or the operator that
// stands for one. recv, when set, stands for the receiver, of type
// rt: that of a ?. once it is known not to be null.
func (e *emitter) call(at syntax.Node, c *check.Call, recv *operand, rt check.Type) (string, check.Type) {
	if c == nil { // one of the five calls of spec.md 10.2, which the checker settles apart, on a Tx that is not null
		return e.sql(at.(*syntax.Call)), e.info.Types[at]
	}
	f := c.Func
	// bind, raise, ensure and ensureNotNull are given the handle of
	// the raise scope they are in, and fail with its E (prelude/kogo).
	form, bind := e.info.Fails[at] != nil, ""
	if form && f.Name != "bind" { // called on the scope, in place of this
		args := slices.Clone(c.TypeArgs)
		args[0] = e.raise.typ.Args[0]
		c = &check.Call{Func: f, TypeArgs: args, Args: c.Args}
		recv, rt = &operand{code: e.raise.handle}, e.raise.typ
	}
	var ops []operand
	if f == nil { // a value of a function type is called
		x := at.(*syntax.Call)
		ft, ok := e.info.Types[x.Fun].(*check.FuncType)
		if !ok || recv != nil {
			e.notYet(at, "a ?. call of a property that holds a function", "bind the property to a val and test it for null")
			return "nil", e.info.Types[x]
		}
		ops = append(ops, operand{x: x.Fun, code: e.expr(x.Fun)})
		for i, as := range c.Args {
			ops = append(ops, operand{x: as[0], code: e.value(as[0], ft.Params[i])})
		}
		return e.ordered(ops, ft.Result, func(codes []string) string {
			return codes[0] + "(" + strings.Join(codes[1:], ", ") + ")"
		}), ft.Result
	}
	e.instantiate(at, f, c.TypeArgs)
	sub := func(t check.Type) check.Type { return check.Subst(t, f.TypeParams, c.TypeArgs) }
	if f.Receiver != nil {
		switch {
		case recv != nil:
		case c.Receiver != nil:
			recv, rt = &operand{x: c.Receiver, code: e.expr(c.Receiver)}, e.info.Types[c.Receiver]
		default: // this, unwritten: at is the name, or the call of it
			id, _ := at.(*syntax.Ident)
			if x, ok := at.(*syntax.Call); ok {
				id = x.Fun.(*syntax.Ident)
			}
			code, t := e.thisAt(id)
			recv, rt = &operand{code: code}, t
		}
		if f.Kind == check.Fun || f.Kind == check.Getter {
			recv.code = e.convert(recv.code, rt, sub(f.Receiver))
		}
		ops = append(ops, *recv)
	}
	if f.Kind == check.Copy {
		return e.copied(c, ops[0].code, rt), rt
	}
	for i, p := range f.Params {
		pd, _ := p.Decl.(*syntax.Param)
		pt := sub(p.Type)
		for _, a := range c.Args[i] {
			code := e.value(a, pt)
			if pd != nil && pd.Default != nil && f.Kind != check.Constructor {
				code = fmt.Sprintf("%s.Ptr[%s](%s)", e.rt(), e.typ(pt), code)
			}
			ops = append(ops, operand{x: a, code: code})
		}
		switch {
		case len(c.Args[i]) > 0 || pd != nil && pd.Vararg:
		case f.Kind == check.Constructor:
			ops = append(ops, operand{code: e.atCall(f.TypeParams, c.TypeArgs, func() string { return e.value(pd.Default, pt) }), late: true})
		default: // the fun gives its parameter the default
			ops = append(ops, operand{code: "nil"})
		}
	}
	if form && f.Name == "bind" {
		scopeErr := e.raise.typ.Args[0]
		bind = ", " + e.typ(scopeErr)
		ops = append(ops, operand{code: e.raise.handle}, operand{code: e.widener(c.TypeArgs[0], scopeErr)})
	}
	result := sub(f.Result)
	build := func(codes []string) string {
		name := e.fn.names[f] // a local fun
		if name == "" {
			name = e.qualify(f.Pkg, e.names[f])
			if len(c.TypeArgs) > 0 {
				name += "[" + e.typs(c.TypeArgs) + bind + "]"
			}
		}
		return name + "(" + strings.Join(codes, ", ") + ")"
	}
	switch f.Kind {
	case check.Constructor:
		result = &check.Named{Decl: f.Owner, Args: c.TypeArgs}
		build = func(codes []string) string {
			for i, p := range f.Params {
				codes[i] = export(p.Name) + ": " + codes[i]
			}
			return e.typ(result) + "{" + strings.Join(codes, ", ") + "}"
		}
	case check.ToString:
		build = func(codes []string) string { return e.rt() + ".ToString(" + codes[0] + ")" }
	case check.Method:
		build = func(codes []string) string {
			return codes[0] + "." + export(f.Name) + "(" + strings.Join(codes[1:], ", ") + ")"
		}
	}
	return e.ordered(ops, result, build), result
}

// atCall gives the Go that write makes of a declaration's own code
// written at a call of it, such as a constructor's default: each of
// its type parameters names the Go type of the call's type argument.
func (e *emitter) atCall(tps []*check.TypeParam, args []check.Type, write func() string) string {
	for i, tp := range tps {
		name, ok := e.fn.names[tp]
		e.fn.names[tp] = e.typ(args[i])
		defer func() {
			if ok {
				e.fn.names[tp] = name
			} else {
				delete(e.fn.names, tp)
			}
		}()
	}
	return write()
}

// copied writes x.copy(...): the value, with each property that the
// call names given its new value, in the order written.
func (e *emitter) copied(c *check.Call, recv string, t check.Type) string {
	n := t.(*check.Named)
	tmp := e.temp()
	var args []syntax.Expr
	field := map[syntax.Expr]*check.Var{}
	for i, p := range c.Func.Params {
		for _, a := range c.Args[i] {
			args = append(args, a)
			field[a] = p
		}
	}
	slices.SortFunc(args, func(a, b syntax.Expr) int { return first(a).Offset - first(b).Offset })
	body := e.inner(func() {
		e.w("%s := %s", tmp, recv)
		for _, a := range args {
			e.w("%s.%s = %s", tmp, export(field[a].Name), e.value(a, check.Subst(field[a].Type, n.Decl.Params, n.Args)))
		}
		e.w("return %s", tmp)
	})
	return "func() " + e.typ(t) + " {\n" + body + e.dir() + "}()"
}
