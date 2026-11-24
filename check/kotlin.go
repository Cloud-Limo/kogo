package check

import (
	"cmp"
	"slices"

	"github.com/Cloud-Limo/kogo/syntax"
)

// The forms of this file are refused because kotlinc refuses them
// (spec.md 1); kotlin/refused holds a file of each.

// topLevel refuses what no top-level declaration of a program has:
// override, and a marker on a typealias.
func (c *checker) topLevel(d syntax.Decl) {
	var m syntax.Modifiers
	switch d := d.(type) {
	case *syntax.FunDecl:
		m = d.Mods
	case *syntax.ValDecl:
		m = d.Mods
	case *syntax.ClassDecl:
		m = d.Mods
	case *syntax.TypeAlias:
		m = d.Mods
		for _, a := range m.Annotations {
			c.errorf(a, "marker", "a marker goes on a fun, a val or a constructor, not a typealias: remove @%s", a.Name)
		}
	}
	if m.Override {
		c.errorf(d, "override", "override goes on a data class's member, and this is declared at the top level: remove it")
	}
}

// constant refuses a const val that is not a number, a Boolean, a
// Char or a String, or whose value is not made of literals and other
// const vals.
func (c *checker) constant(d *syntax.ValDecl, t Type) {
	ok := slices.ContainsFunc([]*Named{tInt, tLong, tDouble, tBoolean, tChar, tString}, func(n *Named) bool { return is(t, n) })
	if _, null := t.(*Nullable); !ok || null {
		c.errorf(d, "const", "a const val is an Int, a Long, a Double, a Boolean, a Char or a String, and this one is a %s: remove const", t)
		return
	}
	var fixed func(e syntax.Expr) bool
	fixed = func(e syntax.Expr) bool {
		switch e := e.(type) {
		case *syntax.Literal:
			return true
		case *syntax.StringLit:
			return !slices.ContainsFunc(e.Parts, func(p syntax.Expr) bool { return !fixed(p) })
		case *syntax.Unary:
			return fixed(e.X)
		case *syntax.Binary:
			return !slices.Contains([]string{"?:", "..", "until", "to", "in"}, e.Op) && fixed(e.X) && fixed(e.Y)
		case *syntax.Ident:
			v, _ := c.info.Uses[e].(*Var)
			if v == nil || v.Kind != Global {
				return false
			}
			vd, _ := v.Decl.(*syntax.ValDecl)
			return vd != nil && vd.Mods.Const
		}
		return false
	}
	if d.Value != nil && !fixed(d.Value) {
		c.errorf(d.Value, "const", "a const val's value is made of literals and other const vals: remove const, or write the value")
	}
}

// exposed refuses a type narrower than a declaration that shows it:
// a private type in a public fun's parameter, say.
func (c *checker) exposed(at syntax.Node, v vis, t Type) {
	switch t := t.(type) {
	case *Nullable:
		c.exposed(at, v, t.Elem)
	case *FuncType:
		for _, p := range append(slices.Clone(t.Params), t.Receiver, t.Result) {
			c.exposed(at, v, p)
		}
	case *Named:
		if narrower(t.Decl.visibility, v.visibility) && c.visible(t.Decl.vis) {
			c.errorf(at, "exposed", "%s is %s and what shows it here is %s, which kotlinc refuses: make it %s too, or %s wider", t.Decl.Name, t.Decl.visibility, cmp.Or(v.visibility, "public"), t.Decl.visibility, t.Decl.Name)
			return
		}
		for _, a := range t.Args {
			c.exposed(at, v, a)
		}
	}
}

// narrower reports whether visibility a is narrower than b.
func narrower(a, b string) bool {
	rank := map[string]int{"": 0, "internal": 1, "private": 2}
	return rank[a] > rank[b]
}

// shows refuses a type in f's signature narrower than v.
func (c *checker) shows(f *Func, v vis) {
	if f.Receiver != nil && f.Owner == nil {
		c.exposed(f.Decl, v, f.Receiver)
	}
	for _, p := range f.Params {
		c.exposed(p.Decl, v, p.Type)
	}
	c.exposed(f.Decl, v, f.Result)
}

// interfaceMember refuses what an interface's member does not have:
// a visibility, external, and the names of Any's members.
func (c *checker) interfaceMember(m syntax.Decl, mods syntax.Modifiers, name string, params int) {
	switch {
	case mods.Visibility != "":
		c.errorf(m, "visibility", "an interface's member has its interface's visibility, and kotlinc refuses %s here: remove it", mods.Visibility)
	case mods.External:
		c.errorf(m, "external", "an interface's member is not external: remove the word")
	case params == 0 && (name == "toString" || name == "hashCode"):
		c.errorf(m, "override", "every value has %s(), which this would hide without override, as kotlinc has it: rename it", name)
	}
}

// twice refuses a property or a supertype a type names twice.
func (c *checker) twice(td *TypeDecl) {
	seen := map[string]bool{}
	for _, p := range td.Props {
		if seen[p.Name] {
			c.errorf(p.Decl, "duplicate", "%s declares %s twice: rename one", td.Name, p.Name)
		}
		seen[p.Name] = true
	}
	names := map[string]bool{}
	for _, s := range td.Decl.Supers {
		if n, ok := s.(*syntax.NamedType); ok && names[n.Name] {
			c.errorf(s, "duplicate", "%s names %s twice: name it once", td.Name, n.Name)
		} else if ok {
			names[n.Name] = true
		}
	}
	if td.Kind == "data class" && len(td.Decl.Params) == 0 {
		c.errorf(td.Decl, "data-class", "a data class has at least one property, as kotlinc has it: write data object %s", td.Name)
	}
}

// inferredCycles refuses funs that take their result types from one
// another: an = body with no stated result whose calls reach itself
// through such funs alone.
func (c *checker) inferredCycles() {
	inferred := func(o Object) bool {
		f, ok := o.(*Func)
		if !ok {
			return false
		}
		d, _ := f.Decl.(*syntax.FunDecl)
		return d != nil && d.Expr != nil && d.Result == nil
	}
	for _, f := range c.info.Funcs {
		if !inferred(f) {
			continue
		}
		seen := map[Object]bool{}
		for queue := slices.Clone(c.reads[f]); len(queue) > 0; queue = queue[1:] {
			o := queue[0]
			if seen[o] || !inferred(o) {
				continue
			}
			if seen[o] = true; o == f {
				c.file = f.file
				c.errorf(f.Decl, "return-type", "%s takes its return type from a fun that takes its own from %s, which kotlinc refuses: state it, fun %s(): Type", f.Name, f.Name, f.Name)
				break
			}
			queue = append(queue, c.reads[o]...)
		}
	}
}

// mentions reports whether t holds the type parameter p.
func mentions(t Type, p *TypeParam) bool {
	switch t := t.(type) {
	case *TypeParam:
		return t == p
	case *Nullable:
		return mentions(t.Elem, p)
	case *Named:
		return slices.ContainsFunc(t.Args, func(a Type) bool { return mentions(a, p) })
	case *FuncType:
		return slices.ContainsFunc(append(slices.Clone(t.Params), t.Receiver, t.Result), func(a Type) bool { return mentions(a, p) })
	}
	return false
}
