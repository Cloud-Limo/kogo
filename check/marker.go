package check

import "github.com/Cloud-Limo/kogo/syntax"

// Markers (spec.md section 11). A marker is declared @RequiresOptIn
// annotation class Name; a fun, a val or a constructor that carries
// it may be used only in a file that begins with
// @file:OptIn(Name::class).

func (c *checker) declareMarker(d *syntax.ClassDecl, name string, v vis, taken func(string) bool) {
	if len(d.Mods.Annotations) != 1 || d.Mods.Annotations[0].Name != "RequiresOptIn" {
		c.errorf(d, "marker", "a marker is declared with @RequiresOptIn and nothing else: @RequiresOptIn annotation class %s", d.Name)
	}
	td := &TypeDecl{Kind: d.Kind, Name: name, Decl: d, vis: v}
	c.info.Classes[d] = td
	if !taken(name) {
		c.pkg.types[name] = td
	}
}

// marks reads the markers a declaration carries; a binding's @go is
// the emitter's to read.
func (c *checker) marks(m syntax.Modifiers) []*TypeDecl {
	var out []*TypeDecl
	for _, a := range m.Annotations {
		if a.Name == "go" {
			continue
		}
		if d := c.marker(a, a.Name); d != nil {
			out = append(out, d)
		}
	}
	return out
}

// marker finds the marker called name.
func (c *checker) marker(at syntax.Node, name string) *TypeDecl {
	d, _ := c.findType(name)
	switch {
	case d == nil && name == "RequiresOptIn":
		c.errorf(at, "marker", "@RequiresOptIn declares a marker, and goes only on one: @RequiresOptIn annotation class Name")
	case d == nil:
		c.missing(at, name)
	case d.Kind != "annotation class":
		c.errorf(at, "marker", "%s is not a marker: a marker is declared @RequiresOptIn annotation class %s", name, name)
	default:
		c.see(at, name, d.vis)
		return d
	}
	return nil
}

// optIn reads the markers a file opts in to.
func (c *checker) optIn(f *syntax.File) {
	c.optIns[f] = map[*TypeDecl]bool{}
	for _, a := range f.OptIn {
		for _, arg := range a.Args {
			ref := arg.(*syntax.ClassRef) // the parser takes nothing else
			if d := c.marker(ref, ref.Name); d != nil {
				c.optIns[f][d] = true
				c.info.Uses[ref] = d
			}
		}
	}
}

// demand reports a use of a marked declaration in a file that does not
// opt in to each of its markers.
func (c *checker) demand(at syntax.Node, name string, v vis) {
	for _, m := range v.markers {
		if !c.optIns[c.file][m] {
			c.errorf(at, "opt-in", "%s is marked @%s: begin this file with @file:OptIn(%s::class)", name, m.Name, m.Name)
		}
	}
}

// builds reports a value of the data class d made other than by
// calling its constructor, by copy or from a sql row, in a file that
// could not call it (spec.md 3.1, 10.3 and 11).
func (c *checker) builds(at syntax.Node, d *TypeDecl) {
	c.see(at, d.Name+"'s constructor", d.Ctor.vis)
	c.demand(at, d.Name, d.Ctor.vis)
}
