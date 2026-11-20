package check

import (
	"cmp"
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

// heldTx spells the path, as clerk.tx or box.tx, by which t, what a
// gate gives, holds a transaction that is no proof's own marked one,
// and the type whose property it is, nil for a type argument or a
// function's result; or gives "". It looks at any depth:
// properties, type arguments, a sealed interface's variants and a
// function's result (spec.md 15.2).
func (c *checker) heldTx(t Type, seen []*TypeDecl) (string, *TypeDecl) {
	switch t := nonNull(t).(type) {
	case *FuncType:
		return c.heldIn(t.Result, "()", nil, seen)
	case *Named:
		if slices.Contains(seen, t.Decl) {
			return "", nil
		}
		seen = append(seen, t.Decl)
		for _, a := range t.Args {
			if path, in := c.heldIn(a, "", nil, seen); path != "" {
				return path, in
			}
		}
		own := c.proofTx(t)
		for _, p := range t.Decl.Props {
			if path, in := c.heldIn(p.Type, p.Name, t.Decl, seen); p != own && path != "" {
				return path, in
			}
		}
		for _, v := range t.Decl.Variants {
			if path, in := c.heldTx(&Named{Decl: v}, seen); path != "" {
				return path, in
			}
		}
	}
	return "", nil
}

// holdFix says how to hold the transaction that in holds as its
// property held, as heldTx gives them.
func (c *checker) holdFix(in *TypeDecl, held string) string {
	last := held[strings.LastIndex(held, ".")+1:]
	switch {
	case in == nil:
		return "outside any proof: hold it as a proof's marked transaction"
	case c.proofTx(&Named{Decl: in}) != nil:
		return fmt.Sprintf("and %s is a proof that holds it outside its marked one: a proof holds one transaction, so take %s out of %s", in.Name, last, in.Name)
	case in.Kind == "interface":
		return fmt.Sprintf("in %s, which is no proof: mark it with a marker the gates' file opts in to, as @M val %s: Tx", in.Name, last)
	}
	return fmt.Sprintf("in %s, which is no proof: mark the constructor and the transaction of %s with a marker the gates' file opts in to, as data class %s @M constructor(..., @property:M val %s: Tx)", in.Name, in.Name, in.Name, last)
}

// heldIn is heldTx of t, which holder holds as its property name, or
// as a type argument where name is "", or as a function's result
// where name is "()".
func (c *checker) heldIn(t Type, name string, holder *TypeDecl, seen []*TypeDecl) (string, *TypeDecl) {
	if c.isTx(nonNull(t)) {
		return cmp.Or(name, t.String()), holder
	}
	if path, in := c.heldTx(t, seen); path != "" {
		return strings.Trim(strings.ReplaceAll(name+"."+path, ".()", "()"), "."), in
	}
	return "", nil
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

// runsOn reads one of the five calls, sel, where it runs on a proof's
// transaction: a fun that runs outside runs none on one.
func (c *checker) runsOn(sel *syntax.Select) {
	tx := c.txReads[sel.X]
	if delete(c.txReads, sel.X); tx != nil && c.outside != nil {
		why, fix := c.ended("statement")
		c.errorf(first(sel), "outside-passes-proof", "%s runs a statement on %s, and %s: that one has ended, so %s (spec.md 15.7)", c.outside.Name, spell(sel.X), why, fix)
	}
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
