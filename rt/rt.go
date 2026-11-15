// Package rt is what every emitted Kogo program runs on, whatever its
// prelude: the types of spec.md section 3 that Go lacks, the helpers
// the emitter calls, and the walk behind == and toString().
package rt

import (
	"iter"
	"math"
)

// Unit is the type of the value Unit, which every fun that states no
// result returns: (T) -> Unit and (T) -> R are then one Go type.
type Unit struct{}

func (Unit) String() string { return "kotlin.Unit" } // as Kotlin writes it

// Nothing is the type that has no value.
type Nothing struct{}

// A Char is one UTF-16 code unit.
type Char uint16

// Never stands where a Nothing is used as a T. Nothing has no value,
// so the expression that gives its argument never finishes.
func Never[T any](Nothing) T { panic("kogo: a Nothing has a value") }

// Null gives null as a T?, from a value that can only be null.
func Null[T any](*Nothing) *T { return nil }

// Ptr gives a T as a T?.
func Ptr[T any](x T) *T { return &x }

// Widen gives an A? as a B?, when A is used as a wider B.
func Widen[A, B any](p *A, f func(A) B) *B {
	if p == nil {
		return nil
	}
	return Ptr(f(*p))
}

// Val reads a local var. Go evaluates calls left to right, and not a
// variable read beside a call that changes it, so the read is a call.
func Val[T any](x T) T { return x }

// Div is a / b. It is a call so that a division by zero panics in
// source order, and so that Go never folds it at compile time.
func Div[T int32 | int64 | float64](a, b T) T { return a / b }

// Rem is a % b on whole numbers, and Mod on Doubles.
func Rem[T int32 | int64](a, b T) T { return a % b }
func Mod(a, b float64) float64      { return math.Mod(a, b) }

// In reports x in lo..hi, and InUntil x in lo until hi.
func In(x, lo, hi int64) bool      { return lo <= x && x <= hi }
func InUntil(x, lo, hi int64) bool { return lo <= x && x < hi }

// Range gives lo..hi to a for, and Until gives lo until hi.
func Range[T int32 | int64](lo, hi T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := lo; i <= hi && yield(i) && i != hi; i++ {
		}
	}
}

func Until[T int32 | int64](lo, hi T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := lo; i < hi && yield(i); i++ {
		}
	}
}

// Is reports x is T, for a value of a sealed interface, and IsNullable
// the same for a nullable one.
func Is[T any](x any) bool {
	_, ok := x.(T)
	return ok
}

func IsNullable[T, I any](x *I) bool { return x != nil && Is[T](any(*x)) }
