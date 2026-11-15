package check

import (
	"fmt"
	"slices"
	"unicode"
	"unicode/utf8"

	"kogo/syntax"
)

// Operations (spec.md 15): an api list names top-level funs, each of
// the shape of 15.2, whose input and answer cross the wire (15.4).

// An API is an api list as the checker read it.
type API struct {
	Env Type  // the environment every operation of the list takes
	Ops []*Op // in the list's order
	// Types holds the wire types the operations reach, enums and data
	// classes, in the order first reached.
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
	lists := map[string]string{} // where each list's name is first declared
	for _, f := range program {
		c.enter(f, c.pkgOf(f.Package.Name))
		for _, d := range f.APIs {
			if at, ok := lists[d.Name]; ok {
				c.errorf(d, "duplicate", "api %s is already declared at %s: kogo ts writes each list to the file of its name, %s.ts, so rename one (spec.md 15.1)", d.Name, at, d.Name)
			} else {
				lists[d.Name] = fmt.Sprintf("%s:%d", f.Name, d.Line)
			}
			if len(d.Funs) == 0 {
				c.errorf(d, "operation", "api %s names no fun: name its operations, as api %s { f, g } (spec.md 15.1)", d.Name, d.Name)
			}
			a := &API{}
			c.info.APIs[d] = a
			w := &reach{API: a, list: d.Name, seen: map[*TypeDecl]bool{}, names: map[string]*TypeDecl{}}
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
					c.wire(id, "input", op.Input, w)
				}
				if !is(op.Answer, tUnit) {
					c.wire(id, "answer", op.Answer, w)
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
	if t := f.Params[1].Type; t != unknown && (decl(t) == nil || decl(t).Kind != "data class") {
		bad("takes a %s second: an operation's environment is a data class of the application's, of what lives as long as the server", t)
	}
	if t := f.Params[2].Type; t != unknown && (decl(t) == nil || decl(t).Pkg != "kogo.rpc" || decl(t).Name != "Request") {
		bad("takes a %s third: an operation takes a Request, with import kogo.rpc.Request", t)
	}
	fd := f.Decl.(*syntax.FunDecl)
	if e := c.either(f.Result); e != nil && is(e, tString) {
		op.Answer = f.Result.(*Named).Args[1]
	} else if fd.Result == nil && fd.Expr != nil {
		bad("does not state its return type: an operation returns Either<String, A>, so write it, fun %s(...): Either<String, A>", id.Name)
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
	notFun := func() {
		c.errorf(id, "operation", "%s is not a top-level fun: an api list names funs declared in a file, with no receiver (spec.md 15.1)", id.Name)
	}
	for _, p := range c.homes(id.Name) {
		for _, f := range p.funcs[id.Name] {
			if !c.visible(f.vis) {
				c.see(id, id.Name, f.vis)
				return
			}
		}
		if p.named(id.Name) != nil {
			notFun()
			return
		}
	}
	if len(c.members[id.Name]) > 0 {
		notFun()
		return
	}
	c.missing(id, id.Name)
}

// Interface gives the name of the TypeScript interface of the api
// list called list (spec.md 15.6): Ops for ops.
func Interface(list string) string {
	r, n := utf8.DecodeRuneInString(list)
	return string(unicode.ToUpper(r)) + list[n:]
}

// A reach is the wire types an api list reaches so far.
type reach struct {
	*API
	list  string
	seen  map[*TypeDecl]bool
	names map[string]*TypeDecl // by simple name, the first of each
}

// add puts d in w.Types, reporting a name that TypeScript would take
// twice.
func (c *checker) add(id *syntax.Ident, what string, w *reach, d *TypeDecl) {
	w.seen[d] = true
	if e := w.names[d.Simple()]; e != nil {
		c.errorf(id, "wire-name", "%s's %s reaches %s.%s, and api %s reaches %s.%s: two wire types of one name in a list are one type in TypeScript, so rename one (spec.md 15.4)", id.Name, what, d.Pkg, d.Name, w.list, e.Pkg, e.Name)
	} else {
		w.names[d.Simple()] = d
	}
	if d.Simple() == "Outcome" {
		c.errorf(id, "wire-name", "%s's %s reaches %s, and Outcome is the answer's own type in TypeScript: rename it (spec.md 15.4)", id.Name, what, d.Name)
	}
	if s := Interface(w.list); d.Simple() == s {
		c.errorf(id, "wire-name", "%s's %s reaches %s, and %s is api %s's interface in TypeScript: rename it (spec.md 15.4)", id.Name, what, d.Name, s, w.list)
	}
	w.Types = append(w.Types, d)
}

// wire checks that whole, the input or the answer of the operation at
// id, crosses the wire (spec.md 15.4), and adds the types it reaches
// to w. A type already reached is not read again, so a type that holds
// itself ends.
func (c *checker) wire(id *syntax.Ident, what string, whole Type, w *reach) {
	bad := func(t Type, path, why string) {
		where := "is a " + t.String()
		if path != "" {
			where = "holds " + path + ": " + t.String()
		} else if t != whole {
			where = "holds a " + t.String()
		}
		c.errorf(id, "wire", "%s's %s %s, %s (spec.md 15.4)", id.Name, what, where, why)
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
		case is(n, tBoolean), number(n), is(n, tString), d == c.prelude("Uuid"):
		case d == c.prelude("List"):
			walk(n.Args[0], path)
		case d.Kind == "sealed interface" || slices.ContainsFunc(d.Supers, func(s *Named) bool { return s.Decl.Kind == "sealed interface" }):
			bad(t, path, "and no sealed interface or variant of one crosses the wire: send a data class, with an enum that says which case it is")
		case d.Kind == "data object":
			bad(t, path, "which does not cross the wire: send an enum, or Unit as the whole input or answer")
		case d.Kind != "enum class" && d.Kind != "data class":
			bad(t, path, "which does not cross the wire: send text the server has formatted")
		case len(d.Params) > 0:
			bad(t, path, "and a type on the wire has no type parameters: declare one type for each use")
		case w.seen[d]:
		default:
			c.add(id, what, w, d)
			if path == "" {
				path = d.Name
			}
			if d.Kind == "data class" {
				for _, p := range d.Ctor.Params {
					walk(p.Type, path+"."+p.Name)
				}
			}
		}
	}
	walk(whole, "")
}
