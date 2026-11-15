package check

import (
	"slices"
	"strings"

	"kogo/syntax"
)

// Operations (spec.md 15): an api list names top-level funs, each of
// the shape of 15.2, whose input and answer cross the wire (15.4).

// An API is an api list as the checker read it.
type API struct {
	Env Type  // the environment every operation of the list takes
	Ops []*Op // in the list's order
	// Types holds the wire types the operations reach, in the order
	// first reached: enums, data classes, sealed interfaces, and the
	// data objects that are their variants. A sealed interface comes
	// before its variants.
	Types []*TypeDecl
}

// An Op is an operation.
type Op struct {
	Func   *Func
	Tx     bool // it takes a Tx; otherwise a Db
	Input  Type // Unit for none
	Answer Type // the A of its Either<String, A>
}

// apis checks the api lists of the program.
func (c *checker) apis(program []*syntax.File) {
	for _, f := range program {
		c.enter(f, c.pkgOf(f.Package.Name))
		for _, d := range f.APIs {
			a := &API{}
			c.info.APIs[d] = a
			named := map[string]bool{}
			for _, id := range d.Funs {
				if named[id.Name] {
					c.errorf(id, "duplicate", "%s is already in api %s: a fun's name is its operation's name on the wire, so name it once", id.Name, d.Name)
					continue
				}
				named[id.Name] = true
				op := c.operation(id)
				if op == nil {
					continue
				}
				if env := op.Func.Params[1].Type; a.Ops == nil {
					a.Env = env
				} else if !fits(env, a.Env) || !fits(a.Env, env) {
					c.errorf(id, "operation", "%s takes the environment %s, and %s takes %s: the funs of one list take one environment type (spec.md 15.2)", id.Name, env, a.Ops[0].Func.Name, a.Env)
				}
				a.Ops = append(a.Ops, op)
				if !is(op.Input, tUnit) {
					c.wire(id, d.Name, "input", op.Input, a)
				}
				if !is(op.Answer, tUnit) {
					c.wire(id, d.Name, "answer", op.Answer, a)
				}
			}
		}
	}
}

// operation finds the fun an api list names and checks that it has
// an operation's shape. It gives nil where it reports an error.
func (c *checker) operation(id *syntax.Ident) *Op {
	var fs []*Func
	for _, f := range c.funcs(id.Name) {
		if f.Kind == Fun && f.Receiver == nil {
			fs = append(fs, f)
		}
	}
	if len(fs) != 1 {
		c.noOperation(id, len(fs))
		return nil
	}
	f, errs := fs[0], len(c.errs)
	c.info.Uses[id] = f
	bad := func(format string, args ...any) {
		c.errorf(id, "operation", "%s "+format+" (spec.md 15.2)", append([]any{id.Name}, args...)...)
	}
	if len(f.TypeParams) > 0 {
		bad("has type parameters: an operation has none, so write its types out")
	}
	for _, p := range f.Decl.(*syntax.FunDecl).Params {
		if p.Default != nil {
			bad("gives %s a default value: an operation has none, so remove it", p.Name)
		}
	}
	if len(f.Params) != 4 {
		bad("takes %d parameters: an operation takes four, as fun %s(tx: Tx, env: Env, request: Request, input: Input), with a Db for the Tx to open its own transactions and Unit for no input", len(f.Params), id.Name)
		return nil
	}
	op := &Op{Func: f, Input: f.Params[3].Type}
	decl := func(t Type) *TypeDecl {
		if n, ok := t.(*Named); ok {
			return n.Decl
		}
		return nil
	}
	switch t := f.Params[0].Type; decl(t) {
	case c.prelude("Tx"):
		op.Tx = true
	case c.prelude("Db"):
	default:
		if t != unknown {
			bad("takes a %s first: an operation takes a Tx, or a Db to open its own transactions", t)
		}
	}
	if t := f.Params[2].Type; t != unknown && (decl(t) == nil || decl(t).Pkg != "kogo.rpc" || decl(t).Name != "Request") {
		bad("takes a %s third: an operation takes a Request, with import kogo.rpc.Request", t)
	}
	if e := c.either(f.Result); e != nil && is(e, tString) {
		op.Answer = f.Result.(*Named).Args[1]
	} else if f.Result != unknown {
		bad("returns %s: an operation returns Either<String, A>, its refusal in words; turn any other failure into words with mapLeft", f.Result)
	}
	if len(c.errs) > errs {
		return nil
	}
	return op
}

