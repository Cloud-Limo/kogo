package check

import (
	"cmp"
	_ "embed"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Cloud-Limo/kogo/syntax"
)

//go:embed core.kg
var coreSource []byte

// Check checks the files of a program against the prelude's, which
// alone may declare bindings (spec.md 13), asking sigs for the
// signature of each sql literal. It gives what it learned and the
// errors it found, file by file, the prelude's first.
func Check(prelude, program []*syntax.File, sigs Signatures) (*Info, []*syntax.Error) {
	core, err := syntax.Parse("check/core.kg", coreSource)
	if err != nil {
		panic(err) // every test of the checker parses it
	}
	prelude = append([]*syntax.File{core}, prelude...)
	files := append(slices.Clip(prelude), program...)
	c := &checker{
		info: &Info{
			Types: map[syntax.Node]Type{}, Uses: map[syntax.Node]Object{}, Calls: map[syntax.Node]*Call{},
			Narrowed: map[*syntax.Ident]Type{}, Vars: map[syntax.Node][]*Var{}, Funcs: map[syntax.Node]*Func{},
			Classes: map[*syntax.ClassDecl]*TypeDecl{}, Fails: map[syntax.Node]Type{}, SQL: map[*syntax.SQL]*Query{}, APIs: map[*syntax.API]*API{}, Tests: map[*syntax.Tests]*TestList{}, Core: core,
		},
		pkgs: map[string]*pkg{}, members: map[string][]*Func{}, imports: map[*syntax.File]map[string]*pkg{},
		core: core, signer: sigs, optIns: map[*syntax.File]map[*TypeDecl]bool{}, reads: map[Object][]Object{}, acting: map[*syntax.Lambda]string{},
	}
	order := map[string]int{}
	for i, f := range files {
		order[f.Name] = i
		c.enter(f, c.pkgOf(f.Package.Name))
		for _, d := range f.Decls {
			if i >= len(prelude) && external(d) {
				c.errorf(d, "external", "only the prelude declares bindings (spec.md 13): give this a body in Kogo")
			}
			c.declare(d, "")
		}
	}
	for _, f := range files {
		c.enter(f, c.pkgOf(f.Package.Name))
		c.importAll(f)
		c.optIn(f)
	}
	c.cycles(program)
	c.testLists(program)
	for i := 0; i < len(c.sigs); i++ {
		c.sigs[i]()
	}
	c.overloads()
	c.apis(program)
	for i := 0; i < len(c.bodies); i++ {
		c.bodies[i]()
	}
	c.vals(program)
	slices.SortStableFunc(c.errs, func(a, b *syntax.Error) int {
		return cmp.Or(cmp.Compare(order[a.File], order[b.File]), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
	})
	return c.info, c.errs
}

type checker struct {
	info    *Info
	errs    []*syntax.Error
	pkgs    map[string]*pkg
	members map[string][]*Func // the bindings with a receiver, by name: in scope everywhere
	imports map[*syntax.File]map[string]*pkg
	sigs    []func() // reads each declaration's signature, once every name is declared
	bodies  []func() // checks each body, once every signature is read
	core    *syntax.File
	signer  Signatures
	optIns  map[*syntax.File]map[*TypeDecl]bool // the markers each file opts in to
	reads   map[Object][]Object                 // the top-level vals and the funs each val's value and each fun refers to
	tested  map[*syntax.ValDecl]Type            // the Test<E> of each val a tests list names

	// Where the checker is.
	owner      Object // the val whose value, or the fun whose body, is being read
	file       *syntax.File
	pkg        *pkg
	scope      *scope
	result     Type                      // what the enclosing fun returns
	loops      int                       // the loops around this place, inside its fun or lambda
	raise      Type                      // the E of the raise scope around this place, or nil
	lambdas    []*syntax.Lambda          // the lambdas around this place
	acted      bool                      // the fun at hand has called an action
	inTx       int                       // how many of lambdas are handed a Tx
	acting     map[*syntax.Lambda]string // the lambdas that call an action, and not yet seen passed to the prelude: the action's name
	raiseDepth int                       // how many of lambdas are outside the raise scope

	// A jump stands only where a value goes straight to a val, an
	// assignment, a return or a result (spec.md 6). straight is the
	// expression about to be read that does; buried says that this
	// place is inside one that does not, within its fun or lambda,
	// and buriedLoop within its loop.
	straight           syntax.Expr
	buried, buriedLoop bool
	inLambda           bool // inside a lambda of the fun at hand, which holds no return
}

// A pkg is the declarations of the files that share a package line.
type pkg struct {
	name    string
	types   map[string]*TypeDecl
	objects map[string]*TypeDecl // external objects, which may share a class's name
	aliases map[string]*alias
	funcs   map[string][]*Func // funs and extension vals
	vals    map[string]*Var
}

type alias struct {
	decl   *syntax.TypeAlias
	params []*TypeParam
	typ    Type
	state  int // 0 unread, 1 being read, 2 read
	vis
}

func (c *checker) pkgOf(name string) *pkg {
	if c.pkgs[name] == nil {
		c.pkgs[name] = &pkg{name, map[string]*TypeDecl{}, map[string]*TypeDecl{}, map[string]*alias{}, map[string][]*Func{}, map[string]*Var{}}
	}
	return c.pkgs[name]
}

// named gives where each declaration called name lives.
func (p *pkg) named(name string) []vis {
	var out []vis
	if t := p.types[name]; t != nil {
		out = append(out, t.vis)
	}
	if t := p.objects[name]; t != nil {
		out = append(out, t.vis)
	}
	if a := p.aliases[name]; a != nil {
		out = append(out, a.vis)
	}
	if v := p.vals[name]; v != nil {
		out = append(out, v.vis)
	}
	for _, f := range p.funcs[name] {
		out = append(out, f.vis)
	}
	return out
}

func (c *checker) enter(f *syntax.File, p *pkg) {
	c.file, c.pkg, c.scope, c.owner = f, p, &scope{}, nil
}

// later queues work to be done where the checker is now.
func (c *checker) later(queue *[]func(), work func()) {
	f, p := c.file, c.pkg
	*queue = append(*queue, func() { c.enter(f, p); work() })
}

// errorf reports an error at a node of the file at hand and gives the
// unknown type, for the failed expression to have.
func (c *checker) errorf(at syntax.Node, code, format string, args ...any) Type {
	pos := at.At()
	c.errs = append(c.errs, &syntax.Error{File: c.file.Name, Line: pos.Line, Column: pos.Column, Code: code, Message: fmt.Sprintf(format, args...)})
	return unknown
}

// Kotlin's scope functions that section 6 leaves out, and Arrow's
// withError, which section 9 does: each name's code and fix.
var notKogo = map[string][2]string{
	"apply": {"kotlin-scope-function", "name the value and call it"}, "run": {"kotlin-scope-function", "name the value and call it"},
	"with": {"kotlin-scope-function", "name the value and call it"}, "takeUnless": {"kotlin-scope-function", "use takeIf and turn the test around"},
	"withError": {"with-error", "convert the failure of a block with either<E2, A> { ... }.mapLeft { e -> ... }.bind()"},
}

// missing reports a name that nothing in scope declares. One that
// is Kotlin's and not Kogo's is refused only where no package of the
// program declares it.
func (c *checker) missing(at syntax.Node, name string) Type {
	var homes []string
	tested := false // a name only a test file's package declares
	for _, p := range c.pkgs {
		if p.named(name) != nil {
			if testOnly[p.name] && !c.file.IsTest() {
				tested = true
				continue
			}
			homes = append(homes, p.name+"."+name)
		}
	}
	if homes == nil && tested {
		return c.errorf(at, "test-only", "%s is a test file's, one named _test.kg: use it there (spec.md 16)", name)
	}
	if slices.Sort(homes); homes != nil {
		return c.errorf(at, "unknown-name", "%s is not in scope: add import %s", name, strings.Join(homes, " or import "))
	}
	if r, ok := notKogo[name]; ok {
		return c.errorf(at, r[0], "%s is not Kogo: %s", name, r[1])
	}
	return c.errorf(at, "unknown-name", "nothing named %s is declared here, in package %s or in the prelude", name, c.pkg.name)
}

// visible reports whether the file at hand may name a declaration.
func (c *checker) visible(v vis) bool {
	if v.file != nil && v.file.IsTest() && !c.file.IsTest() {
		return false
	}
	return v.visibility == "" || v.visibility == "private" && v.file == c.file || v.visibility == "internal" && v.Pkg == c.pkg.name
}

// see reports a use of a declaration that the file may not name.
func (c *checker) see(at syntax.Node, name string, v vis) {
	switch {
	case c.visible(v):
	case v.file != nil && v.file.IsTest() && !c.file.IsTest():
		c.errorf(at, "not-visible", "%s is declared in the test file %s, which kogo emit leaves out of the program: move it out of there, or this use into a _test.kg file (spec.md 16)", name, v.file.Name)
	case v.visibility == "private":
		c.errorf(at, "not-visible", "%s is private to %s: remove private there, or move this use into that file", name, v.file.Name)
	default:
		c.errorf(at, "not-visible", "%s is internal to package %s: remove internal there, or move this use into that package", name, v.Pkg)
	}
}

// homes gives the packages a name may come from, nearest first: the
// one the file imports it from, the file's own, and kogo.
func (c *checker) homes(name string) []*pkg {
	head, _, _ := strings.Cut(name, ".")
	var out []*pkg
	for _, p := range []*pkg{c.imports[c.file][head], c.pkg, c.pkgs["kogo"]} {
		if p != nil && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func (c *checker) importAll(f *syntax.File) {
	c.imports[f] = map[string]*pkg{}
	for _, imp := range f.Imports {
		i := strings.LastIndexByte(imp.Name, '.')
		if i < 0 {
			c.errorf(imp, "unknown-import", "an import names a declaration in a package, as import shop.model.Order")
			continue
		}
		from, name := c.pkgs[imp.Name[:i]], imp.Name[i+1:]
		switch {
		case from != nil && testOnly[from.name] && !f.IsTest():
			c.errorf(imp, "test-import", "package %s is for test files, those named _test.kg: move what uses %s into one (spec.md 16)", from.name, name)
		case from == nil && c.kotlinFix(imp.Name[:i], name) != "":
			c.errorf(imp, "unknown-import", "there is no package %s in Kogo: %s", imp.Name[:i], c.kotlinFix(imp.Name[:i], name))
		case from == nil:
			c.errorf(imp, "unknown-import", "there is no package %s", imp.Name[:i])
		case from.named(name) == nil && slices.ContainsFunc(c.members[name], func(f *Func) bool { return f.Pkg == from.name }):
			c.errorf(imp, "member-import", "%s is a member, in scope wherever a value of its receiver's type is: remove this import", name)
		case from.named(name) == nil:
			c.errorf(imp, "unknown-import", "package %s declares nothing named %s", from.name, name)
		case c.imports[f][name] != nil && c.imports[f][name] != from:
			c.errorf(imp, "duplicate", "%s is already imported from package %s", name, c.imports[f][name].name)
		default:
			c.imports[f][name] = from
			if all := from.named(name); !slices.ContainsFunc(all, c.visible) {
				c.see(imp, name, all[0])
			}
		}
	}
}

// kotlinFix says what to write for an import a Kotlin program has,
// or "" when Kogo has no such name to point to.
func (c *checker) kotlinFix(from, name string) string {
	has := func(pkg, name string) bool {
		p := c.pkgs[pkg]
		return p != nil && (p.named(name) != nil || slices.ContainsFunc(c.members[name], func(f *Func) bool { return f.Pkg == pkg }))
	}
	kogo := "kogo." + from[strings.IndexByte(from, '.')+1:]
	switch {
	case (from == "java.time" || from == "kotlin.test") && has(kogo, name):
		return fmt.Sprintf("import %s.%s instead", kogo, name)
	case from == "java.util" && name == "UUID" && has("kogo", "Uuid"):
		return "Uuid needs no import: remove this one and write Uuid"
	case (from == "arrow.core" || strings.HasPrefix(from, "arrow.core.")) && has("kogo", name):
		return name + " needs no import: remove this one"
	}
	return ""
}

// testOnly holds the prelude's packages that only a test file imports.
var testOnly = map[string]bool{"kogo.test": true}

func typeParams(ps []*syntax.TypeParam) []*TypeParam {
	var out []*TypeParam
	for _, p := range ps {
		out = append(out, &TypeParam{p.Name, p.Out})
	}
	return out
}

// self gives a declared type applied to its own parameters.
func self(d *TypeDecl) *Named {
	n := &Named{Decl: d}
	for _, p := range d.Params {
		n.Args = append(n.Args, p)
	}
	return n
}

// declare enters a declaration's name in its package and queues the
// reading of its signature. outer is the name of the type it is
// nested in, with a dot.
func (c *checker) declare(d syntax.Decl, outer string) {
	v := vis{Pkg: c.pkg.name, file: c.file}
	taken := func(name string) bool {
		if c.pkg.types[name] == nil && c.pkg.aliases[name] == nil && c.pkg.vals[name] == nil {
			return false
		}
		c.errorf(d, "duplicate", "package %s already declares %s: rename one", c.pkg.name, name)
		return true
	}
	switch d := d.(type) {
	case *syntax.ClassDecl:
		v.visibility = d.Mods.Visibility
		if d.Kind == "annotation class" {
			c.declareMarker(d, outer+d.Name, v, taken)
			return
		}
		for _, a := range d.Mods.Annotations {
			if a.Name != "go" {
				c.errorf(a, "marker", "a marker goes on a fun, a val or a constructor, not a type: data class %s @%s constructor(...)", d.Name, a.Name)
			}
		}
		td := &TypeDecl{Kind: d.Kind, Name: outer + d.Name, Params: typeParams(d.TypeParams), Decl: d, vis: v}
		c.info.Classes[d] = td
		if d.Kind == "external object" {
			if c.pkg.objects[td.Name] != nil {
				c.errorf(d, "duplicate", "package %s already declares the object %s: rename one", c.pkg.name, td.Name)
			}
			c.pkg.objects[td.Name] = td
		} else if !taken(td.Name) {
			c.pkg.types[td.Name] = td
		}
		for _, m := range d.Body {
			if nested, ok := m.(*syntax.ClassDecl); ok {
				c.declare(nested, td.Name+".")
			}
		}
		c.later(&c.sigs, func() { c.class(td) })
	case *syntax.TypeAlias:
		v.visibility = d.Mods.Visibility
		a := &alias{decl: d, params: typeParams(d.TypeParams), vis: v}
		if !taken(d.Name) {
			c.pkg.aliases[d.Name] = a
		}
		c.later(&c.sigs, func() { c.aliased(a) })
	case *syntax.FunDecl:
		v.visibility = d.Mods.Visibility
		f := &Func{Kind: Fun, Name: d.Name, External: d.Mods.External, Action: d.Mods.Calls == "action", Decl: d, vis: v}
		c.addFunc(f, d.Receiver != nil)
		c.later(&c.sigs, func() {
			f.markers = c.marks(d.Mods)
			c.signature(f, d, nil)
			c.keepsOut(f, d)
			c.later(&c.bodies, func() { c.body(f, d) })
		})
	case *syntax.ValDecl:
		v.visibility = d.Mods.Visibility
		switch {
		case d.Destructure != nil:
			c.errorf(d, "val-type", "a top-level val is one name with its type: val name: Type = value")
		case d.Receiver != nil:
			f := &Func{Kind: Getter, Name: d.Name, TypeParams: typeParams(d.TypeParams), External: d.Mods.External, Decl: d, vis: v}
			c.addFunc(f, true)
			c.later(&c.sigs, func() { c.getter(f, d) })
		default:
			g := &Var{Kind: Global, Name: d.Name, Decl: d, vis: v}
			c.info.Vars[d] = []*Var{g}
			if !taken(d.Name) {
				c.pkg.vals[d.Name] = g
			}
			c.later(&c.sigs, func() {
				g.markers = c.marks(d.Mods)
				g.Type = c.declared(d)
				c.later(&c.bodies, func() {
					c.owner = g
					if d.Value != nil {
						c.value(d.Value, g.Type)
					} else if !d.Mods.External {
						c.errorf(d, "val-type", "the val %s needs a value: val %s: %s = ...", d.Name, d.Name, g.Type)
					}
				})
			})
		}
	}
}

// external reports whether a declaration is a binding.
func external(d syntax.Decl) bool {
	switch d := d.(type) {
	case *syntax.ClassDecl:
		return strings.HasPrefix(d.Kind, "external ")
	case *syntax.FunDecl:
		return d.Mods.External
	case *syntax.ValDecl:
		return d.Mods.External
	}
	return false
}

// overloads reports a fun or an extension val with the receiver and
// parameter types of one declared before it (spec.md 5.2): no call
// could choose between them. Two private ones in different files do
// not meet; bindings meet whatever their visibility, as every call
// sees them. A fun beside a constructor is reported at the fun, and
// an interface's funs are compared among themselves.
func (c *checker) overloads() {
	groups := slices.Collect(maps.Values(c.members))
	for _, p := range c.pkgs {
		for name, fs := range p.funcs {
			if d := p.types[name]; d != nil && d.Ctor != nil {
				fs = append([]*Func{d.Ctor}, fs...)
			}
			groups = append(groups, fs)
		}
		for _, d := range p.types {
			if strings.HasSuffix(d.Kind, "interface") {
				groups = slices.AppendSeq(groups, maps.Values(byName(d.Methods)))
			}
		}
	}
	for _, fs := range groups {
		seen := map[string][]*Func{}
		for _, f := range fs {
			s, ok := shape(f)
			if !ok {
				continue
			}
			i := slices.IndexFunc(seen[s], func(g *Func) bool {
				return f.file == g.file || f.visibility != "private" || g.visibility != "private"
			})
			seen[s] = append(seen[s], f)
			if i < 0 {
				continue
			}
			g, where := seen[s][i], "package "+seen[s][i].Pkg
			if g.Owner != nil {
				where = g.Owner.Name
			}
			c.file = f.file
			if g.Kind == Constructor {
				c.errorf(f.Decl, "duplicate", "the constructor of %s takes these parameter types: rename this fun", g.Name)
			} else {
				c.errorf(f.Decl, "duplicate", "%s is already declared in %s with this receiver and these parameter types: rename one", describe(g, nil), where)
			}
		}
	}
}

func byName(fs []*Func) map[string][]*Func {
	out := map[string][]*Func{}
	for _, f := range fs {
		out[f.Name] = append(out[f.Name], f)
	}
	return out
}

// shape gives what tells two functions of one name apart: whether
// they are vals, their receiver and parameter types, each declared
// type by its identity and each type parameter by its first use.
// ok is false when one of those types failed to read.
func shape(f *Func) (s string, ok bool) {
	var b strings.Builder
	used := map[*TypeParam]int{}
	ok = true
	var put func(Type)
	put = func(t Type) {
		switch t := t.(type) {
		case *TypeParam:
			n, found := used[t]
			if !found {
				n = len(used)
				used[t] = n
			}
			fmt.Fprint(&b, "#", n)
		case *Nullable:
			put(t.Elem)
			b.WriteString("?")
		case *Named:
			fmt.Fprintf(&b, "%p<", t.Decl)
			for _, a := range t.Args {
				put(a)
				b.WriteString(",")
			}
			b.WriteString(">")
		case *FuncType:
			b.WriteString("(")
			if t.Receiver != nil {
				put(t.Receiver)
				b.WriteString(".")
			}
			for _, p := range t.Params {
				put(p)
				b.WriteString(",")
			}
			b.WriteString(")->")
			put(t.Result)
		default:
			ok = false
		}
	}
	fmt.Fprint(&b, f.Kind == Getter, " ")
	if f.Receiver != nil {
		put(f.Receiver)
	}
	for _, p := range f.Params {
		b.WriteString(", ")
		put(p.Type)
	}
	return b.String(), ok
}

func (c *checker) addFunc(f *Func, receiver bool) {
	c.info.Funcs[f.Decl] = f
	if f.External && receiver {
		c.members[f.Name] = append(c.members[f.Name], f)
	} else {
		c.pkg.funcs[f.Name] = append(c.pkg.funcs[f.Name], f)
	}
}

// declared gives the type a top-level val states, which it must
// (spec.md section 5: inference works inside a function body only).
func (c *checker) declared(d *syntax.ValDecl) Type {
	if t := c.tested[d]; d.Type == nil && t != nil {
		return t // a test, whose type its list gives (spec.md 16.1)
	}
	if d.Type == nil && c.file.IsTest() {
		return c.errorf(d, "val-type", "the val %s states no type, and no tests list of this file names it: add it to the list, as tests name<Env> { %s }, or state its type (spec.md 16.1)", d.Name, d.Name)
	}
	if d.Type == nil {
		return c.errorf(d, "val-type", "a top-level val states its type: val %s: Type", d.Name)
	}
	return c.typ(d.Type)
}

func (c *checker) getter(f *Func, d *syntax.ValDecl) {
	c.open(f.TypeParams)
	defer c.pop()
	f.Receiver, f.Result = c.typ(d.Receiver), c.declared(d)
	f.markers = c.marks(d.Mods)
	c.failing(d, f)
	c.later(&c.bodies, func() {
		c.owner = f
		c.open(f.TypeParams)
		defer c.pop()
		c.define(d, &Var{Kind: Param, Name: "this", Type: f.Receiver})
		switch {
		case d.Getter != nil && d.Value == nil:
			c.straight = d.Getter
			c.value(d.Getter, f.Result)
		case !f.External:
			c.errorf(d, "val-type", "an extension val has a getter and no value: val %s.%s: %s\n    get() = ...", f.Receiver, f.Name, f.Result)
		}
	})
}

// signature reads a fun's type parameters, receiver, parameters and
// result. A member has its owner's type parameters before its own
// and, unless the owner is an external object, its owner as receiver.
func (c *checker) signature(f *Func, d *syntax.FunDecl, owner *TypeDecl) {
	if owner != nil {
		f.Owner, f.TypeParams, f.owners = owner, slices.Clone(owner.Params), len(owner.Params)
		if owner.Kind != "external object" {
			f.Receiver = self(owner)
		}
	}
	f.TypeParams = append(f.TypeParams, typeParams(d.TypeParams)...)
	c.open(f.TypeParams)
	defer c.pop()
	if d.Receiver != nil {
		f.Receiver = c.typ(d.Receiver)
	}
	for _, p := range d.Params {
		v := &Var{Kind: Param, Name: p.Name, Type: c.typ(p.Type), Decl: p}
		f.Params = append(f.Params, v)
		c.info.Vars[p] = []*Var{v}
	}
	if f.Result = tUnit; d.Result != nil {
		f.Result = c.typ(d.Result)
	}
	c.failing(d, f)
}

// body checks a fun's default values and its body. Where its receiver
// is a Raise<E>, refused, what it raises says no more.
func (c *checker) body(f *Func, d *syntax.FunDecl) {
	defer func(o Object) { c.owner = o }(c.owner)
	c.owner = f
	c.open(f.TypeParams)
	defer c.pop()
	var raise Type
	if f.Receiver != nil {
		c.define(d, &Var{Kind: Param, Name: "this", Type: f.Receiver})
	}
	if n, ok := d.Receiver.(*syntax.NamedType); ok && f.Receiver == unknown {
		if t, _ := c.findType(n.Name); t != nil && t == c.prelude("Raise") {
			raise = unknown
		}
	}
	for i, p := range d.Params {
		if p.Default != nil {
			c.value(p.Default, f.Params[i].Type)
		}
		c.define(p, f.Params[i])
	}
	defer func(r Type, l int, e Type, n int, b, bl, il bool) {
		c.result, c.loops, c.raise, c.raiseDepth, c.buried, c.buriedLoop, c.inLambda = r, l, e, n, b, bl, il
	}(c.result, c.loops, c.raise, c.raiseDepth, c.buried, c.buriedLoop, c.inLambda)
	c.result, c.loops, c.raise, c.raiseDepth, c.buried, c.buriedLoop, c.inLambda = f.Result, 0, raise, len(c.lambdas), false, false, false
	defer func(a bool, n int, k map[*syntax.Lambda]string) { c.acted, c.inTx, c.acting = a, n, k }(c.acted, c.inTx, c.acting)
	c.acted, c.inTx, c.acting = false, 0, map[*syntax.Lambda]string{}
	errs := len(c.errs)
	defer func() {
		if c.actionKept(); f.Action && !f.External && !c.acted && len(c.errs) == errs {
			c.errorf(d, "idle-action", "%s is an action fun and calls no action: leave the word out, fun %s, so that it may run in a transaction (spec.md 7.1)", d.Name, d.Name)
		}
	}()
	c.straight = d.Expr
	switch {
	case d.Expr != nil && d.Result == nil:
		if t := c.expr(d.Expr, nil); !fits(t, tUnit) {
			c.errorf(d, "return-type", "%s gives a %s: state its return type, fun %s(...): %s", d.Name, t, d.Name, t)
		}
	case d.Expr != nil:
		c.value(d.Expr, f.Result)
	case d.Body != nil:
		if t := c.block(d.Body, nil, false, nil); !is(t, tNothing) && !fits(tUnit, f.Result) {
			c.errorf(d, "missing-return", "%s can reach its end without a value: end it with return and a %s", d.Name, f.Result)
		}
	}
}

// class reads what a type declares: the interfaces it implements, its
// properties, its constants and its members.
func (c *checker) class(td *TypeDecl) {
	d := td.Decl
	c.open(td.Params)
	defer c.pop()
	if td.file == c.core && td.Name == "SerializableTx" { // it fits a Tx (spec.md 10.7)
		td.Supers = []*Named{{Decl: c.prelude("Tx")}}
	}
	for _, s := range d.Supers {
		st, ok := c.typ(s).(*Named)
		switch sealed := ok && st.Decl.Kind == "sealed interface"; {
		case !ok:
		case !sealed && st.Decl.Kind != "interface":
			c.errorf(s, "override", "%s is not an interface: a data class implements interfaces and extends nothing", st)
		case sealed && st.Decl.file != td.file:
			c.errorf(s, "override", "%s is sealed: declare its variant %s in %s, where it is", st, td.Name, st.Decl.file.Name)
		default:
			if td.Supers = append(td.Supers, st); sealed {
				st.Decl.Variants = append(st.Decl.Variants, td)
			}
		}
	}
	for _, p := range d.Params {
		v := &Var{Kind: Property, Name: p.Name, Type: c.typ(p.Type), Owner: td, Decl: p}
		td.Props = append(td.Props, v)
		c.info.Vars[p] = []*Var{v}
	}
	switch d.Kind {
	case "data class":
		td.Ctor = &Func{Kind: Constructor, Name: td.Name, TypeParams: td.Params, Params: td.Props, Result: self(td), Owner: td, Decl: d, vis: td.vis}
		td.Ctor.markers = c.marks(d.Ctor)
		if d.Ctor.Visibility != "" {
			td.Ctor.visibility = d.Ctor.Visibility
		}
	case "enum class":
		td.Props = []*Var{{Kind: Property, Name: "name", Type: tString, Owner: td}}
		for _, e := range d.Entries {
			td.Entries = append(td.Entries, &Var{Kind: Entry, Name: e.Name, Type: self(td), Owner: td, Decl: e})
		}
	}
	variance := func(at syntax.Node, t Type, out bool, name string) {
		if p := misplaced(t, out); p != nil {
			c.errorf(at, "variance", "%s is out in %s, so a %s gives a %s back and takes none (spec.md 3.2): remove out, or make %s an extension of %s", p, td.Name, td.Name, p, name, self(td))
		}
	}
	for _, m := range d.Body {
		switch m := m.(type) {
		case *syntax.ValDecl:
			if d.Kind == "external object" {
				c.errorf(m, "external-object", "an external object holds funs only (spec.md 13): declare fun %s(): ... instead", m.Name)
				continue
			}
			v := &Var{Kind: Property, Name: m.Name, Type: c.declared(m), Owner: td, Decl: m}
			variance(m, v.Type, true, m.Name)
			td.Props = append(td.Props, v)
			c.info.Vars[m] = []*Var{v}
		case *syntax.FunDecl:
			if m.Mods.Calls != "" {
				c.errorf(m, "action-kept", "%s is a member of %s, and a member is not an %s fun: a value of the type would carry it unseen, so declare %s at the top level and pass it the value (spec.md 7.1)", m.Name, d.Name, m.Mods.Calls, m.Name)
			}
			f := &Func{Kind: Fun, Name: m.Name, External: d.Kind == "external object", Decl: m, vis: td.vis}
			f.markers = c.marks(m.Mods)
			if d.Kind == "interface" || d.Kind == "sealed interface" {
				f.Kind = Method
				if m.TypeParams != nil {
					c.errorf(m, "interface-fun", "an interface's fun takes no type parameters (spec.md 3.4): make %s a top-level fun that takes the value", m.Name)
				}
				for _, p := range m.Params {
					if p.Default != nil {
						c.errorf(p, "interface-fun", "an interface's fun has no default values (spec.md 3.4): state %s at each call, or add a top-level fun that does", p.Name)
					}
				}
			}
			c.info.Funcs[m] = f
			c.signature(f, m, td)
			for i, p := range f.Params {
				variance(m.Params[i], p.Type, false, m.Name)
			}
			variance(m, f.Result, true, m.Name)
			td.Methods = append(td.Methods, f)
			c.later(&c.bodies, func() { c.body(f, m) })
		}
	}
	if d.Kind == "data class" || d.Kind == "data object" {
		c.later(&c.bodies, func() { c.conform(td) })
	}
}

// conform checks that a data class or a data object implements every
// member of its interfaces, and overrides nothing else.
func (c *checker) conform(td *TypeDecl) {
	c.open(td.Params)
	defer c.pop()
	if td.Ctor != nil {
		c.owner = td.Ctor
	}
	for _, p := range td.Decl.Params {
		if p.Default != nil {
			c.value(p.Default, c.info.Vars[p][0].Type)
		}
	}
	used := map[Object]bool{}
	for _, s := range td.Supers {
		b := bind(s.Decl.Params, s.Args)
		for _, p := range s.Decl.Props {
			want := subst(p.Type, b)
			i := slices.IndexFunc(td.Props, func(v *Var) bool {
				return v.Name == p.Name && v.Decl.(*syntax.Param).Override && fits(v.Type, want)
			})
			if i < 0 {
				fix := fmt.Sprintf("give its constructor override val %s: %s", p.Name, want)
				if td.Kind == "data object" {
					fix = fmt.Sprintf("a data object has no constructor to give val %s: make %s a data class, or %s a fun in %s", p.Name, td.Name, p.Name, s.Decl.Name)
				}
				c.errorf(td.Decl, "override", "%s implements %s: %s", td.Name, s, fix)
				continue
			}
			used[td.Props[i]] = true
		}
		for _, m := range s.Decl.Methods {
			want := describe(m, b)
			i := slices.IndexFunc(td.Methods, func(f *Func) bool { return describe(f, nil) == want })
			if j := slices.IndexFunc(td.Methods, func(f *Func) bool { return f.Name == m.Name }); i < 0 && j >= 0 {
				c.errorf(td.Methods[j].Decl, "override", "this does not match %s: write override %s", s, want)
				i = j
			} else if i < 0 {
				c.errorf(td.Decl, "override", "%s implements %s: give it override %s", td.Name, s, want)
				continue
			}
			used[td.Methods[i]] = true
		}
	}
	for _, v := range td.Props {
		if v.Decl.(*syntax.Param).Override && !used[v] {
			c.errorf(v.Decl, "override", "no interface of %s declares a val %s of this type: remove override", td.Name, v.Name)
		}
	}
	for _, f := range td.Methods {
		if !used[f] {
			c.errorf(f.Decl, "override", "no interface of %s declares %s: a data class holds only its interfaces' funs", td.Name, describe(f, nil))
		}
	}
}

// misplaced gives an out type parameter that t has where a value is
// taken rather than given back, or nil. out says which t is in: true
// for a result or a val, false for a parameter.
func misplaced(t Type, out bool) *TypeParam {
	switch t := t.(type) {
	case *TypeParam:
		if t.Out && !out {
			return t
		}
	case *Nullable:
		return misplaced(t.Elem, out)
	case *Named:
		for i, a := range t.Args {
			p := misplaced(a, out)
			if p == nil && !t.Decl.Params[i].Out {
				p = misplaced(a, !out)
			}
			if p != nil {
				return p
			}
		}
	case *FuncType:
		for _, p := range t.Params {
			if tp := misplaced(p, !out); tp != nil {
				return tp
			}
		}
		return misplaced(t.Result, out)
	}
	return nil
}

// describe prints a function as it is declared, without its receiver
// when it is a member: fun fee(distance: Long): Long.
func describe(f *Func, b binding) string {
	var s strings.Builder
	s.WriteString("fun ")
	if f.Kind == Getter {
		s.Reset()
		s.WriteString("val ")
	}
	s.WriteString(list("<", f.TypeParams[f.owners:], "> "))
	if f.Receiver != nil && f.Owner == nil {
		s.WriteString(f.Receiver.String() + ".")
	}
	s.WriteString(f.Name)
	if f.Kind != Getter {
		var ps []string
		for _, p := range f.Params {
			ps = append(ps, p.Name+": "+subst(p.Type, b).String())
		}
		s.WriteString("(" + strings.Join(ps, ", ") + ")")
	}
	return s.String() + ": " + subst(f.Result, b).String()
}

// typ reads a written type.
func (c *checker) typ(t syntax.Type) Type {
	r := c.typ1(t)
	c.info.Types[t] = r
	return r
}

func (c *checker) typ1(t syntax.Type) Type {
	switch t := t.(type) {
	case *syntax.FuncType:
		if t.Receiver != nil && c.file != c.core {
			return c.errorf(t, "function-receiver", "a function type has no receiver (spec.md 5): take the value as a parameter, or for Raise<E> return an Either<E, A>")
		}
		f := &FuncType{Result: c.typ(t.Result)}
		if t.Receiver != nil {
			f.Receiver = c.typ(t.Receiver)
		}
		for _, p := range t.Params {
			f.Params = append(f.Params, c.typ(p))
		}
		if t.Nullable {
			return nullable(f)
		}
		return f
	case *syntax.NamedType:
		var args []Type
		for _, a := range t.Args {
			args = append(args, c.typ(a))
		}
		r, params := c.named(t, t.Name)
		if r == unknown {
			return r
		}
		if len(args) != len(params) {
			return c.errorf(t, "type-arguments", "%s takes %d type arguments, and %d are written here", t.Name, len(params), len(args))
		}
		r = subst(r, bind(params, args))
		if e := c.raised(r); e != nil && c.file != c.core {
			return c.errorf(t, "raise-receiver", "%s is not written in a program (spec.md 9): return an Either<%s, A> from either<%s, A> { ... }, and bind it where it is called", r, e, e)
		}
		if t.Nullable {
			r = nullable(r)
		}
		return r
	}
	panic("unreachable")
}

// named finds the type called name: the type over its own parameters,
// and those parameters.
func (c *checker) named(at syntax.Node, name string) (Type, []*TypeParam) {
	if tp := c.scope.typeParam(name); tp != nil {
		return tp, nil
	}
	if d, a := c.findType(name); a != nil {
		c.see(at, name, a.vis)
		return c.aliased(a), a.params
	} else if d != nil && d.Kind == "annotation class" {
		return c.errorf(at, "not-a-type", "%s is a marker, not a type: it is written only as @%s and in @file:OptIn(%s::class)", name, name, name), nil
	} else if d != nil {
		c.see(at, name, d.vis)
		return self(d), d.Params
	}
	return c.missing(at, name), nil
}

func (c *checker) findType(name string) (*TypeDecl, *alias) {
	for _, p := range c.homes(name) {
		if a := p.aliases[name]; a != nil {
			return nil, a
		}
		if d := p.types[name]; d != nil {
			return d, nil
		}
	}
	return builtins[name], nil
}

// aliased gives what a typealias names, over the alias's parameters.
func (c *checker) aliased(a *alias) Type {
	if a.state == 0 {
		a.state, a.typ = 1, unknown
		defer func(f *syntax.File, p *pkg, s *scope) { c.file, c.pkg, c.scope = f, p, s }(c.file, c.pkg, c.scope)
		c.enter(a.file, c.pkgs[a.Pkg])
		c.open(a.params)
		a.typ, a.state = c.typ(a.decl.Type), 2
	} else if a.state == 1 {
		defer func(f *syntax.File) { c.file = f }(c.file)
		c.file = a.file
		c.errorf(a.decl, "type-arguments", "typealias %s names itself", a.decl.Name)
	}
	return a.typ
}
