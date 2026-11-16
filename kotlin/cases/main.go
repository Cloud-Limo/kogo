// Command cases writes the cases of the check against Kotlin and
// compares the two drivers' results (kotlin/README.md).
//
//	cases gen <dir>                    the cases, for each case name in dir's .kg files
//	cases diff <cases> <kogo> <kotlin> each case whose two results differ; exits 1 if any
package main

import (
	"bufio"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The edges: empty, one element, negatives, the largest values, and
// in text a character outside the BMP and what Kotlin and Go treat
// apart (whitespace, case, digits, a replacement's $ and \).
var (
	lists = [][]int64{{}, {1}, {-1}, {0, 0}, {1, 2, 3}, {math.MaxInt64}, {math.MinInt64, math.MaxInt64, -1}, {3, 1, 3, 2, 1}}
	texts = []string{"", "a", " ", "😀", "\t a b\n", "a😀b", "abcabc", "$1", `\`, "٣4", "ABC é", "  x\u001f", "İΣ", "aaa", "$", "bb c"}
	ints  = []int64{0, 1, -1, 2, 3, math.MaxInt32, math.MinInt32, 7}
	longs = []int64{0, 1, -1, math.MaxInt64, math.MinInt64, math.MaxInt32 + 1, 1 << 53, 10}
	reals = []float64{0, math.Copysign(0, -1), 1.5, -1.5, math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat64,
		math.SmallestNonzeroFloat64, 1 << 31, -(1 << 31) - 0.5, 1 << 63, 1e7, 0.001, 0.0001, 123456789.125, 1e21, 0.1}
	letters = []rune("abcABC 019\t$\\é😀٣ ")
)

// kept are the differences Kogo means to keep, or that wait on a
// ruling: a case that differs where either result holds in is counted
// under why and does not fail the check.
var kept = []struct{ in, why string }{
	{"\uFFFD", "spec.md section 3: a String never holds half of a surrogate pair"},
	{"i\u0307", "spec.md section 12: lowercase maps one character at a time"},
	{"ς", "spec.md section 12: lowercase maps one character at a time"},
	{"Empty list doesn't contain element", "spec.md section 12: a bug's message is not always Kotlin's"},
	{"Illegal group reference", "spec.md section 12: a bug's message is not always Kotlin's"},
	{"No group ", "spec.md section 12: a bug's message is not always Kotlin's"},
	{"4.9E-324", "not ruled: the smallest Double writes as 5.0E-324, Kotlin's as 4.9E-324"},
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "gen" {
		gen(os.Args[2])
	} else if len(os.Args) == 5 && os.Args[1] == "diff" {
		diff(os.Args[2], os.Args[3], os.Args[4])
	} else {
		fmt.Fprintln(os.Stderr, "usage: cases gen <dir> | cases diff <cases> <kogo> <kotlin>")
		os.Exit(2)
	}
}

// gen writes one case a line: its name, then the ten inputs every case
// is given, tab-separated. A list is its numbers, a text its code
// points and a Double its bits, so each driver reads them alike. The
// first inputs walk the edges; the rest mix them with random values,
// from a fixed seed.
func gen(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.kg"))
	var names []string
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			panic(err)
		}
		for _, m := range regexp.MustCompile(`(?m)^ +"(\w+)" ->`).FindAllSubmatch(src, -1) {
			names = append(names, string(m[1]))
		}
	}
	r := rand.New(rand.NewPCG(1, 2))
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for i := range 250 {
		pick := func(k, edges, random int) int { // an edge's index, or -1 for a random value
			if i < 40 {
				return (i*k + i/edges) % edges
			} else if r.IntN(3) == 0 {
				return r.IntN(edges)
			}
			return -1
		}
		list := func(k int) (xs []int64) {
			if e := pick(k, len(lists), 0); e >= 0 {
				return lists[e]
			}
			for range r.IntN(7) {
				xs = append(xs, int64(r.IntN(9)-3))
			}
			return xs
		}
		text := func(k int) (cs []int64) {
			s := []rune(texts[max(0, pick(k, len(texts), 0))])
			if len(s) == 0 || r.IntN(2) == 0 && i >= 40 {
				for range r.IntN(9) {
					s = append(s, letters[r.IntN(len(letters))])
				}
			}
			for _, c := range s {
				cs = append(cs, int64(c))
			}
			return cs
		}
		num := func(k int, edges []int64, random func() int64) int64 {
			if e := pick(k, len(edges), 0); e >= 0 {
				return edges[e]
			}
			return random()
		}
		small := func() int64 { return int64(r.IntN(10) - 2) }
		d := r.NormFloat64() * math.Pow(10, float64(r.IntN(40)-20))
		if e := pick(1, len(reals), 0); e >= 0 {
			d = reals[e]
		}
		in := fmt.Sprint(join(list(1)), "\t", join(list(3)), "\t", join(text(1)), "\t", join(text(5)), "\t",
			num(1, ints, small), "\t", num(3, ints, small), "\t", num(1, longs, r.Int64), "\t", num(5, longs, small), "\t",
			int64(math.Float64bits(d)))
		for _, name := range names {
			fmt.Fprintln(out, name+"\t"+in)
		}
	}
}

func join(xs []int64) string { return strings.Trim(strings.ReplaceAll(fmt.Sprint(xs), " ", ","), "[]") }

// diff pairs the drivers' lines, case by case, and prints each case
// whose results differ, up to five a name, then the counts. A kept
// difference is only counted.
func diff(cases, kogo, kotlin string) {
	c, a, b := lines(cases), lines(kogo), lines(kotlin)
	if len(a) != len(c) || len(b) != len(c) {
		fmt.Printf("%d cases, but %d results from Kogo and %d from Kotlin\n", len(c), len(a), len(b))
		os.Exit(1)
	}
	differ, names := map[string]int{}, map[string]bool{}
	total := 0
	for i := range c {
		name, _, _ := strings.Cut(c[i], "\t")
		names[name] = true
		if a[i] == b[i] {
			continue
		}
		if i := slices.IndexFunc(kept, func(k struct{ in, why string }) bool { return strings.Contains(a[i]+b[i], k.in) }); i >= 0 {
			differ["kept, "+kept[i].why]++
			continue
		}
		total++
		if differ[name]++; differ[name] <= 5 {
			fmt.Printf("%s\n  kogo   %s\n  kotlin %s\n", c[i], a[i], b[i])
		}
	}
	for name, n := range differ {
		fmt.Printf("%s: %d differ\n", name, n)
	}
	fmt.Printf("%d cases of %d names, %d differ\n", len(c), len(names), total)
	if total > 0 {
		os.Exit(1)
	}
}

func lines(file string) []string {
	text, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
}
