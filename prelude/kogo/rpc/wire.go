package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"kogo/prelude/kogo"
)

// JSON as spec.md 15.4 has it, by one walk over the Go types the
// emitter writes: a data class is a struct tagged with its Kogo names
// (kogo/rt), T? a pointer, a List a slice, an enum a named string, a
// sealed interface a Go interface, Unit an empty struct. Go's
// reflection lists neither an enum's constants nor a sealed
// interface's variants, so a table carries those.

const maxLong = 1<<53 - 1 // the whole numbers JavaScript holds exactly

var uuidType = reflect.TypeFor[kogo.Uuid]()

// A Sealed is one sealed interface's variants.
type Sealed struct {
	of       reflect.Type
	variants []reflect.Type
}

// Variants gives the variants of the sealed interface I, from one
// value of each.
func Variants[I any](variants ...I) Sealed {
	s := Sealed{of: reflect.TypeFor[I]()}
	for _, v := range variants {
		s.variants = append(s.variants, reflect.TypeOf(v))
	}
	return s
}

// A reader reads input strictly. It knows each enum's constants and
// each sealed interface's variants by name.
type reader struct {
	enums    map[reflect.Type][]string
	variants map[reflect.Type]map[string]reflect.Type
}

// newReader takes each enum's entries as the emitter writes them, a
// slice of its constants, and each sealed interface's variants.
func newReader(enums []any, sealed []Sealed) reader {
	r := reader{map[reflect.Type][]string{}, map[reflect.Type]map[string]reflect.Type{}}
	for _, entries := range enums {
		v := reflect.ValueOf(entries)
		for i := range v.Len() {
			r.enums[v.Type().Elem()] = append(r.enums[v.Type().Elem()], v.Index(i).String())
		}
	}
	for _, s := range sealed {
		r.variants[s.of] = map[string]reflect.Type{}
		for _, v := range s.variants {
			r.variants[s.of][name(v)] = v
		}
	}
	return r
}

// name is a data class's or a data object's simple Kogo name, from its
// tag.
func name(t reflect.Type) string {
	if t.NumField() == 0 {
		return "Unit"
	}
	n, _, _ := strings.Cut(t.Field(0).Tag.Get("kogo"), ",")
	return n
}

func quote(s string) string {
	text, _ := json.Marshal(s)
	return string(text)
}

func bad(at, why string, args ...any) error {
	return fmt.Errorf("%s %s", at, fmt.Sprintf(why, args...))
}

// readBody reads one JSON value of type t from body, and nothing after
// it. Its error names the key, never the value.
func (r reader) readBody(t reflect.Type, body io.Reader) (reflect.Value, error) {
	d := json.NewDecoder(body)
	d.UseNumber()
	var j any
	if err := d.Decode(&j); err != nil {
		return reflect.Value{}, bad("the input", "is not JSON")
	}
	if _, err := d.Token(); err != io.EOF {
		return reflect.Value{}, bad("the input", "holds more than one JSON value")
	}
	return r.read(t, j, "input")
}

