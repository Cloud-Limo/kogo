package text

import (
	"math"
	"strings"
	"testing"
)

// A Double is written as its shortest digits, with no exponent and no
// trailing ".0", as Java's BigDecimal chain writes it.
func TestPlain(t *testing.T) {
	for _, c := range []struct {
		value float64
		want  string
	}{
		{5, "5"},
		{100, "100"},
		{12.5, "12.5"},
		{0.30000000000000004, "0.30000000000000004"},
		{0.0005, "0.0005"},
		{1.5e-7, "0.00000015"},
		{10_000_000, "10000000"},
		{1e22, "1" + strings.Repeat("0", 22)},
		{-2.75, "-2.75"},
		{math.Copysign(0, -1), "0"},
		// Java writes these two as 49 and 99, ending one place later.
		{math.SmallestNonzeroFloat64, "0." + strings.Repeat("0", 323) + "5"},
		{2 * math.SmallestNonzeroFloat64, "0." + strings.Repeat("0", 322) + "1"},
	} {
		if got := Plain(c.value); got != c.want {
			t.Errorf("Plain(%v) = %s, want %s", c.value, got, c.want)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Plain(%v) does not panic", v)
				}
			}()
			Plain(v)
		}()
	}
}
