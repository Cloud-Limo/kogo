package check

import "strings"

// A Type is a *Named, a *Nullable, a *FuncType or a *TypeParam.
type Type interface{ String() string }

// A Named is a declared type with its type arguments, one for each
// of its parameters: Int, List<T>, Outcome.Ok<Int>. A typealias is
// already replaced by what it names.
type Named struct {
	Decl *TypeDecl
	Args []Type
}

// A Nullable is T?. Its Elem is never a Nullable; null itself is
// Nothing?.
type Nullable struct{ Elem Type }

// A FuncType is (A, B) -> R.
type FuncType struct {
	Params []Type
	Result Type
}

// A TypeParam is a type parameter of a declaration, read inside it.
type TypeParam struct {
	Name string
	Out  bool
}

// unknown is the type of an expression that failed to check. It fits
// anything, so that one error does not cause others.
type unknownType struct{}

var unknown Type = unknownType{}

func (unknownType) String() string  { return "?" }
func (t *TypeParam) String() string { return t.Name }
func (t *Named) String() string     { return t.Decl.Name + list("<", t.Args, ">") }
func (t *FuncType) String() string  { return "(" + list("", t.Params, "") + ") -> " + t.Result.String() }

func (t *Nullable) String() string {
	if f, ok := t.Elem.(*FuncType); ok {
		return "(" + f.String() + ")?"
	}
	return t.Elem.String() + "?"
}

// list prints ts between open and close, or nothing when there are
// none and open is set.
func list[T interface{ String() string }](open string, ts []T, close string) string {
	if len(ts) == 0 && open != "" {
		return ""
	}
	var parts []string
	for _, t := range ts {
		parts = append(parts, t.String())
	}
	return open + strings.Join(parts, ", ") + close
}

var builtins = map[string]*TypeDecl{}

func builtin(name string) *Named {
	d := &TypeDecl{Kind: "builtin", Name: name, vis: vis{Pkg: "kogo"}}
	builtins[name] = d
	return &Named{Decl: d}
}

var (
	tBoolean = builtin("Boolean")
	tInt     = builtin("Int")
	tLong    = builtin("Long")
	tDouble  = builtin("Double")
	tString  = builtin("String")
	tChar    = builtin("Char")
	tUnit    = builtin("Unit")
	tNothing = builtin("Nothing")
	tNull    = &Nullable{tNothing}
)

func is(t Type, n *Named) bool {
	m, ok := t.(*Named)
	return ok && m.Decl == n.Decl
}

// whole reports whether t is Int or Long.
func whole(t Type) bool { return is(t, tInt) || is(t, tLong) }

func nullable(t Type) Type {
	if _, ok := t.(*Nullable); ok || t == unknown {
		return t
	}
	return &Nullable{t}
}

func nonNull(t Type) Type {
	if n, ok := t.(*Nullable); ok {
		return n.Elem
	}
	return t
}

// fits reports whether a value of type got may stand where want is
// wanted (spec.md 5.1).
func fits(got, want Type) bool {
	if got == unknown || want == unknown || is(got, tNothing) {
		return true
	}
	if w, ok := want.(*Nullable); ok {
		return fits(nonNull(got), w.Elem)
	}
	switch g := got.(type) {
	case *Named:
		w, ok := want.(*Named)
		if !ok {
			return false
		}
		if g = lift(g, w.Decl); g == nil {
			return false
		}
		for i, p := range w.Decl.Params {
			if !fits(g.Args[i], w.Args[i]) || !p.Out && !fits(w.Args[i], g.Args[i]) {
				return false
			}
		}
		return true
	case *FuncType:
		w, ok := want.(*FuncType)
		if !ok || len(g.Params) != len(w.Params) || !fits(g.Result, w.Result) {
			return false
		}
		for i := range g.Params {
			if !fits(w.Params[i], g.Params[i]) {
				return false
			}
		}
		return true
	}
	return got == want
}

