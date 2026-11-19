package emit

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Cloud-Limo/kogo/check"
	"github.com/Cloud-Limo/kogo/syntax"
)

// decl writes a top-level declaration. A typealias is written
// nowhere: the checker has already put its type in its place.
func (e *emitter) decl(d syntax.Decl) {
	defer e.enter(d)()
	e.fn = &scope{names: map[any]string{}, used: map[string]bool{"this_": true}}
	e.this, e.result, e.loops, e.raise = nil, nil, nil, nil
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
		e.open("func %s%s(%s%s) %s {", e.names[f], tps, recv, params, e.typ(f.Result))
		e.body(d, f, defaults)
	case *syntax.ValDecl:
		if f := e.info.Funcs[d]; f != nil { // an extension val
			tps, _ := e.typeParams(f.TypeParams)
			e.open("func %s%s(this_ %s) %s {", e.names[f], tps, e.typ(f.Receiver), e.typ(f.Result))
			e.this, e.result = f.Receiver, f.Result
			e.into(d.Getter, dest{ret, "", f.Result})
			e.end(f.Result)
			e.shut("}")
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
				e.open("if %s != nil {", arg)
				e.w("%s = *%s", name, arg)
				e.turn("} else {")
				e.into(d.Params[i].Default, dest{set, name, v.Type})
				e.shut("}")
			}
		}
	}
}

