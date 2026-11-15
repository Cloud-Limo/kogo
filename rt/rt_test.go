package rt

import (
	"math"
	"reflect"
	"slices"
	"testing"
)

type leg struct {
	_     struct{} `kogo:"Leg"`
	From  string   `kogo:"from"`
	Via   *string  `kogo:"via"`
	Stops []int32  `kogo:"stops"`
}

type dot struct {
	_ struct{} `kogo:"Dot,object"`
}

type box[T any] struct {
	_     struct{} `kogo:"Box"`
	Value T        `kogo:"value"`
}

// bag is a data class whose members are named as a collection's are.
type bag struct {
	_    struct{} `kogo:"Bag"`
	Id   int32    `kogo:"id"`
	Tags []any    `kogo:"tags"`
}

func (b bag) Elements() []any     { return b.Tags }
func (b bag) Keys() reflect.Value { return reflect.ValueOf(b.Tags) }

// pile is a collection Go implements whose keys == compares in any order.
type pile struct {
	Unordered
	keys []int32
}

func (p pile) Keys() reflect.Value { return reflect.ValueOf(p.keys) }

// == walks both values (spec.md section 3): a T? is its value or
// null, and never its address.
func TestEqual(t *testing.T) {
	a, b := "B", "B"
	var none *int32
	var shape any = dot{}
	nan := math.NaN()
	for _, c := range []struct {
		a, b any
		want bool
	}{
		{leg{From: "A", Via: &a, Stops: []int32{1}}, leg{From: "A", Via: &b, Stops: []int32{1}}, true},
		{leg{From: "A", Via: &a}, leg{From: "A"}, false},
		{leg{Stops: []int32{1, 2}}, leg{Stops: []int32{1}}, false},
		{Ptr(int32(5)), int32(5), true}, // an Int? and an Int
		{none, nil, true},
		{none, int32(0), false},
		{&shape, dot{}, true},                                   // a Shape? holding Dot, and Dot
		{box[any]{Value: int32(1)}, box[int32]{Value: 1}, true}, // an Ok<Int> held as an Outcome<Int?>
		{box[int32]{Value: 1}, dot{}, false},
		{nan, nan, true}, // as Kotlin's equals has it, for a Double inside a value
		{0.0, math.Copysign(0, -1), false},
		{Char('a'), Char('a'), true},
		{func() {}, func() {}, false},
		{bag{Id: 1, Tags: []any{"a"}}, bag{Id: 2, Tags: []any{"a"}}, false}, // not a set
		{pile{keys: []int32{1, 2}}, pile{keys: []int32{2, 1}}, true},
		{pile{keys: []int32{1, 2}}, pile{keys: []int32{1, 3}}, false},
		{pile{keys: []int32{1}}, pile{keys: []int32{1, 2}}, false},
	} {
		if got := Equal(c.a, c.b); got != c.want {
			t.Errorf("Equal(%v, %v) is %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// toString() writes what Kotlin writes.
func TestToString(t *testing.T) {
	via, tenth := "B", 0.1 // a variable, so that Go adds as a program does
	for _, c := range []struct {
		x    any
		want string
	}{
		{leg{From: "A", Via: &via, Stops: []int32{1, 2}}, "Leg(from=A, via=B, stops=[1, 2])"},
		{leg{From: "A"}, "Leg(from=A, via=null, stops=[])"},
		{dot{}, "Dot"},
		{nil, "null"},
		{true, "true"},
		{int64(-7), "-7"},
		{Char('x'), "x"},
		{Char(0xD83D), "�"}, // half of a surrogate pair
		{Unit{}, "kotlin.Unit"},
		{1.0, "1.0"},
		{tenth + 0.2, "0.30000000000000004"},
		{-0.0015, "-0.0015"},
		{0.0001, "1.0E-4"},
		{9999999.0, "9999999.0"},
		{1.0e7, "1.0E7"},
		{1.5e300, "1.5E300"},
		{math.Copysign(0, -1), "-0.0"},
		{math.NaN(), "NaN"},
		{math.Inf(-1), "-Infinity"},
	} {
		if got := ToString(c.x); got != c.want {
			t.Errorf("ToString(%v) is %s, want %s", c.x, got, c.want)
		}
	}
}

// A range holds both its ends, an until not its last, and neither
// runs past the largest Int.
func TestRanges(t *testing.T) {
	for _, c := range []struct {
		got  []int32
		want []int32
	}{
		{slices.Collect(Range[int32](1, 3)), []int32{1, 2, 3}},
		{slices.Collect(Until[int32](1, 3)), []int32{1, 2}},
		{slices.Collect(Range[int32](3, 1)), nil},
		{slices.Collect(Range[int32](math.MaxInt32-1, math.MaxInt32)), []int32{math.MaxInt32 - 1, math.MaxInt32}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("got %v, want %v", c.got, c.want)
		}
	}
	if !In(3, 1, 3) || InUntil(3, 1, 3) || In(0, 1, 3) {
		t.Error("In holds both ends, and InUntil not its last")
	}
}
