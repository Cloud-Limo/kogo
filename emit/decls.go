package emit

import (
	"fmt"
	"slices"
	"strings"

	"kogo/check"
	"kogo/syntax"
)

// decl writes a top-level declaration. A typealias is written
// nowhere: the checker has already put its type in its place.
func (e *emitter) decl(d syntax.Decl) {
	defer e.enter(d)()
	e.fn = &scope{names: map[any]string{}, used: map[string]bool{"this_": true}}
	e.this, e.result, e.lambdas, e.loops = nil, nil, 0, nil
	if e.info.Fails[d] != nil {
		e.notYet(d, "a fun that can fail (spec.md 9)", "the next slice of the emitter writes Either and Raise")
		return
	}
	switch d := d.(type) {
	case *syntax.ClassDecl:
		e.class(d)
	case *syntax.FunDecl:
		f := e.info.Funcs[d]
		tps, _ := e.typeParams(f.TypeParams)
		recv := ""
		if f.Receiver != nil {
			recv = "this_ " + e.typ(f.Receiver) + ", "
		}
		params, defaults := e.params(d, f)
		e.w("func %s%s(%s%s) %s {", e.names[f], tps, recv, params, e.typ(f.Result))
		e.body(d, f, defaults)
	case *syntax.ValDecl:
		if f := e.info.Funcs[d]; f != nil { // an extension val
			tps, _ := e.typeParams(f.TypeParams)
			e.w("func %s%s(this_ %s) %s {", e.names[f], tps, e.typ(f.Receiver), e.typ(f.Result))
			e.this, e.result = f.Receiver, f.Result
			e.into(d.Getter, dest{ret, "", f.Result})
			e.end(f.Result)
			e.w("}")
		} else if v := e.info.Vars[d][0]; true {
			e.w("var %s %s = %s", e.names[v], e.typ(v.Type), e.value(d.Value, v.Type))
		}
	}
}

// params declares a fun's parameters and gives them as Go writes
// them. One with a default is a pointer, nil when the call leaves it
// out; defaults writes the Go that then gives it its value.
func (e *emitter) params(d *syntax.FunDecl, f *check.Func) (params string, defaults func()) {
	var ps []string
	late := map[int]string{}
	for i, v := range f.Params {
		if d.Params[i].Default == nil {
			ps = append(ps, e.local(v, v.Name)+" "+e.typ(v.Type))
		} else {
			late[i] = e.temp()
			ps = append(ps, late[i]+" *"+e.typ(v.Type))
		}
	}
	return strings.Join(ps, ", "), func() {
		for i, v := range f.Params {
			if arg, ok := late[i]; ok {
				name := e.local(v, v.Name)
				e.w("var %s %s", name, e.typ(v.Type))
				e.w("if %s != nil {", arg)
				e.w("%s = *%s", name, arg)
				e.w("} else {")
				e.into(d.Params[i].Default, dest{set, name, v.Type})
				e.w("}")
			}
		}
	}
}

// body writes a fun's body after its opening line, and closes it.
func (e *emitter) body(d *syntax.FunDecl, f *check.Func, defaults func()) {
	defer func(this, result check.Type, lambdas int, loops []*loop) {
		e.this, e.result, e.lambdas, e.loops = this, result, lambdas, loops
	}(e.this, e.result, e.lambdas, e.loops)
	if e.result, e.lambdas, e.loops = f.Result, 0, nil; f.Receiver != nil {
		e.this = f.Receiver
	}
	defaults()
	switch {
	case d.Expr != nil && d.Result == nil:
		e.into(d.Expr, dest{})
	case d.Expr != nil:
		e.into(d.Expr, dest{ret, "", f.Result})
	default:
		e.block(d.Body, dest{})
	}
	e.end(f.Result)
	e.w("}")
}

