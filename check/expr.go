package check

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Cloud-Limo/kogo/syntax"
)

// facts are what is known of names at some place: the type each is
// narrowed to (spec.md section 4).
type facts map[*Var]Type

// A scope is the names a block or a declaration brings in, and what
// is known inside it.
type scope struct {
	parent     *scope
	names      map[string]Object // a *Var, or the *Func of a local fun
	typeParams map[string]*TypeParam
	known      facts
}

func (c *checker) push(known facts) { c.scope = &scope{parent: c.scope, known: maps.Clone(known)} }
func (c *checker) pop()             { c.scope = c.scope.parent }

// open begins a scope that holds a declaration's type parameters.
func (c *checker) open(tps []*TypeParam) {
	c.push(nil)
	c.scope.typeParams = map[string]*TypeParam{}
	for _, p := range tps {
		c.scope.typeParams[p.Name] = p
	}
}

func (c *checker) define(at syntax.Node, o Object) {
	name := ""
	switch o := o.(type) {
	case *Var:
		name = o.Name
	case *Func:
		name = o.Name
	}
	if c.scope.names == nil {
		c.scope.names = map[string]Object{}
	}
	if c.scope.names[name] != nil {
		c.errorf(at, "duplicate", "%s is already declared in this block: rename one", name)
	}
	c.scope.names[name] = o
}

// local declares a name of a body: info.Vars lists it under at.
func (c *checker) local(at syntax.Node, name string, t Type, mutable bool) {
	v := &Var{Kind: Local, Name: name, Type: t, Mutable: mutable, Decl: at}
	c.info.Vars[at] = append(c.info.Vars[at], v)
	c.define(at, v)
}

// assume records facts that hold for the rest of the block.
func (c *checker) assume(known facts) {
	if c.scope.known == nil {
		c.scope.known = facts{}
	}
	maps.Copy(c.scope.known, known)
}

func (s *scope) lookup(name string) Object {
	for ; s != nil; s = s.parent {
		if o := s.names[name]; o != nil {
			return o
		}
	}
	return nil
}

func (s *scope) typeParam(name string) *TypeParam {
	for ; s != nil; s = s.parent {
		if p := s.typeParams[name]; p != nil {
			return p
		}
	}
	return nil
}

// typeOf gives a name's type where the checker is: narrowed, when a
// test has shown more than its declaration says.
func (c *checker) typeOf(v *Var) Type {
	for s := c.scope; s != nil; s = s.parent {
		if t := s.known[v]; t != nil {
			return t
		}
	}
	return v.Type
}

// this gives the type of this, or nil where there is none.
func (c *checker) this() Type {
	if v, ok := c.scope.lookup("this").(*Var); ok {
		return c.typeOf(v)
	}
	return nil
}

// expr checks an expression that is a value and gives its type. want
// is the type the result must have, where something states one: it
// settles a literal and a call's type arguments (spec.md 3 and 5.1).
// The caller checks that the type fits.
func (c *checker) expr(e syntax.Expr, want Type) Type {
	defer func(b, bl bool) { c.buried, c.buriedLoop = b, bl }(c.buried, c.buriedLoop)
	if c.straight != e {
		c.buried, c.buriedLoop = true, true
	}
	t := c.expr1(e, want)
	c.info.Types[e] = t
	c.read(c.info.Uses[e])
	if call := c.info.Calls[e]; call != nil && call.Func != nil {
		c.read(call.Func)
		if call.Func.Kind == Method {
			c.readOverride(call)
		}
	}
	return t
}

// readOverride records that a call of an interface's fun on a data
// class or a data object refers to that one's override: in Go it is a
// call on the struct, which Go follows, not through the interface.
func (c *checker) readOverride(call *Call) {
	rt := c.this()
	if call.Receiver != nil {
		rt = c.info.Types[call.Receiver]
	}
	n, ok := nonNull(rt).(*Named)
	if !ok || n.Decl.Kind != "data class" && n.Decl.Kind != "data object" {
		return
	}
	for _, m := range n.Decl.Methods {
		if m.Name == call.Func.Name {
			c.read(m)
		}
	}
}

// read records that the val or the fun at hand refers to o, where o
// is a top-level val or a fun (spec.md 6).
func (c *checker) read(o Object) {
	v, isVar := o.(*Var)
	_, isFunc := o.(*Func)
	if c.owner != nil && (isVar && v.Kind == Global || isFunc) && !slices.Contains(c.reads[c.owner], o) {
		c.reads[c.owner] = append(c.reads[c.owner], o)
	}
}

// value checks an expression against the type it must have.
func (c *checker) value(e syntax.Expr, want Type) Type {
	t := c.expr(e, want)
	if want != nil && !fits(t, want) {
		c.mismatch(e, t, want)
	}
	return t
}

func number(t Type) bool { return whole(t) || is(t, tDouble) }