// body writes a fun's body after its opening line, and closes it.
func (e *emitter) body(d *syntax.FunDecl, f *check.Func, defaults func()) {
	defer func(this, result check.Type, loops []*loop, r *raise) {
		e.this, e.result, e.loops, e.raise = this, result, loops, r
	}(e.this, e.result, e.loops, e.raise)
	// A local fun is in no raise scope (spec.md 9).
	if e.result, e.loops, e.raise = f.Result, nil, nil; f.Receiver != nil {
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
	e.shut("}")
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
		e.open("type %s%s interface {", name, tps)
		for _, v := range td.Props {
			e.w("%s() %s", claim(v.Decl, "Get"+export(v.Name)), e.typ(v.Type))
		}
		for _, m := range td.Methods {
			var ps []check.Type
			for _, p := range m.Params {
				ps = append(ps, p.Type)
			}
			e.w("%s(%s) %s", claim(m.Decl, export(m.Name)), e.typs(ps), e.typ(m.Result))
		}
		e.shut("}")
	case "enum class":
		e.w("type %s string", name)
		var all []string
		for _, v := range td.Entries {
			e.w("const %s %s = %q", e.names[v], name, v.Name)
			all = append(all, e.names[v])
		}
		e.w("var %s = []%s{%s}", e.names[entries{td}], name, strings.Join(all, ", "))
	case "data class", "data object":
		simple := td.Simple()
		if d.Kind == "data object" {
			simple += ",object"
		}
		e.open("type %s%s struct {", name, tps)
		e.w("_ struct{} `kogo:%q`", simple)
		for _, v := range td.Props {
			e.w("%s %s `kogo:%q`", claim(v.Decl, export(v.Name)), e.typ(v.Type), v.Name)
		}
		e.shut("}")
		e.chain(td, name, tps, args)
		recv := fmt.Sprintf("func (this_ %s%s) ", name, args)
		getters := map[string]bool{} // one for a val that two interfaces declare
		for _, s := range td.Supers {
			for _, p := range s.Decl.Props {
				i := slices.IndexFunc(td.Props, func(v *check.Var) bool { return v.Name == p.Name })
				if n := "Get" + export(p.Name); i >= 0 && !getters[n] {
					getters[n] = true
					want := check.Subst(p.Type, s.Decl.Params, s.Args)
					e.w("%s%s() %s { return %s }", recv, claim(td.Props[i].Decl, n), e.typ(want), e.convert("this_."+export(p.Name), td.Props[i].Type, want))
				}
			}
		}
		for _, m := range td.Methods {
			md := m.Decl.(*syntax.FunDecl)
			back := e.enter(md)
			params, defaults := e.params(md, m)
			e.open("%s%s(%s) %s {", recv, claim(md, export(m.Name)), params, e.typ(m.Result))
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

// chain writes what hand-written Go builds a data class by, where it
// builds one: a list's environment, or a class an input reaches. Name_()
// begins it, and a step for each property, named for it and in the
// class's order, gives a type that has only the next step, the last
// one the class. So a property left out, misplaced or misnamed does
// not compile, and Go's error names the step's type, which names the
// property it needs. Emitted Go itself writes the keyed literal.
func (e *emitter) chain(td *check.TypeDecl, name, tps, args string) {
	if !e.chained[td] || len(td.Props) == 0 {
		return
	}
	next := func(i int) string {
		if i == len(td.Props) {
			return name + args
		}
		return e.names[step{td, i}] + args
	}
	e.w("func %s_%s() %s { return %s{} }", name, tps, next(0), next(0))
	for i, v := range td.Props {
		e.w("type %s%s struct{ v %s%s }", e.names[step{td, i}], tps, name, args)
		give := "s.v"
		if i+1 < len(td.Props) {
			give = next(i+1) + "(s)"
		}
		e.w("func (s %s) %s(x %s) %s { s.v.%s = x; return %s }", next(i), export(v.Name), e.typ(v.Type), next(i+1), export(v.Name), give)
	}
}

// chains marks the data classes that hand-written Go builds: each
// list's environment and each data class an operation's input reaches.
func (e *emitter) chains() {
	var reach func(t check.Type)
	reach = func(t check.Type) {
		switch t := t.(type) {
		case *check.Nullable:
			reach(t.Elem)
		case *check.Named:
			for _, a := range t.Args {
				reach(a)
			}
			if t.Decl.Kind == "data class" && !e.chained[t.Decl] {
				e.chained[t.Decl] = true
				for _, v := range t.Decl.Props {
					reach(v.Type)
				}
			}
		}
	}
	for _, a := range e.info.APIs {
		reach(a.Env)
		for _, op := range a.Ops {
			reach(op.Input)
		}
	}
}

// table writes an api list's rpc.Table: its check, each operation's
// name on the wire and its fun, or its gate, step and work, in the
// list's order, and the entries of each enum the operations reach, by
// which the server reads an input's enum. An unchecked list is an
// rpc.Unserved, which no handler takes. Beside it goes each
// operation's rpc.Op, the typed value that rpc.Call runs it by: its
// environment, its input and what its fun answers, a Reply whole.
func (e *emitter) table(d *syntax.API) {
	defer e.enter(d)()
	a := e.info.APIs[d]
	rpc := func(name string) string { return e.qualify("kogo.rpc", name) }
	fun := func(f *check.Func) string { return e.qualify(f.Pkg, e.names[f]) }
	var ops, enums []string
	for _, op := range a.Ops {
		kind, funs := "Db", fun(op.Func)
		if op.Tx {
			kind = "Tx"
		}
		if op.Gate != nil {
			// A gate that reads no input is shared: rpc.Shared gives
			// it the row's input. A row with no step has a nil one.
			gate, step := fun(op.Gate), "nil"
			if len(op.Gate.Params) == 3 {
				gate = fmt.Sprintf("%s[%s](%s)", rpc("Shared"), e.typ(op.Input), gate)
			}
			if op.After != nil {
				step = fun(op.After)
			}
			if kind, funs = "Gated", gate+", "+step+", "+funs; op.Func.Action {
				// Work that is an action fun runs after its gate's
				// transaction, with none open; it has no step.
				kind, funs = "Outside", gate+", "+fun(op.Func)
			}
		}
		if kind != "Db" { // the kind of transaction the server opens
			funs = fmt.Sprint(op.Serializable) + ", " + funs
		}
		ops = append(ops, fmt.Sprintf("%s(%q, %s)", rpc(kind), op.Func.Name, funs))
		e.w("var %s = %s[%s, %s, %s](%q)", e.names[opValue{op}], rpc("NewOp"), e.typ(a.Env), e.typ(op.Input), e.typ(op.Func.Result.(*check.Named).Args[1]), op.Func.Name)
	}
	for _, t := range a.Types {
		if t.Kind == "enum class" {
			enums = append(enums, e.entries(t))
		}
	}
	env := e.typ(a.Env)
	rows := fmt.Sprintf("[]%s[%s]{%s}, []any{%s}", rpc("Operation"), env, strings.Join(ops, ", "), strings.Join(enums, ", "))
	if a.Check == nil {
		e.w("var %s = %s(%s)", e.names[d], rpc("NewUnserved"), rows)
		return
	}
	e.w("var %s = %s[%s]{%s, %s}", e.names[d], rpc("Table"), env, fun(a.Check), rows)
}

// tests writes a tests list's test.List: each test with the file,
// line and column of its val, which kogo test prints when it fails.
// The file is named as kogo was given it.
func (e *emitter) tests(d *syntax.Tests) {
	defer e.enter(d)()
	l := e.info.Tests[d]
	var tests []string
	for _, v := range l.Tests {
		at := v.Decl.At()
		tests = append(tests, fmt.Sprintf("%s(%s, %q, %d, %d)", e.qualify("kogo.test", "At"), e.names[v], e.file.Name, at.Line, at.Column))
	}
	env, at := e.typ(l.Env), d.At()
	e.w("var %s = %s(%s[%s]{Name: %q, File: %q, Line: %d, Column: %d, Tests: []%s[%s]{%s}})",
		e.names[d], e.qualify("kogo.test", "Register"), e.qualify("kogo.test", "List"), env, e.file.Package.Name+"."+d.Name,
		e.file.Name, at.Line, at.Column, e.qualify("kogo.test", "Placed"), env, strings.Join(tests, ", "))
}
