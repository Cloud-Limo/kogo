package emit

import (
	"fmt"
	"maps"
	"strings"

	"kogo/check"
	"kogo/syntax"
)

// The Go type of each builtin type that Go has; the others are rt's.
var goTypes = map[string]string{"Boolean": "bool", "Int": "int32", "Long": "int64", "Double": "float64", "String": "string"}

// builtin reports whether t is the builtin type called name.
func builtin(t check.Type, name string) bool {
	n, ok := t.(*check.Named)
	return ok && n.Decl.Kind == "builtin" && n.Decl.Name == name
}

// typ gives the Go type of a Kogo type (doc/design.md, "Values"): T?
// is a pointer for every T, a data class its struct, a sealed
// interface and an interface a Go interface, an enum its named string
// and a type that Go implements the prelude's Go type of its name.
func (e *emitter) typ(t check.Type) string {
	switch t := t.(type) {
	case *check.Nullable:
		if tp, ok := t.Elem.(*check.TypeParam); ok {
			e.nullable[tp] = true
		}
		return "*" + e.typ(t.Elem)
	case *check.TypeParam:
		return e.fn.names[t]
	case *check.FuncType:
		return "func(" + e.typs(t.Params) + ") " + e.typ(t.Result)
	case *check.Named:
		if t.Decl.Kind == "builtin" {
			if g := goTypes[t.Decl.Name]; g != "" {
				return g
			}
			return e.rt() + "." + t.Decl.Name
		}
		for i, a := range t.Args {
			if _, ok := a.(*check.Nullable); ok {
				e.insts = append(e.insts, inst{t.Decl.Params[i], e.file, e.at})
			}
		}
		s := e.qualify(t.Decl.Pkg, e.names[t.Decl])
		if len(t.Args) > 0 {
			s += "[" + e.typs(t.Args) + "]"
		}
		return s
	}
	panic(fmt.Sprintf("emit: no Go type for %v", t))
}

func (e *emitter) typs(ts []check.Type) string {
	var out []string
	for _, t := range ts {
		out = append(out, e.typ(t))
	}
	return strings.Join(out, ", ")
}

// typeParams declares a fun's or a type's type parameters, and gives
// them as Go declares them and as a use of the type names them.
func (e *emitter) typeParams(tps []*check.TypeParam) (decl, use string) {
	var ds, us []string
	for _, tp := range tps {
		us = append(us, e.local(tp, tp.Name))
		ds = append(ds, us[len(us)-1]+" any")
	}
	if tps == nil {
		return "", ""
	}
	return "[" + strings.Join(ds, ", ") + "]", "[" + strings.Join(us, ", ") + "]"
}

// instantiate notes a use of a declaration with type arguments. One
// settled as Nothing is refused: a sealed value is used as one of a
// wider type argument for free only because no value's Go type holds
// Nothing where its declaration holds a type parameter.
func (e *emitter) instantiate(at syntax.Node, f *check.Func, args []check.Type) {
	for i, a := range args {
		if builtin(a, "Nothing") {
			e.notYet(at, fmt.Sprintf("the type argument Nothing, settled here for %s", f.TypeParams[i].Name), "write the type arguments, "+f.Name+"<...>(...)")
		}
		if _, ok := a.(*check.Nullable); ok {
			e.insts = append(e.insts, inst{f.TypeParams[i], e.file, at})
		}
	}
	if !e.scanned[f] {
		e.scanned[f] = true
		e.scan(f.Receiver, f.Result)
		for _, p := range f.Params {
			e.scan(p.Type)
		}
	}
}

// scan notes each type parameter that types use as T?.
func (e *emitter) scan(ts ...check.Type) {
	for _, t := range ts {
		switch t := t.(type) {
		case *check.Nullable:
			if tp, ok := t.Elem.(*check.TypeParam); ok {
				e.nullable[tp] = true
			}
			e.scan(t.Elem)
		case *check.Named:
			e.scan(t.Args...)
		case *check.FuncType:
			e.scan(t.Receiver, t.Result)
			e.scan(t.Params...)
		}
	}
}

