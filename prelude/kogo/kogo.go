// Package kogo is the Go behind the declarations of Kogo's package
// kogo: prelude/kogo.kg and prelude/either.kg, and what check/core.kg
// declares for section 9. Each Go name follows the emitter's naming
// rule (decisions.md), and a test of the emitter compiles a use of
// every one of prelude/ against its declaration.
//
// A failure is a panic (either.go), so it may unwind through any
// function here that calls a lambda. Such a function holds no lock
// and leaves nothing half done while the lambda runs: what it builds
// is its own until it returns.
package kogo

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"kogo/rt"
)

// A List is a slice that nothing changes once it is built.
type List[T any] = []T

// List_widen gives a List<A> as a List<B>, when A is used as a wider
// B: Go has no wider type for a slice, so it is copied.
func List_widen[A, B any](xs List[A], f func(A) B) List[B] {
	out := make(List[B], len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

type Pair[A, B any] struct {
	First  A
	Second B
}

func (p Pair[A, B]) String() string {
	return "(" + rt.ToString(p.First) + ", " + rt.ToString(p.Second) + ")"
}

func Pair_widen[A, B, C, D any](p Pair[A, B], f func(A) C, g func(B) D) Pair[C, D] {
	return Pair[C, D]{f(p.First), g(p.Second)}
}

func ListOf[T any](elements ...T) List[T] { return elements }

func List_Size[T any](this List[T]) int32 { return int32(len(this)) }

func List_Filter[T any](this List[T], predicate func(T) bool) List[T] {
	out := List[T]{}
	for _, x := range this {
		if predicate(x) {
			out = append(out, x)
		}
	}
	return out
}

func List_MapIndexed[T, R any](this List[T], transform func(int32, T) R) List[R] {
	out := make(List[R], len(this))
	for i, x := range this {
		out[i] = transform(int32(i), x)
	}
	return out
}

func List_Zip[T, R any](this List[T], other List[R]) List[Pair[T, R]] {
	out := make(List[Pair[T, R]], min(len(this), len(other)))
	for i := range out {
		out[i] = Pair[T, R]{this[i], other[i]}
	}
	return out
}

func List_Sum(this List[int64]) int64 {
	return ListT_SumOf_TToLong(this, func(x int64) int64 { return x })
}

func ListT_SumOf_TToInt[T any](this List[T], selector func(T) int32) int32 {
	var sum int32
	for _, x := range this {
		sum += selector(x)
	}
	return sum
}

func ListT_SumOf_TToLong[T any](this List[T], selector func(T) int64) int64 {
	var sum int64
	for _, x := range this {
		sum += selector(x)
	}
	return sum
}

// A String is a Go string counted as Kotlin counts it, in UTF-16
// units (spec.md section 3). An operation that would leave half of a
// surrogate pair leaves U+FFFD, which utf16.Decode writes for one.
func units(s string) []uint16 { return utf16.Encode([]rune(s)) }

func String_Length(this string) int32 {
	n := int32(0)
	for _, r := range this {
		if n++; r >= 0x10000 {
			n++
		}
	}
	return n
}

func String_Plus(this, other string) string { return this + other }

// Kotlin's whitespace is Unicode's separators and the ASCII controls
// that Java counts, which differs from Go's unicode.IsSpace.
func isWhitespace(r rune) bool {
	return unicode.Is(unicode.Z, r) || r >= 0x09 && r <= 0x0D || r >= 0x1C && r <= 0x1F
}

func String_Trim(this string) string          { return strings.TrimFunc(this, isWhitespace) }
func String_Lowercase(this string) string     { return strings.ToLower(this) }
func String_Contains(this, other string) bool { return strings.Contains(this, other) }
func String_IsNotEmpty(this string) bool      { return this != "" }
func String_Substring_Int(this string, startIndex int32) string {
	return string(utf16.Decode(units(this)[startIndex:]))
}

func String_Substring_Int_Int(this string, startIndex, endIndex int32) string {
	return string(utf16.Decode(units(this)[startIndex:endIndex]))
}

func String_Map[R any](this string, transform func(rt.Char) R) List[R] {
	out := List[R]{}
	for _, u := range units(this) {
		out = append(out, transform(rt.Char(u)))
	}
	return out
}

// Unicode's decimal digits come in runs of ten from a zero.
func Char_DigitToInt(this rt.Char) int32 {
	for _, r := range unicode.Nd.R16 {
		if r.Lo <= uint16(this) && uint16(this) <= r.Hi {
			return int32(uint16(this)-r.Lo) % 10
		}
	}
	panic("Char " + this.String() + " is not a decimal digit")
}

// A Regex is RE2, as Go's is; two of one pattern are equal.
type Regex struct {
	Pattern string
	whole   *regexp.Regexp
}

func (r Regex) String() string { return r.Pattern }

// Regex_String is the fun Regex(pattern), named apart from the type.
func Regex_String(pattern string) Regex {
	return Regex{pattern, regexp.MustCompile(`\A(?:` + pattern + `)\z`)}
}

func Regex_Matches(this Regex, input string) bool { return this.whole.MatchString(input) }

func Long_ToDouble(this int64) float64 { return float64(this) }

// Double_ToLong rounds toward zero, gives 0 for NaN and stops at the
// ends of Long, as Kotlin does; Go leaves those to the machine.
func Double_ToLong(this float64) int64 {
	switch {
	case this != this:
		return 0
	case this >= 1<<63:
		return 1<<63 - 1
	case this <= -1<<63:
		return -1 << 63
	}
	return int64(this)
}

// Double_ToInt is Double_ToLong at the ends of Int.
func Double_ToInt(this float64) int32 {
	switch {
	case this != this:
		return 0
	case this >= 1<<31:
		return 1<<31 - 1
	case this <= -1<<31:
		return -1 << 31
	}
	return int32(this)
}

func T_Let[T, R any](this T, block func(T) R) R { return block(this) }

func T_TakeIf[T any](this T, predicate func(T) bool) *T {
	if predicate(this) {
		return &this
	}
	return nil
}

func RequireNotNull[T any](value *T, lazyMessage func() string) T {
	if value == nil {
		panic(lazyMessage())
	}
	return *value
}

func Check(value bool, lazyMessage func() string) rt.Unit {
	if !value {
		panic(lazyMessage())
	}
	return rt.Unit{}
}

// A Uuid is an id's sixteen bytes.
type Uuid [16]byte

func (u Uuid) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// NewId gives an id that sorts by when it was made and is random
// after that: a version 7 UUID.
func NewId() Uuid {
	var u Uuid
	binary.BigEndian.PutUint64(u[:8], uint64(time.Now().UnixMilli())<<16)
	rand.Read(u[6:])
	u[6], u[8] = u[6]&0x0F|0x70, u[8]&0x3F|0x80
	return u
}
