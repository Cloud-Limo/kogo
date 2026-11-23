package prelude

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/Cloud-Limo/kogo/prelude/kogo/http"
	"github.com/Cloud-Limo/kogo/prelude/kogo/mail"
	"github.com/Cloud-Limo/kogo/prelude/kogo/rpc"
	"github.com/Cloud-Limo/kogo/prelude/kogo/work"
	"github.com/Cloud-Limo/kogo/syntax"
)

// Each data class the prelude declares and Go writes by hand is the
// struct the emitter would write (decisions.md, DEV-988): its name
// tag, then each property's field and tag in order, so that a property
// dropped, renamed or moved on one side alone fails here, and its text
// stays Kotlin's. A nested class is found by its Go name, Outer_Name:
// HttpFailure, Act and Failure each have a Refused.
func TestHandWritten(t *testing.T) {
	goes := map[string]reflect.Type{}
	for _, v := range []any{mail.Address{}, mail.Attachment{}, mail.Mail{}, rpc.Cookie{}, rpc.Reply[int]{},
		http.HttpFailure_NotSent{}, http.HttpFailure_Unknown{}, http.HttpFailure_Refused{}, http.HttpFailure_Unreadable{},
		work.Act_Done[int]{}, work.Act_Refused{}, work.Act_NotMade{}, work.Act_Unknown{},
		work.Failure_Refused{}, work.Failure_NotMade{}, work.Failure_Exhausted{}, work.Failure_Unrecorded{}, work.Alert{}} {
		name, _, _ := strings.Cut(reflect.TypeOf(v).Name(), "[")
		if tag := reflect.TypeOf(v).Field(0).Tag.Get("kogo"); name != tag && !strings.HasSuffix(name, "_"+tag) {
			t.Errorf("%s is tagged %s", name, tag)
		}
		goes[name] = reflect.TypeOf(v)
	}
	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	var walk func(string, []syntax.Decl)
	walk = func(outer string, decls []syntax.Decl) {
		for _, d := range decls {
			c, ok := d.(*syntax.ClassDecl)
			if !ok {
				continue
			}
			name := outer + c.Name
			walk(name+"_", c.Body)
			if c.Kind != "data class" {
				continue
			}
			g, ok := goes[name]
			if !ok {
				t.Errorf("data class %s has no Go here", name)
				continue
			}
			delete(goes, name)
			var want, got []string
			for _, p := range c.Params {
				want = append(want, fmt.Sprintf("%c%s %s", unicode.ToUpper(rune(p.Name[0])), p.Name[1:], p.Name))
			}
			for i := 1; i < g.NumField(); i++ {
				got = append(got, g.Field(i).Name+" "+g.Field(i).Tag.Get("kogo"))
			}
			if strings.Join(got, ", ") != strings.Join(want, ", ") {
				t.Errorf("%s: Go has %v, the prelude %v", c.Name, got, want)
			}
		}
	}
	for _, f := range files {
		walk("", f.Decls)
	}
	for name := range goes {
		t.Errorf("%s is in no prelude file", name)
	}
}