// literal types a whole-number literal written at, minus being "-" when
// one is written before it. Kotlin takes the two together, so that
// -2147483648 is an Int, but checks a Long's digits alone (spec.md 3).
func (c *checker) literal(at syntax.Node, l *syntax.Literal, minus string, want Type) Type {
	t := tInt
	if l.Kind == "Long" || is(nonNull(want), tLong) {
		t = tLong
	}
	n, err := strconv.ParseInt(minus+l.Value, 10, 64)
	switch {
	case err != nil || minus != "" && n == math.MinInt64:
		c.errorf(at, "number-range", "%s%s is past a Long's range, as in Kotlin (spec.md 3): a whole number's digits are at most %d, even after a minus", minus, l.Value, math.MaxInt64)
		return unknown // its one error
	case is(t, tInt) && (n < math.MinInt32 || n > math.MaxInt32):
		c.errorf(at, "number-range", "%s%s is past an Int's range, where Kotlin reads it as a Long (spec.md 3): write %s%sL", minus, l.Value, minus, l.Value)
		return unknown
	}
	return t
}

// first gives the node an expression's text begins with: an operator
// and a call are positioned further in.
func first(at syntax.Node) syntax.Node {
	switch e := at.(type) {
	case *syntax.Call:
		return first(e.Fun)
	case *syntax.Select:
		return first(e.X)
	case *syntax.Binary:
		return first(e.X)
	case *syntax.Index:
		return first(e.X)
	case *syntax.Is:
		return first(e.X)
	}
	return at
}

func (c *checker) mismatch(at syntax.Node, got, want Type) {
	at = first(at)
	fix := ""
	switch {
	case fits(nonNull(got), want):
		fix = ": it may be null, so test it first, or give the value to use instead with ?:"
	case number(got) && number(want):
		fix = ": write .to" + want.String() + "()"
	}
	g, w := got.String(), want.String()
	if gd, wd := named(got), named(want); g == w && gd != wd && gd != nil && wd != nil {
		g, w = gd.Pkg+"."+g, wd.Pkg+"."+w // two types of one name
	}
	c.errorf(at, "type-mismatch", "this is a %s where a %s is wanted%s", g, w, fix)
}

func (c *checker) expr1(e syntax.Expr, want Type) Type {
	switch e := e.(type) {
	case *syntax.Literal:
		switch e.Kind {
		case "Int", "Long":
			return c.literal(e, e, "", want)
		case "Double":
			return tDouble
		case "Char":
			return tChar
		case "Boolean":
			return tBoolean
		case "Null":
			return tNull
		}
		return tString
	case *syntax.StringLit:
		for _, p := range e.Parts {
			if t := c.expr(p, nil); t != nil && named(nonNull(t)) == c.prelude("ByteArray") {
				c.errorf(p, "bytes-text", "a %s in a template is written as Kotlin writes its identity, [B@ and a number that changes every run: write a name or a size kept beside it, or what the bytes were made from", t)
			}
		}
		return tString
	case *syntax.SQL: // the parser reads one only as the argument of the five calls
		return c.errorf(e, "sql-placement", "a sql literal is only the argument of tx.query<R>, queryOne<R>, querySingle<R>, exists or execute")
	case *syntax.ClassRef: // the parser reads one only in @file:OptIn(...)
		return c.errorf(e, "class-ref", "%s::class is written only in @file:OptIn(...)", e.Name)
	case *syntax.Ident:
		return c.ident(e)
	case *syntax.Select:
		return c.sel(e)
	case *syntax.Unary:
		if e.Op == "!" {
			c.value(e.X, tBoolean)
			return tBoolean
		}
		if l, ok := e.X.(*syntax.Literal); ok && (l.Kind == "Int" || l.Kind == "Long") {
			c.info.Types[l] = c.literal(e, l, "-", want)
			return c.info.Types[l]
		}
		t := c.expr(e.X, want)
		if t != unknown && !number(t) {
			return c.errorf(e, "type-mismatch", "- takes a number, and this is a %s", t)
		}
		return t
	case *syntax.Binary:
		return c.binary(e, want)
	case *syntax.Is:
		c.variant(e.Type, c.expr(e.X, nil))
		return tBoolean
	case *syntax.Call:
		return c.call(e, want)
	case *syntax.Index:
		return c.operator(e, "get", e.X, c.expr(e.X, nil), e.Index, want)
	case *syntax.Lambda:
		// Here a lambda is a value, not an argument that a call runs.
		var ft *FuncType
		if f, ok := nonNull(want).(*FuncType); ok {
			ft = c.lambda(e, f.Params, f.Result)
		} else {
			ft = c.lambda(e, make([]Type, len(e.Params)), nil)
		}
		if err := c.info.Fails[e]; err != nil {
			c.errorf(e, "raise-escape", "this lambda fails into the raise scope around it, and is kept as a value, which could run outside the scope: pass it straight to the call that runs it, or make it return an Either<%s, A>", err)
		}
		return ft
	case *syntax.If:
		return c.ifExpr(e, want, true)
	case *syntax.When:
		return c.when(e, want, true)
	case *syntax.FuncRef:
		var found []*Func
		for _, f := range c.funcs(e.Name) {
			if f.Kind == Fun && f.Receiver == nil && f.TypeParams == nil {
				found = append(found, f)
			}
		}
		if len(found) != 1 {
			return c.errorf(e, "unknown-name", "::%s needs one top-level fun %s with no receiver and no type parameters, and %d are in scope", e.Name, e.Name, len(found))
		}
		c.info.Uses[e] = found[0]
		c.demand(e, e.Name, found[0].vis)
		if found[0].Action {
			c.errorf(e, "action-kept", "::%s names an action fun, and an action is not kept in a value: call %s where it is wanted, in an action fun (spec.md 7.1)", e.Name, e.Name)
		}
		ft := &FuncType{Result: found[0].Result}
		for _, p := range found[0].Params {
			ft.Params = append(ft.Params, p.Type)
		}
		return ft
	case *syntax.Jump:
		switch {
		case e.Op == "return" && c.inLambda:
			c.errorf(e, "kotlin-lambda-return", "a lambda holds no return (spec.md 6): end the lambda with its value, and return after the call that runs it")
			if e.Value != nil {
				c.expr(e.Value, nil)
			}
		case e.Op == "return" && c.buried || e.Op != "return" && c.buriedLoop:
			c.errorf(e, "buried-jump", "this %s is buried in a larger expression, where Go has no place for it (spec.md 6): bind the value to a val first, as val v = x ?: %s, and use v", e.Op, e.Op)
			if e.Value != nil {
				c.expr(e.Value, nil)
			}
		case e.Op != "return":
			if c.loops == 0 {
				c.errorf(e, "outside-loop", "%s is written in a for or a while, and not in a lambda there", e.Op)
			}
		case c.result == nil:
			c.errorf(e, "outside-loop", "return is written in a fun")
		case e.Value != nil:
			c.straight = e.Value
			c.value(e.Value, c.result)
		case !fits(tUnit, c.result):
			c.errorf(e, "type-mismatch", "return needs a value here: a %s", c.result)
		}
		return tNothing
	}
	panic("unreachable")
}

