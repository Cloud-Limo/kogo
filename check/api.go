package check

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Cloud-Limo/kogo/syntax"
)

// Operations (spec.md 15): an api list names top-level funs, each of
// the shape its place in the list gives it (15.2), and an operation's
// input and answer cross the wire (15.4).

// An API is an api list as the checker read it.
type API struct {
	Env   Type  // the environment every fun of the list takes
	Check *Func // the list's check; nil in an unchecked list
	Ops   []*Op // in the list's order
	// Types holds the wire types the operations reach, enums and data
	// classes, in the order first reached.
	Types []*TypeDecl
}

// An Op is an operation.
type Op struct {
	Func *Func
	Tx   bool // ungated, it takes a Tx; otherwise a Db
	// Serializable says the transaction the server opens is a
	// SerializableTx, as the first parameter of the gate, or of an
	// ungated fun, names; otherwise it is at READ COMMITTED.
	Serializable bool
	Gate         *Func // nil for an ungated operation
	Given        Type  // what the gate answers and the work takes
	Input        Type  // Unit for none
	Answer       Type  // the A of its Either<String, A>, or of an ungated one's Reply<A>
}

// reply gives A where t is kogo.rpc's Reply<A>, and nil otherwise.
func reply(t Type) Type {
	if n, _ := t.(*Named); n != nil && n.Decl.Pkg == "kogo.rpc" && n.Decl.Name == "Reply" {
		return n.Args[0]
	}
	return nil
}

// A role is the shape of a fun that an api list names (spec.md 15.2).
type role struct {
	code  string
	what  string   // as a message names it
	kinds []string // of its parameters: tx, handle (a Tx or a Db), env, request, or "" for any type
	as    string   // its parameters as an author writes them, and what else a message says
	gives string   // what it returns
}

var (
	ungatedRole = &role{"operation", "an ungated operation", []string{"handle", "env", "request", ""},
		"tx: Tx, env: Env, request: Request, input: Input), with a Db for the Tx to open its own transactions and Unit for no input; work that takes what a gate answers names the gate in its row, with gatedBy", "Either<String, A>"}
	workRole = &role{"operation", "gated work", []string{"env", "", ""},
		"env: Env, call: Call, input: Input), with what its gate answers for the Call and Unit for no input: the gate has read the Request, and its answer is all the work is handed", "Either<String, A>"}
	gateRole = &role{"gate", "a gate", []string{"tx", "env", "request"},
		"tx: Tx, env: Env, request: Request), and its operation's input fourth where it reads it", "Either<String, Call>"}
	inputGateRole = &role{"gate", "a gate", []string{"tx", "env", "request", ""}, gateRole.as, gateRole.gives}
	checkRole     = &role{"check-shape", "a list's check", []string{"tx", "env", "request"},
		"tx: Tx, env: Env, request: Request)", "Either<String, Unit>"}
)

