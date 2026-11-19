package check

import (
	"strings"
	"testing"

	"github.com/Cloud-Limo/kogo/prelude"
	"github.com/Cloud-Limo/kogo/syntax"
)

// Section 12, an outside service, against the prelude itself: an
// endpoint says its kind once, where it is declared, so ask takes
// only a Lookup and perform only an Action, and a fun that performs
// is an action fun.
func TestHttpKinds(t *testing.T) {
	pre, err := prelude.Files()
	if err != nil {
		t.Fatal(err)
	}
	head := "package t\nimport kogo.http.Action\nimport kogo.http.Http\nimport kogo.http.HttpError\nimport kogo.http.Json\nimport kogo.http.Lookup\n" +
		"import kogo.http.SendFailure\nimport kogo.http.action\nimport kogo.http.field\nimport kogo.http.lookup\n" +
		"val RENAME: Action = action(\"POST\", \"/v1/names\")\nval RATE: Lookup = lookup(\"GET\", \"/v1/rates\")\n"
	for _, c := range [][2]string{
		{"fun rate(service: Http): Either<HttpError, Json> = service.ask(RATE, listOf(field(\"of\", \"AUD\")))\n" +
			"action fun rename(service: Http): Either<SendFailure, Json> = service.perform(RENAME, listOf(field(\"name\", \"a\")))", ""},
		{"fun rename(service: Http): Either<HttpError, Json> = service.ask(RENAME, listOf(field(\"name\", \"a\")))", "13:66: type-mismatch: this is a Action where a Lookup is wanted"},
		{"action fun rate(service: Http): Either<SendFailure, Json> = service.perform(RATE, emptyList())", "13:77: type-mismatch: this is a Lookup where a Action is wanted"},
		{"fun rename(service: Http): Either<SendFailure, Json> = service.perform(RENAME, emptyList())", "13:64: unmarked-action: "},
		{"val RATES: Lookup = action(\"GET\", \"/v1/rates\")", "13:21: type-mismatch: "},
	} {
		f, err := syntax.Parse("t.kg", []byte(head+c[0]))
		if err != nil {
			t.Fatal(err)
		}
		_, errs := Check(pre, []*syntax.File{f}, nil)
		if c[1] == "" && errs != nil || c[1] != "" && (len(errs) != 1 || !strings.HasPrefix(errs[0].Error(), "t.kg:"+c[1])) {
			t.Errorf("%s\n got %v\nwant %q", c[0], errs, c[1])
		}
	}
}
