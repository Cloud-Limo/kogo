package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	netmail "net/mail"
	"strings"
	"testing"
	gotime "time"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	"github.com/Cloud-Limo/kogo/prelude/kogo/time"
)

// The date is written in UTC wherever the server is.
func init() { gotime.Local = gotime.FixedZone("east", 10*3600) }

func full() Mail {
	html := "<p>Café</p>"
	return Mail{
		MessageId: "m-1@shop.example", Date: time.Instant{Seconds: 1794839400, Nanos: 5},
		From: Address{Name: "Shop", Address: "shop@shop.example"}, ReplyTo: &Address{Address: "desk@shop.example"},
		To:      kogo.List[Address]{{Name: `Ann "A" Lee`, Address: "ann@a.example"}, {Name: "Zoë", Address: "zoe@a.example"}},
		Cc:      kogo.List[Address]{{Name: strings.Repeat("n", 61), Address: "long@a.example"}},
		Bcc:     kogo.List[Address]{{Address: "audit@shop.example"}},
		Subject: "Your order ✓", Text: "Café\nline two\r\n", Html: &html,
		Attachments: kogo.List[Attachment]{
			{Name: "a b.txt", MimeType: "text/plain", Data: kogo.ByteArray("x\x00\xff")},
			{Name: "reçu.pdf", MimeType: "application/pdf", Data: bytes.Repeat([]byte{7}, 100)},
		},
	}
}

// Every byte of a message is stated in spec.md section 12: the order
// of the headers, the date in UTC, an address to a line, the fixed
// boundary, base64 at 76 to a line, and text lines ended by CR LF.
func TestBytes(t *testing.T) {
	plain := Mail{MessageId: "m-2@shop.example", Date: time.Instant{Seconds: 0}, From: Address{Address: "shop@shop.example"},
		To: kogo.List[Address]{{Name: "Ann", Address: "ann@a.example"}, {Address: "bob@a.example"}}, Subject: "Hello", Text: strings.Repeat("a", 57) + "\nb"}
	want := strings.ReplaceAll(`Message-ID: <m-2@shop.example>
Date: Thu, 01 Jan 1970 00:00:00 +0000
From: <shop@shop.example>
To: "Ann" <ann@a.example>,
 <bob@a.example>
Subject: Hello
MIME-Version: 1.0
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: base64

YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh
DQpi
`, "\n", "\r\n")
	if got := string(Eml(plain)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	plain.Attachments = kogo.List[Attachment]{{Name: `a"b.txt`, MimeType: "text/plain", Data: kogo.ByteArray("hi")}}
	html := "<b>b</b>"
	plain.Html, plain.Text, plain.Subject = &html, "t", strings.Repeat("s", 70)
	_, got, _ := strings.Cut(string(Eml(plain)), "<bob@a.example>\r\n")
	want = strings.ReplaceAll(`Subject: =?UTF-8?B?c3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nz?=
 =?UTF-8?B?c3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzcw==?=
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary=kogo-mixed

--kogo-mixed
Content-Type: multipart/alternative; boundary=kogo-alternative

--kogo-alternative
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: base64

dA==

--kogo-alternative
Content-Type: text/html; charset=UTF-8
Content-Transfer-Encoding: base64

PGI+YjwvYj4=

--kogo-alternative--

--kogo-mixed
Content-Type: text/plain
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename="a\"b.txt"

aGk=

--kogo-mixed--
`, "\n", "\r\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if again := Eml(full()); !bytes.Equal(again, Eml(full())) {
		t.Error("the same Mail gave other bytes")
	}
}

// Go's own readers give back what the Mail held: the headers through
// net/mail, the parts through mime/multipart.
func TestReadBack(t *testing.T) {
	m := full()
	msg, err := netmail.ReadMessage(bytes.NewReader(Eml(m)))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	add := func(format string, a ...any) { got = append(got, fmt.Sprintf(format, a...)) }
	date, err := msg.Header.Date()
	add("%s %s %v", msg.Header.Get("Message-ID"), date.UTC().Format("2006-01-02T15:04:05Z07:00"), err)
	for _, h := range []string{"From", "Reply-To", "To", "Cc", "Bcc"} {
		list, err := msg.Header.AddressList(h)
		if err != nil {
			t.Fatal(h, err)
		}
		for _, a := range list {
			add("%s: %s|%s", h, a.Name, a.Address)
		}
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	add("Subject: %s %v", subject, err)
	var read func(contentType, encoding, disposition string, body io.Reader)
	read = func(contentType, encoding, disposition string, body io.Reader) {
		kind, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(kind, "multipart/") {
			add("%s", kind)
			for r := multipart.NewReader(body, params["boundary"]); ; {
				p, err := r.NextRawPart()
				if err == io.EOF {
					add("end of %s", kind)
					return
				} else if err != nil {
					t.Fatal(err)
				}
				read(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), p.Header.Get("Content-Disposition"), p)
			}
		}
		data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, body))
		_, file, _ := mime.ParseMediaType(disposition)
		add("%s %s %s %q %q %v", kind, params["charset"], encoding, file["filename"], data, err)
	}
	read(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), "", msg.Body)
	want := []string{
		"<m-1@shop.example> 2026-11-16T14:30:00Z <nil>",
		"From: Shop|shop@shop.example", "Reply-To: |desk@shop.example",
		`To: Ann "A" Lee|ann@a.example`, "To: Zoë|zoe@a.example", "Cc: " + strings.Repeat("n", 61) + "|long@a.example", "Bcc: |audit@shop.example",
		"Subject: Your order ✓ <nil>",
		"multipart/mixed", "multipart/alternative",
		`text/plain UTF-8 base64 "" "Café\r\nline two\r\n" <nil>`,
		`text/html UTF-8 base64 "" "<p>Café</p>" <nil>`,
		"end of multipart/alternative",
		`text/plain  base64 "a b.txt" "x\x00\xff" <nil>`,
		`application/pdf  base64 "reçu.pdf" "` + strings.Repeat(`\a`, 100) + `" <nil>`,
		"end of multipart/mixed",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, line := range strings.Split(string(Eml(m)), "\r\n") {
		if len(line) > 78 || strings.ContainsAny(line, "\r\n") || !printable(line) {
			t.Errorf("a line of %d characters, or not of printable ASCII: %q", len(line), line)
		}
	}
}

// What is written as it is cannot end its header, and what is encoded
// cannot either: a line break in a subject or a name stays inside its
// words.
func TestNoInjectedHeader(t *testing.T) {
	m := full()
	m.Subject, m.From.Name = "Hi\r\nBcc: eve@e.example", "Shop\nX-Evil: 1"
	if s := string(Eml(m)); strings.Contains(s, "eve@") || strings.Contains(s, "X-Evil") {
		t.Errorf("a header was injected:\n%s", s)
	}
	for what, change := range map[string]func(*Mail){
		"message id": func(m *Mail) { m.MessageId = "a>\r\nBcc: eve@e.example" },
		"address":    func(m *Mail) { m.To[0].Address = "ann@a.example>, <eve@e.example" },
		"mime type":  func(m *Mail) { m.Attachments[0].MimeType = "text/plain\r\nX-Evil: 1" },
	} {
		func() {
			defer func() {
				if p := fmt.Sprint(recover()); !strings.HasPrefix(p, "a mail's "+what+" is printable ASCII with no space or angle bracket: ") {
					t.Errorf("%s: %s", what, p)
				}
			}()
			m := full()
			change(&m)
			Eml(m)
		}()
	}
}