// apis checks the api lists of the program.
func (c *checker) apis(program []*syntax.File) {
	type first struct{ name, at string }
	lists := map[string]first{} // by name in lower case, the list first declared and where
	for _, f := range program {
		c.enter(f, c.pkgOf(f.Package.Name))
		for _, d := range f.APIs {
			key := strings.ToLower(d.Name)
			if e, ok := lists[key]; !ok {
				lists[key] = first{d.Name, fmt.Sprintf("%s:%d", f.Name, d.Line)}
			} else if e.name == d.Name {
				c.errorf(d, "duplicate", "api %s is already declared at %s: kogo ts writes each list to the file of its name, %s.ts, so rename one (spec.md 15.1)", d.Name, e.at, d.Name)
			} else {
				c.errorf(d, "duplicate", "api %s is declared at %s as api %s: kogo ts writes each list to the file of its name, and %s.ts and %s.ts are one file where case is ignored, as on macOS, so rename one (spec.md 15.1)", d.Name, e.at, e.name, d.Name, e.name)
			}
			if Interface(d.Name) == "Outcome" {
				c.errorf(d, "wire-name", "api %s's interface in TypeScript would be Outcome, the answer's own type: rename the list (spec.md 15.1)", d.Name)
			}
			if len(d.Rows) == 0 {
				c.errorf(d, "operation", "api %s names no fun: name its operations, as api %s unchecked { f ungated } (spec.md 15.1)", d.Name, d.Name)
			}
			if d.Check == nil && !d.Unchecked {
				c.errorf(d, "no-check", "api %s names no check: write api %s checkedBy <check>, with the fun the server runs before every row, or api %s unchecked for a list that no browser calls (spec.md 15.1)", d.Name, d.Name, d.Name)
			}
			a := &API{}
			c.info.APIs[d] = a
			// env holds every fun of the list to one environment: the
			// first that states one sets it.
			var envOf *syntax.Ident
			env := func(id *syntax.Ident, f *Func, r *role) {
				if t := f.Params[slices.Index(r.kinds, "env")].Type; envOf == nil {
					a.Env, envOf = t, id
				} else if !same(t, a.Env) {
					c.errorf(id, r.code, "%s takes the environment %s, and %s takes %s: the funs of one list take one environment type (spec.md 15.2)", id.Name, t, envOf.Name, a.Env)
				}
			}
			if d.Check != nil {
				if f, gives := c.shaped(d.Check, checkRole); f != nil {
					if env(d.Check, f, checkRole); !is(gives, tUnit) && gives != unknown {
						c.errorf(d.Check, "check-shape", "%s returns %s: a list's check returns Either<String, Unit>, a refusal or nothing (spec.md 15.2)", d.Check.Name, f.Result)
					}
					a.Check = f
				}
			}
			w := &reach{API: a, list: d.Name, seen: map[*TypeDecl]bool{}, names: map[string]*TypeDecl{}}
			listed := map[string]bool{}
			gates := map[*Func]string{} // by gate, the first operation that names it
			for _, row := range d.Rows {
				id := row.Op
				if listed[id.Name] {
					c.errorf(id, "duplicate", "%s is already in api %s: a fun's name is its operation's name on the wire, so name it once", id.Name, d.Name)
					continue
				}
				listed[id.Name] = true
				op := c.operation(row, env, gates)
				if op == nil {
					continue
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

// same reports whether a and b are one type.
func same(a, b Type) bool { return fits(a, b) && fits(b, a) }

// operation checks a row: that each fun it names has its role's
// shape, and that what one stage answers is what the next takes. env
// holds a fun to the list's environment, and gates has the list's
// gates so far. It gives nil where it reports an error.
func (c *checker) operation(row *syntax.Row, env func(*syntax.Ident, *Func, *role), gates map[*Func]string) *Op {
	id, errs := row.Op, len(c.errs)
	if row.Gate == nil {
		f, answer := c.shaped(id, ungatedRole)
		if f == nil {
			return nil
		}
		env(id, f, ungatedRole)
		op := &Op{Func: f, Tx: named(f.Params[0].Type) != c.prelude("Db"), Serializable: c.serializable(f.Params[0].Type), Input: f.Params[3].Type, Answer: answer}
		if a := reply(answer); a != nil {
			op.Answer = a
		}
		return op
	}
	g, given := c.shaped(row.Gate, gateRole)
	f, answer := c.shaped(id, workRole)
	if g == nil || f == nil {
		return nil
	}
	env(row.Gate, g, gateRole)
	env(id, f, workRole)
	op := &Op{Func: f, Gate: g, Serializable: c.serializable(g.Params[0].Type), Given: given, Input: f.Params[2].Type, Answer: answer}
	if d := named(given); d != nil && (c.isTx(given) || d == c.prelude("Db") || d.Pkg == "kogo.rpc" && d.Name == "Request") {
		c.errorf(row.Gate, "gate", "%s answers a %s: gated work is handed no bare transaction, Db or Request, so answer a call, a data class that holds the transaction and what the gate found, or the facts alone (spec.md 15.2)", g.Name, given)
	} else if f.Action {
		c.outlives(row.Gate, "action-gate", fmt.Sprintf("%s is an action fun, and its gate %s", id.Name, g.Name), given,
			fmt.Sprintf("the server ends the gate's transaction before work that is an action fun runs, so name a gate that answers the facts %s needs, or make %s a fun, which runs in the transaction (spec.md 15.7)", id.Name, id.Name))
	}
	if t := f.Params[1].Type; !same(t, given) {
		c.errorf(id, "operation", "%s takes a %s second, and its gate %s answers a %s: gated work takes what its gate answers, so take a %s or name the gate that answers a %s (spec.md 15.2)", id.Name, t, g.Name, given, given, t)
	}
	if len(g.Params) == 4 {
		if t := g.Params[3].Type; !same(t, op.Input) {
			c.errorf(row.Gate, "gate", "%s reads a %s, and %s's input is a %s: a gate that reads an input takes its operation's (spec.md 15.2)", g.Name, t, id.Name, op.Input)
		}
		if other, ok := gates[g]; ok {
			c.errorf(row.Gate, "gate", "%s is %s's gate too, and it reads an input: a gate that more than one row names reads none, so write %s a gate of its own or take the input out of %s (spec.md 15.2)", g.Name, other, id.Name, g.Name)
		}
	}
	if _, ok := gates[g]; !ok {
		gates[g] = id.Name
	}
	if len(c.errs) > errs {
		return nil
	}
	return op
}

// named gives the declaration of a named type, or nil.
func named(t Type) *TypeDecl {
	if n, ok := t.(*Named); ok {
		return n.Decl
	}
	return nil
}

// shaped finds the one fun that an api list names at id and checks
// that it has the shape of r, but for a gate's fourth parameter and
// what a check gives, which the list fits. It gives the fun
// and the A of its Either<String, A>, or nil where it reports an
// error.
func (c *checker) shaped(id *syntax.Ident, r *role) (*Func, Type) {
	var fs []*Func
	for _, f := range c.funcs(id.Name) {
		if f.Kind == Fun && f.Receiver == nil {
			fs = append(fs, f)
		}
	}
	if len(fs) != 1 {
		c.noOperation(id, len(fs))
		return nil, nil
	}
	f, errs := fs[0], len(c.errs)
	c.info.Uses[id] = f
	if f.Name != id.Name && r.code == "operation" {
		c.errorf(id, "operation", "%s is imported as %s: a list names an operation by its fun's own name, its name on the wire, so write %s in a file that imports it by that name (spec.md 2)", f.Name, id.Name, f.Name)
		return nil, nil
	}
	if r == gateRole && len(f.Params) == 4 {
		r = inputGateRole
	}
	bad := func(format string, args ...any) {
		c.errorf(id, r.code, "%s "+format+" (spec.md 15.2)", append([]any{id.Name}, args...)...)
	}
	if len(f.TypeParams) > 0 {
		bad("has type parameters: %s has none, so write its types out", r.what)
	}
	for _, p := range f.Decl.(*syntax.FunDecl).Params {
		if p.Default != nil {
			bad("gives %s a default value: %s has none, so remove it", p.Name, r.what)
		}
	}
	if len(f.Params) != len(r.kinds) {
		bad("takes %d parameters: %s takes %s, as fun %s(%s", len(f.Params), r.what, [...]string{3: "three", 4: "four"}[len(r.kinds)], id.Name, r.as)
		return nil, nil
	}
	for i, kind := range r.kinds {
		t, nth := f.Params[i].Type, [...]string{"first", "second", "third"}[min(i, 2)]
		d := named(t)
		switch {
		case t == unknown || kind == "":
		case kind == "handle" && !c.isTx(t) && d != c.prelude("Db"):
			bad("takes a %s %s: %s takes a SerializableTx or a Tx, or a Db to open its own transactions", t, nth, r.what)
		case kind == "tx" && r == checkRole && named(t) != c.prelude("Tx"):
			bad("takes a %s %s: %s takes a Tx, the transaction the server runs it in, which is not rerun on a conflict", t, nth, r.what)
		case kind == "tx" && !c.isTx(t):
			bad("takes a %s %s: %s takes a SerializableTx or a Tx, the transaction the server runs it in", t, nth, r.what)
		case kind == "env" && (d == nil || d.Kind != "data class"):
			bad("takes a %s %s: %s takes the list's environment there, a data class of the application's, of what lives as long as the server", t, nth, r.what)
		case kind == "request" && (d == nil || d.Pkg != "kogo.rpc" || d.Name != "Request"):
			bad("takes a %s %s: %s takes a Request, with import kogo.rpc.Request", t, nth, r.what)
		}
	}
	var gives Type = unknown
	fd := f.Decl.(*syntax.FunDecl)
	if e := c.either(f.Result); e != nil && is(e, tString) {
		gives = f.Result.(*Named).Args[1]
	} else if fd.Result == nil && fd.Expr != nil {
		bad("does not state its return type: %s returns %s, so write it, fun %s(...): %s", r.what, r.gives, id.Name, r.gives)
	} else if f.Result != unknown {
		bad("returns %s: %s returns %s, its refusal in words; turn any other failure into words with mapLeft", f.Result, r.what, r.gives)
	}
	if len(c.errs) > errs {
		return nil, nil
	}
	return f, gives
}

// noOperation reports a name in an api list that is not one fun in
// scope: n funs of the name are.
func (c *checker) noOperation(id *syntax.Ident, n int) {
	if n > 1 {
		c.errorf(id, "operation", "%d funs called %s are in scope, and an api list names a fun by its name alone: rename all but one", n, id.Name)
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
	if r, _ := utf8.DecodeRuneInString(d.Simple()); unicode.IsLower(r) {
		c.errorf(id, "wire-name", "%s's %s reaches %s, and a wire type's name begins with an upper-case letter, so that TypeScript does not keep it for its own, as it keeps string: rename it (spec.md 15.4)", id.Name, what, d.Name)
	}
	if s := Interface(w.list); d.Simple() == s {
		c.errorf(id, "wire-name", "%s's %s reaches %s, and %s is api %s's interface in TypeScript: rename it (spec.md 15.4)", id.Name, what, d.Name, s, w.list)
	}
	w.Types = append(w.Types, d)
}

// wireTime says whether d is one of the two types of kogo.time that
// cross the wire as their text, LocalDate and LocalTime (spec.md 15.4).
func wireTime(d *TypeDecl) bool {
	return d.Pkg == "kogo.time" && d.Kind == "external class" && (d.Name == "LocalDate" || d.Name == "LocalTime")
}

// wire checks that whole, the input or the answer of the operation at
// id, crosses the wire (spec.md 15.4), and adds the types it reaches
// to w. An answer does not read a type the list already reached; an
// input reads every type it reaches, so a marked, private or internal
// constructor behind one reached earlier is found. Each walk reads a
// type once, so a type that holds itself ends.
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
	been := map[*TypeDecl]bool{} // this walk's, for the input's marks
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
		case is(n, tBoolean), number(n), is(n, tString), d == c.prelude("Uuid"), wireTime(d):
		case d == c.prelude("List"):
			walk(n.Args[0], path)
		case d == c.prelude("ByteArray"):
			bad(t, path, "and bytes do not cross the wire: keep them in a row, and answer what the page shows of them")
		case d.Kind == "sealed interface" || slices.ContainsFunc(d.Supers, func(s *Named) bool { return s.Decl.Kind == "sealed interface" }):
			bad(t, path, "and no sealed interface or variant of one crosses the wire: send a data class, with an enum that says which case it is")
		case d.Kind == "data object":
			bad(t, path, "which does not cross the wire: send an enum, or Unit as the whole input or answer")
		case d.Kind != "enum class" && d.Kind != "data class":
			bad(t, path, "which does not cross the wire: send text the server has formatted")
		case reply(n) != nil:
			bad(t, path, "and only an ungated operation answers with a Reply, its whole answer: answer the value, and set cookies from an ungated operation")
		case len(d.Params) > 0:
			bad(t, path, "and a type on the wire has no type parameters: declare one type for each use")
		case what == "input" && d.Kind == "data class" && (len(d.Ctor.markers) > 0 || d.Ctor.visibility != ""):
			wall := d.Ctor.visibility
			if len(d.Ctor.markers) > 0 {
				wall = "marked @" + d.Ctor.markers[0].Name
			}
			bad(t, path, fmt.Sprintf("whose constructor is %s, and no request makes one: take the id or the slug in the input and make the value with the function that checks it", wall))
		case been[d], w.seen[d] && what != "input":
		default:
			been[d] = true
			if !w.seen[d] {
				c.add(id, what, w, d)
			}
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