// ident reads a name: a local, a member of this, a top-level val, a
// data object or Unit.
func (c *checker) ident(e *syntax.Ident) Type {
	switch o := c.scope.lookup(e.Name).(type) {
	case *Var:
		c.info.Uses[e] = o
		t := c.typeOf(o)
		if t != o.Type {
			c.info.Narrowed[e] = t
		}
		return t
	case *Func:
		return c.errorf(e, "not-a-value", "%s is a local fun: call it, or pass a lambda that does", e.Name)
	}
	if e.Name == "this" {
		return c.errorf(e, "unknown-name", "there is no this here: an extension and a data class's fun have one")
	}
	if this := c.this(); this != nil {
		if t, ok := c.member(e, nil, this, e.Name); ok {
			c.narrowedThis(e)
			return t
		}
	}
	for _, p := range c.homes(e.Name) {
		if v := c.val(p, e.Name); v != nil {
			c.see(e, e.Name, v.vis)
			c.demand(e, e.Name, v.vis)
			c.info.Uses[e] = v
			return v.Type
		}
	}
	d, a := c.findType(e.Name)
	switch {
	case d != nil && (d.Kind == "data object" || d == tUnit.Decl):
		c.see(e, e.Name, d.vis)
		c.info.Uses[e] = d
		return self(d)
	case d != nil || a != nil:
		return c.errorf(e, "not-a-value", "%s is a type, not a value", e.Name)
	}
	return c.missing(e, e.Name)
}

// narrowedThis notes that the name id reads a member of this,
// unwritten, where this is narrowed.
func (c *checker) narrowedThis(id *syntax.Ident) {
	if v, ok := c.scope.lookup("this").(*Var); ok && c.typeOf(v) != v.Type {
		c.info.Narrowed[id] = c.typeOf(v)
	}
}

// member reads the property or the extension val called name of a
// value of type rt. at is the name that reads it and recv the
// receiver, nil when it is this, unwritten.
func (c *checker) member(at, recv syntax.Expr, rt Type, name string) (Type, bool) {
	if n, ok := rt.(*Named); ok {
		for _, v := range n.Decl.Props {
			if v.Name == name {
				c.info.Uses[at] = v
				c.demand(at, name, v.vis)
				return subst(v.Type, bind(n.Decl.Params, n.Args)), true
			}
		}
	}
	var cands []*Func
	for _, f := range c.funcs(name) {
		if _, ok := c.receives(f, rt, true); ok && f.Kind == Getter {
			cands = append(cands, f)
		}
	}
	if cands == nil {
		return nil, false
	}
	t := c.resolve(at, at, name, cands, recv, rt, nil, nil, nil)
	if call := c.info.Calls[at]; call != nil {
		c.info.Uses[at] = call.Func
	}
	return t, true
}

