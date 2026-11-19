package kogo

import (
	"context"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Cloud-Limo/kogo/rt"
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
	if got := String_Trim_fun("\u001F  a b\t\n\u0085"); got != "a b\t\n\u0085" {
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
	for _, c := range []struct {
		d    float64
		want int32
	}{{2.9, 2}, {-2.9, -2}, {math.NaN(), 0}, {3e9, math.MaxInt32}, {-3e9, math.MinInt32}, {1e30, math.MaxInt32}, {math.Inf(-1), math.MinInt32}} {
		if got := Double_ToInt(c.d); got != c.want {
			t.Errorf("%v.toInt() is %d, want %d", c.d, got, c.want)
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

// An index outside a list panics with Kotlin's message (spec.md
// section 8).
func TestIndexOutOfBounds(t *testing.T) {
	for i, want := range map[int32]string{
		5:  "Index 5 out of bounds for length 3",
		3:  "Index 3 out of bounds for length 3",
		-1: "Index -1 out of bounds for length 3",
	} {
		func() {
			defer func() {
				if p := recover(); p != want {
					t.Errorf("list[%d] panics with %v, want %s", i, p, want)
				}
			}()
			List_Get(ListOf[int32](1, 2, 3), i)
		}()
	}
}

func TestMoreLists(t *testing.T) {
	five, xs := int64(5), make(List[int64], 1, 4)
	if got := ListOfNotNull(nil, &five, nil); !slices.Equal(got, []int64{5}) {
		t.Errorf("listOfNotNull gives %v", got)
	}
	if got := List_Map(ListOf[int64](1, 2), func(x int64) string { return rt.ToString(x * 2) }); !slices.Equal(got, []string{"2", "4"}) {
		t.Errorf("map gives %v", got)
	}
	a, b := List_Plus(xs, ListOf[int64](1)), List_Plus(xs, ListOf[int64](2))
	if !slices.Equal(a, []int64{0, 1}) || !slices.Equal(b, []int64{0, 2}) {
		t.Errorf("one plus wrote over another: %v %v", a, b)
	}
	if List_Plus(List[int64]{}, nil) == nil {
		t.Error("an empty plus an empty is nil, which pgx and JSON write as null")
	}
	if List_Get(a, 1) != 1 || Int_ToLong(-3) != -3 {
		t.Error("get or toLong is wrong")
	}
	if Long_CoerceAtLeast(-1, 0) != 0 || Long_CoerceAtLeast(3, 0) != 3 || Long_CoerceAtMost(9, 4) != 4 || Long_CoerceAtMost(2, 4) != 2 {
		t.Error("coerceAtLeast or coerceAtMost is wrong")
	}
	defer func() {
		if recover() != "no" {
			t.Error("a require that fails does not panic with its message")
		}
	}()
	Require(true, func() string { return "yes" })
	Require(false, func() string { return "no" })
}

// A set holds each key once, in the order first given, finds a key as
// == does, and is equal to a set of the same keys in any order.
func TestSets(t *testing.T) {
	b1, b2 := "B", "B"
	s := SetOf(Pair[string, *string]{"A", &b1}, Pair[string, *string]{"C", nil}, Pair[string, *string]{"A", &b2})
	if got := rt.ToString(s); got != "[(A, B), (C, null)]" {
		t.Errorf("a set's text is %s", got)
	}
	if !Set_Contains(s, Pair[string, *string]{"A", rt.Ptr("B")}) || Set_Contains(s, Pair[string, *string]{"A", nil}) {
		t.Error("contains does not compare as == does")
	}
	ints := SetOf[int32](1, 2, 3)
	for _, c := range []struct {
		a, b any
		want bool
	}{
		{ints, SetOf[int32](3, 1, 2), true},
		{ints, SetOf[int32](1, 2), false},
		{ints, SetOf[int32](1, 2, 4), false},
		{ints, ListOf[int32](1, 2, 3), false},
		{SetOf[int32](), SetOf[int32](), true},
		{Set_widen(ints, rt.Ptr[int32]), ints, true},
	} {
		if got := rt.Equal(c.a, c.b); got != c.want {
			t.Errorf("%s == %s is %v", rt.ToString(c.a), rt.ToString(c.b), got)
		}
	}
	if got := rt.ToString(SetOf[int32]()); got != "[]" {
		t.Errorf("an empty set's text is %s", got)
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

// randomString gives its length in letters and digits, each of the 62
// within 6 deviations of n/62 over 200,000 draws, a new string each
// call, and panics on a negative length (spec.md 12). A byte taken
// modulo 62 makes a to h 5/256 likely, about 4,000 draws against
// 3,226, and a rejection bound of 247 or 249 for 248 moves 9 or a
// about as far; a fair draw fails about once in 10 million runs.
func TestRandomString(t *testing.T) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if s := RandomString(0); s != "" {
		t.Errorf("randomString(0) = %q", s)
	}
	if RandomString(20) == RandomString(20) {
		t.Error("two calls gave the same string")
	}
	seen, n := map[rune]int{}, 200000.0
	for range 10000 {
		s := RandomString(20)
		if len(s) != 20 || strings.Trim(s, alphabet) != "" {
			t.Fatalf("randomString(20) = %q", s)
		}
		for _, c := range s {
			seen[c]++
		}
	}
	for _, c := range alphabet {
		if math.Abs(float64(seen[c])-n/62) > 6*math.Sqrt(n/62*61/62) {
			t.Errorf("%c drawn %d of %.0f", c, seen[c], n)
		}
	}
	defer func() {
		if r := recover(); r != "randomString's length -1 is negative" {
			t.Errorf("randomString(-1) panics with %v", r)
		}
	}()
	RandomString(-1)
}

// Uuid.parseOrNull reads what a template writes, hex of either case, and
// nothing else: anything else is null, never a panic (spec.md 12).
func TestUuidParseOrNull(t *testing.T) {
	want := Uuid{0x01, 0x93, 0x4f, 0x2a, 0xbc, 0xde, 0x7f, 0x01, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67}
	for _, s := range []string{"01934f2a-bcde-7f01-89ab-cdef01234567", "01934F2A-BCDE-7F01-89AB-CDEF01234567"} {
		if u := Uuid_ParseOrNull(s); u == nil || *u != want {
			t.Errorf("Uuid.parseOrNull(%q) = %v", s, u)
		}
	}
	for _, s := range []string{"", "01934f2a-bcde-7f01-89ab-cdef0123456", "01934f2a-bcde-7f01-89ab-cdef012345678",
		"01934f2a0bcde-7f01-89ab-cdef01234567", "01934f2a-bcde07f01-89ab-cdef01234567", "01934f2a-bcde-7f01089ab-cdef01234567",
		"01934f2a-bcde-7f01-89ab0cdef01234567", "01934f2a-bcde-7f01-89ab-cdef0123456g", "{01934f2a-bcde-7f01-89ab-cdef01234567}",
		"01934f2abcde7f0189abcdef01234567"} {
		if u := Uuid_ParseOrNull(s); u != nil {
			t.Errorf("Uuid.parseOrNull(%q) = %v, want null", s, u)
		}
	}
	id := NewId()
	if u := Uuid_ParseOrNull(id.String()); u == nil || *u != id {
		t.Errorf("Uuid.parseOrNull(\"%s\") = %v", id, u)
	}
}

// A raise scope recovers only the failures raised on its own handle: one
// raised on an outer scope's, inside an inner scope of the same E,
// passes the inner one. The checker keeps a program from writing it,
// but db.tx's raise scope may run inside another's.
func TestRaiseScopes(t *testing.T) {
	got := Either_RaiseEToA(func(outer Raise[string]) string {
		inner := Either_RaiseEToA(func(Raise[string]) int32 { Raise_Raise(outer, "outer"); return 1 })
		return "inner gave " + rt.ToString(inner)
	})
	if s := rt.ToString(got); s != "Either.Left(outer)" {
		t.Errorf("got %s, want Either.Left(outer)", s)
	}
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
		{show(right), "Either.Right(2)"},
		{show(Either_Map[string, int32, int32](right, double)), "Either.Right(4)"},
		{show(Either_Map[string, int32, int32](left, double)), "Either.Left(no)"},
		{show(Either_MapLeft[string, int32, int32](left, String_Length)), "Either.Left(2)"},
		{show(Either_MapLeft[string, int32, int32](right, String_Length)), "Either.Right(2)"},
		{show(Either_FlatMap[string, int32, int32](right, func(n int32) Either[string, int32] { return E_Left("gone") })), "Either.Left(gone)"},
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

// What the stage-1 ports call, each giving what Kotlin 2.4 gives.
func TestPortCalls(t *testing.T) {
	re := func(p string) Regex { return Regex_String(p) }
	ws := func(c rt.Char) bool { return Char_IsWhitespace(c) }
	for _, c := range [][2]string{
		{String_Replace("a1b22", re(`([a-z])(\d+)`), `<$2\$1>`), "<1$1><22$1>"},
		{String_Replace("a1b22", re(`(?P<l>[a-z])(\d+)`), `${l}$12`), "aa2bb2"},
		{String_Replace("Heron Draft", re(`\bdraft$`), ""), "Heron Draft"},
		{String_Replace("heron draft", re(`\bdraft$`), "") + "|" + String_Replace("herondraft", re(`\bdraft$`), ""), "heron |herondraft"},
		{String_Trim_Char("--a-b--", '-'), "a-b"},
		{String_Trim_Char("😀", 0xD83D) + String_Trim_Char("😀", 0xDE00), "\uFFFD\uFFFD"},
		{String_IfEmpty("", func() string { return "e" }) + String_IfEmpty("x", func() string { return "e" }), "ex"},
		{String_FilterNot("1 2\t3", ws) + String_FilterNot("a😀b", func(c rt.Char) bool { return c == 'b' }), "123a😀"},
		{rt.ToString(List_SingleOrNull(EmptyList[int32]())) + rt.ToString(List_SingleOrNull(ListOf[int32](1, 2))), "nullnull"},
		{rt.ToString(List_SingleOrNull(ListOf[int32](3))) + rt.ToString(ListT_FirstOrNull_fun(ListOf[int32](4, 5))), "34"},
		{rt.ToString(ListT_FirstOrNull_fun(EmptyList[int32]())), "null"},
		{rt.ToString(List_ToSet(ListOf[int32](2, 1, 2, 3))), "[2, 1, 3]"},
		{rt.ToString(A_To[int32, string](1, "a")), "(1, a)"},
		{rt.ToString(Decimal("1.50")) + " " + rt.ToString(Decimal_ToDouble(Decimal("1.50"))), "1.50 1.5"},
		// Go's regexp skips an empty match just after a match, where
		// Kotlin's gives "-a--c-" (spec.md 12).
		{String_Replace("abc", re("b*"), "-"), "-a-c-"},
	} {
		if c[0] != c[1] {
			t.Errorf("got %q, want %q", c[0], c[1])
		}
	}
	if !String_IsBlank("  \t") || !String_IsBlank("") || String_IsBlank("\u0085") || !String_IsNotBlank("a ") {
		t.Error("isBlank is not Kotlin's: whitespace is Unicode's separators and Java's controls")
	}
	if !String_StartsWith("+61 4", "+") || String_StartsWith("61", "+") || !List_IsNotEmpty(ListOf(1)) || List_IsNotEmpty(EmptyList[int]()) {
		t.Error("startsWith or isNotEmpty")
	}
	if !String_EndsWith("a.test/", "/") || String_EndsWith("a.test", "/") || !String_EndsWith("", "") {
		t.Error("endsWith")
	}
	// A list holds a value equal to one of its elements, as == has it.
	if l := ListOf(rt.Ptr("a"), nil); !List_Contains(l, rt.Ptr("a")) || !List_Contains(l, nil) || List_Contains(l, rt.Ptr("b")) || List_Contains(EmptyList[string](), "") {
		t.Error("contains on a list")
	}
	if EmptyList[int]() == nil || !math.IsInf(Decimal_ToDouble(Decimal("1e400")), 1) {
		t.Error("emptyList is nil, or a numeric past Double is not Infinity")
	}
	if rt.Equal(Decimal("1.50"), Decimal("1.5")) || !rt.Equal(Decimal("1.50"), Decimal("1.50")) {
		t.Error("a Decimal's == is not Java's, which counts the scale")
	}
	list := ListOf[int32](1, 2)
	*ListT_FirstOrNull_fun(list), *List_SingleOrNull(list[1:]) = 8, 9
	*ListT_FirstOrNull_TToBoolean(list, func(x int32) bool { return x == 2 }) = 7
	if !slices.Equal(list, []int32{1, 2}) {
		t.Errorf("firstOrNull or singleOrNull gives a pointer into the list, which now holds %v", list)
	}
	var seen []int32
	List_ForEach(ListOf[int32](1, 2), func(x int32) rt.Unit { seen = append(seen, x); return rt.Unit{} })
	if !slices.Equal(seen, []int32{1, 2}) {
		t.Errorf("forEach saw %v", seen)
	}
	if got := CheckNotNull(rt.Ptr("x"), func() string { return "" }); got != "x" {
		t.Errorf("checkNotNull gives %q", got)
	}
	for _, c := range []struct{ replacement, panic string }{
		{"$9", "no group 9"}, {"$x", "illegal group reference"}, {`x\`, "character to be escaped is missing"},
		{"${y}", "no group"}, {"${l", "no group"},
	} {
		func() {
			defer func() {
				if p, _ := recover().(string); !strings.Contains(strings.ToLower(p), c.panic) {
					t.Errorf("replace with %q panics with %q, want %q", c.replacement, p, c.panic)
				}
			}()
			String_Replace("ab", re("(?P<l>a)"), c.replacement)
		}()
	}
	defer func() {
		if recover() == nil {
			t.Error("checkNotNull(null) does not panic")
		}
	}()
	CheckNotNull[int](nil, func() string { return "gone" })
}

// A Decimal is a numeric as Postgres writes it, digit for digit, and
// goes back as it came. A null is no Decimal and no Uuid. The rest of
// db.go needs a database: emit's tests run it.
func TestPostgres(t *testing.T) {
	for _, text := range []string{"12.50", "-0.001", "1500", "NaN", "Infinity", "-Infinity"} {
		var back Decimal
		if n, err := Decimal(text).NumericValue(); err != nil || back.ScanNumeric(n) != nil || string(back) != text {
			t.Errorf("%s comes back as %s (%v)", text, back, err)
		}
	}
	if new(Decimal).ScanNumeric(pgtype.Numeric{}) == nil || new(Uuid).ScanUUID(pgtype.UUID{}) == nil {
		t.Error("a null is read as a value")
	}
}

// first, last, single, take and drop give Kotlin's values and panic
// with Kotlin's messages (spec.md section 8).
func TestListEnds(t *testing.T) {
	xs, none := ListOf[int32](1, 2, 3), EmptyList[int32]()
	if List_First(xs) != 1 || List_Last(xs) != 3 || List_Single(ListOf[int32](7)) != 7 || List_LastOrNull(none) != nil || *List_LastOrNull(xs) != 3 {
		t.Error("first, last, single or lastOrNull is wrong")
	}
	if !slices.Equal(List_Take(xs, 2), []int32{1, 2}) || !slices.Equal(List_Take(xs, 9), xs) || !slices.Equal(List_Drop(xs, 1), []int32{2, 3}) || len(List_Drop(xs, 9)) != 0 {
		t.Error("take or drop is wrong")
	}
	if List_Plus(List_Take(xs, 1), ListOf[int32](9)); !slices.Equal(xs, []int32{1, 2, 3}) {
		t.Errorf("plus on a take wrote into its list: %v", xs)
	}
	empty, more, negative := "List is empty.", "List has more than one element.", "Requested element count -1 is less than zero."
	for _, c := range []struct {
		f    func()
		want string
	}{
		{func() { List_First(none) }, empty},
		{func() { List_Last(none) }, empty},
		{func() { List_Single(none) }, empty},
		{func() { List_Single(xs) }, more},
		{func() { List_Take(xs, -1) }, negative},
		{func() { List_Drop(xs, -1) }, negative},
		{func() { String_Substring_Int_Int("abc", 5, 3) }, "Range [5, 3) out of bounds for length 3"},
		{func() { String_Substring_Int_Int("abc", 2, 1) }, "Range [2, 1) out of bounds for length 3"},
		{func() { String_Substring_Int("abc", -1) }, "Range [-1, 3) out of bounds for length 3"},
		{func() { String_Substring_Int("a😀", 4) }, "Range [4, 3) out of bounds for length 3"},
	} {
		func() {
			defer func() {
				if p := recover(); p != c.want {
					t.Errorf("panics with %v, want %s", p, c.want)
				}
			}()
			c.f()
		}()
	}
}

// orEmpty, isNullOrBlank, firstOrNull with a predicate and
// contains(char), as Kotlin's: contains looks by UTF-16 units.
func TestNullableAndOverloads(t *testing.T) {
	list := ListOf[int32](1, 2, 3)
	odd := func(x int32) bool { return x%2 == 1 }
	big := func(x int32) bool { return x > 1 }
	for _, c := range [][2]string{
		{rt.ToString(List_OrEmpty(&list)) + rt.ToString(List_OrEmpty[int32](nil)), "[1, 2, 3][]"},
		{rt.ToString(ListT_FirstOrNull_TToBoolean(list, odd)) + rt.ToString(ListT_FirstOrNull_TToBoolean(list, big)) + rt.ToString(ListT_FirstOrNull_TToBoolean(list, func(x int32) bool { return x > 3 })), "12null"},
		{rt.ToString(ListT_FirstOrNull_TToBoolean(EmptyList[int32](), odd)), "null"},
		{rt.ToString([]bool{String_IsNullOrBlank(nil), String_IsNullOrBlank(rt.Ptr(" \t")), String_IsNullOrBlank(rt.Ptr("\u0085")), String_IsNullOrBlank(rt.Ptr(" a"))}), "[true, true, false, false]"},
		{rt.ToString([]bool{String_Contains_Char("a@b", '@'), String_Contains_Char("ab", '@'), String_Contains_Char("😀", 0xDE00), String_Contains_Char("", 'a')}), "[true, false, true, false]"},
	} {
		if c[0] != c[1] {
			t.Errorf("got %q, want %q", c[0], c[1])
		}
	}
	if List_OrEmpty[int32](nil) == nil {
		t.Error("orEmpty of null is a nil list")
	}
}

// A plain db.tx is run once: a deadlock in it is a panic, not a rerun
// (spec.md 10.7).
func TestPlainTxRunsOnce(t *testing.T) {
	ctx, pool := testPool(t)
	runs := 0
	defer func() {
		if p, _ := recover().(*pgconn.PgError); p == nil || p.Code != "40P01" || runs != 1 {
			t.Errorf("a deadlock in a plain db.tx ran it %d times and panicked with %v", runs, p)
		}
	}()
	Db_Tx_Boolean_TxToA(NewDb(ctx, pool), nil, func(Tx) rt.Unit {
		if runs++; runs == 1 {
			panic(&pgconn.PgError{Code: "40P01"})
		}
		return rt.Unit{}
	})
}

// A test's transaction is SERIALIZABLE, as RolledBack states it
// (spec.md 16.2).
func TestRolledBackIsSerializable(t *testing.T) {
	ctx, pool := testPool(t)
	var level string
	RolledBack(NewDb(ctx, pool), func(tx Tx) {
		must(0, tx.tx.QueryRow(ctx, "show transaction_isolation").Scan(&level))
	})
	if level != "serializable" {
		t.Errorf("a test's transaction is at %q, want serializable", level)
	}
}

// testPool connects to the test database, closing it when t ends.
func testPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	url := os.Getenv("KOGO_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}
