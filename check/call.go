package check

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/Cloud-Limo/kogo/syntax"
)

// call checks a call: of a value of a function type, of a local fun,
// or of the one function its name and arguments choose.
func (c *checker) call(e *syntax.Call, want Type) Type {
	var (
		recv  syntax.Expr
		rt    Type // the receiver's type: recv's, or this's when none is written
		cands []*Func
		name  string
		safe  bool
	)
	switch fun := e.Fun.(type) {
	case *syntax.Ident:
		name = fun.Name
		switch o := c.scope.lookup(name).(type) {
		case *Var:
			return c.callValue(e, c.expr(fun, nil))
		case *Func:
			cands = []*Func{o}
		default:
			if rt = c.this(); rt != nil {
				cands = c.methods(rt, name)
			}
			// this's interface fun comes before an extension, but in a
			// raise scope not before the core's forms (spec.md 9).
			member := cands != nil
			for _, f := range c.funcs(name) {
				if f.Kind == Fun && !c.otherTx(f) && (!member || f.Receiver == nil || c.form(f)) {
					cands = append(cands, f)
				}
			}
			if d, _ := c.findType(name); d != nil && d.Ctor != nil && c.visible(d.Ctor.vis) {
				cands = append(cands, d.Ctor)
			}
		}
	case *syntax.Select:
		if len(e.Args) == 1 {
			if lit, ok := e.Args[0].Value.(*syntax.SQL); ok {
				return c.sql(e, lit)
			}
		}
		name = fun.Name
		if q := c.qualifier(fun.X); q != "" {
			if d, _ := c.findType(q + "." + name); d != nil && d.Ctor != nil && c.visible(d.Ctor.vis) {
				cands = []*Func{d.Ctor}
			} else if d != nil && d.Ctor != nil {
				c.loose(e.Args, nil)
				if outer, _ := c.findType(q); outer == c.prelude("Either") {
					return c.errorf(fun, "either-constructor", "an Either is built with e.left() or a.right(), not with its constructor")
				}
				c.see(fun, q+"."+name+"'s constructor", d.Ctor.vis)
				return unknown
			} else if o := c.findObject(q); o != nil {
				cands = slices.DeleteFunc(slices.Clone(o.Methods), func(f *Func) bool { return f.Name != name })
			}
			if len(cands) == 0 {
				c.loose(e.Args, nil)
				return c.errorf(fun, "unknown-name", "%s has no function %s", q, name)
			}
			break
		}
		recv, safe = fun.X, fun.Safe
		if rt = c.expr(recv, nil); rt == unknown {
			c.loose(e.Args, nil)
			return unknown
		}
		if safe {
			rt = nonNull(rt)
		}
		cands = c.methods(rt, name)
		// In Kotlin, this inside either { } is the Raise, and
		// this.raise(...) the scope's form (spec.md 9).
		if x, ok := recv.(*syntax.Ident); ok && x.Name == "this" && cands != nil && slices.ContainsFunc(c.funcs(name), c.form) {
			c.loose(e.Args, nil)
			return c.errorf(fun, "ambiguous-call", "in Kotlin this.%s inside either { } is the raise scope's %s, not %s's: rename %s's %s", name, name, rt, rt, name)
		}
		if cands == nil {
			for _, f := range c.funcs(name) {
				if f.Kind == Fun && f.Receiver != nil && !c.otherTx(f) {
					cands = append(cands, f)
				}
			}
		}
	default:
		return c.callValue(e, c.expr(e.Fun, nil))
	}
	switch {
	case len(cands) > 0:
	case !c.holds(rt, recv != nil, name):
		c.loose(e.Args, nil)
		return c.noFunc(e.Fun, name, rt, recv != nil)
	case recv == nil:
		return c.callValue(e, c.expr(e.Fun, nil)) // a val of a function type
	default:
		// A property of a function type: its receiver is read already.
		t, _ := c.member(e.Fun, recv, rt, name)
		if c.info.Types[e.Fun] = t; safe {
			c.info.Types[e.Fun] = nullable(t)
			return nullable(c.callValue(e, t))
		}
		return c.callValue(e, t)
	}
	t := c.resolve(e, e.Fun, name, cands, recv, rt, e.TypeArgs, e.Args, want)
	if call := c.info.Calls[e]; call != nil {
		c.info.Uses[e.Fun] = call.Func
		c.fails(e, call)
		c.zoneOffset(e, call.Func)
		if id, ok := e.Fun.(*syntax.Ident); ok && call.Func.Receiver != nil && c.raised(call.Func.Receiver) == nil {
			c.narrowedThis(id)
		}
	}
	if safe {
		t = nullable(t)
	}
	return t
}

