// Package test is the Go behind Kogo's package kotlin.test, in
// prelude/kotlin_test.kg: kotlin.test's assertions (spec.md 12).
// One that fails panics with the message Kotlin's gives, which fails
// the test that called it.
package test

import (
	"slices"
	"strings"

	"kogo/rt"
)

// said gives Kotlin's message: the caller's, a full stop and what the
// assertion says.
func said(message **string, what string) string {
	if message != nil && *message != nil {
		return **message + ". " + what
	}
	return what
}

// or gives the caller's message alone where it has one, as Kotlin's
// assertTrue, assertFalse and fail do.
func or(message **string, what string) string {
	if message != nil && *message != nil {
		return **message
	}
	return what
}

func AssertEquals[T any](expected, actual T, message **string) rt.Unit {
	if !rt.Equal(expected, actual) {
		panic(said(message, "Expected <"+rt.ToString(expected)+">, actual <"+rt.ToString(actual)+">."))
	}
	return rt.Unit{}
}

func AssertNotEquals[T any](illegal, actual T, message **string) rt.Unit {
	if rt.Equal(illegal, actual) {
		panic(said(message, "Illegal value: <"+rt.ToString(actual)+">."))
	}
	return rt.Unit{}
}

func AssertTrue(actual bool, message **string) rt.Unit {
	if !actual {
		panic(or(message, "Expected value to be true."))
	}
	return rt.Unit{}
}

func AssertFalse(actual bool, message **string) rt.Unit {
	if actual {
		panic(or(message, "Expected value to be false."))
	}
	return rt.Unit{}
}

func AssertNull[T any](actual *T, message **string) rt.Unit {
	if actual != nil {
		panic(said(message, "Expected value to be null, but was: <"+rt.ToString(*actual)+">."))
	}
	return rt.Unit{}
}

func AssertContains_ListT_T_StringOrNull[T any](iterable []T, element T, message **string) rt.Unit {
	if !slices.ContainsFunc(iterable, func(x T) bool { return rt.Equal(x, element) }) {
		panic(said(message, "Expected the collection to contain the element.\nCollection <"+rt.ToString(iterable)+">, element <"+rt.ToString(element)+">."))
	}
	return rt.Unit{}
}

func AssertContains_String_String_StringOrNull(charSequence, other string, message **string) rt.Unit {
	if !strings.Contains(charSequence, other) {
		panic(said(message, "Expected the char sequence to contain the substring.\nCharSequence <"+charSequence+">, substring <"+other+">, ignoreCase <false>."))
	}
	return rt.Unit{}
}

// Fail panics with the message, or with Kotlin's null where there is
// none.
func Fail(message **string) rt.Nothing { panic(or(message, "null")) }