// funcs gives the funs and the extension vals called name that the
// file may name: its own package's, the ones it imports, kogo's, and
// every binding with a receiver.
func (c *checker) funcs(name string) []*Func {
	out := slices.Clone(c.members[name])
	for _, p := range c.homes(name) {
		for _, f := range p.funcs[name] {
			if c.visible(f.vis) {
				out = append(out, f)
			}
		}
	}
	return out
}

// qualifier gives the name x spells when it names a type or an object
// and not a value: Outcome in Outcome.Ok, ZoneId in ZoneId.of.
func (c *checker) qualifier(x syntax.Expr) string {
	name := ""
	switch x := x.(type) {
	case *syntax.Ident:
		if c.scope.lookup(x.Name) != nil {
			return ""
		}
		name = x.Name
	case *syntax.Select:
		q := c.qualifier(x.X)
		if q == "" || x.Safe {
			return ""
		}
		name = q + "." + x.Name
	default:
		return ""
	}
	d, a := c.findType(name)
	if o := c.findObject(name); o != nil {
		c.info.Uses[x] = o
	} else if d != nil && d.Kind != "data object" && d.Kind != "builtin" {
		c.info.Uses[x] = d
	} else if a == nil || d != nil {
		return ""
	}
	return name
}

func (c *checker) findObject(name string) *TypeDecl {
	for _, p := range c.homes(name) {
		if o := p.objects[name]; o != nil {
			return o
		}
	}
	return nil
}

// prelude gives a type of package kogo that the core knows by name:
// List, Set, Map and Pair, and the core's own Either and Raise.
func (c *checker) prelude(name string) *TypeDecl {
	if kogo := c.pkgs["kogo"]; kogo != nil {
		return kogo.types[name]
	}
	return nil
}

// isTx reports whether t is a transaction: a Tx or a SerializableTx.
func (c *checker) isTx(t Type) bool {
	d := named(t)
	return d != nil && (d == c.prelude("Tx") || c.serializable(t))
}

// serializable reports whether t is a SerializableTx.
func (c *checker) serializable(t Type) bool {
	return named(t) != nil && named(t) == c.prelude("SerializableTx")
}

// sel reads x.name or x?.name that is not called.
func (c *checker) sel(e *syntax.Select) Type {
	if q := c.qualifier(e.X); q != "" {
		if d, _ := c.findType(q + "." + e.Name); d != nil {
			if c.see(e, d.Name, d.vis); d.Kind != "data object" {
				return c.errorf(e, "not-a-value", "%s is a type, not a value", d.Name)
			}
			c.info.Uses[e] = d
			return self(d)
		}
		if d, _ := c.findType(q); d != nil && d.Kind == "enum class" {
			var names []string
			for _, v := range d.Entries {
				if names = append(names, v.Name); v.Name == e.Name {
					c.info.Uses[e] = v
					return v.Type
				}
			}
			if list := c.prelude("List"); e.Name == "entries" && list != nil {
				c.info.Uses[e] = &Var{Kind: Property, Name: "entries", Type: &Named{list, []Type{self(d)}}, Owner: d}
				return c.info.Uses[e].(*Var).Type
			}
			return c.errorf(e, "unknown-name", "%s has no constant %s: its constants are %s", q, e.Name, strings.Join(names, ", "))
		}
		return c.errorf(e, "unknown-name", "%s has nothing named %s that is a value", q, e.Name)
	}
	if c.packaged(e) {
		return unknown
	}
	rt := c.expr(e.X, nil)
	if rt == unknown {
		return unknown
	}
	base := rt
	if e.Safe {
		base = nonNull(rt)
	}
	if t, ok := c.member(e, e.X, base, e.Name); ok {
		if e.Safe {
			t = nullable(t)
		}
		return t
	}
	if _, ok := base.(*Nullable); ok {
		return c.errorf(e, "nullable", "this %s may be null: write ?.%s, or test it for null first", rt, e.Name)
	}
	return c.errorf(e, "unknown-name", "a %s has no property %s", base, e.Name)
}

// The bindings that stand behind the operators (spec.md section 6).
var operators = map[string]string{
	"+": "plus", "-": "minus", "*": "times", "/": "div", "%": "rem",
	"<": "compareTo", ">": "compareTo", "<=": "compareTo", ">=": "compareTo", "to": "to",
}

