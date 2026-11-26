// Package kogo is the Go behind the declarations of Kogo's package
// kogo: prelude/kogo.kg and prelude/either.kg, and what check/core.kg
// declares for sections 9 and 10 (either.go, db.go). Each Go name
// follows the emitter's naming rule (decisions.md), and a test of the
// emitter compiles a use of every one of prelude/ against its
// declaration.
//
// A failure is a panic (either.go), so it may unwind through any
// function here that calls a lambda. Such a function holds no lock
// and leaves nothing half done while the lambda runs: what it builds
// is its own until it returns.
package kogo

import (
	"cmp"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/Cloud-Limo/kogo/rt"
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

// ListOfNotNull gives the elements that are not null.
func ListOfNotNull[T any](elements ...*T) List[T] {
	out := List[T]{}
	for _, x := range elements {
		if x != nil {
			out = append(out, *x)
		}
	}
	return out
}

// EmptyList is never nil, as nothing the prelude builds is.
func EmptyList[T any]() List[T] { return List[T]{} }

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

// List_Get is list[i]; an index outside the list is a bug, and panics
// with Kotlin's message, but for a list Kotlin built as its empty list,
// which Kotlin words by the list's class (spec.md section 12).
func List_Get[T any](this List[T], index int32) T {
	if uint32(index) >= uint32(len(this)) {
		panic("Index " + strconv.Itoa(int(index)) + " out of bounds for length " + strconv.Itoa(len(this)))
	}
	return this[index]
}

// List_GetOrNull is the element at index, or null outside the list.
func List_GetOrNull[T any](this List[T], index int32) *T {
	if index < 0 || int(index) >= len(this) {
		return nil
	}
	return rt.Ptr(this[index])
}

func List_IsNotEmpty[T any](this List[T]) bool { return len(this) > 0 }

// List_Contains compares as == does, by value.
func List_Contains[T any](this List[T], element T) bool {
	return slices.ContainsFunc(this, func(x T) bool { return rt.Equal(x, element) })
}

func List_None[T any](this List[T], predicate func(T) bool) bool {
	return !slices.ContainsFunc(this, predicate)
}

// List_All stops at the first element predicate refuses, as Kotlin's.
func List_All[T any](this List[T], predicate func(T) bool) bool {
	return !slices.ContainsFunc(this, func(x T) bool { return !predicate(x) })
}

// ListT_FirstOrNull_fun, as every nullable result, is a copy: a
// pointer into the list would hold all of it and could change it.
func ListT_FirstOrNull_fun[T any](this List[T]) *T {
	if len(this) == 0 {
		return nil
	}
	return rt.Ptr(this[0])
}

func ListT_FirstOrNull_TToBoolean[T any](this List[T], predicate func(T) bool) *T {
	for _, x := range this {
		if predicate(x) {
			return rt.Ptr(x)
		}
	}
	return nil
}

// List_First, List_Last and List_Single panic, with Kotlin's
// messages, where their OrNull is null.
func List_First[T any](this List[T]) T { return orPanic(ListT_FirstOrNull_fun(this), "List is empty.") }
func List_Last[T any](this List[T]) T  { return orPanic(List_LastOrNull(this), "List is empty.") }

func List_Single[T any](this List[T]) T {
	if len(this) > 1 {
		panic("List has more than one element.")
	}
	return orPanic(List_SingleOrNull(this), "List is empty.")
}

func orPanic[T any](p *T, message string) T {
	if p == nil {
		panic(message)
	}
	return *p
}

func List_LastOrNull[T any](this List[T]) *T {
	if len(this) == 0 {
		return nil
	}
	return rt.Ptr(this[len(this)-1])
}

// List_Take and List_Drop share the list's array, which no list
// operation writes.
func List_Take[T any](this List[T], n int32) List[T] { return this[:min(count(n), len(this))] }
func List_Drop[T any](this List[T], n int32) List[T] { return this[min(count(n), len(this)):] }

func count(n int32) int {
	if n < 0 {
		panic("Requested element count " + strconv.Itoa(int(n)) + " is less than zero.")
	}
	return int(n)
}

// List_SingleOrNull is null for an empty list and for one of two or
// more elements.
func List_SingleOrNull[T any](this List[T]) *T {
	if len(this) != 1 {
		return nil
	}
	return rt.Ptr(this[0])
}

func List_ForEach[T any](this List[T], action func(T) rt.Unit) rt.Unit {
	for _, x := range this {
		action(x)
	}
	return rt.Unit{}
}

func List_ToSet[T any](this List[T]) Set[T] { return SetOf(this...) }

func List_Map[T, R any](this List[T], transform func(T) R) List[R] {
	return List_widen(this, transform)
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

// A joinToString's separator is ", " when the call gives none, as
// Kotlin's is.
func ListString_JoinToString_String(this List[string], separator *string) string {
	return ListT_JoinToString_String_TToString(this, separator, func(s string) string { return s })
}

func ListT_JoinToString_String_TToString[T any](this List[T], separator *string, transform func(T) string) string {
	sep := ", "
	if separator != nil {
		sep = *separator
	}
	return strings.Join(List_widen(this, transform), sep)
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

// List_Plus is a new list, never nil: neither operand's array is
// written.
func List_Plus[T any](this, elements List[T]) List[T] {
	return append(append(make(List[T], 0, len(this)+len(elements)), this...), elements...)
}

// EnumValueOf is an enum's valueOf: its constant of that name, or a
// panic with Kotlin's message. enum is the enum's name with its
// package's.
func EnumValueOf[E ~string](entries []E, name, enum string) E {
	for _, x := range entries {
		if string(x) == name {
			return x
		}
	}
	panic("No enum constant " + enum + "." + name)
}

func List_IsEmpty[T any](this List[T]) bool { return len(this) == 0 }

func List_MapNotNull[T, R any](this List[T], transform func(T) *R) List[R] {
	return ListOfNotNull(List_widen(this, transform)...)
}

func List_FlatMap[T, R any](this List[T], transform func(T) List[R]) List[R] {
	out := List[R]{}
	for _, x := range this {
		out = append(out, transform(x)...)
	}
	return out
}

// List_MaxOrNull is the first of the largest elements, ordered as
// compare orders: a NaN above every Double and 0.0 above -0.0, as
// Kotlin's, whose Math.max gives the same.
func List_MaxOrNull[T any](this List[T]) *T {
	if len(this) == 0 {
		return nil
	}
	m := this[0]
	for _, x := range this[1:] {
		if compare(reflect.ValueOf(m), reflect.ValueOf(x)) < 0 {
			m = x
		}
	}
	return rt.Ptr(m)
}

// List_SortedBy is stable, and orders keys as Kotlin's compareValues
// does (compare).
func List_SortedBy[T, R any](this List[T], selector func(T) R) List[T] {
	return sortBy(this, selector, 1)
}

// List_SortedByDescending is SortedBy turned round, null last, and
// still stable: equal keys keep their order, as in Kotlin.
func List_SortedByDescending[T, R any](this List[T], selector func(T) R) List[T] {
	return sortBy(this, selector, -1)
}

func sortBy[T, R any](this List[T], selector func(T) R, sign int) List[T] {
	out := List_Plus(this, nil)
	slices.SortStableFunc(out, func(a, b T) int {
		return sign * compare(reflect.ValueOf(selector(a)), reflect.ValueOf(selector(b)))
	})
	return out
}

// compare orders two keys of one type: null first, then by Kotlin's
// compareTo, for a number, a String (by UTF-16 units), a Char or a
// Boolean. Any other key is outside Kogo's subset (spec.md 12).
func compare(a, b reflect.Value) int {
	if a.Kind() == reflect.Pointer {
		if a.IsNil() || b.IsNil() {
			return cmp.Compare(boolInt(!a.IsNil()), boolInt(!b.IsNil()))
		}
		return compare(a.Elem(), b.Elem())
	}
	switch a.Type() {
	case reflect.TypeFor[int32](), reflect.TypeFor[int64]():
		return cmp.Compare(a.Int(), b.Int())
	case reflect.TypeFor[rt.Char]():
		return cmp.Compare(a.Uint(), b.Uint())
	case reflect.TypeFor[bool]():
		return cmp.Compare(boolInt(a.Bool()), boolInt(b.Bool()))
	case reflect.TypeFor[string]():
		return slices.Compare(units(a.String()), units(b.String()))
	case reflect.TypeFor[float64](): // NaN last and -0.0 before 0.0, as Java's
		x, y := a.Float(), b.Float()
		if x != x || y != y {
			return cmp.Compare(boolInt(x != x), boolInt(y != y))
		}
		return cmp.Or(cmp.Compare(x, y), cmp.Compare(boolInt(math.Signbit(y)), boolInt(math.Signbit(x))))
	}
	panic("sortedBy, sortedByDescending and maxOrNull order by an Int, a Long, a Double, a String, a Char or a Boolean, or one of these that may be null (spec 12)")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// List_DistinctBy keeps the first element of each key, comparing keys
// as == does.
func List_DistinctBy[T, K any](this List[T], selector func(T) K) List[T] {
	var seen ordered[K]
	out := List[T]{}
	for _, x := range this {
		if k := selector(x); seen.index(k) < 0 {
			seen.add(k)
			out = append(out, x)
		}
	}
	return out
}

func List_Distinct[T any](this List[T]) List[T] {
	return List_DistinctBy(this, func(x T) T { return x })
}

func List_Reversed[T any](this List[T]) List[T] {
	out := List_Plus(this, nil)
	slices.Reverse(out)
	return out
}

// List_GroupBy keeps the keys in the order first given, and each
// key's elements in the list's order.
func List_GroupBy[T, K any](this List[T], keySelector func(T) K) Map[K, List[T]] {
	var m Map[K, List[T]]
	for _, x := range this {
		k := keySelector(x)
		if i := m.index(k); i >= 0 {
			m.values[i] = append(m.values[i], x)
		} else {
			m.add(k)
			m.values = append(m.values, List[T]{x})
		}
	}
	return m
}

// List_AssociateBy and List_Associate keep each key where it was first
// given and the last value given for it, as Kotlin's LinkedHashMap.
func List_AssociateBy[T, K any](this List[T], keySelector func(T) K) Map[K, T] {
	return List_Associate(this, func(x T) Pair[K, T] { return Pair[K, T]{keySelector(x), x} })
}

func List_Associate[T, K, V any](this List[T], transform func(T) Pair[K, V]) Map[K, V] {
	var m Map[K, V]
	for _, x := range this {
		p := transform(x)
		if i := m.index(p.First); i >= 0 {
			m.values[i] = p.Second
		} else {
			m.add(p.First)
			m.values = append(m.values, p.Second)
		}
	}
	return m
}

// ordered holds each key once, in the order first given, comparing
// keys as == does (decisions.md). Set and Map are built on it. A key
// of a plain kind is found through at, where Go's == is Kotlin's;
// any other is found by walking the keys (spec.md 12). Add only to a
// value being built: a copy shares the keys' array, at and a Map's
// values, so whatever adds to a copy (a plus, say) builds its own first.
type ordered[K any] struct {
	keys []K
	at   map[any]int // each key's position, or nil to walk
}

// orderedOf holds keys that are each there once already.
func orderedOf[K any](keys []K) ordered[K] {
	o := ordered[K]{keys: keys}
	if plain(reflect.TypeFor[K]()) {
		o.at = make(map[any]int, len(keys))
		for i, k := range keys {
			o.at[k] = i
		}
	}
	return o
}

// plain is whether t is a String, an Int, a Long, a Boolean, a Uuid
// or an enum (a named string), whose == is Go's.
func plain(t reflect.Type) bool {
	switch t {
	case reflect.TypeFor[int32](), reflect.TypeFor[int64](), reflect.TypeFor[bool](), reflect.TypeFor[Uuid]():
		return true
	}
	return t.Kind() == reflect.String && t != reflect.TypeFor[Decimal]()
}

func (o ordered[K]) index(k K) int {
	if o.at == nil {
		return slices.IndexFunc(o.keys, func(x K) bool { return rt.Equal(x, k) })
	}
	if i, ok := o.at[k]; ok {
		return i
	}
	return -1
}

// add decides the kind with the first key.
func (o *ordered[K]) add(k K) {
	if len(o.keys) == 0 {
		*o = orderedOf(o.keys)
	}
	if o.index(k) < 0 {
		if o.at != nil {
			o.at[k] = len(o.keys)
		}
		o.keys = append(o.keys, k)
	}
}

// Set embeds rt.Unordered so that == compares its keys regardless of
// order.
type Set[T any] struct {
	rt.Unordered
	ordered[T]
}

func (s Set[T]) String() string      { return rt.ToString(s.keys) }
func (s Set[T]) Keys() reflect.Value { return reflect.ValueOf(s.keys) }

// Set_widen keeps every key: values that differ as Ts differ as Bs.
func Set_widen[A, B any](s Set[A], f func(A) B) Set[B] {
	return Set[B]{ordered: orderedOf(List_widen(s.keys, f))}
}

func SetOf[T any](elements ...T) Set[T] {
	var s Set[T]
	for _, x := range elements {
		s.add(x)
	}
	return s
}

func Set_Contains[T any](this Set[T], element T) bool { return this.index(element) >= 0 }

func Set_None[T any](this Set[T], predicate func(T) bool) bool {
	return List_None(this.keys, predicate)
}

func Set_All[T any](this Set[T], predicate func(T) bool) bool {
	return List_All(this.keys, predicate)
}

func Set_MapNotNull[T, R any](this Set[T], transform func(T) *R) List[R] {
	return List_MapNotNull(this.keys, transform)
}

// A Set's plus keeps its keys' order and adds each new one after them;
// its minus keeps the order of those left, as Kotlin's LinkedHashSet.
// Each builds a new Set: this one's keys may be shared (ordered).
func SetT_Plus_T[T any](this Set[T], element T) Set[T] {
	return SetT_Plus_ListT(this, List[T]{element})
}

func SetT_Plus_ListT[T any](this Set[T], elements List[T]) Set[T] {
	return SetOf(List_Plus(this.keys, elements)...)
}

func SetT_Plus_SetT[T any](this Set[T], elements Set[T]) Set[T] {
	return SetT_Plus_ListT(this, elements.keys)
}

func SetT_Minus_T[T any](this Set[T], element T) Set[T] { return SetT_Minus_SetT(this, SetOf(element)) }

func SetT_Minus_ListT[T any](this Set[T], elements List[T]) Set[T] {
	return SetT_Minus_SetT(this, SetOf(elements...))
}

func SetT_Minus_SetT[T any](this Set[T], elements Set[T]) Set[T] {
	return Set[T]{ordered: orderedOf(List_Filter(this.keys, func(x T) bool { return elements.index(x) < 0 }))}
}

// A Map holds a value beside each key. Its Keys for == are its
// entries, so that two maps are equal when they hold the same entries
// in any order, as in Kotlin.
type Map[K, V any] struct {
	rt.Unordered
	ordered[K]
	values []V
}

func (m Map[K, V]) entries() List[Pair[K, V]] {
	return List_MapIndexed(m.keys, func(i int32, k K) Pair[K, V] { return Pair[K, V]{k, m.values[i]} })
}

func (m Map[K, V]) Keys() reflect.Value { return reflect.ValueOf(m.entries()) }

// String is Kotlin's text of a map: {a=1, b=2}.
func (m Map[K, V]) String() string {
	return "{" + ListT_JoinToString_String_TToString(m.entries(), nil, func(p Pair[K, V]) string {
		return rt.ToString(p.First) + "=" + rt.ToString(p.Second)
	}) + "}"
}

func Map_widen[K, A, B any](m Map[K, A], f func(A) B) Map[K, B] {
	return Map[K, B]{ordered: m.ordered, values: List_widen(m.values, f)}
}

// Map_Keys shares the map's keys, which nothing adds to (ordered).
func Map_Keys[K, V any](this Map[K, V]) Set[K] { return Set[K]{ordered: this.ordered} }

// Map_Get is map[k]: the key's value, or null where it has none.
func Map_Get[K, V any](this Map[K, V], key K) *V {
	if i := this.index(key); i >= 0 {
		return rt.Ptr(this.values[i])
	}
	return nil
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

func String_Trim_fun(this string) string { return strings.TrimFunc(this, isWhitespace) }
func String_TrimEnd(this string) string  { return strings.TrimRightFunc(this, isWhitespace) }

// String_Trim_Char trims by UTF-16 units, as Kotlin does: a half of
// a pair trims it, and the other half is left as U+FFFD (spec.md
// section 3).
func String_Trim_Char(this string, char rt.Char) string {
	u := units(this)
	i, j := 0, len(u)
	for i < j && rt.Char(u[i]) == char {
		i++
	}
	for j > i && rt.Char(u[j-1]) == char {
		j--
	}
	return string(utf16.Decode(u[i:j]))
}

func String_IsBlank(this string) bool    { return strings.TrimFunc(this, isWhitespace) == "" }
func String_IsNotBlank(this string) bool { return !String_IsBlank(this) }

func String_IfEmpty(this string, defaultValue func() string) string {
	if this == "" {
		return defaultValue()
	}
	return this
}

func String_IfBlank(this string, defaultValue func() string) string {
	if String_IsBlank(this) {
		return defaultValue()
	}
	return this
}

func String_StartsWith(this, prefix string) bool { return strings.HasPrefix(this, prefix) }
func String_EndsWith(this, suffix string) bool   { return strings.HasSuffix(this, suffix) }

// String_Filter keeps the UTF-16 units that predicate takes.
func String_Filter(this string, predicate func(rt.Char) bool) string {
	var kept []uint16
	for _, u := range units(this) {
		if predicate(rt.Char(u)) {
			kept = append(kept, u)
		}
	}
	return string(utf16.Decode(kept))
}

func String_FilterNot(this string, predicate func(rt.Char) bool) string {
	return String_Filter(this, func(c rt.Char) bool { return !predicate(c) })
}

// String_Lowercase is Go's strings.ToLower but for U+0130, which it
// maps to i and a combining dot as Kotlin does (Java's ROOT locale).
// It leaves the rest of Java's special casing, which reads the letters
// either side: a final Σ is σ here and ς in Kotlin (spec.md 12).
func String_Lowercase(this string) string {
	return strings.ToLower(strings.ReplaceAll(this, "İ", "i\u0307"))
}

func String_Contains_String(this, other string) bool { return strings.Contains(this, other) }

// String_Contains_Char looks by UTF-16 units: a half of a pair is
// found in a text holding the pair, as in Kotlin.
func String_Contains_Char(this string, char rt.Char) bool {
	return slices.Contains(units(this), uint16(char))
}

func String_IsNotEmpty(this string) bool { return this != "" }
func String_Substring_Int(this string, startIndex int32) string {
	return String_Substring_Int_Int(this, startIndex, String_Length(this))
}

// String_Substring_Int_Int counts in UTF-16 units and panics with
// Kotlin's message for a range outside the string.
func String_Substring_Int_Int(this string, startIndex, endIndex int32) string {
	u := units(this)
	if startIndex < 0 || endIndex < startIndex || int(endIndex) > len(u) {
		panic(fmt.Sprintf("Range [%d, %d) out of bounds for length %d", startIndex, endIndex, len(u)))
	}
	return string(utf16.Decode(u[startIndex:endIndex]))
}

// String_SubstringAfter is the whole string when delimiter is absent,
// as Kotlin's is; an empty delimiter is found at the start.
func String_SubstringAfter(this, delimiter string) string {
	if _, after, found := strings.Cut(this, delimiter); found {
		return after
	}
	return this
}

// String_Take counts in UTF-16 units, as substring does, and panics
// with Kotlin's message for a negative n.
func String_Take(this string, n int32) string {
	if n < 0 {
		panic(fmt.Sprintf("Requested character count %d is less than zero.", n))
	}
	return String_Substring_Int_Int(this, 0, min(n, String_Length(this)))
}

// String_Split cuts at each delimiter, as Kotlin's does: a delimiter
// at either end gives an empty string there, and an empty string is
// one empty string. An empty delimiter is found before every UTF-16
// unit and at the end.
func String_Split(this, delimiter string) List[string] {
	if delimiter != "" {
		return strings.Split(this, delimiter)
	}
	out := List[string]{""}
	for _, u := range units(this) {
		out = append(out, string(utf16.Decode([]uint16{u})))
	}
	return append(out, "")
}

// String_PadStart counts in UTF-16 units and panics with Kotlin's
// message for a negative length.
func String_PadStart(this string, length int32, padChar *rt.Char) string {
	if length < 0 {
		panic(fmt.Sprintf("Desired length %d is less than zero.", length))
	}
	pad := rt.Char(' ')
	if padChar != nil {
		pad = *padChar
	}
	n := length - String_Length(this)
	if n <= 0 {
		return this
	}
	return string(utf16.Decode(slices.Repeat([]uint16{uint16(pad)}, int(n)))) + this
}

func String_ToIntOrNull(this string) *int32 {
	if n, ok := whole(this, 32); ok {
		v := int32(n)
		return &v
	}
	return nil
}

func String_ToLongOrNull(this string) *int64 {
	if n, ok := whole(this, 64); ok {
		return &n
	}
	return nil
}

// whole reads a whole number as Kotlin's toIntOrNull does: an
// optional + or - and then decimal digits alone, any of Unicode's,
// within bits. Blank, a sign alone and one out of range are not one.
func whole(s string, bits int) (int64, bool) {
	var ascii []byte
	for i, u := range units(s) {
		if d := digit(u); d >= 0 {
			ascii = append(ascii, byte('0'+d))
		} else if i == 0 && (u == '+' || u == '-') {
			ascii = append(ascii, byte(u))
		} else {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(string(ascii), 10, bits)
	return n, err == nil
}

// digit is a UTF-16 unit's value as a decimal digit, or -1: Unicode's
// decimal digits come in runs of ten from a zero.
func digit(u uint16) int {
	for _, r := range unicode.Nd.R16 {
		if r.Lo <= u && u <= r.Hi {
			return int(u-r.Lo) % 10
		}
	}
	return -1
}

func String_Map[R any](this string, transform func(rt.Char) R) List[R] {
	out := List[R]{}
	for _, u := range units(this) {
		out = append(out, transform(rt.Char(u)))
	}
	return out
}

func Char_DigitToInt(this rt.Char) int32 {
	if d := digit(uint16(this)); d >= 0 {
		return int32(d)
	}
	panic("Char " + this.String() + " is not a decimal digit")
}

func Char_IsWhitespace(this rt.Char) bool { return isWhitespace(rune(this)) }

// A Regex is RE2, as Go's is. Kotlin compares two by identity, so its
// fields are unexported and rt's == panics on one.
type Regex struct {
	pattern   string
	whole, re *regexp.Regexp
}

func (r Regex) String() string { return r.pattern }

// Regex_String is the fun Regex(pattern), named apart from the type.
func Regex_String(pattern string) Regex {
	return Regex{pattern, regexp.MustCompile(`\A(?:` + pattern + `)\z`), regexp.MustCompile(pattern)}
}

func Regex_Matches(this Regex, input string) bool { return this.whole.MatchString(input) }

// String_Replace reads replacement as Java does: $n is
// group n, ${name} a named group, and a backslash takes the character
// after it as it is. Go's own expansion differs in each.
func String_Replace(this string, regex Regex, replacement string) string {
	var b strings.Builder
	last := 0
	for _, m := range regex.re.FindAllStringSubmatchIndex(this, -1) {
		b.WriteString(this[last:m[0]])
		expand(&b, regex.re, replacement, this, m)
		last = m[1]
	}
	b.WriteString(this[last:])
	return b.String()
}

func expand(b *strings.Builder, re *regexp.Regexp, replacement, s string, m []int) {
	group := func(n int) {
		if m[2*n] >= 0 {
			b.WriteString(s[m[2*n]:m[2*n+1]])
		}
	}
	for i := 0; i < len(replacement); i++ {
		switch c := replacement[i]; {
		case c == '\\' && i+1 < len(replacement):
			i++
			b.WriteByte(replacement[i])
		case c == '\\':
			panic("character to be escaped is missing")
		case c != '$':
			b.WriteByte(c)
		case i+1 < len(replacement) && replacement[i+1] == '{':
			end := strings.IndexByte(replacement[i:], '}')
			n := -1
			if end > 0 {
				n = re.SubexpIndex(replacement[i+2 : i+end])
			}
			if n < 0 {
				panic("no group in " + strconv.Quote(replacement) + " at " + strconv.Itoa(i))
			}
			group(n)
			i += end
		case i+1 < len(replacement) && '0' <= replacement[i+1] && replacement[i+1] <= '9':
			// As Java: the first digit, then each next digit while the
			// number still names a group.
			i++
			n := int(replacement[i] - '0')
			if n > re.NumSubexp() {
				panic("no group " + strconv.Itoa(n))
			}
			for i+1 < len(replacement) && '0' <= replacement[i+1] && replacement[i+1] <= '9' {
				next := n*10 + int(replacement[i+1]-'0')
				if next > re.NumSubexp() {
					break
				}
				n, i = next, i+1
			}
			group(n)
		default:
			panic("illegal group reference in " + strconv.Quote(replacement))
		}
	}
}

func Int_ToLong(this int32) int64      { return int64(this) }
func Long_ToDouble(this int64) float64 { return float64(this) }
func Long_ToInt(this int64) int32      { return int32(this) } // the low 32 bits, as Kotlin's

// Abs of Long's least value is that value, as Kotlin's is.
func Abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// A number's toString is its text in a template, by the same call.
func Int_ToString(this int32) string       { return rt.ToString(this) }
func Long_ToString(this int64) string      { return rt.ToString(this) }
func Double_ToString(this float64) string  { return rt.ToString(this) }
func Decimal_ToString(this Decimal) string { return rt.ToString(this) }

// Decimal_ToDouble is the closest Double, as Java's is; NaN and
// Infinity, which Postgres holds, are Double's own.
func Decimal_ToDouble(this Decimal) float64 {
	f, err := strconv.ParseFloat(string(this), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		panic("not a numeric: " + string(this))
	}
	return f
}

func Long_CoerceAtLeast(this, minimumValue int64) int64 { return max(this, minimumValue) }
func Long_CoerceAtMost(this, maximumValue int64) int64  { return min(this, maximumValue) }

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

func A_To[A, B any](this A, that B) Pair[A, B] { return Pair[A, B]{this, that} }

func T_Also[T any](this T, block func(T) rt.Unit) T {
	block(this)
	return this
}

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

func CheckNotNull[T any](value *T, lazyMessage func() string) T {
	return RequireNotNull(value, lazyMessage)
}

func Require(value bool, lazyMessage func() string) rt.Unit { return Check(value, lazyMessage) }

func Check(value bool, lazyMessage func() string) rt.Unit {
	if !value {
		panic(lazyMessage())
	}
	return rt.Unit{}
}

func Error(message string) rt.Nothing { panic(message) }

// A Uuid is an id's sixteen bytes.
type Uuid [16]byte

func (u Uuid) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// Uuid_ParseOrNull is Uuid.parseOrNull(text): the id that a template
// writes as text, hex of either case, or nil for any other text.
func Uuid_ParseOrNull(s string) *Uuid {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return nil
	}
	var u Uuid
	if _, err := hex.Decode(u[:], []byte(s[:8]+s[9:13]+s[14:18]+s[19:23]+s[24:])); err != nil {
		return nil
	}
	return &u
}

// NewId gives an id no other call gives: by default a version 7
// UUID, each sorting after the one before it in this process, or
// else one from the maker an application's main handed MakeIdsWith.
func NewId() Uuid {
	m := idMaker.Load()
	if m == nil {
		idMaker.CompareAndSwap(nil, &ownIds)
		m = idMaker.Load()
	}
	return (*m)()
}

// MakeIdsWith has newId() give maker's ids. A main calls it once, before
// any id is made: how ids are made does not change while a program runs.
// That no other call gives the same id, and that ids sort by when they
// were made, is then maker's to keep (spec.md 7).
func MakeIdsWith(maker func() Uuid) {
	if !idMaker.CompareAndSwap(nil, &maker) {
		if idMaker.Load() == &ownIds {
			panic("kogo.MakeIdsWith after an id was made: call it first in main")
		}
		panic("kogo.MakeIdsWith twice: call it once in main")
	}
}

var (
	idMaker atomic.Pointer[func() Uuid]
	ownIds  = ownId
	lastId  struct {
		sync.Mutex
		ms, count uint64
	}
)

// ownId is RFC 9562's method 1 (6.2): rand_a is a 12-bit counter, set
// at random below 2048 in each new millisecond and counted up within
// it, so a millisecond holds at least 2048 ids before it borrows the
// next. The time never goes back from the last id's, so neither does
// an id when the clock does.
func ownId() Uuid {
	var u Uuid
	rand.Read(u[6:])
	lastId.Lock()
	ms := uint64(time.Now().UnixMilli())
	switch {
	case ms > lastId.ms:
		lastId.ms, lastId.count = ms, uint64(binary.BigEndian.Uint16(u[6:]))&0x7FF
	case lastId.count < 0xFFF:
		lastId.count++
	default:
		lastId.ms, lastId.count = lastId.ms+1, uint64(binary.BigEndian.Uint16(u[6:]))&0x7FF
	}
	binary.BigEndian.PutUint64(u[:8], lastId.ms<<16|0x7000|lastId.count)
	lastId.Unlock()
	u[8] = u[8]&0x3F | 0x80
	return u
}

// RandomString gives length letters and digits from the operating
// system's cryptographic source, each as likely as any other: a byte
// of 248 or more, past four times the 62, is drawn again, not folded.
func RandomString(length int32) string {
	if length < 0 {
		panic("randomString's length " + strconv.Itoa(int(length)) + " is negative")
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out, b := make([]byte, 0, length), make([]byte, 1)
	for len(out) < int(length) {
		if rand.Read(b); b[0] < 248 {
			out = append(out, alphabet[b[0]%62])
		}
	}
	return string(out)
}
