package check

import "kogo/syntax"

// Expected failures (spec.md section 9). A raise scope for E is a
// block of type Raise<E>.() -> A, either's or db.tx's. A lambda inside
// one is in it, as in Kotlin; a local fun is not. Only the core writes
// Raise<E>: its raise, ensure and ensureNotNull are funs whose
// receiver is Raise<E>, called on the raise scope around the call,
// which takes the place of this, and bind is one on its Either.

// raised gives E when t is Raise<E>, and nil otherwise.
func (c *checker) raised(t Type) Type {
	if n, ok := t.(*Named); ok && n.Decl == c.prelude("Raise") {
		return n.Args[0]
	}
	return nil
}

// either gives E when t is Either<E, A>, and nil otherwise.
func (c *checker) either(t Type) Type {
	if n, ok := t.(*Named); ok && n.Decl == c.prelude("Either") {
		return n.Args[0]
	}
	return nil
}

// failing records the error type of a fun that can fail: one that
// returns Either<E, A>.
func (c *checker) failing(at syntax.Node, f *Func) {
	if e := c.either(f.Result); e != nil {
		c.info.Fails[at] = e
	}
}

// raiseBlock reads a lambda passed where a block with the receiver
// Raise<E> is wanted: a raise scope for E.
func (c *checker) raiseBlock(l *syntax.Lambda, receiver Type, params []Type, want Type) *FuncType {
	defer func(e Type, n int) { c.raise, c.raiseDepth = e, n }(c.raise, c.raiseDepth)
	c.raise, c.raiseDepth = c.raised(receiver), len(c.lambdas)
	c.info.Fails[l] = c.raise
	got := c.lambda(l, params, want)
	got.Receiver = receiver
	return got
}

// fails checks a call that makes the raise scope around it fail: bind,
// or a fun whose receiver is Raise<E>, called on the scope. The E it
// fails with must fit the scope's.
func (c *checker) fails(e *syntax.Call, call *Call) {
	f := call.Func
	var err Type
	switch {
	case call.Receiver == nil && c.raised(f.Receiver) != nil:
		err = subst(c.raised(f.Receiver), bind(f.TypeParams, call.TypeArgs))
	case f.Name == "bind" && f.file == c.core:
		err = call.TypeArgs[0]
	default:
		return
	}
	switch {
	case err == unknown, c.raise == unknown: // something failed already: say no more
	case c.raise == nil:
		c.errorf(e.Fun, "raise-scope", "%s can fail with a %s, and this is not a raise scope: wrap it in either<%s, ...> { }", f.Name, err, err)
	case !fits(err, c.raise):
		c.errorf(e.Fun, "raise-type", "%s can fail with a %s, and this raise scope fails with a %s: convert the failure, .mapLeft { e -> ... }.bind()", f.Name, err, c.raise)
	default:
		c.info.Fails[e] = err
		for _, l := range c.lambdas[c.raiseDepth:] {
			c.info.Fails[l] = c.raise
		}
	}
}

// txRaise reports whether f is the core's db.tx whose block is a
// raise scope (spec.md 10.7).
func (c *checker) txRaise(f *Func) bool {
	return f.file == c.core && f.Name == "tx" && len(f.TypeParams) == 2
}

// otherTx reports whether f is the core's db.tx that does not apply
// here: the one whose block is a raise scope is called where the
// enclosing fun returns an Either, and the other everywhere else.
func (c *checker) otherTx(f *Func) bool {
	return f.file == c.core && f.Name == "tx" && c.txRaise(f) != (c.either(c.result) != nil)
}

// form reports whether f is one of the core's forms, raise, ensure
// and ensureNotNull, called in a raise scope.
func (c *checker) form(f *Func) bool {
	return c.raise != nil && f.file == c.core && c.raised(f.Receiver) != nil
}
