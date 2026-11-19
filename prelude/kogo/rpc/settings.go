package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// ReadSettings fills the data class S that an application declares
// for its settings from the "settings" object of its configuration
// file, by the strict reader that reads an operation's input
// (decisions.md, "Kogo declares an application's settings and the
// runtime fills them"). Every property is required: a key that is
// missing or of the wrong type is an error that names it, such as
// "settings.opsOrigin is missing", and never its value. A key that S
// does not have is reported on report, a line each, and ignored, so
// that one file serves two versions of the program across a deploy.
// An enum or a class of the prelude is no setting yet, and an error.
func ReadSettings[S any](settings []byte, report io.Writer) (S, error) {
	var s S
	t := reflect.TypeFor[S]()
	if err := settable(t, "settings", map[reflect.Type]bool{}); err != nil {
		return s, err
	}
	d := json.NewDecoder(bytes.NewReader(settings))
	d.UseNumber()
	var j any
	if len(settings) == 0 {
		return s, bad("settings", "is missing")
	} else if err := d.Decode(&j); err != nil {
		return s, bad("settings", "is not JSON")
	}
	r := reader{extra: func(at, class string) { fmt.Fprintf(report, "%s is not a key of %s: ignored\n", at, class) }}
	v, err := r.read(t, j, "settings")
	if err != nil {
		return s, err
	}
	return v.Interface().(S), nil
}

// settable says why a setting of type t, the property at, cannot be
// read: a type that crosses no wire, an enum or a class of the prelude.
func settable(t reflect.Type, at string, seen map[reflect.Type]bool) error {
	switch {
	case seen[t]:
	case t == uuidType, t == reflect.TypeFor[string]():
	case t.Kind() == reflect.Bool, t.Kind() == reflect.Int32, t.Kind() == reflect.Int64, t.Kind() == reflect.Float64:
	case t.Kind() == reflect.String:
		return bad(at, "is an enum, %s, which no setting is yet: declare it a String", t.Name())
	case t.Kind() == reflect.Pointer, t.Kind() == reflect.Slice:
		return settable(t.Elem(), at, seen)
	case t.Kind() == reflect.Struct && strings.Contains(t.PkgPath(), "/prelude/kogo") && !strings.HasSuffix(t.PkgPath(), "_test"):
		return bad(at, "is the prelude's %s, which no setting is yet: declare a data class of your own", t.Name())
	case t.Kind() == reflect.Struct:
		seen[t] = true
		for i := range t.NumField() {
			if f := t.Field(i); f.Name != "_" {
				if err := settable(f.Type, at+"."+f.Tag.Get("kogo"), seen); err != nil {
					return err
				}
			}
		}
	default:
		return bad(at, "is a %s, which no setting can be", t)
	}
	return nil
}