// zoneOffset refuses kogo.time's ZoneId.of on a string literal that
// is an offset ID, outside Kogo's subset (spec.md 12); any other
// string is refused when the program runs.
func (c *checker) zoneOffset(e *syntax.Call, f *Func) {
	if f.Pkg != "kogo.time" || f.Owner == nil || f.Owner.Name != "ZoneId" || f.Name != "of" || len(e.Args) != 1 {
		return
	}
	if s, ok := e.Args[0].Value.(*syntax.StringLit); ok && len(s.Parts) == 1 {
		if l, ok := s.Parts[0].(*syntax.Literal); ok && offsetID.MatchString(l.Value) {
			c.errorf(s, "zone-offset", "ZoneId.of takes a region name, such as Australia/Melbourne; offsets are not supported (spec 12)")
		}
	}
}

// offsetID is java.time's rule for an ID that ZoneId.of reads as an
// offset: Z, a sign, or UTC, GMT or UT and then a sign.
var offsetID = regexp.MustCompile(`^(Z$|[+-]|(UTC|GMT|UT)[+-])`)

// holds reports whether name is a val: a property of a value of type
// rt, or when the call names no receiver, a top-level val.
func (c *checker) holds(rt Type, written bool, name string) bool {
	if n, ok := rt.(*Named); ok && slices.ContainsFunc(n.Decl.Props, func(v *Var) bool { return v.Name == name }) {
		return true
	}
	return !written && slices.ContainsFunc(c.homes(name), func(p *pkg) bool { return p.vals[name] != nil })
}

// noFunc reports a call of a name that no function in scope has.
func (c *checker) noFunc(at syntax.Node, name string, rt Type, member bool) Type {
	for _, p := range c.homes(name) {
		for _, f := range p.funcs[name] {
			if !c.visible(f.vis) {
				c.see(at, name, f.vis)
				return unknown
			}
		}
	}
	d, _ := c.findType(name)
	switch {
	case member && notKogo[name][0] == "":
		return c.errorf(at, "unknown-name", "a %s has no function %s", rt, name)
	case member || d == nil:
		return c.missing(at, name)
	case d.Ctor != nil:
		c.see(at, name+"'s constructor", d.Ctor.vis)
		return unknown
	}
	return c.errorf(at, "not-callable", "%s is a type with no constructor: only a data class is made by calling its name", name)
}

// methods gives the members called name of a value of type rt: its
// interfaces' funs, and a data class's copy and toString.
func (c *checker) methods(rt Type, name string) []*Func {
	n, ok := rt.(*Named)
	if !ok {
		return nil
	}
	var out []*Func
	decls := []*TypeDecl{n.Decl}
	for _, s := range n.Decl.Supers {
		decls = append(decls, s.Decl)
	}
	for _, d := range decls {
		for _, f := range d.Methods {
			if f.Kind == Method && f.Name == name {
				out = append(out, f)
			}
		}
	}
	if d := n.Decl; out == nil && d.Kind == "data class" && (name == "copy" || name == "toString") {
		if d.copy == nil {
			f := Func{TypeParams: d.Params, Receiver: self(d), Owner: d, vis: d.vis, owners: len(d.Params)}
			d.copy, d.toString = &f, &Func{}
			*d.toString = f
			d.copy.vis = d.Ctor.vis // copy builds a value, as the constructor does
			d.copy.Kind, d.copy.Name, d.copy.Params, d.copy.Result = Copy, "copy", d.Props, self(d)
			d.toString.Kind, d.toString.Name, d.toString.Result = ToString, "toString", tString
		}
		out = []*Func{d.copy}
		if name == "toString" {
			out = []*Func{d.toString}
		}
	}
	return out
}

