//go:build kotlin

// Command driver is the Go driver of the check against Kotlin
// (kotlin/README.md): it reads one case a line, runs it in the Go
// emitted from the shared source and prints its result, or the panic's
// message. The tag keeps it out of ./..., where that Go does not exist.
package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/Cloud-Limo/kogo/kotlin/_work/builtins"
)

func main() {
	in := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for in.Scan() {
		fmt.Fprintln(out, strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`).Replace(run(strings.Split(in.Text(), "\t"))))
	}
}

func run(c []string) (result string) {
	defer func() {
		if p := recover(); p != nil {
			result = fmt.Sprint("panic: ", p)
		}
	}()
	return builtins.Call(c[0], longs(c[1]), longs(c[2]), text(c[3]), text(c[4]),
		int32(longs(c[5])[0]), int32(longs(c[6])[0]), longs(c[7])[0], longs(c[8])[0], math.Float64frombits(uint64(longs(c[9])[0])))
}

func longs(field string) []int64 {
	out := []int64{}
	for _, n := range strings.FieldsFunc(field, func(r rune) bool { return r == ',' }) {
		x, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			panic(err)
		}
		out = append(out, x)
	}
	return out
}

func text(field string) string {
	var b strings.Builder
	for _, r := range longs(field) {
		b.WriteRune(rune(r))
	}
	return b.String()
}