// read gives j, as encoding/json decoded it, as a value of type t. at
// is j's key.
func (r reader) read(t reflect.Type, j any, at string) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	n, isNumber := j.(json.Number)
	s, isString := j.(string)
	m, isObject := j.(map[string]any)
	switch {
	case j == nil && t.Kind() != reflect.Pointer:
		return v, bad(at, "is null")
	case j == nil:
	case t.Kind() == reflect.Pointer:
		e, err := r.read(t.Elem(), j, at)
		v.Set(reflect.New(t.Elem()))
		v.Elem().Set(e)
		return v, err
	case t.Kind() == reflect.Bool:
		b, ok := j.(bool)
		if !ok {
			return v, bad(at, "is not true or false")
		}
		v.SetBool(b)
	case t.Kind() == reflect.Int32 || t.Kind() == reflect.Int64:
		lo, hi, what := int64(math.MinInt32), int64(math.MaxInt32), "an Int"
		if t.Kind() == reflect.Int64 {
			lo, hi, what = -maxLong, maxLong, "a Long, which crosses from -9007199254740991 to 9007199254740991"
		}
		i, err := strconv.ParseInt(n.String(), 10, 64)
		switch {
		case !isNumber:
			return v, bad(at, "is not a number")
		case strings.ContainsAny(n.String(), ".eE"):
			return v, bad(at, "is not a whole number")
		case err != nil || i < lo || i > hi:
			return v, bad(at, "does not fit %s", what)
		}
		v.SetInt(i)
	case t.Kind() == reflect.Float64:
		f, err := strconv.ParseFloat(n.String(), 64)
		if !isNumber {
			return v, bad(at, "is not a number")
		} else if err != nil {
			return v, bad(at, "does not fit a Double")
		}
		v.SetFloat(f)
	case (t == uuidType || t.Kind() == reflect.String) && !isString:
		return v, bad(at, "is not a string")
	case t == uuidType:
		u, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
		if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' || err != nil {
			return v, bad(at, "is not a Uuid")
		}
		v.Set(reflect.ValueOf(kogo.Uuid(u)))
	case t.Kind() == reflect.String && t != reflect.TypeFor[string](): // an enum
		names, ok := r.enums[t]
		if !ok {
			panic("the table holds no constants of the enum " + t.String())
		} else if !slices.Contains(names, s) {
			return v, bad(at, "is not one of %s", strings.Join(names, ", "))
		}
		v.SetString(s)
	case t.Kind() == reflect.String:
		v.SetString(s)
	case t.Kind() == reflect.Slice:
		xs, ok := j.([]any)
		if !ok {
			return v, bad(at, "is not an array")
		}
		v.Set(reflect.MakeSlice(t, len(xs), len(xs)))
		for i, x := range xs {
			e, err := r.read(t.Elem(), x, fmt.Sprintf("%s[%d]", at, i))
			if err != nil {
				return v, err
			}
			v.Index(i).Set(e)
		}
	case !isObject:
		return v, bad(at, "is not an object")
	case t.Kind() == reflect.Interface: // a sealed interface
		variants, ok := r.variants[t]
		tag, _ := m["type"].(string)
		vt, known := variants[tag]
		switch {
		case !ok:
			panic("the table holds no variants of the sealed interface " + t.String())
		case m["type"] == nil:
			return v, bad(at+".type", "is missing")
		case !known:
			return v, bad(at+".type", "is not one of %s", strings.Join(slices.Sorted(maps.Keys(variants)), ", "))
		}
		e, err := r.object(vt, m, at, true)
		v.Set(e)
		return v, err
	case t.Kind() == reflect.Struct: // a data class, a data object or Unit
		return r.object(t, m, at, false)
	default:
		panic("no " + t.String() + " crosses the wire")
	}
	return v, nil
}

// object reads a struct from the keys of m: each property's, and type
// too where the struct is a sealed interface's variant.
func (r reader) object(t reflect.Type, m map[string]any, at string, variant bool) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	keys := map[string]bool{"type": variant}
	for i := range t.NumField() {
		if f := t.Field(i); f.Name != "_" {
			key := f.Tag.Get("kogo")
			x, ok := m[key]
			if !ok {
				return v, bad(at+"."+key, "is missing")
			}
			e, err := r.read(f.Type, x, at+"."+key)
			if err != nil {
				return v, err
			}
			v.Field(i).Set(e)
			keys[key] = true
		}
	}
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !keys[k] {
			return v, bad(at+"."+k, "is not a key of %s", name(t))
		}
	}
	return v, nil
}

// write writes v as JSON; variant is whether v is a sealed interface's
// variant, which writes its name under type. A Long past what
// JavaScript holds exactly, and a Double that is NaN or an infinity,
// have no JSON and are a bug: a panic that names where in the answer
// of op it is.
func write(b *bytes.Buffer, v reflect.Value, op, at string, variant bool) {
	bug := func(what string) { panic(fmt.Sprintf("the answer of %s holds %s at %s", op, what, at)) }
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			b.WriteString("null")
		} else {
			write(b, v.Elem(), op, at, v.Kind() == reflect.Interface)
		}
	case reflect.Bool:
		b.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int32, reflect.Int64:
		if v.Int() > maxLong || v.Int() < -maxLong {
			bug("a Long past ±9007199254740991, which JavaScript cannot hold exactly; send it as a String")
		}
		b.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Float64:
		if math.IsNaN(v.Float()) || math.IsInf(v.Float(), 0) {
			bug("NaN or an infinity, which JSON has no number for")
		}
		text, _ := json.Marshal(v.Float())
		b.Write(text)
	case reflect.String:
		b.WriteString(quote(v.String()))
	case reflect.Array: // a Uuid
		b.WriteString(quote(v.Interface().(kogo.Uuid).String()))
	case reflect.Slice:
		b.WriteByte('[')
		for i := range v.Len() {
			if i > 0 {
				b.WriteByte(',')
			}
			write(b, v.Index(i), op, fmt.Sprintf("%s[%d]", at, i), false)
		}
		b.WriteByte(']')
	case reflect.Struct:
		sep := "{"
		if variant {
			b.WriteString(sep + `"type":` + quote(name(v.Type())))
			sep = ","
		}
		for i := range v.NumField() {
			if f := v.Type().Field(i); f.Name != "_" {
				b.WriteString(sep + quote(f.Tag.Get("kogo")) + ":")
				write(b, v.Field(i), op, at+"."+f.Tag.Get("kogo"), false)
				sep = ","
			}
		}
		if sep == "{" {
			b.WriteString(sep)
		}
		b.WriteByte('}')
	default:
		panic("no " + v.Type().String() + " crosses the wire")
	}
}
