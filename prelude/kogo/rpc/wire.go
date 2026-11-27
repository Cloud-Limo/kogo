package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	ktime "github.com/Cloud-Limo/kogo/prelude/kogo/time"
)

// JSON as spec.md 15.4 has it, by one walk over the Go types the
// emitter writes: a data class is a struct tagged with its Kogo names
// (rt), T? a pointer, a List a slice, an enum a named string,
// Unit an empty struct; a Uuid, a LocalDate and a LocalTime cross as
// their text. Go's reflection does not list an enum's constants, so a
// table carries those.

const maxLong = 1<<53 - 1 // the whole numbers JavaScript holds exactly

var uuidType = reflect.TypeFor[kogo.Uuid]()
var dateType, timeType = reflect.TypeFor[ktime.LocalDate](), reflect.TypeFor[ktime.LocalTime]()

// textual says whether t crosses as a string that is not a String's.
func textual(t reflect.Type) bool { return t == uuidType || t == dateType || t == timeType }

// A reader reads input strictly. It knows each enum's constants. With
// extra, it reports a key that a data class does not have, by where
// it is and the class's name, and reads on; with none, the key is an
// error.
type reader struct {
	enums map[reflect.Type][]string
	extra func(at, class string)
}

// newReader takes each enum's entries as the emitter writes them, a
// slice of its constants.
func newReader(enums []any) reader {
	r := reader{enums: map[reflect.Type][]string{}}
	for _, entries := range enums {
		v := reflect.ValueOf(entries)
		t := v.Type().Elem()
		names := r.enums[t]
		for i := range v.Len() {
			names = append(names, v.Index(i).String())
		}
		r.enums[t] = names // an enum with no constants is in the table too
	}
	return r
}

// name is a data class's simple Kogo name, from its tag.
func name(t reflect.Type) string {
	if t.NumField() == 0 {
		return "Unit"
	}
	return t.Field(0).Tag.Get("kogo")
}

// quote writes s as a JSON string, as kotlinx.serialization does:
// only a quote, a backslash and a character below U+0020 are escaped, so
// & < > and U+2028 go out as themselves, where encoding/json escapes
// them.
func quote(s string) string {
	b := []byte{'"'}
	for _, r := range s { // a byte that is not UTF-8 is U+FFFD
		if e, ok := escapes[r]; ok {
			b = append(b, e...)
		} else if r < 0x20 {
			b = fmt.Appendf(b, `\u%04x`, r)
		} else {
			b = utf8.AppendRune(b, r)
		}
	}
	return string(append(b, '"'))
}

var escapes = map[rune]string{'"': `\"`, '\\': `\\`, '\b': `\b`, '\t': `\t`, '\n': `\n`, '\f': `\f`, '\r': `\r`}

func bad(at, why string, args ...any) error {
	return fmt.Errorf("%s %s", at, fmt.Sprintf(why, args...))
}

// readBody reads one JSON value of type t from body, and nothing after
// it, UTF-8 with each surrogate escape paired: encoding/json reads
// anything else as U+FFFD, which would store another text than was
// sent. Its error names the key, never the value, or is body's
// *http.MaxBytesError, let through for the Handler's 413.
func (r reader) readBody(t reflect.Type, body io.Reader) (reflect.Value, error) {
	var text bytes.Buffer
	d := json.NewDecoder(io.TeeReader(body, &text))
	d.UseNumber()
	var j any
	var tooLarge *http.MaxBytesError
	if err := d.Decode(&j); errors.As(err, &tooLarge) {
		return reflect.Value{}, err
	} else if err != nil {
		return reflect.Value{}, bad("the input", "is not JSON")
	}
	if _, err := d.Token(); errors.As(err, &tooLarge) {
		return reflect.Value{}, err
	} else if err == nil {
		return reflect.Value{}, bad("the input", "holds more than one JSON value")
	} else if err != io.EOF {
		return reflect.Value{}, bad("the input", "is not JSON")
	} else if !utf8.Valid(text.Bytes()) {
		return reflect.Value{}, bad("the input", "is not UTF-8: send its text as UTF-8")
	} else if !paired(text.Bytes()) {
		return reflect.Value{}, bad("the input", "holds half of a surrogate pair, which no String may: send both halves or neither")
	}
	return r.read(t, j, "input")
}

