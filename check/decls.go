package check

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"kogo/syntax"
)

// Check checks the files of a program, the prelude's among them. It
// gives what it learned and the errors it found, file by file in
// source order.
func Check(files []*syntax.File) (*Info, []*syntax.Error) {
	c := &checker{
		info: &Info{
			Types: map[syntax.Node]Type{}, Uses: map[syntax.Node]Object{}, Calls: map[syntax.Node]*Call{},
			Narrowed: map[*syntax.Ident]Type{}, Vars: map[syntax.Node][]*Var{}, Funcs: map[syntax.Node]*Func{},
			Classes: map[*syntax.ClassDecl]*TypeDecl{},
		},
		pkgs: map[string]*pkg{}, members: map[string][]*Func{}, imports: map[*syntax.File]map[string]*pkg{},
	}
	order := map[string]int{}
	for i, f := range files {
		order[f.Name] = i
		c.enter(f, c.pkgOf(f.Package.Name))
		for _, a := range f.OptIn {
			c.notYet(a, "@file:OptIn", 11)
		}
		for _, d := range f.Decls {
			c.declare(d, "")
		}
	}
	for _, f := range files {
		c.enter(f, c.pkgOf(f.Package.Name))
		c.importAll(f)
	}
	for i := 0; i < len(c.sigs); i++ {
		c.sigs[i]()
	}
	for i := 0; i < len(c.bodies); i++ {
		c.bodies[i]()
	}
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

	// Where the checker is.
	file   *syntax.File
	pkg    *pkg
	scope  *scope
	result Type // what the enclosing fun returns
	loops  int  // the loops around this place, inside its fun or lambda
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

func (c *checker) enter(f *syntax.File, p *pkg) { c.file, c.pkg, c.scope = f, p, &scope{} }

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

func (c *checker) notYet(at syntax.Node, what string, section int) Type {
	return c.errorf(at, "not-yet-checked", "%s belongs to spec.md section %d, which is not checked yet", what, section)
}

// The names of sections 9 and 10 that a program can write without
// their declarations existing yet.
var later = map[string]int{
	"Either": 9, "Raise": 9, "either": 9, "raise": 9, "ensure": 9, "ensureNotNull": 9, "bind": 9,
	"withError": 9, "left": 9, "right": 9, "Tx": 10, "Db": 10,
}

// Kotlin's scope functions that section 6 leaves out.
var notKogo = map[string]string{
	"apply": "name the value and call it", "run": "name the value and call it", "with": "name the value and call it",
	"takeUnless": "use takeIf and turn the test around",
}

// missing reports a name that nothing in scope declares.
func (c *checker) missing(at syntax.Node, name string) Type {
	if s := later[name]; s != 0 {
		return c.notYet(at, name, s)
	}
	if fix := notKogo[name]; fix != "" {
		return c.errorf(at, "kotlin-scope-function", "%s is not Kogo: %s", name, fix)
	}
	var homes []string
	for _, p := range c.pkgs {
		if p.named(name) != nil {
			homes = append(homes, p.name+"."+name)
		}
	}
	if slices.Sort(homes); homes != nil {
		return c.errorf(at, "unknown-name", "%s is not in scope: add import %s", name, strings.Join(homes, " or import "))
	}
	return c.errorf(at, "unknown-name", "nothing named %s is declared here, in package %s or in the prelude", name, c.pkg.name)
}

// visible reports whether the file at hand may name a declaration.
func (c *checker) visible(v vis) bool {
	return v.visibility == "" || v.visibility == "private" && v.file == c.file || v.visibility == "internal" && v.Pkg == c.pkg.name
}

// see reports a use of a declaration that the file may not name.
func (c *checker) see(at syntax.Node, name string, v vis) {
	switch {
	case c.visible(v):
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

// marks reports the markers among a declaration's annotations; a
// binding's @go is the emitter's to read.
func (c *checker) marks(m syntax.Modifiers) {
	for _, a := range m.Annotations {
		if a.Name != "go" {
			c.notYet(a, "the marker @"+a.Name, 11)
		}
	}
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
		if d.Kind == "annotation class" {
			c.notYet(d, "a marker", 11)
			return
		}
		c.marks(d.Mods)
		v.visibility = d.Mods.Visibility
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
		c.marks(d.Mods)
		v.visibility = d.Mods.Visibility
		f := &Func{Kind: Fun, Name: d.Name, External: d.Mods.External, Decl: d, vis: v}
		c.addFunc(f, d.Receiver != nil)
		c.later(&c.sigs, func() {
			c.signature(f, d, nil)
			c.later(&c.bodies, func() { c.body(f, d) })
		})
	case *syntax.ValDecl:
		c.marks(d.Mods)
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
				g.Type = c.declared(d)
				c.later(&c.bodies, func() {
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
	if d.Type == nil {
		return c.errorf(d, "val-type", "a top-level val states its type: val %s: Type", d.Name)
	}
	return c.typ(d.Type)
}

func (c *checker) getter(f *Func, d *syntax.ValDecl) {
	c.open(f.TypeParams)
	defer c.pop()
	f.Receiver, f.Result = c.typ(d.Receiver), c.declared(d)
	c.later(&c.bodies, func() {
		c.open(f.TypeParams)
		defer c.pop()
		c.define(d, &Var{Kind: Param, Name: "this", Type: f.Receiver})
		switch {
		case d.Getter != nil && d.Value == nil:
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
}

// body checks a fun's default values and its body.
func (c *checker) body(f *Func, d *syntax.FunDecl) {
	c.open(f.TypeParams)
	defer c.pop()
	if f.Receiver != nil {
		c.define(d, &Var{Kind: Param, Name: "this", Type: f.Receiver})
	}
	for i, p := range d.Params {
		if p.Default != nil {
			c.value(p.Default, f.Params[i].Type)
		}
		c.define(p, f.Params[i])
	}
	defer func(r Type, l int) { c.result, c.loops = r, l }(c.result, c.loops)
	c.result, c.loops = f.Result, 0
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
		c.marks(d.Ctor)
		td.Ctor = &Func{Kind: Constructor, Name: td.Name, TypeParams: td.Params, Params: td.Props, Result: self(td), Owner: td, Decl: d, vis: td.vis}
		if d.Ctor.Visibility != "" {
			td.Ctor.visibility = d.Ctor.Visibility
		}
	case "enum class":
		td.Props = []*Var{{Kind: Property, Name: "name", Type: tString, Owner: td}}
		for _, e := range d.Entries {
			td.Entries = append(td.Entries, &Var{Kind: Entry, Name: e.Name, Type: self(td), Owner: td, Decl: e})
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
			td.Props = append(td.Props, v)
			c.info.Vars[m] = []*Var{v}
		case *syntax.FunDecl:
			f := &Func{Kind: Fun, Name: m.Name, External: d.Kind == "external object", Decl: m, vis: td.vis}
			if d.Kind == "interface" || d.Kind == "sealed interface" {
				f.Kind = Method
			}
			c.info.Funcs[m] = f
			c.signature(f, m, td)
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
				c.errorf(td.Decl, "override", "%s implements %s: give its constructor override val %s: %s", td.Name, s, p.Name, want)
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
		if t.Receiver != nil {
			return c.notYet(t, "a function type with a receiver", 9)
		}
		f := &FuncType{Result: c.typ(t.Result)}
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
		if r = subst(r, bind(params, args)); t.Nullable {
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
