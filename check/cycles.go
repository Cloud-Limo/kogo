package check

import (
	"fmt"
	"strings"

	"kogo/syntax"
)

// A step is what one file takes from another package: its imports
// of that package, the first one first.
type step struct {
	file     *syntax.File
	imps     []*syntax.Ident
	from, to *pkg
}

// cycles reports each group of the program's packages that import
// each other, directly or through others (spec.md 2), once, at the
// shortest cycle in it. Steps rank in the order the files were given,
// then by line. Of the shortest cycles it tells the one through the
// first-ranked step on any, and from there, at each package, the
// first-ranked step that keeps it shortest, so that a program always
// gets the same error. Only the program's files take steps, so the
// prelude's own packages are on no cycle.
func (c *checker) cycles(program []*syntax.File) {
	// The steps, in rank order, and those into and out of each package.
	var steps []*step
	into, out := map[*pkg][]*step{}, map[*pkg][]*step{}
	for _, f := range program {
		own, byPkg := c.pkgs[f.Package.Name], map[*pkg]*step{}
		for _, imp := range f.Imports {
			i := strings.LastIndexByte(imp.Name, '.')
			if i < 0 {
				continue
			}
			to := c.imports[f][imp.Name[i+1:]]
			if to == nil || to.name != imp.Name[:i] || to == own {
				continue
			}
			if byPkg[to] == nil {
				s := &step{file: f, from: own, to: to}
				byPkg[to], steps = s, append(steps, s)
				into[to], out[own] = append(into[to], s), append(out[own], s)
			}
			byPkg[to].imps = append(byPkg[to].imps, imp)
		}
	}
	// back[p] gives each package that reaches p its least count of
	// steps to p.
	back := map[*pkg]map[*pkg]int{}
	for _, s := range steps {
		if back[s.from] != nil {
			continue
		}
		d := map[*pkg]int{s.from: 0}
		for queue := []*pkg{s.from}; len(queue) > 0; queue = queue[1:] {
			for _, n := range into[queue[0]] {
				if _, seen := d[n.from]; !seen {
					d[n.from] = d[queue[0]] + 1
					queue = append(queue, n.from)
				}
			}
		}
		back[s.from] = d
	}
	// length gives the shortest cycle through s, or 0 if it is on none.
	length := func(s *step) int {
		if d, ok := back[s.from][s.to]; ok {
			return d + 1
		}
		return 0
	}
	done := map[*pkg]bool{}
	for _, first := range steps {
		if length(first) == 0 || done[first.from] {
			continue
		}
		// The group's first-ranked step on a shortest cycle.
		best := first
		for _, s := range steps {
			_, reaches := back[first.from][s.from]
			_, reached := back[s.from][first.from]
			if reaches && reached {
				done[s.from] = true
				if n := length(s); n > 0 && n < length(best) {
					best = s
				}
			}
		}
		d, cycle := back[best.from], []*step{best}
		for at := best.to; at != best.from; {
			for _, n := range out[at] {
				if left, ok := d[n.to]; ok && left == d[at]-1 {
					cycle, at = append(cycle, n), n.to
					break
				}
			}
		}
		var told []string
		for _, n := range cycle {
			var names []string
			for _, imp := range n.imps {
				names = append(names, imp.Name)
			}
			told = append(told, fmt.Sprintf("%s:%d in %s imports %s", n.file.Name, n.imps[0].At().Line, n.from.name, strings.Join(names, ", ")))
		}
		c.enter(best.file, best.from)
		c.errorf(best.imps[0], "import-cycle", "packages import each other: %s; move what one package takes from another into a package both may import",
			strings.Join(told, "; "))
	}
}

