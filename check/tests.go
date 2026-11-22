package check

import "github.com/Cloud-Limo/kogo/syntax"

// Tests (spec.md 16): a tests list names top-level vals of its own
// file, each a Test<E> of the list's one E.

// A TestList is a tests list as the checker read it.
type TestList struct {
	Env   Type   // the environment every test of the list takes
	Tests []*Var // in the list's order
}

// testLists reads each tests list, before any signature is read: a
// val that a list names and that states no type is a Test of the
// list's environment, which is how its value is then read.
func (c *checker) testLists(program []*syntax.File) {
	c.tested = map[*syntax.ValDecl]Type{}
	for _, f := range program {
		c.enter(f, c.pkgOf(f.Package.Name))
		own := map[string]*syntax.ValDecl{}
		for _, d := range f.Decls {
			if v, ok := d.(*syntax.ValDecl); ok && v.Receiver == nil && v.Destructure == nil {
				own[v.Name] = v
			}
		}
		for _, v := range own { // a test that states its type and stands in no list
			c.later(&c.sigs, func() {
				g := c.info.Vars[v][0]
				if n, ok := g.Type.(*Named); ok && n.Decl.Pkg == "kogo.test" && n.Decl.Name == "Test" && c.tested[v] == nil {
					c.errorf(v, "test", "%s is a test that no tests list of this file names, so kogo test would never run it: add it to the list (spec.md 16.1)", v.Name)
				}
			})
		}
		for _, d := range f.Tests {
			l := &TestList{Env: c.typ(d.Env)}
			c.info.Tests[d] = l
			var test *TypeDecl
			if p := c.pkgs["kogo.test"]; p != nil {
				test = p.types["Test"]
			}
			if !f.IsTest() || test == nil {
				c.errorf(d, "test-file", "tests %s is in %s: a tests list stands in a file named _test.kg, which kogo emit leaves out and kogo test runs (spec.md 16)", d.Name, f.Name)
				continue
			}
			if len(d.Vals) == 0 {
				c.errorf(d, "test", "tests %s names no test: name its tests, as tests %s<%s> { a, b } (spec.md 16.1)", d.Name, d.Name, l.Env)
			}
			want := &Named{test, []Type{l.Env}}
			for _, id := range d.Vals {
				v := own[id.Name]
				switch g := c.val(c.pkg, id.Name); {
				case v == nil || g == nil || g.Decl != v:
					c.errorf(id, "test", "%s is no top-level val of this file: a tests list names the tests of its own file, each as val %s = test(\"what it shows\") { tx, env -> ... } (spec.md 16.1)", id.Name, id.Name)
				case c.tested[v] != nil:
					c.errorf(id, "duplicate", "%s is already in a tests list, and kogo test would run it twice: name it once", id.Name)
				default:
					c.tested[v], c.info.Uses[id] = want, g
					l.Tests = append(l.Tests, g)
					c.later(&c.sigs, func() { // once the val's own type is read
						if !fits(g.Type, want) || !fits(want, g.Type) {
							c.errorf(id, "test", "%s is a %s, and tests %s runs a %s: leave its type out, as val %s = test(\"...\") { tx, env -> ... } (spec.md 16.1)", id.Name, g.Type, d.Name, want, id.Name)
						}
					})
				}
			}
		}
	}
}
