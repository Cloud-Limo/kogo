// Package http is the Go behind Kogo's package kogo.http, in
// prelude/http.kg: the prelude's one outbound call (spec.md section
// 12, "An outside service"). Where Go's net/http has a default the
// spec does not state, it is turned off here, and a test shows it.
package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	"github.com/Cloud-Limo/kogo/rt"
)

// An Encoding is how a handle's fields travel in a body. A GET's
// travel in the query, whatever the handle says.
type Encoding int

const (
	Form Encoding = iota + 1
	JSON
)

// A Service is what main states of one outside service. Headers and
// Query go with every call and hold its credentials, which no Kogo
// code sees; neither is ever logged.
type Service struct {
	Address   string // scheme and host, with no path: "https://api.example.com"
	Encoding  Encoding
	MaxAnswer int64             // the most bytes of an answer that are read
	Timeout   time.Duration     // of one call, from its start to the end of its answer
	Headers   map[string]string // sent with every call
	Query     map[string]string // added to every call's query
	Log       *slog.Logger      // where a refused send is written; nil is slog.Default()
}

// An Http is a handle for one service.
type Http struct {
	s       *Service
	base    *url.URL
	client  *http.Client
	headers http.Header
}

// New makes a handle. It checks what it is given and touches no
// network.
func New(s Service) (Http, error) {
	base, err := url.Parse(s.Address)
	switch {
	case err != nil || base.Scheme != "http" && base.Scheme != "https" || base.Host == "" || strings.Trim(base.Path, "/") != "" || base.RawQuery != "":
		return Http{}, fmt.Errorf("kogo.http: the address %q is not http:// or https:// and a host, with no path", s.Address)
	case s.Encoding != Form && s.Encoding != JSON:
		return Http{}, fmt.Errorf("kogo.http: %s states no encoding: Form or JSON", s.Address)
	case s.MaxAnswer <= 0:
		return Http{}, fmt.Errorf("kogo.http: %s states no MaxAnswer, the most bytes of an answer to read", s.Address)
	case s.Timeout <= 0:
		return Http{}, fmt.Errorf("kogo.http: %s states no Timeout, the longest one call may take", s.Address)
	}
	base.Path = ""
	if s.Log == nil {
		s.Log = slog.Default()
	}
	headers := http.Header{}
	for name, value := range s.Headers {
		headers.Set(name, value)
	}
	// A Transport of our own and not Go's default one, whose Proxy
	// reads HTTP_PROXY and its kin: here Proxy is nil, so nothing of
	// the environment says where a call goes. HTTP/1.1 only: the rules
	// for a second send below are that protocol's. No Accept-Encoding
	// is added, so an answer arrives as the service wrote it, and its
	// headers are held to the answer's limit, not Go's 10 MiB.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	client := &http.Client{
		Transport: &http.Transport{Proxy: nil, Protocols: protocols, DisableCompression: true, MaxResponseHeaderBytes: s.MaxAnswer},
		// No redirect is followed: a 3xx is the answer.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return Http{&s, base, client, headers}, nil
}

// Http_WithHeader is a new handle with one more header. One that the
// handle has, from main or from an earlier call, cannot be set again.
func Http_WithHeader(this Http, name, value string) Http {
	if _, has := this.headers[http.CanonicalHeaderKey(name)]; has {
		panic(fmt.Sprintf("withHeader: the handle already has the header %s", name))
	}
	this.headers = this.headers.Clone()
	this.headers.Set(name, value)
	return this
}

// An endpoint is a method and a path, with any id in it escaped.
type endpoint struct{ method, path string }

// A Lookup asks and changes nothing; an Action changes something
// outside. They are two types so that ask takes only the one and
// perform only the other.
type Lookup struct{ endpoint }
type Action struct{ endpoint }

// path joins the parts as written and escapes the id alone, so an id
// that holds a / or a ? stays one piece of the path. A written part
// holds nothing to escape: Go would otherwise escape the whole path
// again, the id's %2F by then a /.
func path(method, before string, id *string, after string) endpoint {
	for piece := range strings.SplitSeq(before+after, "/") {
		if piece != url.PathEscape(piece) {
			panic(fmt.Sprintf("an endpoint's path is written as it is sent, and %q is not: %q", piece, before+after))
		}
	}
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		panic(fmt.Sprintf("an endpoint's method is GET, POST, PUT, PATCH or DELETE, not %q", method))
	}
	if !strings.HasPrefix(before, "/") {
		panic(fmt.Sprintf("an endpoint's path begins with /: %q", before))
	}
	if id == nil {
		return endpoint{method, before}
	}
	switch {
	case !strings.HasSuffix(before, "/"):
		panic(fmt.Sprintf("the path before an id ends in /: %q", before))
	case after != "" && !strings.HasPrefix(after, "/"):
		panic(fmt.Sprintf("the path after an id begins with /: %q", after))
	case *id == "" || *id == "." || *id == "..":
		panic(fmt.Sprintf("an id in a path is not %q", *id))
	}
	return endpoint{method, before + url.PathEscape(*id) + after}
}

