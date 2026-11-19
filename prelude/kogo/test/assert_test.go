package test

import (
	"testing"

	"kogo/rt"
)

// Each assertion that fails panics with the message Kotlin's
// gives (2.2, DefaultAsserter), and one that holds does nothing.
func TestMessages(t *testing.T) {
	said := rt.Ptr(rt.Ptr("the total"))
	for want, assert := range map[string]func(){
		"Expected <1>, actual <2>.":                func() { AssertEquals[int32](1, 2, nil) },
		"the total. Expected <a>, actual <null>.":  func() { AssertEquals(rt.Ptr("a"), nil, said) },
		"Illegal value: <[1, 2]>.":                 func() { AssertNotEquals([]int32{1, 2}, []int32{1, 2}, nil) },
		"Expected value to be true.":               func() { AssertTrue(false, nil) },
		"the total":                                func() { AssertTrue(false, said) },
		"Expected value to be false.":              func() { AssertFalse(true, nil) },
		"Expected value to be null, but was: <7>.": func() { AssertNull(rt.Ptr(int32(7)), nil) },
		"Expected the collection to contain the element.\nCollection <[1, 2]>, element <3>.": func() {
			AssertContains_ListT_T_StringOrNull([]int32{1, 2}, 3, nil)
		},
		"Expected the char sequence to contain the substring.\nCharSequence <abc>, substring <d>, ignoreCase <false>.": func() {
			AssertContains_String_String_StringOrNull("abc", "d", nil)
		},
		"no such case": func() { Fail(rt.Ptr(rt.Ptr("no such case"))) },
		"null":         func() { Fail(nil) },
		"": func() { // each holds
			AssertEquals([]int32{1}, []int32{1}, nil)
			AssertNotEquals[int32](1, 2, nil)
			AssertTrue(true, nil)
			AssertFalse(false, nil)
			AssertNull[int32](nil, nil)
			AssertContains_ListT_T_StringOrNull([]string{"a"}, "a", nil)
			AssertContains_String_String_StringOrNull("abc", "bc", nil)
		},
	} {
		func() {
			defer func() {
				if got := recover(); got != nil && got != want || got == nil && want != "" {
					t.Errorf("got %v, want %q", got, want)
				}
			}()
			assert()
		}()
	}
}
