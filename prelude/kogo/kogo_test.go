package kogo

import (
	"math"
	"regexp"
	"slices"
	"testing"
	"time"
	"unicode"

	"kogo/rt"
)

// A String counts UTF-16 units, as Kotlin's does, and an operation
// that would leave half of a surrogate pair leaves U+FFFD (spec.md 3).
func TestStringUnits(t *testing.T) {
	if n := String_Length("a😀é"); n != 4 {
		t.Errorf(`"a😀é".length is %d, want 4`, n)
	}
	for _, c := range [][2]string{
		{String_Substring_Int_Int("a😀b", 0, 2), "a�"},
		{String_Substring_Int_Int("a😀b", 1, 3), "😀"},
		{String_Substring_Int("a😀b", 2), "�b"},
		{String_Substring_Int("a😀b", 4), ""},
		{rt.ToString(String_Map("😀", func(c rt.Char) rt.Char { return c })), "[�, �]"},
	} {
		if c[0] != c[1] {
			t.Errorf("got %q, want %q", c[0], c[1])
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("a substring past the end does not panic")
		}
	}()
	String_Substring_Int("ab", 3)
}

// Where Kotlin and Go differ, the prelude follows Kotlin.
func TestAsKotlin(t *testing.T) {
	// U+0085 is whitespace to Go and not to Kotlin; U+001F the other way.
	if got := String_Trim("\u001F  a b\t\n\u0085"); got != "a b\t\n\u0085" {
		t.Errorf("trim gives %q", got)
	}
	for _, c := range []struct {
		d    float64
		want int64
	}{{2.9, 2}, {-2.9, -2}, {math.NaN(), 0}, {1e300, math.MaxInt64}, {math.Inf(-1), math.MinInt64}} {
		if got := Double_ToLong(c.d); got != c.want {
			t.Errorf("%v.toLong() is %d, want %d", c.d, got, c.want)
		}
	}
	// Decimal digits come in runs of ten from a zero, which is what
	// digitToInt counts on.
	for _, r := range unicode.Nd.R16 {
		if r.Stride != 1 || (r.Hi-r.Lo+1)%10 != 0 {
			t.Errorf("the digits %U to %U are not runs of ten", r.Lo, r.Hi)
		}
	}
	if Char_DigitToInt('7') != 7 || Char_DigitToInt('٣') != 3 {
		t.Error("digitToInt misreads 7 or the Arabic-Indic 3")
	}
	re := Regex_String("[0-9]{2}|a")
	if !Regex_Matches(re, "12") || Regex_Matches(re, "123") || Regex_Matches(re, "ab") || !rt.Equal(re, Regex_String("[0-9]{2}|a")) {
		t.Error("matches is of the whole input, and two of one pattern are equal")
	}
}

func TestLists(t *testing.T) {
	xs := ListOf[int64](3, 4, 5)
	odd := List_Filter(xs, func(x int64) bool { return x%2 == 1 })
	indexed := List_MapIndexed(xs, func(i int32, x int64) int64 { return int64(i) * x })
	pairs := List_Zip(xs, ListOf("a", "b"))
	if !slices.Equal(odd, []int64{3, 5}) || !slices.Equal(indexed, []int64{0, 4, 10}) || rt.ToString(pairs) != "[(3, a), (4, b)]" {
		t.Errorf("filter %v, mapIndexed %v, zip %v", odd, indexed, rt.ToString(pairs))
	}
	if List_Sum(xs) != 12 || ListT_SumOf_TToInt(pairs, func(p Pair[int64, string]) int32 { return String_Length(p.Second) }) != 2 {
		t.Error("sum or sumOf is wrong")
	}
	wide := List_widen(xs, func(x int64) *int64 { return &x })
	if xs[0] = 9; *wide[0] != 3 || *wide[2] != 5 || rt.ToString(Pair_widen(pairs[0], rt.Ptr[int64], rt.Ptr[string])) != "(3, a)" {
		t.Error("a widened list is not a copy, or a widened pair is not the pair")
	}
	if List_Size(xs) != 3 {
		t.Error("size is wrong")
	}
	if got := T_TakeIf("a", String_IsNotEmpty); got == nil || T_TakeIf("", String_IsNotEmpty) != nil || T_Let(2, func(n int) int { return n * 2 }) != 4 {
		t.Error("takeIf or let is wrong")
	}
}

// An id is written as a UUID is, sorts after one made earlier, and is
// equal to itself and to no other.
func TestIds(t *testing.T) {
	a := NewId()
	time.Sleep(2 * time.Millisecond)
	b := NewId()
	if s := rt.ToString(a); !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(s) || s >= rt.ToString(b) {
		t.Errorf("%s then %s", a, b)
	}
	if !rt.Equal(a, a) || rt.Equal(a, b) {
		t.Error("an id is equal to itself alone")
	}
	defer func() {
		if recover() != "no" {
			t.Error("a check that fails does not panic with its message")
		}
	}()
	Check(true, func() string { return "yes" })
	Check(false, func() string { return "no" })
}

// The functions on an Either value (spec.md section 9). Go cannot
// read E and A off an Either, so a call writes them, as the emitter
// always does.
func TestEither(t *testing.T) {
	var right Either[string, int32] = A_Right(int32(2))
	var left Either[string, int32] = E_Left("no")
	show := func(e any) string { return rt.ToString(e) }
	double := func(n int32) int32 { return n * 2 }
	for _, c := range [][2]string{
		{show(right), "Right(value=2)"},
		{show(Either_Map[string, int32, int32](right, double)), "Right(value=4)"},
		{show(Either_Map[string, int32, int32](left, double)), "Left(value=no)"},
		{show(Either_MapLeft[string, int32, int32](left, String_Length)), "Left(value=2)"},
		{show(Either_MapLeft[string, int32, int32](right, String_Length)), "Right(value=2)"},
		{show(Either_FlatMap[string, int32, int32](right, func(n int32) Either[string, int32] { return E_Left("gone") })), "Left(value=gone)"},
		{show(Either_Fold[string, int32, int32](left, String_Length, double)), "2"},
		{show(Either_GetOrElse[string, int32](left, String_Length)), "2"},
		{show(Either_GetOrNull[string, int32](right)), "2"},
		{show(Either_GetOrNull[string, int32](left)), "null"},
	} {
		if c[0] != c[1] {
			t.Errorf("got %s, want %s", c[0], c[1])
		}
	}
}
