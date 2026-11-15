package check

import "kogo/syntax"

// block checks a block's statements in a scope of their own, where
// known holds. When used, its last statement is its value, checked
// against want. It gives the block's type: Nothing when a statement
// of it cannot finish.
func (c *checker) block(b *syntax.Block, want Type, used bool, known facts) Type {
	c.push(known)
	defer c.pop()
	used = used && !is(want, tUnit)
	var last Type = tUnit
	value, dead := false, false
	for i, s := range b.Stmts {
		e, ok := s.(syntax.Expr)
		if x, isIf := s.(*syntax.If); isIf && x.Else == nil && want == nil {
			ok = false // with no else and nothing wanted it is a statement, as in Kotlin
		}
		if value = ok && used && i == len(b.Stmts)-1; value {
			c.straight = e
			last = c.value(e, want)
		} else {
			last = c.stmt(s)
		}
		dead = dead || is(last, tNothing)
	}
	switch {
	case dead:
		return tNothing
	case value:
		return last
	case used && want != nil && !fits(tUnit, want):
		c.errorf(b, "type-mismatch", "this block ends without a value where a %s is wanted: end it with one", want)
	}
	return tUnit
}

// stmt checks a statement whose value nothing uses. It gives Nothing
// when the statement cannot finish.
func (c *checker) stmt(s syntax.Stmt) Type {
	switch s := s.(type) {
	case *syntax.ValDecl:
		var t Type = unknown
		switch {
		case s.Receiver != nil:
			c.errorf(s, "val-type", "an extension val is declared at the top level of a file")
		case s.Value == nil:
			c.errorf(s, "val-type", "a local %s is given its value where it is declared", s.Name)
		case s.Type != nil:
			t = c.typ(s.Type)
			c.straight = s.Value
			c.value(s.Value, t)
		default:
			c.straight = s.Value
			t = c.expr(s.Value, nil)
		}
		if s.Destructure != nil {
			c.destructure(s, s.Destructure, t)
		} else {
			c.local(s, s.Name, t, s.Var)
		}
	case *syntax.FunDecl:
		f := &Func{Kind: Fun, Name: s.Name, Decl: s, vis: vis{Pkg: c.pkg.name, file: c.file}}
		c.info.Funcs[s] = f
		if s.Receiver != nil {
			c.errorf(s, "unknown-name", "a local fun has no receiver: declare an extension at the top level")
		}
		c.signature(f, s, nil)
		c.define(s, f)
		c.body(f, s)
	case *syntax.Assign:
		c.assign(s)
	case *syntax.For:
		var elem Type = unknown
		if r := c.rangeOf(s.Iter); r != nil {
			elem = r
		} else {
			t := c.expr(s.Iter, nil)
			switch n, _ := t.(*Named); {
			case t == unknown:
			case n != nil && (n.Decl == c.prelude("List") || n.Decl == c.prelude("Set")):
				elem = n.Args[0]
			case n != nil && n.Decl == c.prelude("Map") && c.prelude("Pair") != nil:
				elem = &Named{c.prelude("Pair"), n.Args}
			default:
				c.errorf(s.Iter, "type-mismatch", "for goes over a List, a Set, a Map or a range, and this is a %s", t)
			}
		}
		c.push(nil)
		if s.Destructure != nil {
			c.destructure(s, s.Destructure, elem)
		} else {
			c.local(s, s.Name, elem, false)
		}
		c.loop(s.Body, nil)
		c.pop()
	case *syntax.While:
		c.value(s.Cond, tBoolean)
		holds, _ := c.test(s.Cond)
		c.loop(s.Body, holds)
	case *syntax.If:
		return c.ifExpr(s, nil, false)
	case *syntax.When:
		return c.when(s, nil, false)
	case syntax.Expr:
		c.straight = s
		t := c.expr(s, nil)
		b, _ := s.(*syntax.Binary)
		switch {
		case b != nil && b.Op == "?:" && is(c.info.Types[b.Y], tNothing):
			// After the statement x ?: return y, x is not null.
			if v := c.narrowable(b.X); v != nil {
				c.assume(facts{v: nonNull(c.info.Types[b.X])})
			}
		case t != unknown && onlyValue(s):
			c.errorf(first(s), "value-unused", "this statement is only a value, which nothing uses: assign it, return it or remove it")
		case c.either(nonNull(t)) != nil:
			c.errorf(first(s), "either-dropped", "this %s is dropped, and a failure with it: bind it, fold it, return it or assign it", t)
		}
		return t
	}
	return tUnit
}

// loop checks the body of a for or a while, where break and continue
// may stand.
func (c *checker) loop(body *syntax.Block, known facts) {
	defer func(b bool) { c.buriedLoop = b }(c.buriedLoop)
	c.buriedLoop = false
	c.loops++
	c.block(body, nil, false, known)
	c.loops--
}

// onlyValue reports whether an expression does nothing but give a
// value (spec.md section 6): a call and a jump do more.
func onlyValue(e syntax.Expr) bool {
	switch e.(type) {
	case *syntax.Call, *syntax.Jump:
		return false
	}
	return true
}

func (c *checker) assign(s *syntax.Assign) {
	id := s.Target.(*syntax.Ident) // the parser takes only a name
	t := c.expr(id, nil)
	v, _ := c.info.Uses[id].(*Var)
	switch {
	case t == unknown:
		c.expr(s.Value, nil)
		return
	case v == nil || v.Kind != Local || !v.Mutable:
		c.errorf(id, "val-assigned", "%s is not a local var: declare it with var to give it another value", id.Name)
	}
	if s.Op == "=" {
		c.straight = s.Value
		c.value(s.Value, t)
	} else if got := c.operate(s, s.Op[:1], id, t, s.Value, t); !fits(got, t) {
		c.mismatch(id, got, t)
	}
}

// destructure declares names for the parts of a value of type t: a
// data class's properties in order, or a Pair's two.
func (c *checker) destructure(at syntax.Node, names []string, t Type) {
	var parts []Type
	if n, ok := t.(*Named); ok && n.Decl.Kind == "data class" {
		for _, p := range n.Decl.Props {
			parts = append(parts, subst(p.Type, bind(n.Decl.Params, n.Args)))
		}
	} else if ok && n.Decl == c.prelude("Pair") {
		parts = n.Args
	}
	if t != unknown && len(parts) < len(names) {
		c.errorf(at, "destructure", "%d names are written for a %s, which has %d parts: only a data class and a Pair come apart", len(names), t, len(parts))
	}
	for i, name := range names {
		var part Type = unknown
		if i < len(parts) {
			part = parts[i]
		}
		c.local(at, name, part, false)
	}
}
