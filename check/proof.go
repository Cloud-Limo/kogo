package check

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Cloud-Limo/kogo/syntax"
)

// Proofs (spec.md 15.7; doc/gates.md, "The proof is the only
// handle"). A proof carries what a gate found and the transaction it
// found it in. The checker knows one by its shape: a data class whose
// constructor is marked and which has a marked property that is a
// Tx, or an interface that declares such a val.

// proofTx gives the marked transaction of the proof t, or nil where t
// is no proof.
func (c *checker) proofTx(t Type) *Var {
	d := named(nonNull(t))
	if d == nil || d.Kind != "interface" && (d.Kind != "data class" || len(d.Ctor.markers) == 0) {
		return nil
	}
	for _, p := range d.Props {
		if len(p.markers) > 0 && c.isTx(nonNull(p.Type)) {
			return p
		}
	}
	return nil
}

// spell gives a name or a chain of properties as it is written:
// booking.tx. Anything else is "".
func spell(e syntax.Expr) string {
	switch e := e.(type) {
	case *syntax.Ident:
		return e.Name
	case *syntax.Select:
		if x := spell(e.X); x != "" {
			return x + "." + e.Name
		}
	}
	return ""
}

// holder names the proof whose transaction tx reads.
func holder(tx syntax.Expr) string {
	if s, ok := tx.(*syntax.Select); ok && spell(s.X) != "" {
		return spell(s.X)
	}
	return "the proof"
}

// binds reads one of the five calls, sel, where it runs on a proof's
// transaction: its literal binds another property of that proof,
// written as the transaction is, booking.id beside booking.tx. A
// proof with nothing but the transaction is not held to it.
func (c *checker) binds(sel *syntax.Select, lit *syntax.SQL) {
	tx := c.txReads[sel.X]
	if delete(c.txReads, sel.X); tx != nil && c.outside != nil {
		why, fix := c.ended("statement")
		c.errorf(first(sel), "outside-passes-proof", "%s runs a statement on %s, and %s: that one has ended, so %s (spec.md 15.7)", c.outside.Name, spell(sel.X), why, fix)
	}
	if tx == nil || len(tx.Owner.Props) < 2 {
		return
	}
	if spell(sel.X) == "" { // xs.first().tx: nothing says whose a bound value is
		c.errorf(first(sel), "unbound-statement", "this statement runs on the transaction of a proof that has no name here, so nothing it binds is seen to be that proof's: keep the proof in a val, and bind what it found beside its transaction, ${held.id} with held.tx (spec.md 15.7)")
		return
	}
	of := strings.TrimSuffix(spell(sel.X), tx.Name) // booking. or, on this, nothing
	for _, p := range lit.Params {
		for e := p.Expr; e != nil; {
			v, _ := c.info.Uses[e].(*Var)
			if v != nil && v.Owner == tx.Owner && spell(e) == of+v.Name {
				return
			}
			s, _ := e.(*syntax.Select)
			if s == nil {
				break
			}
			e = s.X
		}
	}
	other := tx.Owner.Props[0]
	if other == tx {
		other = tx.Owner.Props[1]
	}
	c.errorf(first(sel), "unbound-statement", "this statement runs on %s's transaction and binds nothing of %s: bind what it found, as ${%s%s}, so that the statement is about the thing the gate let in, or take a proof that says less (spec.md 15.7)", holder(sel.X), holder(sel.X), of, other.Name)
}

// ended says why the fun that runs outside has no transaction, and
// where what needs one goes: the statement or the call.
func (c *checker) ended(what string) (why, fix string) {
	if name := c.outside.Name; c.placed[c.outside] != workRole {
		return "an outside step has no transaction", fmt.Sprintf("move the %s to the work, or to the gate where %s is the work", what, name)
	}
	return "work that runs outside has no transaction", fmt.Sprintf("move the %s into the gate, or make %s a fun, which runs in the transaction", what, c.outside.Name)
}

// passed reads the argument or the receiver a, of type t, that a call
// to f at at hands to p, nil for the receiver. A proof's transaction
// goes only into a proof, as its own marked transaction, and a fun
// that runs outside passes no proof on, nor a block that takes or
// gives one.
func (c *checker) passed(at syntax.Node, f *Func, p *Var, a syntax.Expr, t Type) {
	if tx := c.txReads[a]; tx != nil {
		delete(c.txReads, a)
		if own := (*Var)(nil); f.Kind == Constructor || f.Kind == Copy {
			own = c.proofTx(self(f.Owner))
			if p != nil && own != nil && p.Name == own.Name {
				return
			}
		}
		c.errorf(first(at), "proof-tx-escapes", "%s is passed to %s; a proof's transaction is used only as %s.query/execute/...: pass %s instead (spec.md 15.7)", spell(a), f.Name, spell(a), holder(a))
	}
	parts := []Type{t}
	if ft, ok := t.(*FuncType); ok {
		parts = append(append(parts, ft.Params...), ft.Result)
	}
	if c.outside != nil && slices.ContainsFunc(parts, func(t Type) bool { return c.proofTx(t) != nil }) {
		what := "a proof"
		if s := spell(a); s != "" {
			what = s
		}
		why, fix := c.ended("call")
		c.errorf(first(at), "outside-passes-proof", "%s passes %s to %s, and %s: %s (spec.md 15.7)", c.outside.Name, what, f.Name, why, fix)
	}
}

// apart refuses a destructuring that takes the transaction p out of
// the proof n.
func (c *checker) apart(at syntax.Node, n *Named, p *Var) {
	if c.proofTx(n) == p {
		c.errorf(at, "proof-tx-escapes", "this takes %s out of a %s; a proof's transaction is used only as proof.%s.query/execute/...: name the proof whole, and pass it instead (spec.md 15.7)", p.Name, n, p.Name)
	}
}

// escaped refuses each read of a proof's transaction, in the body
// just read, that is neither the receiver of one of the five calls
// nor handed to a proof's constructor: one kept in a val, returned,
// or compared.
func (c *checker) escaped() {
	for e := range c.txReads {
		c.errorf(first(e), "proof-tx-escapes", "%s is kept or handed on here; a proof's transaction is used only as %s.query/execute/...: keep and pass %s instead (spec.md 15.7)", spell(e), spell(e), holder(e))
	}
	clear(c.txReads)
}

// takesProof reads the parameters of the action fun f. It runs with
// no transaction open, so it takes a proof only where an api list
// puts it after a gate, as a step or as gated work, and hands it that
// gate's, second (spec.md 7.1). It reports whether f takes one.
func (c *checker) takesProof(f *Func, d *syntax.FunDecl) bool {
	takes := false
	for i, p := range f.Params {
		if !f.Action || i >= len(d.Params) || c.proofTx(p.Type) == nil {
			continue
		}
		if takes = true; c.placed[f] == nil {
			c.errorf(d.Params[i], "action-proof", "%s is an action fun and takes a proof, %s: an action runs with no transaction open, and a proof is handed to one only by an api list, as its row's step after a gate or as gated work, so name %s there, or take the facts it reads and no proof (spec.md 7.1)", f.Name, p.Name, f.Name)
		}
	}
	return takes
}
