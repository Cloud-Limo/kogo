package math

import (
	gomath "math"
	"testing"
)

// PI is Kotlin's, and outside a function's domain is NaN, as in
// Kotlin: kotlin/builtins/math.kg compares the rest with Kotlin.
func TestEdges(t *testing.T) {
	if PI != 3.141592653589793 || PI/180.0 != 0.017453292519943295 {
		t.Errorf("PI is %v and PI / 180.0 %v", PI, PI/180.0)
	}
	for _, x := range []float64{Sqrt(-1), Sqrt(gomath.Inf(-1)), Asin(1.0000000000000002), Asin(-2), Sin(gomath.Inf(1)), Cos(gomath.NaN())} {
		if !gomath.IsNaN(x) {
			t.Errorf("%v is not NaN", x)
		}
	}
	if Sqrt(2) != 1.4142135623730951 || Asin(1) != PI/2 || !gomath.Signbit(Sqrt(gomath.Copysign(0, -1))) {
		t.Errorf("sqrt(2) is %v, asin(1) %v", Sqrt(2), Asin(1))
	}
}
