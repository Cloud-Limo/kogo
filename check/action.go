package check

import (
	"slices"

	"github.com/Cloud-Limo/kogo/syntax"
)

// Actions (spec.md 7.1; doc/gates.md, "Outside calls"). An action fun
// changes something outside, which no transaction rolls back. The
// word is carried up: a fun that calls one is one. It takes no Tx,
// calls none where a block holds a Tx, and is not kept in a value.

// acts reads a call to f, written at at.
func (c *checker) acts(at syntax.Node, f *Func) {
	if !f.Action {
		return
	}
	c.acted = true
	switch owner, _ := c.owner.(*Func); {
	case owner == nil || owner.Kind != Fun:
		c.errorf(at, "unmarked-action", "%s is an action, and only an action fun calls one: move the call into an action fun (spec.md 7.1)", f.Name)
	case !owner.Action && (owner.Owner != nil || !slices.Contains(c.file.Decls, syntax.Decl(owner.Decl.(*syntax.FunDecl)))):
		c.errorf(at, "unmarked-action", "%s calls %s, an action, and only a top-level fun is an action fun: move the call out of %s, into the action fun around it (spec.md 7.1)", owner.Name, f.Name, owner.Name)
	case !owner.Action:
		c.errorf(at, "unmarked-action", "%s calls %s, an action: make %s an action fun, or move the call (spec.md 7.1)", owner.Name, f.Name, owner.Name)
	case c.inTx > 0:
		c.errorf(at, "action-in-tx", "%s is an action, called in a block that holds a Tx: a transaction may roll back or run again and the action cannot, so move the call before or after the block (spec.md 7.1)", f.Name)
	default:
		for _, l := range c.lambdas {
			c.acting[l] = f.Name
		}
	}
}

// keepsOut refuses an action fun that takes a Tx.
func (c *checker) keepsOut(f *Func, d *syntax.FunDecl) {
	isTx := func(t Type) bool { return t != nil && c.isTx(nonNull(t)) }
	if !f.Action {
		return
	}
	if isTx(f.Receiver) {
		c.errorf(d, "action-tx", "%s is an action fun on a Tx: an action runs with no transaction open, so take a Db and call the action between its transactions, or move the action out of %s (spec.md 7.1)", f.Name, f.Name)
	}
	for i, p := range f.Params {
		if isTx(p.Type) {
			c.errorf(d.Params[i], "action-tx", "%s is an action fun and takes a Tx, %s: an action runs with no transaction open, so take a Db and call the action between its transactions, or move the action out of %s (spec.md 7.1)", f.Name, p.Name, f.Name)
		}
	}
}

// ran reads the lambda a, passed to f. One that acts goes straight to
// a fun of the prelude, which runs it before it returns.
func (c *checker) ran(f *Func, a syntax.Expr) {
	if l, ok := a.(*syntax.Lambda); ok && f.External {
		delete(c.acting, l)
	}
}

// actionKept refuses each lambda of the fun just read that acts and
// was not passed straight to a fun of the prelude.
func (c *checker) actionKept() {
	for l, name := range c.acting {
		c.errorf(l, "action-kept", "this lambda calls %s, an action, and is not passed straight to a fun of the prelude, which runs it at once: an action is not kept in a value, so call %s where it is wanted or write an action fun and call that (spec.md 7.1)", name, name)
	}
	clear(c.acting)
}