func Lookup_String_String(method, path_ string) Lookup { return Lookup{path(method, path_, nil, "")} }
func Lookup_String_String_String(method, before, id string) Lookup {
	return Lookup{path(method, before, &id, "")}
}
func Lookup_String_String_String_String(method, before, id, after string) Lookup {
	if after == "" {
		panic(`the path after an id begins with /: ""`)
	}
	return Lookup{path(method, before, &id, after)}
}
func Action_String_String(method, path_ string) Action { return Action{path(method, path_, nil, "")} }
func Action_String_String_String(method, before, id string) Action {
	return Action{path(method, before, &id, "")}
}
func Action_String_String_String_String(method, before, id, after string) Action {
	return Action{Lookup_String_String_String_String(method, before, id, after).endpoint}
}

// HttpFailure is prelude/http.kg's, written here as the emitter would
// write it.
type HttpFailure interface{}

type HttpFailure_NotSent struct {
	_   struct{} `kogo:"NotSent"`
	Why string   `kogo:"why"`
}

type HttpFailure_Refused struct {
	_      struct{} `kogo:"Refused"`
	Status int32    `kogo:"status"`
	Body   *Json    `kogo:"body"`
}

type HttpFailure_Unknown struct {
	_   struct{} `kogo:"Unknown"`
	Why string   `kogo:"why"`
}

type HttpFailure_Unreadable struct {
	_   struct{} `kogo:"Unreadable"`
	Why string   `kogo:"why"`
}

// Http_Ask sends a lookup. Go may send it a second time, at once,
// when a kept connection fails before any answer: a lookup changes
// nothing, so whatever its method it is marked as safe to repeat.
func Http_Ask(this Http, endpoint Lookup, fields kogo.List[Field]) kogo.Either[HttpFailure, Json] {
	return sorted(this.send(endpoint.endpoint, fields, true))
}

// Http_Perform sends an action, once.
func Http_Perform(this Http, endpoint Action, fields kogo.List[Field]) kogo.Either[HttpFailure, Json] {
	return sorted(this.send(endpoint.endpoint, fields, false))
}

// sorted is a call's end by how far it got and its status alone,
// whichever call it was: never left is NotSent; a 4xx is Refused; a
// 2xx that is not a JSON object is Unreadable; and a 3xx, a 5xx or no
// whole answer is Unknown, since an action may have been done: a 303
// says it was.
func sorted(answer *Json, f *failed) kogo.Either[HttpFailure, Json] {
	switch {
	case f == nil:
		return kogo.Either_Right[Json]{Value: *answer}
	case f.refused:
		return kogo.Either_Left[HttpFailure]{Value: HttpFailure_Refused{Status: f.status, Body: f.body}}
	case f.unreadable:
		return kogo.Either_Left[HttpFailure]{Value: HttpFailure_Unreadable{Why: f.why}}
	case !f.left:
		return kogo.Either_Left[HttpFailure]{Value: HttpFailure_NotSent{Why: f.why}}
	}
	return kogo.Either_Left[HttpFailure]{Value: HttpFailure_Unknown{Why: f.why}}
}

// maxLogged is the most of a refused body that is logged: it may echo
// what was sent, a person's name or email with it.
const maxLogged = 4 << 10

// A failed send: refused, a 4xx, with the service's status and body;
// or why, with whether it left and whether a 2xx came that is not a
// JSON object.
type failed struct {
	refused    bool
	status     int32
	body       *Json
	left       bool
	unreadable bool
	why        string
}

