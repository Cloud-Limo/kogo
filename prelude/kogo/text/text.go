// Package text is the Go behind Kogo's package kogo.text, in
// prelude/text.kg.
package text

import (
	"math"
	"strconv"

	"kogo/rt"
)

// Plain is plain(value), the text of Java's
// BigDecimal.valueOf(value).stripTrailingZeros().toPlainString().
// Negative zero is "0", as Java's is. NaN and an infinity have no
// digits: they panic, where Java throws. The smallest Double ends in 5
// where Java writes 49, since Go's digits are the shortest that read
// back and Java's are not.
func Plain(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		panic("plain: " + rt.ToString(value) + " has no digits")
	}
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
