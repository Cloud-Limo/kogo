package rpc_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Cloud-Limo/kogo/prelude/kogo/rpc"
)

// The classes are declared outside rpc, as an application's are: one
// of the prelude's own is no setting.
type Limits struct {
	_       struct{} `kogo:"Limits"`
	Retries int32    `kogo:"retries"`
}

type Settings struct {
	_         struct{} `kogo:"Settings"`
	OpsOrigin string   `kogo:"opsOrigin"`
	Flags     []bool   `kogo:"flags"`
	Limits    Limits   `kogo:"limits"`
	Note      *string  `kogo:"note"`
}

// Settings are read as an input is, but that a key the class does not
// have is reported and ignored, at any depth; an error names its key
// and never the value.
func TestReadSettings(t *testing.T) {
	const whole = `{"opsOrigin":"https://ops.example","flags":[true],"limits":{"retries":3},"note":null`
	var report strings.Builder
	s, err := rpc.ReadSettings[Settings]([]byte(whole+`,"old":1,"limits":{"retries":3,"gone":"secret"}}`), &report)
	if err != nil || s.OpsOrigin != "https://ops.example" || !s.Flags[0] || s.Limits.Retries != 3 || s.Note != nil {
		t.Errorf("read %+v, %v", s, err)
	}
	if want := "settings.limits.gone is not a key of Limits: ignored\nsettings.old is not a key of Settings: ignored\n"; report.String() != want {
		t.Errorf("reported %q, want %q", report.String(), want)
	}
	for _, c := range [][2]string{
		{`{"flags":[],"limits":{"retries":3},"note":null}`, "settings.opsOrigin is missing"},
		{`{"opsOrigin":"secret","flags":[],"limits":{"retries":"secret"},"note":null}`, "settings.limits.retries is not a number"},
		{`{"opsOrigin":"secret","flags":[],"limits":{},"note":null}`, "settings.limits.retries is missing"},
		{`{"opsOrigin":"secret","flags":[],"limits":{"retries":3}}`, "settings.note is missing"},
		{`{"opsOrigin":"x\u0000","flags":[],"limits":{"retries":3},"note":null}`, "settings.opsOrigin holds U+0000, which no input or setting may: leave it out"},
		{`null`, "settings is null"},
		{``, "settings is missing"},
		{`{`, "settings is not JSON"},
	} {
		if _, err := rpc.ReadSettings[Settings]([]byte(c[0]), &report); err == nil || err.Error() != c[1] {
			t.Errorf("%s gives %v, want %s", c[0], err, c[1])
		}
	}
	type Colour string
	// What no setting is yet is refused whatever the file holds.
	type withEnum struct {
		_    struct{} `kogo:"WithEnum"`
		Mode []Colour `kogo:"mode"`
	}
	type withPrelude struct {
		_      struct{}    `kogo:"WithPrelude"`
		Cookie *rpc.Cookie `kogo:"cookie"`
	}
	type withFunc struct {
		_ struct{} `kogo:"WithFunc"`
		F func()   `kogo:"f"`
	}
	_, enum := rpc.ReadSettings[withEnum](nil, &report)
	_, prelude := rpc.ReadSettings[withPrelude](nil, &report)
	_, fn := rpc.ReadSettings[withFunc](nil, &report)
	if got, want := fmt.Sprint(enum, "; ", prelude, "; ", fn), "settings.mode is an enum, Colour, which no setting is yet: declare it a String; "+
		"settings.cookie is the prelude's Cookie, which no setting is yet: declare a data class of your own; settings.f is a func(), which no setting can be"; got != want {
		t.Errorf("refused with %s, want %s", got, want)
	}
}