// callValue checks a call of a value of a function type.
func (c *checker) callValue(e *syntax.Call, t Type) Type {
	ft, ok := t.(*FuncType)
	named := slices.ContainsFunc(e.Args, func(a *syntax.Arg) bool { return a.Name != "" })
	switch {
	case t == unknown:
		c.loose(e.Args, nil)
		return unknown
	case !ok:
		c.loose(e.Args, nil)
		return c.errorf(e.Fun, "not-callable", "this is a %s, which is not a function and cannot be called", t)
	case len(e.Args) != len(ft.Params) || named || e.TypeArgs != nil:
		c.loose(e.Args, nil)
		return c.errorf(e.Fun, "arguments", "this %s is called with its %d arguments in order, with no names and no type arguments", ft, len(ft.Params))
	}
	call := &Call{}
	for i, a := range e.Args {
		c.value(a.Value, ft.Params[i])
		call.Args = append(call.Args, []syntax.Expr{a.Value})
	}
	c.info.Calls[e] = call
	return ft.Result
}

// loose checks the arguments of a call that failed, each on its own,
// leaving out those in done.
func (c *checker) loose(args []*syntax.Arg, done map[syntax.Expr]Type) {
	for _, a := range args {
		if _, ok := done[a.Value]; ok {
			continue
		}
		if l, ok := a.Value.(*syntax.Lambda); ok {
			params := make([]Type, max(len(l.Params), 1))
			for i := range params {
				params[i] = unknown
			}
			outer := c.raise // it may be a raise scope's block: say no more
			c.raise = unknown
			c.lambda(l, params, nil)
			c.raise = outer
		} else if t := c.expr(a.Value, nil); done != nil {
			done[a.Value] = t
		}
	}
}

// A candidate is a function that a call may mean, the type parameters
// settled for it so far, and what fills each of its parameters.
type candidate struct {
	f      *Func
	b      binding
	placed [][]syntax.Expr
}

// resolve chooses among the functions called name the one that a call
// means (spec.md 5.2), settles its type arguments (5.1) and gives the
// call's type. at is the call, where is its name, recv its receiver
// when one is written, and rt the type it is called on, or nil.
func (c *checker) resolve(at, where syntax.Node, name string, cands []*Func, recv syntax.Expr, rt Type, typeArgs []syntax.Type, args []*syntax.Arg, want Type) Type {
	// Keep the candidates whose receiver fits, and the count and the
	// names of whose arguments fit.
	var left []candidate
	why, bare := "", false // why one dropped out; whether one would fit the receiver were it not null
	for _, f := range cands {
		b, ok := c.receives(f, rt, recv != nil)
		if _, null := rt.(*Nullable); !ok && null && !bare {
			_, bare = c.receives(f, nonNull(rt), recv != nil)
		}
		if !ok && rt == nil {
			why = "is called on a value"
			continue
		} else if !ok {
			why = fmt.Sprintf("is not called on a %s", rt)
			continue
		}
		placed, fault := place(f, args)
		if fault != "" {
			why = fault
			continue
		}
		left = append(left, candidate{f, b, placed})
	}
	// When several are left, the types of the arguments that are not
	// lambdas choose; when they still do not, what one lambda returns.
	typed := map[syntax.Expr]Type{}
	if len(left) > 1 {
		for _, a := range args {
			if _, lambda := a.Value.(*syntax.Lambda); !lambda {
				typed[a.Value] = c.expr(a.Value, nil)
			}
		}
		left = slices.DeleteFunc(left, func(k candidate) bool { return !c.accepts(k, typed) })
		if len(left) > 1 {
			left = c.byLambda(left, typed)
		}
	}
	sigs := func(fs []*Func) string { // for the errors only
		var out []string
		for _, f := range fs {
			out = append(out, describe(f, nil))
		}
		return strings.Join(out, "; ")
	}
	vague := func(t Type) bool {
		f, ok := t.(*FuncType)
		return t == unknown || ok && f.Result == unknown
	}
	switch {
	case len(left) == 1:
		return c.apply(at, where, left[0], recv, typeArgs, typed, want)
	case len(left) > 1 && slices.ContainsFunc(slices.Collect(maps.Values(typed)), vague):
		c.loose(args, typed)
		return unknown // an argument failed, so nothing chooses: say no more
	case len(left) > 1:
		c.loose(args, typed)
		var fs []*Func
		for _, k := range left {
			fs = append(fs, k.f)
		}
		if slices.ContainsFunc(fs, c.form) && slices.ContainsFunc(fs, func(f *Func) bool { return f.Kind == Method }) {
			return c.errorf(where, "ambiguous-call", "both the raise scope's %s and %s's fit this call, and nothing ranks them: rename %s's %s", name, rt, rt, name)
		}
		return c.errorf(where, "ambiguous-call", "%d declarations of %s fit this call and nothing ranks them: rename one of %s", len(left), name, sigs(fs))
	case bare:
		c.loose(args, typed)
		return c.errorf(where, "nullable", "this %s may be null: write ?.%s, or test it for null first", rt, name)
	case len(cands) == 1:
		c.loose(args, typed)
		return c.errorf(where, "arguments", "%s %s: it is %s", name, why, sigs(cands))
	}
	c.loose(args, typed)
	var types []string
	for _, a := range args {
		if _, lambda := a.Value.(*syntax.Lambda); !lambda {
			types = append(types, typed[a.Value].String())
		} else if got, ok := typed[a.Value].(*FuncType); ok {
			types = append(types, "a lambda giving "+got.Result.String())
		} else {
			types = append(types, "a lambda")
		}
	}
	return c.errorf(where, "no-overload", "no %s fits this call with (%s): the candidates are %s", name, strings.Join(types, ", "), sigs(cands))
}