// vals refuses a top-level val whose value refers, directly or through
// the vals and funs it refers to, to itself or to a val declared below
// it in its file, and the files of a package whose vals use each other
// (spec.md 6). Kotlin reads such a val before it holds its value, or
// refuses it; Go orders the vals by what they refer to and refuses a
// cycle. A cycle of vals is told once, at its first val; a cycle of
// files at the val that leads out of its first file.
func (c *checker) vals(program []*syntax.File) {
	// The program's top-level vals, in order, and the file of each
	// top-level val and fun: what Kotlin sets up a file's vals to use.
	var vals []*Var
	top := map[Object]*syntax.File{}
	for _, f := range program {
		for _, d := range f.Decls {
			if fn := c.info.Funcs[d]; fn != nil {
				top[fn] = f
			}
			if vs := c.info.Vars[d]; len(vs) > 0 {
				top[vs[0]], vals = f, append(vals, vs[0])
			}
		}
	}
	// A search from each val: the way to each object it reaches, those
	// in the order reached, and the way back to itself, if any.
	way, order, cycle := map[*Var]map[Object]Object{}, map[*Var][]Object{}, map[*Var][]string{}
	for _, g := range vals {
		way[g] = map[Object]Object{g: nil}
		for queue := []Object{g}; len(queue) > 0; queue = queue[1:] {
			for _, r := range c.reads[queue[0]] {
				if _, back := cycle[g]; r == g && !back {
					cycle[g] = trail(way[g], g, queue[0])
				}
				if _, seen := way[g][r]; !seen {
					way[g][r], queue, order[g] = queue[0], append(queue, r), append(order[g], r)
				}
			}
		}
	}
	type link struct {
		g       *Var
		r       Object
		through []string
	}
	links, linked, told := map[*syntax.File][]*link{}, map[[2]*syntax.File]bool{}, map[*Var]bool{}
	for _, g := range vals {
		f := top[g]
		c.file = f
		through, onCycle := cycle[g]
		if onCycle && !told[g] {
			for _, v := range vals {
				_, reached := way[g][v]
				_, back := way[v][g]
				told[v] = told[v] || reached && back
			}
			c.errorf(g.Decl, "val-order", "the value of %s refers to %s itself%s (spec.md 6): compute it without %s, or make one of these a fun", g.Name, g.Name, by(through), g.Name)
		}
		for _, r := range order[g] {
			if v, ok := r.(*Var); ok && v.file == f && v.Decl.At().Offset > g.Decl.At().Offset {
				if _, back := way[v][g]; !back {
					c.errorf(g.Decl, "val-order", "the value of %s refers to %s%s, which is declared below it in this file (spec.md 6): declare %s above %s", g.Name, v.Name, by(trail(way[g], g, way[g][v])), v.Name, g.Name)
					break
				}
			}
		}
		// A val on a cycle is refused already, so uses no file.
		for _, r := range order[g] {
			if to := top[r]; !onCycle && to != nil && to != f && to.Package.Name == f.Package.Name && !linked[[2]*syntax.File{f, to}] {
				linked[[2]*syntax.File{f, to}] = true
				links[f] = append(links[f], &link{g, r, trail(way[g], g, way[g][r])})
			}
		}
	}
	// The shortest cycle of files from each file on none told yet.
	done := map[*syntax.File]bool{}
	for _, f := range program {
		var last *link
		from := map[*syntax.File]*link{f: nil}
		for queue := []*syntax.File{f}; len(queue) > 0 && last == nil && !done[f]; queue = queue[1:] {
			for _, l := range links[queue[0]] {
				if to := top[l.r]; to == f {
					last = l
					break
				} else if _, seen := from[to]; !seen {
					from[to], queue = l, append(queue, to)
				}
			}
		}
		if last == nil {
			continue
		}
		var files, steps []string
		first := last
		for l := last; l != nil; first, l = l, from[top[l.g]] {
			done[top[l.g]] = true
			files = append([]string{top[l.g].Name}, files...)
			steps = append([]string{fmt.Sprintf("%s refers to %s in %s%s", l.g.Name, name(l.r), top[l.r].Name, by(l.through))}, steps...)
		}
		c.file = f
		c.errorf(first.g.Decl, "val-order", "the vals of %s use each other's files (spec.md 6): %s; Kotlin sets up a file's vals when it first uses the file, so one may be read before it holds its value: move these vals into one file, or make one of them a fun",
			strings.Join(files, ", "), strings.Join(steps, "; "))
	}
}

// trail names the objects on the way from g to o, o last, g not.
func trail(way map[Object]Object, g, o Object) []string {
	var names []string
	for ; o != g; o = way[o] {
		names = append([]string{name(o)}, names...)
	}
	return names
}

func name(o Object) string {
	if f, ok := o.(*Func); ok {
		return f.Name
	}
	return o.(*Var).Name
}

func by(through []string) string {
	if through == nil {
		return ""
	}
	return ", through " + strings.Join(through, ", ")
}
