// Package math is the Go behind Kogo's package kogo.math, in
// prelude/math.kg.
package math

import gomath "math"

// PI is a variable, not a constant: Go works a constant expression
// such as PI / 180.0 out exactly, where Kotlin divides two Doubles.
var PI = gomath.Pi

// Sin, Cos and Asin are Go's, which may differ from Java's in the last
// place (decisions.md, the entry on kogo.math). Outside asin's domain
// is NaN, as in Kotlin.
func Sin(x float64) float64  { return gomath.Sin(x) }
func Cos(x float64) float64  { return gomath.Cos(x) }
func Asin(x float64) float64 { return gomath.Asin(x) }

// Sqrt is the correctly rounded root, as Java's is; a negative is NaN.
func Sqrt(x float64) float64 { return gomath.Sqrt(x) }