// noOperation reports a name in an api list that is not one fun in
// scope: n funs of the name are.
func (c *checker) noOperation(id *syntax.Ident, n int) {
	if n > 1 {
		c.errorf(id, "operation", "%d funs called %s are in scope, and an operation's name is its name on the wire: rename all but one", n, id.Name)
		return
	}
	for _, p := range c.homes(id.Name) {
		for _, f := range p.funcs[id.Name] {
			if !c.visible(f.vis) {
				c.see(id, id.Name, f.vis)
				return
			}
		}
		if p.named(id.Name) != nil {
			c.errorf(id, "operation", "%s is not a top-level fun: an api list names funs declared in a file, with no receiver (spec.md 15.1)", id.Name)
			return
		}
	}
	c.missing(id, id.Name)
}

// wire checks that t, the input or the answer of the operation at id
// in api list, crosses the wire (spec.md 15.4), and adds the types it
// reaches to a. A type already in a is not read again, so a type that
// holds itself ends.
func (c *checker) wire(id *syntax.Ident, list, what string, whole Type, a *API) {
	bad := func(t Type, path, why string) {
		where := "is a " + t.String()
		if path != "" {
			where = "holds " + path + ": " + t.String()
		} else if t != whole {
			where = "holds a " + t.String()
		}
		c.errorf(id, "wire", "%s's %s %s, %s (spec.md 15.4)", id.Name, what, where, why)
	}
	short := func(d *TypeDecl) string { return d.Name[strings.LastIndexByte(d.Name, '.')+1:] }
	add := func(d *TypeDecl) {
		for _, e := range a.Types {
			if short(e) == short(d) {
				c.errorf(id, "wire-name", "%s's %s reaches %s.%s, and api %s reaches %s.%s: two wire types of one name in a list are one type in TypeScript, so rename one (spec.md 15.4)", id.Name, what, d.Pkg, d.Name, list, e.Pkg, e.Name)
			}
		}
		if short(d) == "Outcome" {
			c.errorf(id, "wire-name", "%s's %s reaches %s, and Outcome is the answer's own type in TypeScript: rename it (spec.md 15.4)", id.Name, what, d.Name)
		}
		a.Types = append(a.Types, d)
	}
	var walk func(t Type, path string)
	walk = func(t Type, path string) {
		if t == unknown {
			return
		}
		n, _ := nonNull(t).(*Named)
		if n == nil {
			bad(t, path, "which does not cross the wire: send text the server has formatted")
			return
		}
		switch d := n.Decl; {
		case d.Kind == "builtin" && slices.Contains([]string{"Boolean", "Int", "Long", "Double", "String"}, d.Name):
		case d.Pkg == "kogo" && d.Name == "Uuid":
		case d == c.prelude("List"):
			walk(n.Args[0], path)
		case d.Kind != "enum class" && d.Kind != "data class" && d.Kind != "sealed interface":
			bad(t, path, "which does not cross the wire: send text the server has formatted")
		case len(d.Params) > 0:
			bad(t, path, "and a type on the wire has no type parameters: declare one type for each use")
		case slices.Contains(a.Types, d):
		default:
			add(d)
			if path == "" {
				path = d.Name
			}
			if d.Kind == "data class" {
				for _, p := range d.Ctor.Params {
					walk(p.Type, path+"."+p.Name)
				}
			}
			for _, v := range d.Variants {
				vpath := path + "." + short(v)
				switch {
				case v.Kind == "data object":
					if !slices.Contains(a.Types, v) {
						add(v)
					}
				case slices.ContainsFunc(v.Ctor.Params, func(p *Var) bool { return p.Name == "type" }):
					c.errorf(id, "wire", "%s's %s holds %s.type, and a variant on the wire has no property type, the key that holds its name: rename the property (spec.md 15.4)", id.Name, what, vpath)
				default:
					walk(self(v), vpath)
				}
			}
		}
	}
	walk(whole, "")
}