// receives reports whether f can be called on a receiver of type rt,
// nil when there is none, and settles the type parameters that the
// receiver mentions (spec.md 5.1, rule 1).
func (c *checker) receives(f *Func, rt Type, written bool) (binding, bool) {
	b := binding{}
	for _, p := range f.TypeParams {
		b[p] = nil
	}
	if e := c.either(c.result); e != nil && c.txRaise(f) {
		b[f.TypeParams[0]] = e // the enclosing fun's (spec.md 10.7)
	}
	switch {
	case f.Receiver == nil:
		return b, !written
	case f.Receiver == unknown: // its declaration failed: say no more
		return b, true
	case !written && c.raised(f.Receiver) != nil:
		// Called on the raise scope around the call; fails checks it.
		if c.raise != nil {
			unify(c.raised(f.Receiver), c.raise, b, func(p *TypeParam, t Type) { b[p] = t })
		}
		return b, true
	case rt == nil:
		return b, false
	}
	unify(f.Receiver, rt, b, func(p *TypeParam, t Type) { b[p] = t })
	return b, fits(rt, subst(f.Receiver, b))
}

func vararg(p *Var) bool {
	d, ok := p.Decl.(*syntax.Param)
	return ok && d.Vararg
}

// place gives what fills each parameter of f, or says why the count
// or the names of the arguments do not fit it.
func place(f *Func, args []*syntax.Arg) ([][]syntax.Expr, string) {
	placed := make([][]syntax.Expr, len(f.Params))
	next := 0
	for _, a := range args {
		i := next
		switch {
		case a.Name != "":
			if i = slices.IndexFunc(f.Params, func(p *Var) bool { return p.Name == a.Name }); i < 0 {
				return nil, "has no parameter named " + a.Name
			}
		case a.Trailing:
			i = len(f.Params) - 1
		case i < len(f.Params) && !vararg(f.Params[i]):
			next++
		}
		if i < 0 || i >= len(f.Params) {
			return nil, fmt.Sprintf("takes %d arguments, and %d are written", len(f.Params), len(args))
		}
		if placed[i] != nil && !vararg(f.Params[i]) {
			return nil, "is given " + f.Params[i].Name + " twice"
		}
		placed[i] = append(placed[i], a.Value)
	}
	for i, p := range f.Params {
		d, _ := p.Decl.(*syntax.Param)
		if placed[i] == nil && f.Kind != Copy && !vararg(p) && (d == nil || d.Default == nil) {
			return nil, "needs an argument for " + p.Name
		}
	}
	return placed, ""
}

