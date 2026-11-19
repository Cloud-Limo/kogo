package emit

import (
	"fmt"
	"strings"

	"github.com/Cloud-Limo/kogo/check"
	"github.com/Cloud-Limo/kogo/syntax"
)

// A dest is where a value goes: nowhere, out of the Go function as
// its result, or into a variable. An if, a when, a ?: and a ?. are
// statements in Go, written toward the dest with each branch storing
// or returning (doc/design.md, "The emitter").
type dest struct {
	kind int
	name string     // of the variable
	typ  check.Type // of the result or the variable
}

const (
	drop = iota
	ret
	set
)

// put sends code, a Go expression of the type from, to d. With no
// from it already has d's type.
func (e *emitter) put(d dest, code string, from check.Type) {
	switch d.kind {
	case drop:
		e.w("_ = %s", code)
	case ret:
		e.w("return %s", e.convert(code, from, d.typ))
	case set:
		e.w("%s = %s", d.name, e.convert(code, from, d.typ))
	}
}

// block writes a block's statements. Its last is its value, sent to
// d, unless nothing or a Unit is wanted: then, as the checker reads
// it, every statement is only a statement.
func (e *emitter) block(b *syntax.Block, d dest) {
	wanted := d.kind != drop && !builtin(d.typ, "Unit")
	for i, s := range b.Stmts {
		if x, ok := s.(syntax.Expr); ok && wanted && i == len(b.Stmts)-1 {
			back := e.enter(s)
			e.into(x, d)
			back()
			return
		}
		e.stmt(s)
	}
	if d.kind != drop && !wanted {
		e.put(d, e.rt()+".Unit{}", nil)
	}
}

// declare writes a local's declaration. Go refuses a variable that
// nothing reads, so each is read once here.
func (e *emitter) declare(v *check.Var) string {
	name := e.local(v, v.Name)
	e.w("var %s %s", name, e.typ(v.Type))
	e.w("_ = %s", name)
	return name
}

// parts declares the names of a destructuring, from the Go value
// code of type t: a data class's properties in order, or a Pair's two.
func (e *emitter) parts(vars []*check.Var, code string, t check.Type) {
	n := t.(*check.Named)
	for i, v := range vars {
		field := []string{"First", "Second"}[min(i, 1)]
		if n.Decl.Kind == "data class" {
			field = export(n.Decl.Props[i].Name)
		}
		e.w("%s = %s.%s", e.declare(v), code, field)
	}
}

func (e *emitter) stmt(s syntax.Stmt) {
	defer e.enter(s)()
	switch s := s.(type) {
	case *syntax.ValDecl:
		vars := e.info.Vars[s]
		if s.Destructure != nil {
			// Written toward a variable, as any val's value is, so
			// that a jump in it is a statement (spec 6).
			t, vt := e.temp(), e.info.Types[s.Value]
			e.w("var %s %s", t, e.typ(vt))
			e.into(s.Value, dest{set, t, vt})
			e.parts(vars, t, vt)
		} else {
			e.into(s.Value, dest{set, e.declare(vars[0]), vars[0].Type})
		}
	case *syntax.FunDecl:
		f := e.info.Funcs[s]
		name := e.local(f, f.Name)
		params, defaults := e.params(s, f)
		e.w("var %s func(%s) %s", name, params, e.typ(f.Result))
		e.w("_ = %s", name)
		e.open("%s = func(%s) %s {", name, params, e.typ(f.Result))
		e.body(s, f, defaults)
	case *syntax.Assign:
		v := e.info.Uses[s.Target].(*check.Var)
		name := e.fn.names[v]
		if s.Op == "=" {
			e.into(s.Value, dest{set, name, v.Type})
		} else if c := e.info.Calls[s]; c != nil { // n += x is n = n.plus(x)
			code, t := e.call(s, c, nil, nil)
			e.put(dest{set, name, v.Type}, code, t)
		} else {
			e.w("%s = %s", name, e.arith(s.Op[:1], e.expr(s.Target), v.Type, e.expr(s.Value), e.info.Types[s.Value]))
		}
	case *syntax.For:
		vars := e.info.Vars[s]
		elem, item := vars[0].Type, e.temp()
		iter := ""
		if r, ok := s.Iter.(*syntax.Binary); ok && (r.Op == ".." || r.Op == "until") {
			fn := map[string]string{"..": "Range", "until": "Until"}[r.Op]
			iter = fmt.Sprintf("%s := range %s.%s(%s, %s)", item, e.rt(), fn, e.num(e.expr(r.X), e.info.Types[r.X], elem), e.num(e.expr(r.Y), e.info.Types[r.Y], elem))
		} else if t := e.info.Types[s.Iter].(*check.Named); t.Decl.Name == "List" {
			iter = fmt.Sprintf("_, %s := range %s", item, e.expr(s.Iter))
			elem = t.Args[0]
		} else {
			e.notYet(s.Iter, "a for over a "+t.Decl.Name, "Map and Set come with a later slice")
		}
		e.loop("for "+iter+" {", func() {
			if s.Destructure != nil {
				e.parts(vars, item, elem)
			} else {
				e.w("%s = %s", e.declare(vars[0]), item)
			}
			e.block(s.Body, dest{})
		})
	case *syntax.While:
		e.loop("for "+e.expr(s.Cond)+" {", func() { e.block(s.Body, dest{}) })
	case syntax.Expr:
		e.into(s, dest{})
	}
}

