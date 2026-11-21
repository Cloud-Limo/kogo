package check

import (
	"cmp"
	"slices"
	"strings"

	"github.com/Cloud-Limo/kogo/syntax"
)

// A transaction used after it ended (spec.md 15.7; doc/gates.md). A
// value may hold its transaction in a plain property and pass it on.
// What is refused is the two places a value outlives the transaction
// it holds: the answer of a gate whose work is an action fun, and
// the answer of a db.tx block.

// holdsTx spells the path, as call.tx or Tx, by which t holds a
// transaction, and the type whose property it is, nil for a type
// argument or a function's result; or gives "". It looks at any
// depth: properties, type arguments, a sealed interface's variants
// and a function's result.
func (c *checker) holdsTx(t Type, seen []string) (string, *TypeDecl) {
	held := func(t Type, name string, holder *TypeDecl) (string, *TypeDecl) {
		if c.isTx(nonNull(t)) {
			return cmp.Or(name, t.String()), holder
		}
		path, in := c.holdsTx(t, seen)
		if path == "" {
			return "", nil
		}
		return strings.Trim(strings.ReplaceAll(name+"."+path, ".()", "()"), "."), in
	}
	switch t := nonNull(t).(type) {
	case *FuncType:
		return held(t.Result, "()", nil)
	case *Named:
		// Keyed on the type with its arguments: List<List<Call>> reads
		// its inner List. Properties are read before substitution, so
		// a type that holds itself, X<List<T>> in X<T>, still ends.
		if slices.Contains(seen, t.String()) {
			return "", nil
		}
		seen = append(seen, t.String())
		for _, a := range t.Args {
			if path, in := held(a, "", nil); path != "" {
				return path, in
			}
		}
		for _, p := range t.Decl.Props {
			if path, in := held(p.Type, p.Name, t.Decl); path != "" {
				return path, in
			}
		}
		for _, v := range t.Decl.Variants {
			if path, in := c.holdsTx(&Named{Decl: v}, seen); path != "" {
				return path, in
			}
		}
	}
	return "", nil
}

// outlives refuses t, the answer that outlives its transaction, where
// it is one or holds one. who is what answers it, at at, and why says
// when the transaction ends and what to answer.
func (c *checker) outlives(at syntax.Node, code, who string, t Type, why string) {
	path, in := c.holdsTx(t, nil)
	switch {
	case c.isTx(nonNull(t)):
		path = "the transaction itself"
	case path == "":
		return
	case in == nil:
		path = "which holds a transaction, " + path
	default:
		path = "which holds a transaction, " + path + ", a property of " + in.Name
	}
	c.errorf(at, code, "%s answers %s, %s: %s", who, t, path, why)
}
