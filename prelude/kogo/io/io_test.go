package io

import (
	"strings"
	"testing"
)

// readln gives each line without its break, the last even with none,
// and panics past the end, as Kotlin's does.
func TestConsole(t *testing.T) {
	var out strings.Builder
	c := NewConsole(strings.NewReader("ACCOUNT\r\nno\nlast"), &out)
	Console_Print(c, "Type: ")
	Console_Println(c, "ok")
	for _, want := range []string{"ACCOUNT", "no", "last"} {
		if got := Console_Readln(c); got != want {
			t.Errorf("readln gives %q, want %q", got, want)
		}
	}
	if out.String() != "Type: ok\n" {
		t.Errorf("wrote %q", out.String())
	}
	defer func() {
		if recover() == nil {
			t.Error("readln past the end does not panic")
		}
	}()
	Console_Readln(c)
}
