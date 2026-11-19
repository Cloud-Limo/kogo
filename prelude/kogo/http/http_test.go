package http

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	"github.com/Cloud-Limo/kogo/rt"
)

// The environment names a proxy for the whole run: a local server
// that counts what it is sent and answers {}. Go's own client would
// send it every call to a host that is not this machine.
var proxied atomic.Int32

func TestMain(m *testing.M) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		io.WriteString(w, "{}")
	}))
	os.Setenv("HTTP_PROXY", proxy.URL)
	os.Setenv("http_proxy", proxy.URL)
	os.Exit(m.Run())
}

// serve starts a local server and gives a handle for it, with what
// the handle logs.
func serve(t *testing.T, s Service, handler http.HandlerFunc) (Http, *bytes.Buffer) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	log := &bytes.Buffer{}
	s.Address, s.Log = server.URL, slog.New(slog.NewTextHandler(log, nil))
	if s.MaxAnswer == 0 {
		s.MaxAnswer = 1 << 10
	}
	if s.Encoding == 0 {
		s.Encoding = Form
	}
	if s.Timeout == 0 {
		s.Timeout = 5 * time.Second
	}
	h, err := New(s)
	if err != nil {
		t.Fatal(err)
	}
	return h, log
}

func answer(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}
}

var (
	get  = Action_String_String("GET", "/thing")
	post = Action_String_String("POST", "/thing")
	look = Lookup_String_String("GET", "/thing")
)

// both sends a lookup and an action and writes each result as Kogo
// would.
func both(h Http) (asked, performed string) {
	return rt.ToString(Http_Ask(h, look, nil)), rt.ToString(Http_Perform(h, post, nil))
}

func want(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s:\n got %s\nwant %s", what, got, want)
	}
}

func panics(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		if got, _ := recover().(string); got != want {
			t.Errorf("panic %q, want %q", got, want)
		}
	}()
	f()
}

// A server that reads the second request it is sent, on the kept
// connection of the first, and drops the connection with no answer.
func TestSecondSend(t *testing.T) {
	for _, c := range []struct {
		what  string
		send  func(Http) any
		seen  int32
		left  string
		again bool
	}{
		{"perform of a GET", func(h Http) any { return Http_Perform(h, get, nil) }, 2, "Either.Left(Unknown(why=EOF))", false},
		{"perform under a key", func(h Http) any {
			return Http_Perform(Http_WithHeader(h, "Idempotency-Key", "k1"), post, nil)
		}, 2, "Either.Left(Unknown(why=EOF))", false},
		{"perform of an empty DELETE under an empty key", func(h Http) any {
			return Http_Perform(Http_WithHeader(h, "Idempotency-Key", ""), Action_String_String_String("DELETE", "/v1/things/", "t1"), nil)
		}, 2, "Either.Left(Unknown(why=EOF))", false},
		{"ask", func(h Http) any { return Http_Ask(h, look, nil) }, 3, "Either.Right({})", true},
		{"ask by a POST", func(h Http) any { return Http_Ask(h, Lookup_String_String("POST", "/search"), nil) }, 3, "Either.Right({})", true},
	} {
		var seen atomic.Int32
		h, log := serve(t, Service{}, func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			if r.Header["Idempotency-Key"] != nil && r.Header.Get("Idempotency-Key") == "" && r.URL.Path != "/v1/things/t1" { // that one sets it empty
				t.Errorf("%s: the empty Idempotency-Key was sent", c.what)
			}
			if seen.Add(1) == 2 {
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
				return
			}
			io.WriteString(w, "{}")
		})
		want(t, c.what+", first", rt.ToString(c.send(h)), "Either.Right({})")
		want(t, c.what, rt.ToString(c.send(h)), c.left)
		if seen.Load() != c.seen {
			t.Errorf("%s: the server was sent %d requests, want %d", c.what, seen.Load(), c.seen)
		}
		if got := strings.Contains(log.String(), "sent a second time"); got != c.again {
			t.Errorf("%s: logged a second send: %v, want %v\n%s", c.what, got, c.again, log)
		}
	}
}