// same reports whether two types are one Go type. It writes both
// only to compare them, so the file imports nothing for it.
func (e *emitter) same(a, b check.Type) bool {
	defer func(imports map[string]string, n int) { e.imports, e.insts = imports, e.insts[:n] }(e.imports, len(e.insts))
	e.imports = maps.Clone(e.imports)
	return e.typ(a) == e.typ(b)
}

// convert gives code, a Go expression of the type from, as the wider
// type to that it is used as (spec.md 5.1). A T as a T? takes an
// address; a data class as its interface is Go's own conversion; a
// list is copied, since Go has no wider type for a slice; a function
// is wrapped.
func (e *emitter) convert(code string, from, to check.Type) string {
	if from == nil || to == nil || e.same(from, to) {
		return code
	}
	if builtin(from, "Nothing") {
		return fmt.Sprintf("%s.Never[%s](%s)", e.rt(), e.typ(to), code)
	}
	fn, fromNull := from.(*check.Nullable)
	if tn, ok := to.(*check.Nullable); ok {
		switch {
		case !fromNull:
			return fmt.Sprintf("%s.Ptr[%s](%s)", e.rt(), e.typ(tn.Elem), e.convert(code, from, tn.Elem))
		case builtin(fn.Elem, "Nothing"):
			return fmt.Sprintf("%s.Null[%s](%s)", e.rt(), e.typ(tn.Elem), code)
		}
		return fmt.Sprintf("%s.Widen(%s, %s)", e.rt(), code, e.widener(fn.Elem, tn.Elem))
	}
	switch f := from.(type) {
	case *check.Named:
		t := to.(*check.Named)
		if t.Decl.Kind == "external class" {
			// The prelude's Name_widen takes a function for each out
			// parameter: List_widen(xs, f), Pair_widen(p, f, g).
			var fs []string
			for i, p := range t.Decl.Params {
				if p.Out {
					fs = append(fs, e.widener(f.Args[i], t.Args[i]))
				}
			}
			return fmt.Sprintf("%s_widen(%s, %s)", e.qualify(t.Decl.Pkg, e.names[t.Decl]), code, strings.Join(fs, ", "))
		}
		// A variant as its sealed interface, or a sealed interface
		// as itself with wider type arguments. Go converts, and the
		// value keeps the Go type it was made with, which an is test
		// must still find: so its type arguments may not change.
		for _, s := range f.Decl.Supers {
			if s.Decl == t.Decl {
				f = check.Subst(s, f.Decl.Params, f.Args).(*check.Named)
			}
		}
		for i := range t.Args {
			if f.Decl == t.Decl && !builtin(f.Args[i], "Nothing") && !e.same(f.Args[i], t.Args[i]) {
				e.notYet(e.at, fmt.Sprintf("a %s used as a %s, whose type argument is another Go type", from, to), "build the value with the wider type argument, as "+t.Decl.Name+"<...>")
			}
		}
	case *check.FuncType:
		t := to.(*check.FuncType)
		var params, args []string
		for i := range t.Params {
			a := fmt.Sprintf("a%d", i)
			params = append(params, a+" "+e.typ(t.Params[i]))
			args = append(args, e.convert(a, t.Params[i], f.Params[i]))
		}
		return fmt.Sprintf("func(f %s) %s { return func(%s) %s { return %s } }(%s)", e.typ(from), e.typ(to),
			strings.Join(params, ", "), e.typ(t.Result), e.convert("f("+strings.Join(args, ", ")+")", f.Result, t.Result), code)
	}
	return code
}

// widener gives a Go function that converts a from to a to.
func (e *emitter) widener(from, to check.Type) string {
	return fmt.Sprintf("func(v %s) %s { return %s }", e.typ(from), e.typ(to), e.convert("v", from, to))
}

// narrow gives code, of the declared type from, as the type to that a
// test has shown it to be: a T? is read through its pointer, and a
// sealed interface is asserted to be its variant.
func (e *emitter) narrow(code string, from, to check.Type) string {
	if n, ok := from.(*check.Nullable); ok {
		if _, still := to.(*check.Nullable); !still {
			code, from = "(*"+code+")", n.Elem
		}
	}
	if !e.same(from, to) {
		code += ".(" + e.typ(to) + ")"
	}
	return code
}
