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
// it in its file (spec.md 6). Kotlin reads such a val before it holds
// its value, or refuses it; Go orders the vals by what they refer to
// and refuses a cycle. A cycle is told once, at its first val.
func (c *checker) vals(program []*syntax.File) {
	told := map[Object]bool{}
	for _, f := range program {
		c.file = f
		for _, d := range f.Decls {
			vs := c.info.Vars[d]
			if len(vs) == 0 || told[vs[0]] {
				continue
			}
			g := vs[0]
			// A search from g, keeping the way to each object reached.
			way, queue := map[Object]Object{g: nil}, []Object{g}
		search:
			for len(queue) > 0 {
				o := queue[0]
				queue = queue[1:]
				for _, r := range c.reads[o] {
					if v, ok := r.(*Var); ok && v.file == f && v.Decl.At().Offset >= d.At().Offset {
						c.badRead(g, v, o, way, told)
						break search
					}
					if _, seen := way[r]; !seen {
						way[r], queue = o, append(queue, r)
					}
				}
			}
		}
	}
}

// badRead reports that g's value reaches v, through o and the way to it.
func (c *checker) badRead(g, v *Var, o Object, way map[Object]Object, told map[Object]bool) {
	var through []string
	for ; o != g; o = way[o] {
		if v == g {
			told[o] = true
		}
		if f, ok := o.(*Func); ok {
			through = append([]string{f.Name}, through...)
		} else {
			through = append([]string{o.(*Var).Name}, through...)
		}
	}
	by := ""
	if through != nil {
		by = ", through " + strings.Join(through, ", ")
	}
	if v == g {
		c.errorf(g.Decl, "val-order", "the value of %s refers to %s itself%s (spec.md 6): compute it without %s, or make one of these a fun", g.Name, g.Name, by, g.Name)
	} else {
		c.errorf(g.Decl, "val-order", "the value of %s refers to %s%s, which is declared below it in this file (spec.md 6): declare %s above %s", g.Name, v.Name, by, v.Name, g.Name)
	}
}