func TestNoRedirectIsFollowed(t *testing.T) {
	var followed atomic.Int32
	h, log := serve(t, Service{}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed.Add(1)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	asked, performed := both(h)
	want(t, "ask", asked, "Either.Left(Unknown(why=the service answered 302))")
	want(t, "perform", performed, "Either.Left(Unknown(why=the service answered 302))") // a 303 says an action was done
	if followed.Load() != 0 {
		t.Errorf("%d redirects were followed", followed.Load())
	}
	if !strings.Contains(log.String(), `msg="kogo.http: redirect not followed" method=POST path=/thing status=302`) || strings.Contains(log.String(), "refused") {
		t.Errorf("the redirect is not logged as one:\n%s", log)
	}
}

func TestNoProxyFromTheEnvironment(t *testing.T) {
	h, err := New(Service{Address: "http://service.invalid", Encoding: Form, MaxAnswer: 10, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, sent := Http_Perform(h, post, nil).(kogo.Either_Left[HttpFailure]).Value.(HttpFailure_NotSent); !sent || proxied.Load() != 0 {
		t.Errorf("a call to a host that does not exist: NotSent %v, and the environment's proxy was sent %d", sent, proxied.Load())
	}
}

func TestAnswerPastTheLimit(t *testing.T) {
	long := `{"a":"` + strings.Repeat("x", 400) + `"}`
	h, _ := serve(t, Service{MaxAnswer: int64(len(long))}, answer(200, long))
	asked, performed := both(h)
	want(t, "ask at the limit", asked, "Either.Right("+long+")")
	want(t, "perform at the limit", performed, "Either.Right("+long+")")
	h, _ = serve(t, Service{MaxAnswer: int64(len(long)) - 1}, answer(200, long))
	asked, performed = both(h)
	want(t, "ask", asked, "Either.Left(Unknown(why=the answer is longer than 407 bytes))")
	want(t, "perform", performed, "Either.Left(Unknown(why=the answer is longer than 407 bytes))")
}

func TestLongIsExact(t *testing.T) {
	j := *Json_Parse(`{"big": 9007199254740993, "min": -9223372036854775808, "past": 9223372036854775808, "half": 1.5, "e": 1e3}`)
	if got := Json_Long(j, "big"); got == nil || *got != 9007199254740993 {
		t.Errorf("big: %s, want 9007199254740993", rt.ToString(got))
	}
	if got := Json_Long(j, "min"); got == nil || *got != -9223372036854775808 {
		t.Errorf("min: %s", rt.ToString(got))
	}
	for _, key := range []string{"past", "half", "e"} {
		if got := Json_Long(j, key); got != nil {
			t.Errorf("%s: %d, want null", key, *got)
		}
	}
	want(t, "a double", rt.ToString(Json_Double(j, "half"))+" "+rt.ToString(Json_Double(j, "big")), "1.5 9.007199254740992E15")
}

// Each reader gives null for an absent key, a null and another type,
// and reads its own.
func TestReaders(t *testing.T) {
	j := *Json_Parse(` {"s": "a\"b", "n": 7, "d": 0.5, "b": true, "o": {"k": "v"}, "l": [{"k": 1}, {}], "null": null, "mixed": [{}, 1], "big": 1e999} `)
	readers := map[string]func(key string) any{
		"s": func(k string) any { return Json_String(j, k) }, "n": func(k string) any { return Json_Long(j, k) },
		"d": func(k string) any { return Json_Double(j, k) }, "b": func(k string) any { return Json_Boolean(j, k) },
		"o": func(k string) any { return Json_At(j, k) }, "l": func(k string) any { return Json_List(j, k) },
	}
	own := map[string]string{"s": `a"b`, "n": "7", "d": "0.5", "b": "true", "o": `{"k": "v"}`, "l": `[{"k": 1}, {}]`}
	for name, reader := range readers {
		for key := range own {
			wanted := "null"
			if key == name || name == "d" && key == "n" {
				wanted = map[bool]string{true: own[key], false: "7.0"}[key == name]
			}
			want(t, name+" of "+key, rt.ToString(reader(key)), wanted)
		}
		for _, key := range []string{"absent", "null", "mixed", "big"} {
			want(t, name+" of "+key, rt.ToString(reader(key)), "null")
		}
	}
	want(t, "raw", Json_Raw(*Json_At(j, "o")), `{"k": "v"}`)
	for _, text := range []string{"", "null", "[]", `"a"`, "{", "{} {}"} {
		if got := Json_Parse(text); got != nil {
			t.Errorf("parse(%q) is %s, want null", text, rt.ToString(got))
		}
	}
}

func TestPath(t *testing.T) {
	want(t, "an id", Action_String_String_String_String("POST", "/v1/things/", "a/b?c d", "/parts").path, "/v1/things/a%2Fb%3Fc%20d/parts")
	want(t, "an id last", Lookup_String_String_String("GET", "/v1/things/", "id_1").path, "/v1/things/id_1")
	panics(t, `the path before an id ends in /: "/v1/things"`, func() { Lookup_String_String_String("GET", "/v1/things", "id_1") })
	panics(t, `the path after an id begins with /: "parts"`, func() { Action_String_String_String_String("POST", "/v1/things/", "id_1", "parts") })
	panics(t, `the path after an id begins with /: ""`, func() { Action_String_String_String_String("POST", "/v1/things/", "id_1", "") })
	panics(t, `an id in a path is not ".."`, func() { Action_String_String_String("DELETE", "/v1/things/", "..") })
	panics(t, `an id in a path is not "."`, func() { Action_String_String_String("DELETE", "/v1/things/", ".") })
	panics(t, `an id in a path is not ""`, func() { Action_String_String_String("DELETE", "/v1/things/", "") })
	panics(t, `an endpoint's method is GET, POST, PUT, PATCH or DELETE, not "get"`, func() { Lookup_String_String("get", "/a") })
	panics(t, `an endpoint's path begins with /: "a"`, func() { Lookup_String_String("GET", "a") })
	// A written part with something to escape would have Go escape
	// the whole path again, and an id's / with it.
	panics(t, `an endpoint's path is written as it is sent, and "roles?expand=1" is not: "/v1/users//roles?expand=1"`, func() { Lookup_String_String_String_String("GET", "/v1/users/", "x/../../admin", "/roles?expand=1") })
	panics(t, `an endpoint's path is written as it is sent, and "my users" is not: "/v1/my users/"`, func() { Action_String_String_String("POST", "/v1/my users/", "x/../../admin") })
	panics(t, `an endpoint's path is written as it is sent, and "100%zz" is not: "/v1/100%zz"`, func() { Lookup_String_String("GET", "/v1/100%zz") })
	panics(t, `an endpoint's path is written as it is sent, and "a%2Fb" is not: "/v1/a%2Fb"`, func() { Lookup_String_String("GET", "/v1/a%2Fb") })
}

// Go adds nothing to what is sent and reads no more than the limit: no
// Accept-Encoding, so an answer is not unpacked; no User-Agent of its
// own; and an answer's headers are held to the limit too.
func TestWhatGoWouldAdd(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/headers" {
			w.Header().Set("X-Long", strings.Repeat("x", 1<<10))
		}
		w.Header().Set("Content-Encoding", "gzip")
		fmt.Fprintf(w, `{"encoding": %q, "agent": %q}`, r.Header["Accept-Encoding"], r.Header["User-Agent"])
	}
	h, _ := serve(t, Service{}, handler)
	want(t, "ask", rt.ToString(Http_Ask(h, look, nil)), `Either.Right({"encoding": [], "agent": []})`)
	want(t, "perform", rt.ToString(Http_Perform(h, post, nil)), `Either.Right({"encoding": [], "agent": []})`)
	want(t, "long headers", rt.ToString(Http_Perform(h, Action_String_String("POST", "/headers"), nil)),
		"Either.Left(Unknown(why=net/http: HTTP/1.x transport connection broken: net/http: server response headers exceeded 1024 bytes; aborted))")
	stated, _ := serve(t, Service{Headers: map[string]string{"User-Agent": "main's"}}, handler)
	want(t, "main's User-Agent", rt.ToString(Http_Ask(stated, look, nil)), `Either.Right({"encoding": [], "agent": ["main's"]})`)
}

// What reaches the server: the path with its id still one piece, the
// handle's headers and query, and the fields by the handle's encoding
// or, for a GET, in the query.
func TestWhatIsSent(t *testing.T) {
	fields := kogo.ListOf(Field_String_String("name", "a&b"), Field_String_Long("amount", 9007199254740993), Field_String_Double("rate", 1),
		Field_String_Boolean("confirm", true), Group("card", kogo.ListOf(Field_String_String("token", "t"), Group("in", kogo.ListOf(Field_String_Long("n", 1))))),
		Field_String_ListString("expand", kogo.ListOf("a", "b")), Field_String_Json("options", *Json_Parse(`{"k":[1]}`)))
	form := "name=a%26b&amount=9007199254740993&rate=1.0&confirm=true&card[token]=t&card[in][n]=1&expand[]=a&expand[]=b&options={\"k\":[1]}"
	for _, c := range []struct {
		encoding Encoding
		method   string
		want     string
	}{
		{Form, "POST", "POST /v1/things/a%2Fb/go?key=k1 [main one application/x-www-form-urlencoded] " + form},
		{JSON, "PUT", `PUT /v1/things/a%2Fb/go?key=k1 [main one application/json] {"name":"a&b","amount":9007199254740993,"rate":1.0,"confirm":true,"card":{"token":"t","in":{"n":1}},"expand":["a","b"],"options":{"k":[1]}}`},
		{JSON, "GET", "GET /v1/things/a%2Fb/go?key=k1&" + form + " [main one ] "},
	} {
		got := ""
		h, _ := serve(t, Service{Encoding: c.encoding, Headers: map[string]string{"Authorization": "main"}, Query: map[string]string{"key": "k1"}},
			func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				unescaped := strings.NewReplacer("%5B", "[", "%5D", "]", "%7B", "{", "%7D", "}", "%22", `"`, "%3A", ":").Replace(r.URL.RawQuery + " " + string(body))
				query, sent, _ := strings.Cut(unescaped, " ")
				got = r.Method + " " + r.URL.EscapedPath() + "?" + query + " [" + r.Header.Get("Authorization") + " " + r.Header.Get("X-Tenant") + " " + r.Header.Get("Content-Type") + "] " + sent
			})
		h = Http_WithHeader(h, "X-Tenant", "one")
		want(t, c.method, rt.ToString(Http_Perform(h, Action_String_String_String_String(c.method, "/v1/things/", "a/b", "/go"), fields)), "Either.Right({})")
		want(t, c.method, got, c.want)
		panics(t, "withHeader: the handle already has the header authorization", func() { Http_WithHeader(h, "authorization", "mine") })
		panics(t, "withHeader: the handle already has the header X-Tenant", func() { Http_WithHeader(h, "X-Tenant", "two") })
	}
	// An action with no fields has a body of no bytes, not one of a
	// length the server is left to find.
	framing := ""
	h, _ := serve(t, Service{}, func(w http.ResponseWriter, r *http.Request) {
		framing = fmt.Sprint(r.ContentLength, r.TransferEncoding)
	})
	for _, method := range []string{"POST", "DELETE", "GET"} {
		Http_Perform(h, Action_String_String(method, "/thing"), nil)
		want(t, "the framing of an empty "+method, framing, "0 []")
	}
	panics(t, "field: rate is NaN, which no service reads", func() { Field_String_Double("rate", rt.Div(0.0, 0.0)) })
}

