// Package text is the Go behind Kogo's package kogo.text, in
// prelude/text.kg.
package text

import (
	"math"
	"strconv"

	"github.com/Cloud-Limo/kogo/rt"
)

// Plain is plain(value), the text of Java's
// BigDecimal.valueOf(value).stripTrailingZeros().toPlainString().
// Negative zero is "0", as Java's is. NaN and an infinity have no
// digits: they panic, where Java throws. Eight Doubles, 1, 2, 10, 12,
// 14, 16, 18 and 20 times the smallest, end in other digits than
// Java's: Java writes at least two significant digits, the closest
// two, so where the shortest digits are one it can write two others.
// The smallest is 5 here and 49 in Java; twice it is 1 here and 99.
func Plain(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		panic("plain: " + rt.ToString(value) + " has no digits")
	}
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
