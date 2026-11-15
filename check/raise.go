package check

import (
	"fmt"

	"kogo/syntax"
)

// Expected failures (spec.md section 9). A raise scope for E is the
// body of a fun whose receiver is Raise<E>, or a block of type
// Raise<E>.() -> A, either's and withError's. A lambda inside one is
// in it, as in Kotlin; a local fun is not. A fun whose receiver is
// Raise<E> is called on the raise scope around the call, which takes
// the place of this; the core's raise, ensure, ensureNotNull and
// withError are such funs, and bind is one on its Either.

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

// receiver reads a fun's receiver, or in the core a function type's:
// the one place where Raise<E> is written.
func (c *checker) receiver(t syntax.Type) Type {
	if n, ok := t.(*syntax.NamedType); ok && !n.Nullable && len(n.Args) == 1 {
		if d, _ := c.findType(n.Name); d != nil && d == c.prelude("Raise") {
			r := &Named{d, []Type{c.typ(n.Args[0])}}
			c.info.Types[t] = r
			return r
		}
	}
	return c.typ(t)
}

// failing records the error type of a fun that can fail: one whose
// receiver is Raise<E>, or that returns Either<E, A>.
func (c *checker) failing(at syntax.Node, f *Func) {
	if e := c.raised(f.Receiver); e != nil {
		c.info.Fails[at] = e
	} else if e := c.either(f.Result); e != nil {
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
	fix := ".mapLeft { e -> ... }.bind()"
	switch {
	case call.Receiver == nil && c.raised(f.Receiver) != nil:
		err = subst(c.raised(f.Receiver), bind(f.TypeParams, call.TypeArgs))
		fix = fmt.Sprintf("withError({ e: %s -> ... }) { %s(...) }", err, f.Name)
	case f.Name == "bind" && f.file == c.core:
		err = call.TypeArgs[0]
	default:
		return
	}
	switch {
	case err == unknown, c.raise == unknown: // something failed already: say no more
	case c.raise == nil:
		c.errorf(e.Fun, "raise-scope", "%s can fail with a %s, and this is not a raise scope: wrap it in either<%s, ...> { }, or call it from a fun Raise<%s>.name(...)", f.Name, err, err, err)
	case !fits(err, c.raise):
		c.errorf(e.Fun, "raise-type", "%s can fail with a %s, and this raise scope fails with a %s: convert the failure, %s", f.Name, err, c.raise, fix)
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