// A failed send is sorted by how far it got and by its status alone,
// whichever call made it.
func TestFailures(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		want   string
	}{
		{402, `{"error": {"type": "declined"}}`, `Either.Left(Refused(status=402, body={"error": {"type": "declined"}}))`},
		{404, `not found`, `Either.Left(Refused(status=404, body=null))`},
		{503, `{}`, `Either.Left(Unknown(why=the service answered 503))`},
		{600, `{}`, `Either.Left(Unknown(why=the service answered 600))`},
		{200, `[1]`, `Either.Left(Unreadable(why=the answer is not a JSON object))`},
		{200, ``, `Either.Right({})`},
	} {
		h, log := serve(t, Service{Query: map[string]string{"key": "secret"}}, answer(c.status, c.body))
		asked, performed := both(h)
		want(t, "ask", asked, c.want)
		want(t, "perform", performed, c.want)
		refused := c.status >= 400 && c.status < 500
		if got := strings.Count(log.String(), "kogo.http: refused"); got != map[bool]int{true: 2}[refused] {
			t.Errorf("%d: %d refused sends are logged:\n%s", c.status, got, log)
		}
		if refused && !strings.Contains(log.String(), "body="+strconv.Quote(c.body)) {
			t.Errorf("%d: the refused body is not logged:\n%s", c.status, log)
		}
		if strings.Contains(log.String(), "secret") {
			t.Errorf("the handle's query is logged:\n%s", log)
		}
	}
	// A long refusal is logged to 4 KiB, and says so.
	long := `{"echo": "` + strings.Repeat("x", 5000) + `"}`
	h, log := serve(t, Service{MaxAnswer: 1 << 13}, answer(400, long))
	want(t, "a long refusal", rt.ToString(Http_Ask(h, look, nil)), "Either.Left(Refused(status=400, body="+long+"))")
	if !strings.Contains(log.String(), strings.Repeat("x", 4086)+" [cut: the first 4096 of 5012 bytes]") {
		t.Errorf("a long refused body is not cut at 4096 bytes:\n%s", log)
	}
	// A refusal past the limit is logged, with what was read.
	h, log = serve(t, Service{MaxAnswer: 1 << 10}, answer(404, long))
	want(t, "a refusal past the limit", rt.ToString(Http_Perform(h, post, nil)), "Either.Left(Unknown(why=the answer is longer than 1024 bytes))")
	if !strings.Contains(log.String(), `msg="kogo.http: refused" method=POST path=/thing status=404 body="{\"echo\": \"xxx`) {
		t.Errorf("a refusal past the limit is not logged:\n%s", log)
	}
	// A slow service: the call left, so its end is unknown.
	slow, _ := serve(t, Service{Timeout: 50 * time.Millisecond, Query: map[string]string{"key": "secret"}}, func(w http.ResponseWriter, r *http.Request) { time.Sleep(200 * time.Millisecond) })
	asked, performed := both(slow)
	want(t, "ask of a slow service", asked, "Either.Left(Unknown(why=context deadline exceeded))")
	want(t, "perform on a slow service", performed, "Either.Left(Unknown(why=context deadline exceeded))")
	// A service that is not there: nothing left.
	gone, _ := serve(t, Service{}, answer(200, "{}"))
	gone.base.Host = "127.0.0.1:1"
	for what, sent := range map[string]kogo.Either[HttpFailure, Json]{"ask": Http_Ask(gone, look, nil), "perform": Http_Perform(gone, post, nil)} {
		if _, notSent := sent.(kogo.Either_Left[HttpFailure]).Value.(HttpFailure_NotSent); !notSent {
			t.Errorf("%s on a closed port is not NotSent", what)
		}
	}
}

func TestNewRefuses(t *testing.T) {
	for _, s := range []Service{
		{Address: "http://a.example", Encoding: Form, Timeout: time.Second},
		{Address: "http://a.example", MaxAnswer: 1, Timeout: time.Second},
		{Address: "http://a.example", Encoding: Form, MaxAnswer: 1},
		{Address: "a.example", Encoding: JSON, MaxAnswer: 1, Timeout: time.Second},
		{Address: "http://a.example/v1", Encoding: JSON, MaxAnswer: 1, Timeout: time.Second},
	} {
		if _, err := New(s); err == nil {
			t.Errorf("New(%+v) gave no error", s)
		}
	}
}