// end writes the last statement of a Go function. Go need not agree
// that every path returns: a function that returns no Unit ends in a
// panic that is never reached.
func (e *emitter) end(result check.Type) {
	if strings.HasPrefix(e.last, "return ") {
		return
	}
	if builtin(result, "Unit") {
		e.w("return %s.Unit{}", e.rt())
	} else {
		e.w(`panic("unreachable")`)
	}
}

// class writes a type. A data class is a struct whose first field
// holds its name for toString, and each property's tag its name; an
// interface's val is a method, since a Go field and method may not
// share a name; an enum is a named string.
func (e *emitter) class(d *syntax.ClassDecl) {
	td := e.info.Classes[d]
	name := e.names[td]
	tps, args := e.typeParams(td.Params)
	members := map[string]bool{}
	claim := func(at syntax.Node, n string) string {
		if members[n] {
			e.refuse(at, "go-name", "two members of %s are both %s in Go: rename one", td.Name, n)
		}
		members[n] = true
		return n
	}
	switch d.Kind {
	case "interface", "sealed interface":
		out := slices.ContainsFunc(td.Params, func(p *check.TypeParam) bool { return p.Out })
		if td.Supers != nil || out && len(td.Props)+len(td.Methods) > 0 {
			e.notYet(d, "an interface that implements another, or has members and an out type parameter", "declare the members on each data class")
		}
		e.w("type %s%s interface {", name, tps)
		for _, v := range td.Props {
			e.w("%s() %s", claim(v.Decl, "Get"+export(v.Name)), e.typ(v.Type))
		}
		for _, m := range td.Methods {
			md := m.Decl.(*syntax.FunDecl)
			if md.TypeParams != nil || slices.ContainsFunc(md.Params, func(p *syntax.Param) bool { return p.Default != nil }) {
				e.notYet(md, "an interface's fun with type parameters or a default value", "pass the value, or make it a top-level fun")
			}
			var ps []check.Type
			for _, p := range m.Params {
				ps = append(ps, p.Type)
			}
			e.w("%s(%s) %s", claim(md, export(m.Name)), e.typs(ps), e.typ(m.Result))
		}
		e.w("}")
	case "enum class":
		e.w("type %s string", name)
		var all []string
		for _, v := range td.Entries {
			e.w("const %s %s = %q", e.names[v], name, v.Name)
			all = append(all, e.names[v])
		}
		e.w("var %s_entries = []%s{%s}", name, name, strings.Join(all, ", "))
	case "data class", "data object":
		simple := td.Name[strings.LastIndexByte(td.Name, '.')+1:]
		if d.Kind == "data object" {
			simple += ",object"
		}
		e.w("type %s%s struct {", name, tps)
		e.w("_ struct{} `kogo:%q`", simple)
		for _, v := range td.Props {
			e.w("%s %s `kogo:%q`", claim(v.Decl, export(v.Name)), e.typ(v.Type), v.Name)
		}
		e.w("}")
		recv := fmt.Sprintf("func (this_ %s%s) ", name, args)
		for _, s := range td.Supers {
			for _, p := range s.Decl.Props {
				i := slices.IndexFunc(td.Props, func(v *check.Var) bool { return v.Name == p.Name })
				if n := "Get" + export(p.Name); i >= 0 && !members[n] {
					want := check.Subst(p.Type, s.Decl.Params, s.Args)
					e.w("%s%s() %s { return %s }", recv, claim(d, n), e.typ(want), e.convert("this_."+export(p.Name), td.Props[i].Type, want))
				}
			}
		}
		for _, m := range td.Methods {
			md := m.Decl.(*syntax.FunDecl)
			back := e.enter(md)
			params, defaults := e.params(md, m)
			e.w("%s%s(%s) %s {", recv, claim(md, export(m.Name)), params, e.typ(m.Result))
			e.body(md, m, defaults)
			back()
		}
	}
	for _, m := range d.Body {
		if nested, ok := m.(*syntax.ClassDecl); ok {
			e.decl(nested)
		}
	}
}
