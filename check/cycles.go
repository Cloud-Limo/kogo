package check

import (
	"cmp"
	"fmt"
	"slices"
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
// shortest cycle in it. Of cycles that tie in length it takes the one
// whose imports come first by file and line, so that a program always
// gets the same error. The prelude's packages take no part.
func (c *checker) cycles(program []*syntax.File) {
	var steps []*step
	for _, f := range program {
		own, byPkg := c.pkgs[f.Package.Name], map[*pkg]*step{}
		for _, imp := range f.Imports {
			i := strings.LastIndexByte(imp.Name, '.')
			if i < 0 {
				continue
			}
			to := c.imports[f][imp.Name[i+1:]]
			if to == nil || to.name != imp.Name[:i] || to == own || to.prelude {
				continue
			}
			if byPkg[to] == nil {
				byPkg[to] = &step{file: f, from: own, to: to}
				steps = append(steps, byPkg[to])
			}
			byPkg[to].imps = append(byPkg[to].imps, imp)
		}
	}
	// A step's rank is its place by file and line.
	slices.SortFunc(steps, func(a, b *step) int {
		return cmp.Or(strings.Compare(a.file.Name, b.file.Name), cmp.Compare(a.imps[0].At().Line, b.imps[0].At().Line))
	})
	// dist gives each package's least count of steps to target, over
	// the steps ranked after skip.
	dist := func(target *pkg, skip int) map[*pkg]int {
		d := map[*pkg]int{target: 0}
		for queue := []*pkg{target}; len(queue) > 0; queue = queue[1:] {
			for _, s := range steps[skip:] {
				if _, seen := d[s.from]; !seen && s.to == queue[0] {
					d[s.from] = d[queue[0]] + 1
					queue = append(queue, s.from)
				}
			}
		}
		return d
	}
	back := map[*pkg]map[*pkg]int{} // who reaches each package, and in how many steps
	for _, s := range steps {
		if back[s.from] == nil {
			back[s.from] = dist(s.from, 0)
		}
	}
	done := map[*pkg]bool{}
	for _, first := range steps {
		if _, cyclic := back[first.from][first.to]; !cyclic || done[first.from] {
			continue
		}
		// first's group, and the length of its shortest cycle.
		group, size := map[*pkg]bool{}, len(steps)
		for _, s := range steps {
			_, in := back[first.from][s.from]
			_, out := back[s.from][first.from]
			if d, cyclic := back[s.from][s.to]; in && out && cyclic {
				group[s.from], done[s.from], size = true, true, min(size, d+1)
			}
		}
		// The first step, by rank, that closes a cycle of that length
		// through steps ranked after it; then, at each package, the
		// first step that stays on such a cycle.
		for r, s := range steps {
			d := dist(s.from, r+1)
			if n, ok := d[s.to]; !group[s.from] || !ok || n != size-1 {
				continue
			}
			cycle := []*step{s}
			for at := s.to; at != s.from; {
				for _, n := range steps[r+1:] {
					if left, ok := d[n.to]; ok && n.from == at && left == d[at]-1 {
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
			c.enter(s.file, s.from)
			c.errorf(s.imps[0], "import-cycle", "packages import each other: %s; move what one package takes from another into a package both may import",
				strings.Join(told, "; "))
			break
		}
	}
}