// lambdaParam gives the function type of a parameter that a lambda
// fills, or nil when the argument is not a lambda or the parameter
// not of a function type.
func lambdaParam(p *Var, a syntax.Expr) *FuncType {
	ft, _ := nonNull(p.Type).(*FuncType)
	if _, ok := a.(*syntax.Lambda); !ok {
		return nil
	}
	return ft
}

// settle applies rule 2 of spec.md 5.1: the arguments already read
// settle the type parameters they mention, several by their common
// type. It gives a type parameter whose arguments have none.
func settle(k candidate, typed map[syntax.Expr]Type) (*TypeParam, []Type) {
	found := map[*TypeParam][]Type{}
	for i, p := range k.f.Params {
		for _, a := range k.placed[i] {
			if t, ok := typed[a]; ok && lambdaParam(p, a) == nil {
				unify(p.Type, t, k.b, func(tp *TypeParam, t Type) { found[tp] = append(found[tp], t) })
			}
		}
	}
	for _, tp := range k.f.TypeParams {
		ts := found[tp]
		if ts == nil {
			continue
		}
		all := ts[0]
		for _, t := range ts[1:] {
			var ok bool
			if all, ok = join(all, t); !ok {
				return tp, ts
			}
		}
		k.b[tp] = all
	}
	return nil, nil
}

// wholeLiteral reports whether e is a whole-number literal, which is
// a Long where one is expected (spec.md section 3).
func wholeLiteral(e syntax.Expr) bool {
	if u, ok := e.(*syntax.Unary); ok && u.Op == "-" {
		e = u.X
	}
	l, ok := e.(*syntax.Literal)
	return ok && l.Kind == "Int"
}

// accepts reports whether the arguments read so far fit a candidate:
// a lambda fits any parameter of a function type.
func (c *checker) accepts(k candidate, typed map[syntax.Expr]Type) bool {
	if tp, _ := settle(k, typed); tp != nil {
		return false
	}
	for i, p := range k.f.Params {
		want := subst(p.Type, k.b)
		for _, a := range k.placed[i] {
			t, read := typed[a]
			_, lambda := a.(*syntax.Lambda)
			_, generic := p.Type.(*TypeParam)
			switch {
			case lambda && lambdaParam(p, a) == nil && !generic:
				return false
			case read && !fits(t, want) && !(wholeLiteral(a) && fits(tLong, want)):
				return false
			}
		}
	}
	return true
}

// byLambda chooses among candidates that differ only in what one
// lambda returns, as sumOf's do: it reads the lambda once, and what
// it returns chooses (spec.md 5.2).
func (c *checker) byLambda(left []candidate, typed map[syntax.Expr]Type) []candidate {
	var l *syntax.Lambda
	for _, as := range left[0].placed {
		for _, a := range as {
			if x, ok := a.(*syntax.Lambda); ok && l != nil {
				return left
			} else if ok {
				l = x
			}
		}
	}
	var fts []*FuncType // for each candidate, the type of the parameter that the lambda fills
	for _, k := range left {
		i := slices.IndexFunc(k.placed, func(as []syntax.Expr) bool { return slices.Contains(as, syntax.Expr(l)) })
		if l == nil || lambdaParam(k.f.Params[i], l) == nil {
			return left
		}
		ft := subst(lambdaParam(k.f.Params[i], l), k.b).(*FuncType)
		if open(&FuncType{Params: ft.Params, Result: tUnit}, k.b) || fts != nil && list("", ft.Params, "") != list("", fts[0].Params, "") {
			return left
		}
		fts = append(fts, ft)
	}
	got := c.lambda(l, fts[0].Params, nil)
	typed[l] = got
	var out []candidate
	for i, k := range left {
		if fits(got.Result, fts[i].Result) {
			out = append(out, k)
		}
	}
	return out
}