func (c *checker) binary(e *syntax.Binary, want Type) Type {
	switch e.Op {
	case "&&", "||":
		c.value(e.X, tBoolean)
		known, not := c.test(e.X)
		if e.Op == "||" {
			known = not
		}
		c.push(known)
		c.value(e.Y, tBoolean)
		c.pop()
		return tBoolean
	case "?:":
		var xt, yt Type
		if want == nil {
			xt = c.expr(e.X, nil)
			c.straight = e.Y
			// A call on the right with no lambda argument takes the
			// left's type as the type its result must have (spec.md 5.1).
			var hint Type
			if call, ok := e.Y.(*syntax.Call); ok && !slices.ContainsFunc(call.Args, isLambda) &&
				xt != unknown && !is(nonNull(xt), tNothing) {
				hint = nonNull(xt)
			}
			yt = c.expr(e.Y, hint)
		} else {
			if xt = c.expr(e.X, nullable(want)); !fits(nonNull(xt), want) {
				c.mismatch(e.X, nonNull(xt), want)
			}
			c.straight = e.Y
			yt = c.value(e.Y, want)
		}
		return c.common(e, want, nonNull(xt), yt)
	case "==", "!=":
		xt, yt := c.expr(e.X, nil), c.expr(e.Y, nil)
		if x, y := nonNull(xt), nonNull(yt); !fits(x, y) && !fits(y, x) {
			fix := ""
			if whole(x) && whole(y) {
				fix = ": write the literal with L, or .toLong()"
			}
			c.errorf(e, "type-mismatch", "%s compares a %s with a %s, which are never equal%s", e.Op, xt, yt, fix)
		} else if !null(e.X) && !null(e.Y) {
			c.funcEquality(e, e.Op, xt, yt)
		}
		return tBoolean
	case "..", "until":
		c.rangeOf(e)
		return c.errorf(e, "range-not-value", "a range is not a value: write it after in, as x in a..b or for (i in a until b)")
	case "in":
		if r := c.rangeOf(e.Y); r != nil {
			if xt := c.expr(e.X, nil); xt != unknown && r != unknown && !whole(xt) {
				c.errorf(e.X, "type-mismatch", "a range holds Ints or Longs, and this is a %s", xt)
			}
		} else {
			c.operator(e, "contains", e.Y, c.expr(e.Y, nil), e.X, nil)
		}
		return tBoolean
	}
	return c.operate(e, e.Op, e.X, c.expr(e.X, nil), e.Y, want)
}

// operate types x op y, x being read already, for an arithmetic or a
// comparison operator or to. Numbers follow spec.md section 3;
// anything else is a binding's.
func (c *checker) operate(at syntax.Node, op string, x syntax.Expr, xt Type, y syntax.Expr, want Type) Type {
	compare := strings.ContainsAny(op, "<>")
	result := func(t Type) Type {
		if compare && t != unknown {
			return tBoolean
		}
		return t
	}
	if !number(xt) || op == "to" {
		t := c.operator(at, operators[op], x, xt, y, want)
		if compare && t != unknown && !is(t, tInt) {
			return c.errorf(at, "type-mismatch", "compareTo gives an Int, and for a %s it gives a %s", xt, t)
		}
		return result(t)
	}
	switch yt := c.expr(y, nil); {
	case yt == unknown:
		return result(unknown)
	case whole(xt) && whole(yt):
		if is(xt, tLong) || is(yt, tLong) {
			return result(tLong)
		}
		return result(tInt)
	case is(xt, tDouble) && is(yt, tDouble):
		return result(tDouble)
	case number(yt):
		return c.errorf(at, "number-mix", "%s mixes a %s with a %s: write .toDouble() on the whole number, so that the change shows", op, xt, yt)
	default:
		return c.errorf(y, "type-mismatch", "%s takes a number here, and this is a %s", op, yt)
	}
}

// operator types an operator that stands for a binding: plus,
// compareTo, contains, get and to, with x as the receiver and y as
// the argument. Only a binding counts, so that application code
// defines no operator.
func (c *checker) operator(at syntax.Node, name string, x syntax.Expr, xt Type, y syntax.Expr, want Type) Type {
	var cands []*Func
	bare := number(nonNull(xt)) // whether it is defined for the value when that is not null
	for _, f := range c.members[name] {
		if _, ok := c.receives(f, xt, true); ok && f.Kind == Fun {
			cands = append(cands, f)
		} else if _, ok := c.receives(f, nonNull(xt), true); ok {
			bare = true
		}
	}
	switch _, null := xt.(*Nullable); {
	case xt == unknown:
		c.expr(y, nil)
		return unknown
	case cands == nil && null && bare:
		c.expr(y, nil)
		return c.errorf(x, "nullable", "this %s may be null: test it for null first, or give the value to use instead with ?:", xt)
	case cands == nil:
		c.expr(y, nil)
		return c.errorf(at, "no-overload", "this operator is not defined for a %s: no binding declares %s for it", xt, name)
	}
	return c.resolve(at, at, name, cands, x, xt, nil, []*syntax.Arg{{Pos: y.At(), Value: y}}, want)
}

// rangeOf gives the type a range holds, Int or Long, or nil when e is
// not a range. A range is not a value: it is read only after in.
func (c *checker) rangeOf(e syntax.Expr) Type {
	b, ok := e.(*syntax.Binary)
	if !ok || b.Op != ".." && b.Op != "until" {
		return nil
	}
	xt, yt := c.expr(b.X, nil), c.expr(b.Y, nil)
	switch {
	case xt == unknown || yt == unknown:
		return unknown
	case !whole(xt) || !whole(yt):
		return c.errorf(b, "type-mismatch", "a range runs between Ints or Longs, and these are a %s and a %s", xt, yt)
	case is(xt, tLong) || is(yt, tLong):
		return tLong
	}
	return tInt
}