// paired says whether each \u escape of a surrogate in text, which is
// JSON, is a high one followed by a low one.
func paired(text []byte) bool {
	unit := func(i int) rune {
		n, _ := strconv.ParseUint(string(text[i:i+4]), 16, 16)
		return rune(n)
	}
	for i := 0; i < len(text); i++ {
		if text[i] != '\\' {
			continue
		}
		i++ // the escaped character
		if text[i] != 'u' || !utf16.IsSurrogate(unit(i+1)) {
			continue
		} else if !bytes.HasPrefix(text[i+5:], []byte(`\u`)) || utf16.DecodeRune(unit(i+1), unit(i+7)) == utf8.RuneError {
			return false
		}
		i += 10 // the pair's last digit
	}
	return true
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
	case (textual(t) || t.Kind() == reflect.String) && !isString:
		return v, bad(at, "is not a string")
	case t == dateType:
		d, ok := ktime.ReadLocalDate(s)
		if !ok {
			return v, bad(at, "is not a LocalDate, a day written YYYY-MM-DD")
		}
		v.Set(reflect.ValueOf(d))
	case t == timeType:
		c, ok := ktime.ReadLocalTime(s)
		if !ok {
			return v, bad(at, "is not a LocalTime, written HH:mm, HH:mm:ss or HH:mm:ss.SSS")
		}
		v.Set(reflect.ValueOf(c))
	case t == uuidType:
		u := kogo.Uuid_ParseOrNull(s)
		if u == nil {
			return v, bad(at, "is not a Uuid")
		}
		v.Set(reflect.ValueOf(*u))
	case t.Kind() == reflect.String && t != reflect.TypeFor[string](): // an enum
		if names := r.enums[t]; !slices.Contains(names, s) {
			return v, bad(at, "is not one of %s", strings.Join(names, ", "))
		}
		v.SetString(s)
	case t.Kind() == reflect.String && strings.ContainsRune(s, 0):
		return v, bad(at, holdsNul)
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
	case t.Kind() == reflect.Struct: // a data class or Unit
		return r.object(t, m, at)
	default:
		panic("no " + t.String() + " crosses the wire")
	}
	return v, nil
}

// object reads a struct from the keys of m, one for each property.
func (r reader) object(t reflect.Type, m map[string]any, at string) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	keys := map[string]bool{}
	n := 0 // the keys of m read, so that only m with more is sorted
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
			n++
		}
	}
	if len(m) == n {
		return v, nil
	}
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !keys[k] && r.extra == nil {
			return v, bad(at+"."+k, "is not a key of %s", name(t))
		} else if !keys[k] {
			r.extra(at+"."+k, name(t))
		}
	}
	return v, nil
}

// holdsNul is how read and valid refuse a String that holds U+0000,
// which Postgres's text cannot hold.
const holdsNul = "holds U+0000, which no input or setting may: leave it out"

// valid says whether each enum that v holds is one of its entries, and
// each String holds no U+0000, as read does of JSON: v is Go's, where
// an enum is any string.
func (r reader) valid(v reflect.Value, at string) error {
	switch t := v.Type(); {
	case t.Kind() == reflect.String && t != reflect.TypeFor[string]():
		if names := r.enums[t]; !slices.Contains(names, v.String()) {
			return bad(at, "is not one of %s", strings.Join(names, ", "))
		}
	case t.Kind() == reflect.String && strings.ContainsRune(v.String(), 0):
		return bad(at, holdsNul)
	case t.Kind() == reflect.Pointer && !v.IsNil():
		return r.valid(v.Elem(), at)
	case t.Kind() == reflect.Slice:
		for i := range v.Len() {
			if err := r.valid(v.Index(i), fmt.Sprintf("%s[%d]", at, i)); err != nil {
				return err
			}
		}
	case t.Kind() == reflect.Struct:
		for i := range t.NumField() {
			if f := t.Field(i); f.Name != "_" {
				if err := r.valid(v.Field(i), at+"."+f.Tag.Get("kogo")); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// check panics if r lacks the constants of an enum that t reaches, or
// if t cannot cross the wire, so that a table missing some fails when
// the server starts and read never panics.
func (r reader) check(t reflect.Type, seen map[reflect.Type]bool) {
	if seen[t] {
		return
	}
	seen[t] = true
	switch {
	case textual(t), t == reflect.TypeFor[string]():
	case t.Kind() == reflect.Bool, t.Kind() == reflect.Int32, t.Kind() == reflect.Int64, t.Kind() == reflect.Float64:
	case t.Kind() == reflect.String:
		if _, ok := r.enums[t]; !ok {
			panic("the table holds no constants of the enum " + t.String())
		}
	case t.Kind() == reflect.Pointer, t.Kind() == reflect.Slice:
		r.check(t.Elem(), seen)
	case t.Kind() == reflect.Struct:
		for i := range t.NumField() {
			if f := t.Field(i); f.Name != "_" {
				r.check(f.Type, seen)
			}
		}
	default:
		panic("no " + t.String() + " crosses the wire")
	}
}

// write writes v as JSON. A Long past what
// JavaScript holds exactly, and a Double that is NaN or an infinity,
// have no JSON and are a bug: a panic that names where in the answer
// of op it is.
func write(b *bytes.Buffer, v reflect.Value, op, at string) {
	bug := func(what string) { panic(fmt.Sprintf("the answer of %s holds %s at %s", op, what, at)) }
	if textual(v.Type()) { // as Kotlin's Uuid and java.time write it
		b.WriteString(quote(v.Interface().(fmt.Stringer).String()))
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			b.WriteString("null")
		} else {
			write(b, v.Elem(), op, at)
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
	case reflect.Slice:
		b.WriteByte('[')
		for i := range v.Len() {
			if i > 0 {
				b.WriteByte(',')
			}
			write(b, v.Index(i), op, fmt.Sprintf("%s[%d]", at, i))
		}
		b.WriteByte(']')
	case reflect.Struct:
		sep := "{"
		for i := range v.NumField() {
			if f := v.Type().Field(i); f.Name != "_" {
				b.WriteString(sep + quote(f.Tag.Get("kogo")) + ":")
				write(b, v.Field(i), op, at+"."+f.Tag.Get("kogo"))
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
