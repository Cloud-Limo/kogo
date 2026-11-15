package rt

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf16"
)

// A data class is a struct whose first field is blank and tagged
// kogo:"Name", or kogo:"Name,object" for a data object, and whose
// other fields are tagged with their property's name. A type that Go
// implements is compared by its exported fields and has a String
// method for its text.

// Equal is a == b for any two Kogo values (spec.md section 3): null
// is nil, a T? holding a value is equal to that T, lists and data
// classes are compared part by part, and a Double as Kotlin's equals
// compares it, so NaN is equal to NaN and 0.0 is not equal to -0.0.
// A function is equal to nothing.
func Equal(a, b any) bool { return equal(reflect.ValueOf(a), reflect.ValueOf(b)) }

// strip gives the value inside pointers and interfaces, or the zero
// Value for null.
func strip(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

// origin names a type without its type arguments: an Ok<Int> held as
// an Outcome<Int?> is still an Ok.
func origin(t reflect.Type) string {
	name, _, _ := strings.Cut(t.Name(), "[")
	return t.PkgPath() + "." + name
}

func equal(a, b reflect.Value) bool {
	a, b = strip(a), strip(b)
	if !a.IsValid() || !b.IsValid() {
		return a.IsValid() == b.IsValid()
	}
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint16, reflect.Uint8: // a Char, and a byte of a Uuid
		return a.Uint() == b.Uint()
	case reflect.Float64:
		x, y := a.Float(), b.Float()
		return x == y && math.Signbit(x) == math.Signbit(y) || x != x && y != y
	case reflect.String:
		return a.Type() == b.Type() && a.String() == b.String()
	case reflect.Slice, reflect.Array: // an array is a Uuid's bytes
		if a.Len() != b.Len() {
			return false
		}
		for i := range a.Len() {
			if !equal(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		if origin(a.Type()) != origin(b.Type()) {
			return false
		}
		for i := range a.NumField() {
			if a.Type().Field(i).IsExported() && !equal(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	}
	return false
}

// ToString is x.toString(), and the text of x in a string template,
// as Kotlin writes it.
func ToString(x any) string { return text(reflect.ValueOf(x)) }

func text(v reflect.Value) string {
	if v = strip(v); !v.IsValid() {
		return "null"
	}
	if v.CanInterface() {
		if s, ok := v.Interface().(interface{ String() string }); ok {
			return s.String()
		}
	}
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Float64:
		return double(v.Float())
	case reflect.String:
		return v.String()
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = text(v.Index(i))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case reflect.Struct:
		t := v.Type()
		name, object := strings.CutSuffix(t.Field(0).Tag.Get("kogo"), ",object")
		if object {
			return name
		}
		var parts []string
		for i := 1; i < t.NumField(); i++ {
			parts = append(parts, t.Field(i).Tag.Get("kogo")+"="+text(v.Field(i)))
		}
		return name + "(" + strings.Join(parts, ", ") + ")"
	}
	return "function"
}

// String is a Char's text: half a surrogate pair is U+FFFD.
func (c Char) String() string { return string(utf16.Decode([]uint16{uint16(c)})) }

// double writes a Double as Kotlin does: the shortest digits that
// give the number back, with a point always, and with an exponent
// below 0.001 and from 10,000,000 up.
func double(f float64) string {
	switch {
	case f != f:
		return "NaN"
	case math.IsInf(f, 0):
		return strings.TrimPrefix(strconv.FormatFloat(f, 'f', -1, 64), "+") + "inity"
	case f == 0 || math.Abs(f) >= 1e-3 && math.Abs(f) < 1e7:
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	mantissa, exp, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	if !strings.Contains(mantissa, ".") {
		mantissa += ".0"
	}
	n, _ := strconv.Atoi(exp)
	return mantissa + "E" + strconv.Itoa(n)
}