// common gives the type of a value that is one of several: their
// common type, or what is wanted where that is stated and they have
// none (spec.md 5.1).
func (c *checker) common(at syntax.Node, want Type, ts ...Type) Type {
	var all Type = tNothing
	for _, t := range ts {
		so, ok := join(all, t)
		if want != nil && (!ok || !fits(so, want)) {
			return want // each was checked against it
		} else if all, so = so, all; !ok {
			fix := "state the type the result must have"
			if whole(so) && whole(t) {
				fix = "write the whole number as a Long, 0L"
			}
			return c.errorf(at, "no-common-type", "the values here are %s, which have no common type: %s", list("", ts, ""), fix)
		}
	}
	return all
}

// variant reads the type of an is test on a value of type subject:
// a variant of its sealed interface, whose type arguments the subject
// settles.
func (c *checker) variant(t syntax.Type, subject Type) Type {
	r := c.variant1(t, subject)
	c.info.Types[t] = r
	return r
}

func (c *checker) variant1(t syntax.Type, subject Type) Type {
	n, named := t.(*syntax.NamedType)
	sn, _ := nonNull(subject).(*Named)
	switch {
	case subject == unknown:
		return unknown
	case !named || sn == nil || sn.Decl.Kind != "sealed interface":
		return c.errorf(t, "is-variant", "is tests for a variant of a sealed interface, and this value is a %s", subject)
	}
	d, _ := c.findType(n.Name)
	if !slices.Contains(sn.Decl.Variants, d) {
		return c.errorf(t, "is-variant", "%s is not a variant of %s: its variants are %s", n.Name, sn.Decl.Name, strings.Join(cases(sn), ", "))
	}
	if n.Args != nil {
		r := c.typ1(n)
		if !fits(r, sn) {
			c.mismatch(t, r, sn)
		}
		return r
	}
	b := binding{}
	for _, p := range d.Params {
		b[p] = nil
	}
	unify(lift(self(d), sn.Decl), sn, b, func(p *TypeParam, a Type) { b[p] = a })
	for _, p := range d.Params {
		if b[p] == nil {
			return c.errorf(t, "type-arguments", "a %s does not settle %s's type parameter %s: write is %s<...>", subject, d.Name, p.Name, d.Name)
		}
	}
	return subst(self(d), b)
}

// narrowable gives the name that e is, when a test can narrow it: a
// local val or a parameter.
func (c *checker) narrowable(e syntax.Expr) *Var {
	if id, ok := e.(*syntax.Ident); ok {
		if v, ok := c.info.Uses[id].(*Var); ok && (v.Kind == Local || v.Kind == Param) && !v.Mutable {
			return v
		}
	}
	return nil
}

func null(e syntax.Expr) bool {
	l, ok := e.(*syntax.Literal)
	return ok && l.Kind == "Null"
}

// test gives what a condition, already checked, shows of names when
// it holds and when it does not (spec.md section 4).
func (c *checker) test(e syntax.Expr) (holds, not facts) {
	merge := func(a, b facts) facts {
		out := maps.Clone(a)
		if out == nil {
			return b
		}
		maps.Copy(out, b)
		return out
	}
	switch e := e.(type) {
	case *syntax.Unary:
		if e.Op == "!" {
			not, holds = c.test(e.X)
		}
	case *syntax.Is:
		if v := c.narrowable(e.X); v != nil && c.info.Types[e.Type] != unknown {
			holds = facts{v: c.info.Types[e.Type]}
		}
	case *syntax.Binary:
		xh, xn := c.test(e.X)
		yh, yn := c.test(e.Y)
		switch e.Op {
		case "&&":
			holds = merge(xh, yh)
		case "||":
			not = merge(xn, yn)
		case "==", "!=":
			x := e.X
			if null(x) {
				x = e.Y
			} else if !null(e.Y) {
				break
			}
			v := c.narrowable(x)
			if n, ok := c.info.Types[x].(*Nullable); ok && v != nil {
				holds = facts{v: n.Elem}
				if e.Op == "==" {
					holds, not = nil, holds
				}
			}
		}
	}
	return holds, not
}

func (c *checker) ifExpr(e *syntax.If, want Type, used bool) Type {
	c.value(e.Cond, tBoolean)
	holds, not := c.test(e.Cond)
	then := c.block(e.Then, want, used, holds)
	switch {
	case e.Else == nil && used:
		return c.errorf(e, "if-needs-else", "an if that is used as a value needs an else")
	case e.Else == nil:
		if is(then, tNothing) {
			c.assume(not)
		}
		return tUnit
	}
	other := c.block(e.Else, want, used, not)
	switch {
	case used:
		return c.common(e, want, then, other)
	case is(then, tNothing) && is(other, tNothing):
		return tNothing
	case is(then, tNothing):
		c.assume(not)
	case is(other, tNothing):
		c.assume(holds)
	}
	return tUnit
}

