package rpc

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"

	"kogo/prelude/kogo"
	"kogo/rt"
)

// Wire types as the emitter writes them: every row of spec.md 15.4.

type State string

const (
	State_Offered   State = "Offered"
	State_Confirmed State = "Confirmed"
)

var State_entries = []State{State_Offered, State_Confirmed}

type Stop struct {
	_    struct{} `kogo:"Stop"`
	Name string   `kogo:"name"`
	Back *Stop    `kogo:"back"`
}

type Job struct {
	_       struct{}  `kogo:"Job"`
	Done    bool      `kogo:"done"`
	Number  int32     `kogo:"number"`
	Cents   int64     `kogo:"cents"`
	Rate    float64   `kogo:"rate"`
	Notes   string    `kogo:"notes"`
	Id      kogo.Uuid `kogo:"id"`
	State   State     `kogo:"state"`
	Stops   []Stop    `kogo:"stops"`
	Tip     *int32    `kogo:"tip"`
	Nothing rt.Unit   `kogo:"nothing"`
}

var wire = newReader([]any{State_entries})

const job = `{"done":true,"number":-2147483648,"cents":-9007199254740991,"rate":0.1,"notes":"a \"b\"",` +
	`"id":"0195a3c4-5b6d-7e8f-9a0b-1c2d3e4f5a6b","state":"Confirmed",` +
	`"stops":[{"name":"x","back":null},{"name":"y","back":{"name":"z","back":null}}],"tip":7,"nothing":{}}`

var jobValue = Job{Done: true, Number: math.MinInt32, Cents: -maxLong, Rate: 0.1, Notes: `a "b"`,
	Id:    kogo.Uuid{0x01, 0x95, 0xa3, 0xc4, 0x5b, 0x6d, 0x7e, 0x8f, 0x9a, 0x0b, 0x1c, 0x2d, 0x3e, 0x4f, 0x5a, 0x6b},
	State: State_Confirmed, Stops: []Stop{{Name: "x"}, {Name: "y", Back: &Stop{Name: "z"}}},
	Tip: rt.Ptr[int32](7)}

func read(text string) (Job, error) {
	v, err := wire.readBody(reflect.TypeFor[Job](), strings.NewReader(text))
	if err != nil {
		return Job{}, err
	}
	return v.Interface().(Job), nil
}

// Each row of the table reads and writes, Unit as {} among them, and
// the largest Int and Long too.
func TestWire(t *testing.T) {
	for text, want := range map[string]Job{
		job: jobValue,
		strings.NewReplacer(`-2147483648`, `2147483647`, `-9007199254740991`, `9007199254740991`).Replace(job): func() Job {
			j := jobValue
			j.Number, j.Cents = math.MaxInt32, maxLong
			return j
		}(),
	} {
		got, err := read(text)
		if err != nil || !rt.Equal(got, want) {
			t.Errorf("%s reads as %v (%v)", text, got, err)
		}
		if back := answer("op", want); string(back) != `{"type":"ok","value":`+text+`}` {
			t.Errorf("%v writes as %s", want, back)
		}
	}
	if back := answer("op", rt.Unit{}); string(back) != `{"type":"ok","value":{}}` {
		t.Errorf("Unit writes as %s", back)
	}
}

// Reading is strict: each refusal names the key and not the value.
func TestStrict(t *testing.T) {
	for _, c := range [][3]string{
		{`"done":true,`, ``, "input.done is missing"},
		{`"tip":7`, `"tip":7,"secret":1`, "input.secret is not a key of Job"},
		{`"done":true`, `"done":1`, "input.done is not true or false"},
		{`-2147483648`, `"-1"`, "input.number is not a number"},
		{`-2147483648`, `1.0`, "input.number is not a whole number"},
		{`-2147483648`, `1e3`, "input.number is not a whole number"},
		{`-2147483648`, `-2147483649`, "input.number does not fit an Int"},
		{`-9007199254740991`, `-9007199254740992`, "input.cents does not fit a Long, which crosses from -9007199254740991 to 9007199254740991"},
		{`-9007199254740991`, `99999999999999999999`, "input.cents does not fit a Long"},
		{`0.1`, `1e400`, "input.rate does not fit a Double"},
		{`"a \"b\""`, `5`, "input.notes is not a string"},
		{`0195a3c4-5b6d`, `0195a3c45-b6d`, "input.id is not a Uuid"},
		{`3e4f5a6b`, `3e4f5a--`, "input.id is not a Uuid"},
		{`"Confirmed"`, `"Secret"`, "input.state is not one of Offered, Confirmed"},
		{`"name":"z"`, `"name":"z","secret":1`, "input.stops[1].back.secret is not a key of Stop"},
		{`"name":"y"`, `"name":null`, "input.stops[1].name is null"},
		{`"tip":7`, `"tip":[7]`, "input.tip is not a number"},
		{`"stops":[`, `"stops":{},"s":[`, "input.stops is not an array"},
		{`"nothing":{}`, `"nothing":{"secret":1}`, "input.nothing.secret is not a key of Unit"},
		{`"nothing":{}`, `"nothing":[]`, "input.nothing is not an object"},
		{`{"done"`, `[{"done"`, "the input is not JSON"},
		{`"nothing":{}}`, `"nothing":{}} {}`, "the input holds more than one JSON value"},
		{`"nothing":{}}`, `"nothing":{}} x`, "the input is not JSON"},
	} {
		text := strings.Replace(job, c[0], c[1], 1)
		if _, err := read(text); err == nil || !strings.HasPrefix(err.Error(), c[2]) || strings.Contains(err.Error(), "Secret") {
			t.Errorf("%s is refused with %v, want %s", text, err, c[2])
		}
	}
}

// A table that lacks an enum's entries that an input reaches, or whose
// input cannot cross the wire, fails when the server starts.
func TestCheck(t *testing.T) {
	job := Db("job", func(kogo.Db, env, Request, Job) kogo.Either[string, rt.Unit] { return nil })
	for _, c := range []struct {
		t    Table[env]
		want string
	}{
		{Table[env]{[]Operation[env]{job}, nil}, "the table holds no constants of the enum rpc.State"},
		{Table[env]{[]Operation[env]{Db("f", func(kogo.Db, env, Request, any) kogo.Either[string, rt.Unit] { return nil })}, nil}, "no interface {} crosses the wire"},
	} {
		func() {
			defer func() {
				if p, _ := recover().(string); p != c.want {
					t.Errorf("panics with %q, want %q", p, c.want)
				}
			}()
			NewHandler(c.t, nil, env{}, nil)
		}()
	}
}

// An answer that holds a Long past JavaScript's exact range, NaN or an
// infinity is a bug: a panic that names where it is.
func TestUnwritable(t *testing.T) {
	for _, c := range []struct {
		change func(*Job)
		want   string
	}{
		{func(j *Job) { j.Cents = maxLong + 1 }, "the answer of op holds a Long past ±9007199254740991, which JavaScript cannot hold exactly; send it as a String at value.cents"},
		{func(j *Job) { j.Cents = -maxLong - 1 }, "at value.cents"},
		{func(j *Job) { j.Rate = math.NaN() }, "holds NaN or an infinity, which JSON has no number for at value.rate"},
		{func(j *Job) { j.Rate = math.Inf(-1) }, "at value.rate"},
	} {
		j := jobValue
		c.change(&j)
		func() {
			defer func() {
				if p, _ := recover().(string); !strings.Contains(p, c.want) {
					t.Errorf("panics with %q, want %q", p, c.want)
				}
			}()
			var b bytes.Buffer
			write(&b, reflect.ValueOf(j), "op", "value")
		}()
	}
}