func (h Http) send(e endpoint, fields kogo.List[Field], again bool) (*Json, *failed) {
	u := *h.base
	u.RawPath = e.path // keeps an escaped id as it was written
	u.Path, _ = url.PathUnescape(e.path)
	var query, body []byte
	for _, name := range slices.Sorted(maps.Keys(h.s.Query)) {
		query = pair(query, name, h.s.Query[name])
	}
	contentType := ""
	switch {
	case e.method == "GET":
		query = form(query, "", fields)
	case h.s.Encoding == Form:
		body, contentType = form(nil, "", fields), "application/x-www-form-urlencoded"
	default:
		body, contentType = object(fields), "application/json"
	}
	u.RawQuery = string(query)

	sends := 0
	ctx, cancel := context.WithTimeout(context.Background(), h.s.Timeout)
	defer cancel()
	// A send has left once it has a connection to write on.
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { sends++ }})
	req, err := http.NewRequestWithContext(ctx, e.method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, &failed{why: "the request could not be made: " + err.Error()}
	}
	maps.Copy(req.Header, h.headers)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "") // Go sends its own unless one is set, and none for an empty one
	}
	keyed := req.Header.Get("Idempotency-Key") != "" || req.Header.Get("X-Idempotency-Key") != ""
	switch {
	case again && !keyed:
		// Go's own mark of a request it may repeat; an empty one is
		// not sent on the wire.
		req.Header["Idempotency-Key"] = []string{}
	case again:
	case len(body) > 0 || keyed || e.method == "GET":
		// Go repeats a request that failed on a kept connection when
		// it can make its body again and takes it for safe: a GET, or
		// one with an Idempotency-Key header. With a body it cannot
		// make again, it never does. An empty POST, PUT or PATCH with
		// no key is left as it is: Go does not repeat one, and a body
		// of unknown length would be sent chunked.
		req.GetBody = nil
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	resp, err := h.client.Do(req)
	if sends > 1 {
		h.s.Log.Warn("kogo.http: sent a second time", "method", e.method, "path", e.path)
	}
	if err != nil {
		return nil, &failed{left: sends > 0, why: reason(err)}
	}
	defer resp.Body.Close()
	text, err := io.ReadAll(io.LimitReader(resp.Body, h.s.MaxAnswer+1))
	switch {
	case err != nil:
		return nil, &failed{left: true, why: "the answer broke off: " + reason(err)}
	case int64(len(text)) > h.s.MaxAnswer:
		return nil, &failed{left: true, why: fmt.Sprintf("the answer is longer than %d bytes", h.s.MaxAnswer)}
	}
	answer := Json_Parse(string(text))
	status := resp.StatusCode
	if status >= 300 && status < 500 {
		logged := string(text)
		if len(logged) > maxLogged {
			logged = fmt.Sprintf("%s [cut: the first %d of %d bytes]", logged[:maxLogged], maxLogged, len(logged))
		}
		h.s.Log.Warn("kogo.http: refused", "method", e.method, "path", e.path, "status", status, "body", logged)
	}
	switch {
	case status >= 400 && status < 500:
		return nil, &failed{refused: true, status: int32(status), body: answer}
	case status < 200 || status >= 300:
		return nil, &failed{left: true, why: fmt.Sprintf("the service answered %d", status)}
	case len(bytes.TrimSpace(text)) == 0:
		return &Json{"{}"}, nil // done, with nothing to say
	case answer == nil:
		return nil, &failed{left: true, unreadable: true, why: "the answer is not a JSON object"}
	}
	return answer, nil
}

// reason is an error's text without the URL that Go puts before it,
// whose query may hold a credential.
func reason(err error) string {
	var u *url.Error
	if errors.As(err, &u) {
		err = u.Err
	}
	return err.Error()
}

// A Field is one name and its value: text, or the fields of a group,
// or a list of strings, or a JSON object sent as it is.
type Field struct {
	name  string
	text  string // a form's value
	json  string // JSON's, when it is not a group or a list
	group []Field
	list  []string
	kind  int // 0 a value, isGroup or isList
}

const (
	isGroup = iota + 1
	isList
)

func quote(s string) string { return string(encode(s)) }

func Field_String_String(name, value string) Field {
	return Field{name: name, text: value, json: quote(value)}
}
func Field_String_Long(name string, value int64) Field {
	s := strconv.FormatInt(value, 10)
	return Field{name: name, text: s, json: s}
}

// Field_String_Double writes a number as Kogo's toString() does. JSON
// has no NaN and no infinity, so neither is a field.
func Field_String_Double(name string, value float64) Field {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		panic(fmt.Sprintf("field: %s is %s, which no service reads", name, rt.ToString(value)))
	}
	return Field{name: name, text: rt.ToString(value), json: rt.ToString(value)}
}
func Field_String_Boolean(name string, value bool) Field {
	s := strconv.FormatBool(value)
	return Field{name: name, text: s, json: s}
}
func Field_String_ListString(name string, values kogo.List[string]) Field {
	return Field{name: name, list: values, kind: isList}
}
func Field_String_Json(name string, json Json) Field {
	return Field{name: name, text: json.raw, json: json.raw}
}
func Group(name string, fields kogo.List[Field]) Field {
	return Field{name: name, group: fields, kind: isGroup}
}

// form adds fields to a form or a query, in the order given: a
// group's as group[name], a list's values each as name[].
func form(to []byte, outer string, fields []Field) []byte {
	for _, f := range fields {
		name := f.name
		if outer != "" {
			name = outer + "[" + f.name + "]"
		}
		switch f.kind {
		case isList:
			for _, v := range f.list {
				to = pair(to, name+"[]", v)
			}
		case isGroup:
			to = form(to, name, f.group)
		default:
			to = pair(to, name, f.text)
		}
	}
	return to
}

func pair(to []byte, name, value string) []byte {
	if len(to) > 0 {
		to = append(to, '&')
	}
	return append(to, url.QueryEscape(name)+"="+url.QueryEscape(value)...)
}

// object writes fields as a JSON object, in the order given.
func object(fields []Field) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(encode(f.name))
		b.WriteByte(':')
		switch f.kind {
		case isList:
			b.Write(encode(append([]string{}, f.list...)))
		case isGroup:
			b.Write(object(f.group))
		default:
			b.WriteString(f.json)
		}
	}
	b.WriteByte('}')
	return b.Bytes()
}