// cases gives what a when over a value of type t must cover, or nil
// when t is not a sealed interface, an enum or Boolean.
func cases(t Type) []string {
	var out []string
	n, _ := nonNull(t).(*Named)
	switch {
	case n == nil:
		return nil
	case n.Decl.Kind == "sealed interface":
		for _, v := range n.Decl.Variants {
			out = append(out, v.Name)
		}
	case n.Decl.Kind == "enum class":
		for _, v := range n.Decl.Entries {
			out = append(out, v.Name)
		}
	case n.Decl == tBoolean.Decl:
		out = []string{"true", "false"}
	default:
		return nil
	}
	if t != Type(n) {
		out = append(out, "null")
	}
	return out
}

func (c *checker) when(e *syntax.When, want Type, used bool) Type {
	var st Type
	var subject *Var
	if e.Subject != nil {
		st, subject = c.expr(e.Subject, nil), c.narrowable(e.Subject)
	}
	c.push(nil) // holds what the branches passed have ruled out
	defer c.pop()
	covered, total := map[string]bool{}, false
	types := []Type{}
	for _, b := range e.Branches {
		var holds, not facts
		for _, cond := range b.Conds {
			holds, not = c.cond(cond, st, subject, covered)
		}
		if len(b.Conds) != 1 {
			holds, not = nil, nil
		}
		total = total || b.Conds == nil
		types = append(types, c.block(b.Body, want, used, holds))
		c.assume(not)
	}
	need := cases(st)
	need = slices.DeleteFunc(need, func(n string) bool { return covered[n] })
	switch {
	case total || st == unknown:
	case e.Subject == nil:
		c.errorf(e, "when-needs-else", "a when with no subject needs an else branch")
	case cases(st) == nil && used:
		c.errorf(e, "when-needs-else", "a when over a %s that is used as a value needs an else branch", st)
	case len(need) > 0:
		c.errorf(e, "when-not-exhaustive", "this when over a %s does not cover %s: add a branch for each, or an else", st, strings.Join(need, ", "))
	default:
		total = cases(st) != nil
	}
	if used {
		return c.common(e, want, types...)
	}
	if total && !slices.ContainsFunc(types, func(t Type) bool { return !is(t, tNothing) }) {
		return tNothing
	}
	return tUnit
}

// cond checks one condition of a when's branch, notes what it covers,
// and gives what it shows of the subject when it is a name.
func (c *checker) cond(cond *syntax.Cond, st Type, subject *Var, covered map[string]bool) (holds, not facts) {
	switch {
	case st == nil:
		c.value(cond.X, tBoolean)
		return c.test(cond.X)
	case cond.Op == "is":
		if n, ok := c.variant(cond.Type, st).(*Named); ok {
			if covered[n.Decl.Name] = true; subject != nil {
				holds = facts{subject: n}
			}
		}
	case cond.Op == "in":
		if r := c.rangeOf(cond.X); r == nil {
			c.expr(cond.X, nil)
			c.errorf(cond, "range-not-value", "a when's in takes a range: in a..b")
		} else if st != unknown && r != unknown && !whole(st) {
			c.errorf(cond, "type-mismatch", "a range holds Ints or Longs, and the when's subject is a %s", st)
		}
	default:
		ct := c.expr(cond.X, nil)
		if x, y := nonNull(st), nonNull(ct); !fits(x, y) && !fits(y, x) {
			c.errorf(cond, "type-mismatch", "this %s is never equal to the when's subject, a %s", ct, st)
		} else if !null(cond.X) {
			c.funcEquality(cond, "this when", st, ct)
		}
		switch o := c.info.Uses[cond.X].(type) {
		case *TypeDecl:
			covered[o.Name] = true
		case *Var:
			covered[o.Name] = covered[o.Name] || o.Kind == Entry
		}
		if l, ok := cond.X.(*syntax.Literal); ok {
			covered[l.Value] = covered[l.Value] || l.Kind == "Boolean" || l.Kind == "Null"
			if n, ok := st.(*Nullable); ok && l.Kind == "Null" && subject != nil {
				not = facts{subject: n.Elem}
			}
		}
	}
	return holds, not
}