// apply settles a function's type arguments in the order of spec.md
// 5.1, checks every argument against them, records the call and
// gives its type. typed holds the arguments read while choosing.
func (c *checker) apply(at, where syntax.Node, k candidate, recv syntax.Expr, typeArgs []syntax.Type, typed map[syntax.Expr]Type, want Type) Type {
	f, b := k.f, k.b
	now := func(p *TypeParam, t Type) {
		if b[p] == nil {
			b[p] = t
		}
	}
	if own := f.TypeParams[f.owners:]; typeArgs != nil && len(typeArgs) != len(own) {
		c.errorf(where, "type-arguments", "%s has %d type parameters: write all of its type arguments, or none", f.Name, len(own))
		for _, p := range own {
			b[p] = unknown
		}
	} else if typeArgs != nil {
		for i, p := range own {
			b[p] = c.typ(typeArgs[i])
		}
	}
	// Rule 2: the arguments that are not lambdas.
	for i, p := range f.Params {
		for _, a := range k.placed[i] {
			if _, read := typed[a]; read || lambdaParam(p, a) != nil {
				continue
			}
			var w Type
			if !open(p.Type, b) {
				w = subst(p.Type, b)
			}
			typed[a] = c.expr(a, w)
		}
	}
	if tp, ts := settle(k, typed); tp != nil {
		fix := "there is no Any"
		if slices.ContainsFunc(ts, whole) && !slices.ContainsFunc(ts, func(t Type) bool { return !whole(t) }) {
			fix = "write each whole number as a Long, 1L"
		}
		c.errorf(where, "no-common-type", "the arguments that settle %s are %s, which have no common type: %s", tp.Name, list("", ts, ""), fix)
		b[tp] = unknown
	}
	// Rule 3: the type the result must have.
	if want != nil {
		unify(f.Result, want, b, now)
	}
	// Rule 4: what each lambda returns, left to right. Its own
	// parameters are settled by now, or it states them.
	for i, p := range f.Params {
		for _, a := range k.placed[i] {
			ft := lambdaParam(p, a)
			if ft == nil {
				continue
			}
			got, read := typed[a].(*FuncType)
			if !read {
				params := make([]Type, len(ft.Params))
				for j, pt := range ft.Params {
					if !open(pt, b) {
						params[j] = subst(pt, b)
					}
				}
				var w Type
				if !open(ft.Result, b) {
					w = subst(ft.Result, b)
				}
				if r := c.raised(ft.Receiver); r == nil {
					got = c.lambda(a.(*syntax.Lambda), params, w)
				} else {
					if open(r, b) { // it says what may be raised in the block
						c.errorf(where, "type-arguments", "nothing settles %s's type parameter %s before its block is read: write its type arguments, %s<...> { ... }", f.Name, r, f.Name)
						unify(r, unknown, b, now)
					}
					got = c.raiseBlock(a.(*syntax.Lambda), subst(ft.Receiver, b), params, w)
				}
				typed[a] = got
			}
			for j := range min(len(ft.Params), len(got.Params)) {
				unify(ft.Params[j], got.Params[j], b, now)
			}
			unify(ft.Result, got.Result, b, now)
		}
	}
	for _, p := range f.TypeParams {
		if b[p] == nil {
			c.errorf(where, "type-arguments", "nothing settles %s's type parameter %s: write its type arguments, %s<...>", f.Name, p.Name, f.Name)
			b[p] = unknown
		}
	}
	// Every argument is checked against what was settled.
	call := &Call{Func: f, Receiver: recv, Args: k.placed}
	for i, p := range f.Params {
		pt := subst(p.Type, b)
		for _, a := range k.placed[i] {
			c.kept(f, a)
			c.ran(f, a)
			switch t := typed[a]; {
			case fits(t, pt):
			case wholeLiteral(a) && fits(tLong, pt):
				c.expr(a, tLong)
			default:
				c.mismatch(a, t, pt)
			}
		}
	}
	for _, p := range f.TypeParams {
		call.TypeArgs = append(call.TypeArgs, b[p])
	}
	c.info.Calls[at] = call
	if f.Kind == Copy {
		c.builds(where, f.Owner)
	} else {
		c.demand(where, f.Name, f.vis)
	}
	c.acts(where, f)
	return subst(f.Result, b)
}
