package http

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
)

// A Json is one JSON object, kept as the text it came as. A read
// gives nil where the key is absent, is JSON's null or holds another
// type. == compares the text, which is exported for rt to reach.
type Json struct{ Text string }

func (j Json) String() string { return j.Text }

// Json_Raw is the object as JSON text, untouched.
func Json_Raw(this Json) string { return this.Text }

// Json_Parse is text as a Json, or nil when it is not one JSON
// object.
func Json_Parse(text string) *Json {
	var members map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &members) != nil || members == nil {
		return nil
	}
	return &Json{text}
}

// encode writes a string or a list of them as JSON, with no escaping
// for HTML: what is sent is not a page.
func encode(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		panic(err) // a string always has JSON
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// read gives a key's value as T, by JSON's own types: a string is not
// read as a number, nor a number as a string.
func read[T any](j Json, key string) *T {
	var members map[string]json.RawMessage
	if json.Unmarshal([]byte(j.Text), &members) != nil {
		return nil
	}
	var v *T
	if json.Unmarshal(members[key], &v) != nil {
		return nil
	}
	return v
}

func Json_String(this Json, key string) *string { return read[string](this, key) }
func Json_Boolean(this Json, key string) *bool  { return read[bool](this, key) }

// number is a key's value as its digits, when it is a JSON number.
func number(j Json, key string) (string, bool) {
	raw := read[json.RawMessage](j, key)
	if raw == nil || len(*raw) == 0 || (*raw)[0] != '-' && ((*raw)[0] < '0' || (*raw)[0] > '9') {
		return "", false
	}
	return string(*raw), true
}

// Json_Long is exact: Go reads every JSON number as a float64, which
// holds no whole number past 2^53 exactly, so the digits are read as
// they are written. A number with a fraction or an exponent, or past
// a Long, is nil.
func Json_Long(this Json, key string) *int64 {
	if n, ok := number(this, key); ok {
		if v, err := strconv.ParseInt(n, 10, 64); err == nil {
			return &v
		}
	}
	return nil
}

// Json_Double is the nearest Double, and nil for a number past one.
func Json_Double(this Json, key string) *float64 {
	if n, ok := number(this, key); ok {
		if v, err := strconv.ParseFloat(n, 64); err == nil {
			return &v
		}
	}
	return nil
}

// Json_At is a nested object.
func Json_At(this Json, key string) *Json {
	raw := read[json.RawMessage](this, key)
	if raw == nil {
		return nil
	}
	return Json_Parse(string(*raw))
}

// Json_List is an array of objects, and nil when any of its elements
// is not one.
func Json_List(this Json, key string) *kogo.List[Json] {
	raws := read[[]json.RawMessage](this, key)
	if raws == nil {
		return nil
	}
	list := kogo.List[Json]{}
	for _, raw := range *raws {
		j := Json_Parse(string(raw))
		if j == nil {
			return nil
		}
		list = append(list, *j)
	}
	return &list
}

// Json_Strings is an array of strings, and nil when any of its
// elements is not one: read as pointers, since Go reads a null into a
// string as "".
func Json_Strings(this Json, key string) *kogo.List[string] {
	elements := read[[]*string](this, key)
	if elements == nil {
		return nil
	}
	list := kogo.List[string]{}
	for _, e := range *elements {
		if e == nil {
			return nil
		}
		list = append(list, *e)
	}
	return &list
}