// lambda checks a lambda whose parameters have the types given; one
// that is nil is settled by nothing, so the lambda must state it. want
// is the type its result must have, when that is known.
func (c *checker) lambda(l *syntax.Lambda, params []Type, want Type) *FuncType {
	c.push(nil)
	c.lambdas = append(c.lambdas, l)
	defer func() { c.pop(); c.lambdas = c.lambdas[:len(c.lambdas)-1] }()
	ft := &FuncType{Params: slices.Clone(params)}
	unsettled := func(at syntax.Node, name string) Type {
		return c.errorf(at, "lambda-parameters", "nothing settles the type of %s before this lambda is read: state it, as { %s: Type -> ... }", name, name)
	}
	given := len(l.Params) == len(params)
	switch {
	case len(l.Params) == 0 && len(params) == 1:
		if params[0] == nil {
			ft.Params[0] = unsettled(l, "it")
		}
		c.local(l, "it", ft.Params[0], false)
	case !given:
		c.errorf(l, "lambda-parameters", "this lambda names %d parameters where %d are passed to it", len(l.Params), len(params))
		params = make([]Type, len(l.Params))
		for i := range max(len(params), len(ft.Params)) {
			if i < len(params) {
				params[i] = unknown
			}
			if i < len(ft.Params) {
				ft.Params[i] = unknown
			}
		}
	}
	for i, p := range l.Params {
		t := params[i]
		if p.Type != nil {
			stated := c.typ(p.Type)
			if t != nil && !fits(t, stated) {
				c.errorf(p.Type, "type-mismatch", "this parameter is passed a %s, which is not a %s", t, stated)
			} else if given {
				ft.Params[i] = stated
			}
			t = stated
		} else if t == nil {
			t = unsettled(p, cmp.Or(p.Name, "this parameter"))
			ft.Params[i] = t
		}
		if p.Destructure != nil {
			c.destructure(p, p.Destructure, t)
		} else {
			c.local(p, p.Name, t, false)
		}
	}
	defer func(n int, b, bl, il bool) { c.loops, c.buried, c.buriedLoop, c.inLambda = n, b, bl, il }(c.loops, c.buried, c.buriedLoop, c.inLambda)
	c.loops, c.buried, c.buriedLoop, c.inLambda = 0, false, false, true
	if slices.ContainsFunc(ft.Params, func(t Type) bool { return t != nil && c.isTx(nonNull(t)) }) {
		c.inTx++
		defer func() { c.inTx-- }()
	}
	if ft.Result = c.block(l.Body, want, true, nil); want != nil {
		ft.Result = want
	}
	c.info.Types[l] = ft
	return ft
}

// funcEquality refuses comparing two values, of types x and y, where
// either holds a function (spec.md 3). op says what compares them.
func (c *checker) funcEquality(at syntax.Node, op string, x, y Type) {
	for _, t := range []Type{x, y} {
		where := holdsFunc(t, nil, map[*TypeDecl]bool{})
		if where == "" {
			// Nor is a ByteArray, which Kotlin compares by identity.
			bytes := c.prelude("ByteArray")
			if where = holdsFunc(t, bytes, map[*TypeDecl]bool{}); where != "" {
				held := ", and " + where + " holds a ByteArray"
				if where == "it" && named(nonNull(t)) == bytes {
					held = ""
				} else if where == "it" {
					held = ", which holds a ByteArray"
				}
				c.errorf(at, "bytes-equality", "%s compares a %s%s, which Kotlin compares by identity, not by its bytes: compare the other properties, or what the bytes were made from", op, t, held)
				return
			}
			continue
		}
		msg := "%s compares a %s, and " + where + " holds a function, which is never compared: compare the other properties"
		if _, fn := nonNull(t).(*FuncType); fn && where == "it" {
			msg = "%s compares a %s, a function, which is never compared: compare a name kept beside it"
		} else if where == "it" {
			msg = "%s compares a %s, which holds a function, which is never compared: compare a name kept beside each function"
		}
		c.errorf(at, "function-equality", msg, op, t)
		return
	}
}

// holdsFunc names where a value of t holds a function (spec.md 3): a
// declaration's property, "it" for t or a type argument itself, or ""
// for nowhere; or, given the ByteArray's declaration, where it holds
// one of those instead. A declaration is read unsubstituted, once, and its type
// arguments apart, so a type that refers to itself ends.
func holdsFunc(t Type, bytes *TypeDecl, seen map[*TypeDecl]bool) string {
	switch t := t.(type) {
	case *FuncType:
		if bytes == nil {
			return "it"
		}
	case *Nullable:
		return holdsFunc(t.Elem, bytes, seen)
	case *Named:
		if bytes != nil && t.Decl == bytes {
			return "it"
		}
		for _, a := range t.Args {
			if where := holdsFunc(a, bytes, seen); where != "" {
				return where
			}
		}
		if seen[t.Decl] {
			return ""
		}
		seen[t.Decl] = true
		for _, v := range t.Decl.Variants {
			if where := holdsFunc(&Named{Decl: v}, bytes, seen); where != "" {
				return where
			}
		}
		for _, p := range t.Decl.Props {
			if where := holdsFunc(p.Type, bytes, seen); where == "it" {
				return t.Decl.Name + "'s property " + p.Name
			} else if where != "" {
				return where
			}
		}
	}
	return ""
}

// isLambda reports whether a is a lambda argument.
func isLambda(a *syntax.Arg) bool {
	_, ok := a.Value.(*syntax.Lambda)
	return ok
}
