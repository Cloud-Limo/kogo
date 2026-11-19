package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
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
		From: Address{Name: "Shop", Address: "shop@shop.example"}, ReplyTo: &Address{Name: strings.Repeat("é", 22), Address: "desk@shop.example"},
		To:      kogo.List[Address]{{Name: `Ann "A" Lee`, Address: "ann@a.example"}, {Name: "Zoë", Address: "zoe@a.example"}},
		Cc:      kogo.List[Address]{{Name: strings.Repeat("n", 61), Address: "long@a.example"}},
		Bcc:     kogo.List[Address]{{Address: "audit@shop.example"}},
		Subject: "Your order ✓ " + strings.Repeat("é", 30), Text: "Café = 1 \nline\x01two\t\r\n" + strings.Repeat("x", 80), Html: &html,
		Attachments: kogo.List[Attachment]{
			{Name: "a b.txt", MimeType: "text/plain", Data: kogo.ByteArray("x\x00\xff")},
			{Name: "reçu.pdf", MimeType: "application/pdf", Data: bytes.Repeat([]byte{7}, 100)},
			{Name: strings.Repeat("b", 40) + ".txt", MimeType: "text/plain", Data: kogo.ByteArray("b")},
		},
	}
}

// Every byte of a message is stated in spec.md section 12: the order
// of the headers, the date in UTC, an address to a line, the fixed
// boundary, base64 at 76 to a line, and text lines ended by CR LF.
func TestBytes(t *testing.T) {
	plain := Mail{MessageId: "m-2@shop.example", Date: time.Instant{Seconds: 0}, From: Address{Address: "shop@shop.example"},
		To:      kogo.List[Address]{{Name: "Ann", Address: "ann@a.example"}, {Address: "bob@a.example"}},
		Subject: "Hello", Text: "a=b é\tc \n" + strings.Repeat("a", 74) + "é\nend\t"}
	want := strings.ReplaceAll(`Message-ID: <m-2@shop.example>
Date: Thu, 01 Jan 1970 00:00:00 +0000
From: <shop@shop.example>
To: "Ann" <ann@a.example>,
 <bob@a.example>
Subject: Hello
MIME-Version: 1.0
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: quoted-printable

a=3Db =C3=A9	c=20
`+strings.Repeat("a", 74)+`=
=C3=A9
end=09=
`, "\n", "\r\n")
	if got := string(Eml(plain)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	plain.Attachments = kogo.List[Attachment]{
		{Name: `a"b.txt`, MimeType: "text/plain; charset=utf-8", Data: kogo.ByteArray("hi")},
		{Name: strings.Repeat("я", 6) + strings.Repeat("n", 40) + ".pdf", MimeType: "application/pdf", Data: kogo.ByteArray("%")},
	}
	html := "<b>b</b>\n"
	plain.Html, plain.Text, plain.Subject = &html, "", strings.Repeat("word ", 20)
	_, got, _ := strings.Cut(string(Eml(plain)), "<bob@a.example>\r\n")
	want = strings.ReplaceAll(`Subject: word word word word word word word word word word word word word word
 word word word word word word 
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="=_kogo-mixed"

--=_kogo-mixed
Content-Type: multipart/alternative; boundary="=_kogo-alternative"

--=_kogo-alternative
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: quoted-printable

=

--=_kogo-alternative
Content-Type: text/html; charset=UTF-8
Content-Transfer-Encoding: quoted-printable

<b>b</b>

--=_kogo-alternative--

--=_kogo-mixed
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename="a\"b.txt"

aGk=

--=_kogo-mixed
Content-Type: application/pdf
Content-Transfer-Encoding: base64
Content-Disposition: attachment;
 filename*0*=UTF-8''%D1%8F%D1%8F%D1%8F%D1%8F%D1%8F%D1%8Fnnnnnnnnnnnnnnnnn;
 filename*1*=nnnnnnnnnnnnnnnnnnnnnnn.pdf

JQ==

--=_kogo-mixed--
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
		decoded := base64.NewDecoder(base64.StdEncoding, body)
		if encoding == "quoted-printable" {
			decoded = quotedprintable.NewReader(body)
		}
		data, err := io.ReadAll(decoded)
		_, file, _ := mime.ParseMediaType(disposition)
		add("%s %s %s %q %q %v", kind, params["charset"], encoding, file["filename"], data, err)
	}
	read(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), "", msg.Body)
	want := []string{
		"<m-1@shop.example> 2026-11-16T14:30:00Z <nil>",
		"From: Shop|shop@shop.example", "Reply-To: " + strings.Repeat("é", 22) + "|desk@shop.example",
		`To: Ann "A" Lee|ann@a.example`, "To: Zoë|zoe@a.example", "Cc: " + strings.Repeat("n", 61) + "|long@a.example", "Bcc: |audit@shop.example",
		"Subject: Your order ✓ " + strings.Repeat("é", 30) + " <nil>",
		"multipart/mixed", "multipart/alternative",
		`text/plain UTF-8 quoted-printable "" "Café = 1 \r\nline\x01two\t\r\n` + strings.Repeat("x", 80) + `" <nil>`,
		`text/html UTF-8 quoted-printable "" "<p>Café</p>" <nil>`,
		"end of multipart/alternative",
		`text/plain  base64 "a b.txt" "x\x00\xff" <nil>`,
		`application/pdf  base64 "reçu.pdf" "` + strings.Repeat(`\a`, 100) + `" <nil>`,
		`text/plain  base64 "` + strings.Repeat("b", 40) + `.txt" "b" <nil>`,
		"end of multipart/mixed",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, line := range strings.Split(string(Eml(m)), "\r\n") {
		if len(line) > 78 || strings.ContainsAny(line, "\r\n") || !printable(line) || strings.HasSuffix(line, ":") {
			t.Errorf("a line of %d characters, or not of printable ASCII: %q", len(line), line)
		}
	}
}

// What is written as it is cannot end its header or name another
// recipient, and what a reader decodes holds no line break to write
// into a header of its own, nor words a reader decodes inside quotes.
func TestNoInjectedHeader(t *testing.T) {
	m := full()
	m.Subject, m.From.Name, m.To[0].Name = "Hi\r\nBcc: eve@e.example", "Shop\nX-Evil: 1", "=?UTF-8?B?UGF5UGFs?="
	msg, err := netmail.ReadMessage(bytes.NewReader(Eml(m)))
	if err != nil {
		t.Fatal(err)
	}
	from, _ := msg.Header.AddressList("From")
	to, _ := msg.Header.AddressList("To")
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	got := fmt.Sprintf("%q %q %q %q %q", subject, from[0].Name, to[0].Name, msg.Header.Get("Bcc"), msg.Header.Get("X-Evil"))
	if want := `"Hi  Bcc: eve@e.example" "Shop X-Evil: 1" "=?UTF-8?B?UGF5UGFs?=" "<audit@shop.example>" ""`; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	if s := string(Eml(m)); !strings.Contains(s, "To: =?UTF-8?B?PT9VVEYtOD9CP1VHRjVVR0ZzPz0=?= <ann@a.example>") {
		t.Errorf("a name holding =? is not encoded:\n%s", s)
	}
	for what, change := range map[string]func(*Mail){
		"message id":  func(m *Mail) { m.MessageId = "a>\r\nBcc: eve@e.example" },
		"message id ": func(m *Mail) { m.MessageId = "justtext" },
		"address":     func(m *Mail) { m.To[0].Address = "ann@a.example,eve@e.example" },
		"address ":    func(m *Mail) { m.ReplyTo.Address = "ann@a.example>, <eve@e.example" },
		"address  ":   func(m *Mail) { m.Cc[0].Address = strings.Repeat("a", 250) + "@a.ex" },
		"mime type":   func(m *Mail) { m.Attachments[0].MimeType = "text/plain\r\nX-Evil: 1" },
	} {
		func() {
			defer func() {
				want := "a mail's " + strings.TrimSpace(what) + " is an ASCII addr-spec, as spec.md section 12 states it: "
				if what == "mime type" {
					want = "a mail's mime type is printable ASCII: "
				}
				if p := fmt.Sprint(recover()); !strings.HasPrefix(p, want) {
					t.Errorf("%s: %s", what, p)
				}
			}()
			m := full()
			change(&m)
			Eml(m)
		}()
	}
	for _, ok := range []string{"a.b+c@d-e.example", "o'neil@x"} {
		if !addressRule.MatchString(ok) {
			t.Errorf("%s is refused", ok)
		}
	}
	for _, bad := range []string{"a@b@c", "a..b@c", ".a@c", "a@", "a b@c", "a\"@c", "a(b)@c", "a;b@c", "é@c"} {
		if addressRule.MatchString(bad) {
			t.Errorf("%s is allowed", bad)
		}
	}
}