// loop writes a Go loop. A when is a Go switch, which a plain break
// would leave, so break and continue name the loop: its label is
// written only when one does, since Go refuses a label nothing uses.
func (e *emitter) loop(head string, body func()) {
	l := &loop{label: "L" + e.temp()}
	e.loops = append(e.loops, l)
	text := e.inner(body)
	e.loops = e.loops[:len(e.loops)-1]
	if l.used {
		e.w("%s:", l.label)
	}
	e.w("%s", head)
	e.out.WriteString(text)
	e.w("}")
}

// into writes Go that gives the value of x to d. An if, a when, a ?:
// and a ?. become Go statements whose branches store or return; a
// jump is a statement whatever d is.
func (e *emitter) into(x syntax.Expr, d dest) {
	switch x := x.(type) {
	case *syntax.Jump:
		switch {
		case x.Op != "return":
			l := e.loops[len(e.loops)-1]
			l.used = true
			e.w("%s %s", x.Op, l.label)
		case x.Value != nil:
			e.into(x.Value, dest{ret, "", e.result})
		default:
			e.w("return %s.Unit{}", e.rt())
		}
		return
	case *syntax.If:
		e.open("if %s {", e.expr(x.Cond))
		if x.Else == nil { // a statement, whatever d is: its value is Unit
			e.block(x.Then, dest{})
			e.shut("}")
			if d.kind != drop {
				e.put(d, e.rt()+".Unit{}", nil)
			}
			return
		}
		e.block(x.Then, d)
		e.turn("} else {")
		e.block(x.Else, d)
		e.shut("}")
		return
	case *syntax.When:
		e.when(x, d)
		return
	case *syntax.Binary:
		if x.Op == "?:" {
			e.elvis(x, d)
			return
		}
	case *syntax.Select:
		if x.Safe {
			e.safe(x.X, d, func(recv *operand, t check.Type) (string, check.Type) { return e.sel(x, recv, t) })
			return
		}
	case *syntax.Call:
		if sel, ok := x.Fun.(*syntax.Select); ok && sel.Safe {
			e.safe(sel.X, d, func(recv *operand, t check.Type) (string, check.Type) { return e.callExpr(x, recv, t) })
			return
		}
	}
	if d.kind == drop {
		e.put(d, e.expr(x), nil)
	} else {
		e.put(d, e.value(x, d.typ), nil)
	}
}

// elvis writes x ?: y toward d: y is read only when x is null.
func (e *emitter) elvis(x *syntax.Binary, d dest) {
	n, ok := e.info.Types[x.X].(*check.Nullable)
	if !ok {
		e.into(x.X, d)
		return
	}
	t := e.temp()
	e.open("if %s := %s; %s != nil {", t, e.expr(x.X), t)
	if _, null := d.typ.(*check.Nullable); null {
		e.put(d, t, n)
	} else if d.kind != drop {
		e.put(d, "(*"+t+")", n.Elem)
	}
	e.turn("} else {")
	e.into(x.Y, d)
	e.shut("}")
}

// safe writes recv?.member toward d. rest gives the Go for the member
// of a receiver that is not null, and its type.
func (e *emitter) safe(recv syntax.Expr, d dest, rest func(*operand, check.Type) (string, check.Type)) {
	n, ok := e.info.Types[recv].(*check.Nullable)
	if !ok {
		code, t := rest(&operand{x: recv, code: e.expr(recv)}, e.info.Types[recv])
		e.put(d, code, t)
		return
	}
	t := e.temp()
	e.open("if %s := %s; %s != nil {", t, e.expr(recv), t)
	code, typ := rest(&operand{code: "(*" + t + ")"}, n.Elem)
	e.put(d, code, typ)
	if d.kind != drop {
		e.turn("} else {")
		e.put(d, "("+e.typ(d.typ)+")(nil)", nil)
	}
	e.shut("}")
}

// when writes a when toward d as one Go switch over conditions: an
// is a type test, a constant an ==, a range a call. The checker has
// shown that the branches cover every case where they must.
func (e *emitter) when(x *syntax.When, d dest) {
	subject := ""
	var st check.Type
	e.open("{")
	if x.Subject != nil {
		subject, st = e.temp(), e.info.Types[x.Subject]
		e.w("%s := %s", subject, e.expr(x.Subject))
		e.w("_ = %s", subject)
	}
	e.open("switch {")
	for _, b := range x.Branches {
		back := e.enter(b)
		e.depth-- // a case and its conditions are at the switch's level
		var conds []string
		for _, c := range b.Conds {
			switch {
			case subject == "":
				conds = append(conds, e.expr(c.X))
			case c.Op == "is":
				conds = append(conds, e.is(subject, st, e.info.Types[c.Type]))
			case c.Op == "in":
				conds = append(conds, e.inRange(subject, c.X.(*syntax.Binary)))
			default:
				conds = append(conds, e.equals(subject, st, c.X))
			}
		}
		if conds == nil {
			e.w("default:")
		} else {
			e.w("case %s:", strings.Join(conds, " || "))
		}
		e.depth++
		e.block(b.Body, d)
		back()
	}
	e.shut("}")
	e.shut("}")
}