// lift gives n as the type target that it implements, or nil: an
// Outcome.Ok<Int> as an Outcome<Int>.
func lift(n *Named, target *TypeDecl) *Named {
	if n.Decl == target {
		return n
	}
	for _, s := range n.Decl.Supers {
		if r := lift(subst(s, bind(n.Decl.Params, n.Args)).(*Named), target); r != nil {
			return r
		}
	}
	return nil
}

// join gives the common type of a and b (spec.md 5.1): the wider when
// one fits the other, and the sealed interface of two of its variants.
func join(a, b Type) (Type, bool) {
	switch {
	case fits(a, b):
		return b, true
	case fits(b, a):
		return a, true
	}
	if an, bn := nonNull(a), nonNull(b); an != a || bn != b {
		t, ok := join(an, bn)
		return nullable(t), ok
	}
	x, _ := a.(*Named)
	y, _ := b.(*Named)
	if x == nil || y == nil {
		return nil, false
	}
supers:
	for _, s := range x.Decl.Supers {
		sx, sy := lift(x, s.Decl), lift(y, s.Decl)
		if s.Decl.Kind != "sealed interface" || sy == nil {
			continue
		}
		args := make([]Type, len(sx.Args))
		for i, p := range s.Decl.Params {
			var ok bool
			if args[i], ok = join(sx.Args[i], sy.Args[i]); !ok || !p.Out && !fits(args[i], sx.Args[i]) {
				continue supers
			}
		}
		return &Named{s.Decl, args}, true
	}
	return nil, false
}

// A binding gives the type that stands for each type parameter. In a
// call, a parameter not yet settled is there with no type.
type binding map[*TypeParam]Type

func bind(params []*TypeParam, args []Type) binding {
	b := binding{}
	for i, p := range params {
		b[p] = args[i]
	}
	return b
}

func subst(t Type, b binding) Type {
	switch t := t.(type) {
	case *TypeParam:
		if r := b[t]; r != nil {
			return r
		}
	case *Nullable:
		return nullable(subst(t.Elem, b))
	case *Named:
		if len(t.Args) > 0 && len(b) > 0 {
			return &Named{t.Decl, substAll(t.Args, b)}
		}
	case *FuncType:
		return &FuncType{substAll(t.Params, b), subst(t.Result, b)}
	}
	return t
}

func substAll(ts []Type, b binding) []Type {
	out := make([]Type, len(ts))
	for i, t := range ts {
		out[i] = subst(t, b)
	}
	return out
}

// open reports whether t mentions a type parameter that b has not
// settled.
func open(t Type, b binding) bool {
	found := false
	unify(t, t, b, func(*TypeParam, Type) { found = true })
	return found
}

// unify matches the pattern p against the type a, and calls found for
// each type parameter of p that b has not settled, with the type that
// a has in its place.
func unify(p, a Type, b binding, found func(*TypeParam, Type)) {
	if a == unknown { // what failed settles them as unknown, to say no more
		if p != unknown {
			unify(p, p, b, func(tp *TypeParam, _ Type) { found(tp, unknown) })
		}
		return
	}
	switch p := p.(type) {
	case *TypeParam:
		if t, mine := b[p]; mine && t == nil {
			found(p, a)
		}
	case *Nullable:
		unify(p.Elem, nonNull(a), b, found)
	case *Named:
		an, ok := nonNull(a).(*Named)
		if !ok {
			return
		}
		if l := lift(an, p.Decl); l != nil {
			an = l
		} else if l := lift(p, an.Decl); l != nil {
			p = l
		} else {
			return
		}
		for i := range p.Args {
			unify(p.Args[i], an.Args[i], b, found)
		}
	case *FuncType:
		if af, ok := nonNull(a).(*FuncType); ok && len(af.Params) == len(p.Params) {
			for i := range p.Params {
				unify(p.Params[i], af.Params[i], b, found)
			}
			unify(p.Result, af.Result, b, found)
		}
	}
}
