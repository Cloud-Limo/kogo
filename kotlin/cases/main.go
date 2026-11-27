// Command cases writes the cases of the check against Kotlin and
// compares the two drivers' results (kotlin/README.md).
//
//	cases gen <dir>                    the cases, for each case name in dir's .kg files
//	cases diff <cases> <kogo> <kotlin> each case whose two results differ; exits 1 if any
package main

import (
	"bufio"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The edges: empty, one element, negatives, the largest values, and
// in text a character outside the BMP and what Kotlin and Go treat
// apart (whitespace, case, digits, a replacement's $ and \); in a
// Double, rounding's ties, 2^52 and above, and asin's domain.
var (
	lists = [][]int64{{}, {1}, {-1}, {0, 0}, {1, 2, 3}, {math.MaxInt64}, {math.MinInt64, math.MaxInt64, -1}, {3, 1, 3, 2, 1}}
	texts = []string{"", "a", " ", "😀", "\t a b\n", "a😀b", "abcabc", "$1", `\`, "٣4", "ABC é", "  x\u001f", "İΣ", "aaa", "$", "bb c", "\u001f", "\u0085", "$9", "${c}$10", "aİ"}
	ints  = []int64{0, 1, -1, 2, 3, math.MaxInt32, math.MinInt32, 7}
	longs = []int64{0, 1, -1, math.MaxInt64, math.MinInt64, math.MaxInt32 + 1, 1 << 53, 10}
	reals = []float64{0, math.Copysign(0, -1), 1.5, -1.5, math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat64,
		1 << 31, -(1 << 31) - 0.5, 1 << 63, 1e7, 0.001, 0.0001, 123456789.125, 1e21, 0.1,
		1 * tiny, 2 * tiny, 10 * tiny, 12 * tiny, 14 * tiny, 16 * tiny, 18 * tiny, 20 * tiny,
		-1 * tiny, -2 * tiny, -10 * tiny, -12 * tiny, -14 * tiny, -16 * tiny, -18 * tiny, -20 * tiny,
		0.5, 2.5, 0.49999999999999994, 1, 1.0000000000000002, 1 << 52, 1<<52 + 1, 1<<52 - 0.5, 1<<53 + 2, 1<<63 - 1024}
	tiny    = math.SmallestNonzeroFloat64 // the eight below 1.0E-322 whose shortest digits are one
	letters = []rune("abcABC 019\t$\\éÉ😀٣ {}")
)

// kept are the differences Kogo means to keep: in a case whose name
// matches names, both results are rewritten, from to to, and a case
// that then agrees is counted under why and does not fail the check.
// A to that begins with % writes each match as that verb writes the
// number it reads: %.10g rounds a Double to ten digits.
var kept = []struct{ names, from, to, why string }{
	{"substringPlus", `[\x{10000}-\x{10FFFF}]`, "\uFFFD\uFFFD", "spec.md section 3: a String never holds half of a surrogate pair"},
	{"lowercase|lowercaseContains", "ς", "σ", "spec.md section 12: lowercase keeps σ at a word's end"},
	{"lowercaseContains", `^(?:true|false) (.*σ.*)$`, "_ $1", "spec.md section 12: lowercase keeps σ at a word's end"},
	{"get", `Empty list doesn't contain element at index (-?\d+)\.`, "Index $1 out of bounds for length 0", "spec.md section 12: an index into Kotlin's empty list"},
	{"replace", `[Ii]llegal group reference.*`, "illegal group reference", "spec.md section 12: replace with a bad group"},
	{"replace", `No group (\d)`, "no group $1", "spec.md section 12: replace with a bad group"},
	{"parseOrNull32", `[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}`, "null", "spec.md section 12: Uuid.parseOrNull of 32 hex digits"},
	{"hSortCalls", `^(\[[^\]]*\]) .*$`, "$1", "spec.md section 12: how often and in what order sortedBy calls its selector"},
	{"replace", `(no group in ".*" at \d+|No group with name \{.*\}|named capturing group .*)$`, "no such named group", "spec.md section 12: replace with a bad group"},
	{"sin|cos|asin|greatCircle", `-?\d+\.\d+(E-?\d+)?`, "%.10g", "spec.md section 12: kogo.math's sin, cos and asin are Go's"},
}

// refused are Kogo's limits (doc/design.md, principle 3): in a case
// whose name matches names, where the same inputs' case input gives
// Kotlin an ID that matches id, Kogo's result must be exactly limit.
// Such a case is counted under why and does not fail the check; a
// limit on any other input is a difference.
var refused = []struct{ names, input, id, limit, why string }{
	{"atZone|plusDays|atZoneEra|atStartOfDay|dateParts|hourMinute", "zoneId", offsetID, zoneLimit, "spec.md section 12: ZoneId.of takes a region name"},
	{"offsetOf", "offsetId", offsetID, zoneLimit, "spec.md section 12: ZoneId.of takes a region name"},
}

// offsetID is java.time's rule for an ID that ZoneId.of reads as an
// offset: Z, a sign, or UTC, GMT or UT and then a sign.
const (
	offsetID  = `^(Z$|[+-]|(UTC|GMT|UT)[+-])`
	zoneLimit = "panic: ZoneId.of takes a region name, such as Australia/Melbourne; offsets are not supported (spec 12)"
)

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
			if i < max(40, len(reals)) { // every Double's edge, the longest list of them
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
		xs, ys, s, t := list(1), list(3), text(1), text(3)
		if i >= 40 && r.IntN(4) == 0 { // so that one is inside, or a reordering of, the other
			ys = slices.Clone(xs)
			r.Shuffle(len(ys), func(a, b int) { ys[a], ys[b] = ys[b], ys[a] })
			a := r.IntN(len(s) + 1)
			t = s[a : a+r.IntN(len(s)-a+1)]
		}
		in := fmt.Sprint(join(xs), "\t", join(ys), "\t", join(s), "\t", join(t), "\t",
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
	differ, refusals, names := map[string]int{}, map[string]int{}, map[string]bool{}
	at := map[string]int{} // each case's line, by its name and inputs
	for i := range c {
		at[c[i]] = i
	}
	total := 0
	for i := range c {
		name, in, _ := strings.Cut(c[i], "\t")
		names[name] = true
		if a[i] == b[i] {
			continue
		}
		if r := refusal(name, a[i], func(input string) string {
			if j, ok := at[input+"\t"+in]; ok {
				return b[j]
			}
			return ""
		}); r != "" {
			refusals[r]++
			continue
		}
		x, y, why := a[i], b[i], ""
		for _, k := range kept {
			if re := regexp.MustCompile(k.from); regexp.MustCompile("^("+k.names+")$").MatchString(name) && re.MatchString(x+y) {
				x, y, why = rewrite(re, x, k.to), rewrite(re, y, k.to), k.why
			}
		}
		if x == y {
			differ["kept, "+why]++
			continue
		}
		total++
		if differ[name]++; differ[name] <= 5 {
			fmt.Printf("%s\n  kogo   %s\n  kotlin %s\n", c[i], a[i], b[i])
		}
	}
	for _, name := range slices.Sorted(maps.Keys(differ)) {
		n := differ[name]
		fmt.Printf("%s: %d differ\n", name, n)
	}
	for _, why := range slices.Sorted(maps.Keys(refusals)) {
		fmt.Printf("refused, %s: %d cases\n", why, refusals[why])
	}
	fmt.Printf("%d cases of %d names, %d differ\n", len(c), len(names), total)
	if total > 0 {
		os.Exit(1)
	}
}

// rewrite replaces each match of re in s by to, or when to begins
// with %, by the number it reads as to writes it.
func rewrite(re *regexp.Regexp, s, to string) string {
	if !strings.HasPrefix(to, "%") {
		return re.ReplaceAllString(s, to)
	}
	return re.ReplaceAllStringFunc(s, func(m string) string {
		f, err := strconv.ParseFloat(m, 64)
		if err != nil {
			panic(err)
		}
		return fmt.Sprintf(to, f)
	})
}

// refusal is why Kogo's result kogo is a limit of refused's, or "".
// kotlin gives Kotlin's result of another case on the same inputs.
func refusal(name, kogo string, kotlin func(input string) string) string {
	for _, r := range refused {
		if regexp.MustCompile("^("+r.names+")$").MatchString(name) && kogo == r.limit &&
			regexp.MustCompile(r.id).MatchString(kotlin(r.input)) {
			return r.why
		}
	}
	return ""
}

func lines(file string) []string {
	text, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
}
