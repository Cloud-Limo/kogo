// Command wire holds the wire's text of a LocalDate and a LocalTime
// (spec.md 15.4) to java.time's parse and toString (kotlin/README.md).
//
//	wire gen                 the texts, one a line: date or time, a tab, the text
//	wire diff <texts> <kotlin> each text the reader and java.time read apart; exits 1 if any
//
// A date java.time reads that the reader refuses is the limit when it
// is outside YYYY-MM-DD, and a difference when it is inside.
package main

import (
	"bufio"
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"strings"

	ktime "github.com/Cloud-Limo/kogo/prelude/kogo/time"
)

// The edges: leap days and the days past a month's end, the first and
// last of each field, and forms java.time reads that the limit refuses.
var edges = map[string][]string{
	"date": {"2028-02-29", "2027-02-29", "2000-02-29", "1900-02-29", "0000-02-29", "2026-02-30", "2026-04-31", "2026-12-31",
		"0000-01-01", "9999-12-31", "2026-13-01", "2026-00-10", "2026-01-00", "2026-2-3", "26-02-03", "2026-02-03T", " 2026-02-03",
		"+2026-02-03", "-0001-01-01", "+10000-01-01", "10000-01-01", "-10000-01-01", "+0000-01-01", "２026-01-01", "2026/01/01", ""},
	"time": {"00:00", "23:59", "10:30:00", "10:30:45", "10:30:45.", "10:30:45.1", "10:30:45.0001", "23:59:59.999999999",
		"10:30:45.1234567890", "24:00", "10:60", "23:59:60", "9:30", "10:3", "10:30:", "10:30.5", "10:30:45,5", " 10:30",
		"10:30Z", "１0:30", "10:30:45.12a", "T10:30", ""},
}

var dateForm = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "gen" {
		gen()
	} else if len(os.Args) == 4 && os.Args[1] == "diff" {
		diff(os.Args[2], os.Args[3])
	} else {
		fmt.Fprintln(os.Stderr, "usage: wire gen | wire diff <texts> <kotlin>")
		os.Exit(2)
	}
}

// gen writes the edges and then texts from a fixed seed, each field
// a little past its range.
func gen() {
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for _, kind := range []string{"date", "time"} {
		for _, e := range edges[kind] {
			fmt.Fprintf(out, "%s\t%s\n", kind, e)
		}
	}
	r := rand.New(rand.NewPCG(3, 4))
	for range 20000 {
		y := []int{r.IntN(10000), 1900, 2000, 2100, 2400, 4 * r.IntN(2500)}[r.IntN(6)]
		fmt.Fprintf(out, "date\t%04d-%02d-%02d\n", y, r.IntN(14), r.IntN(33))
		text := fmt.Sprintf("%02d:%02d", r.IntN(26), r.IntN(62))
		if r.IntN(3) > 0 {
			text += fmt.Sprintf(":%02d", r.IntN(62))
			if r.IntN(2) > 0 {
				text += "." + fmt.Sprintf("%019d", r.Int64())[:r.IntN(11)]
			}
		}
		fmt.Fprintf(out, "time\t%s\n", text)
	}
}

// read is the reader's result for a text: its value as it writes it,
// or refused.
func read(kind, text string) string {
	if kind == "date" {
		if d, ok := ktime.ReadLocalDate(text); ok {
			return d.String()
		}
	} else if t, ok := ktime.ReadLocalTime(text); ok {
		return t.String()
	}
	return "refused"
}

func diff(texts, kotlin string) {
	ins, outs := lines(texts), lines(kotlin)
	if len(ins) != len(outs) {
		fmt.Fprintf(os.Stderr, "%d texts and %d results from Kotlin\n", len(ins), len(outs))
		os.Exit(1)
	}
	same, limit, differ := 0, 0, 0
	for i, in := range ins {
		kind, text, _ := strings.Cut(in, "\t")
		switch ours := read(kind, text); {
		case ours == outs[i]:
			same++
		case ours == "refused" && kind == "date" && !dateForm.MatchString(text):
			limit++
		default:
			differ++
			fmt.Printf("%s %q: the reader gives %s, java.time %s\n", kind, text, ours, outs[i])
		}
	}
	fmt.Printf("wire: %d texts agree, %d dates outside YYYY-MM-DD refused by the limit, %d differ\n", same, limit, differ)
	if differ > 0 {
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
