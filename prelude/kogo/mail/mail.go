// Package mail is the Go behind Kogo's package kogo.mail, in
// prelude/mail.kg: a message written as the bytes of an EML file
// (spec.md section 12, "Mail"). Every byte is decided here and stated
// there, so none moves with a Go release: no header, encoding or
// boundary is left to net/mail or mime.
package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"slices"
	"strings"
	gotime "time"
	"unicode/utf8"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	"github.com/Cloud-Limo/kogo/prelude/kogo/time"
)

// Address, Attachment and Mail are prelude/mail.kg's, written here as
// the emitter would write them.
type Address struct {
	_       struct{} `kogo:"Address"`
	Name    string   `kogo:"name"`
	Address string   `kogo:"address"`
}

type Attachment struct {
	_        struct{}       `kogo:"Attachment"`
	Name     string         `kogo:"name"`
	MimeType string         `kogo:"mimeType"`
	Data     kogo.ByteArray `kogo:"data"`
}

type Mail struct {
	_           struct{}              `kogo:"Mail"`
	MessageId   string                `kogo:"messageId"`
	Date        time.Instant          `kogo:"date"`
	From        Address               `kogo:"from"`
	ReplyTo     *Address              `kogo:"replyTo"`
	To          kogo.List[Address]    `kogo:"to"`
	Cc          kogo.List[Address]    `kogo:"cc"`
	Bcc         kogo.List[Address]    `kogo:"bcc"`
	Subject     string                `kogo:"subject"`
	Text        string                `kogo:"text"`
	Html        *string               `kogo:"html"`
	Attachments kogo.List[Attachment] `kogo:"attachments"`
}

// The boundaries are fixed: quoted-printable writes "=" only before
// two hex digits or a line end, and base64 only at a line's end, so no
// body holds "=_".
const mixed, alternative = "=_kogo-mixed", "=_kogo-alternative"

// addressRule is the rule for Address.address, and for a messageId:
// an ASCII addr-spec of RFC 5322's atext and dots, with one "@", at
// most 254 characters (spec.md section 12).
var addressRule = regexp.MustCompile("^" + dotAtom + "@" + dotAtom + "$")

const dotAtom = "[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+(\\.[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+)*"

// Eml writes the message: the same Mail gives the same bytes.
func Eml(m Mail) kogo.ByteArray {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Message-ID: <%s>\r\n", addrSpec("message id", m.MessageId))
	fmt.Fprintf(&b, "Date: %s\r\n", gotime.Unix(m.Date.Seconds, 0).UTC().Format("Mon, 02 Jan 2006 15:04:05 +0000"))
	addresses(&b, "From", []Address{m.From})
	if m.ReplyTo != nil {
		addresses(&b, "Reply-To", []Address{*m.ReplyTo})
	}
	addresses(&b, "To", m.To)
	addresses(&b, "Cc", m.Cc)
	addresses(&b, "Bcc", m.Bcc)
	subject := words(m.Subject)
	if plain(m.Subject) {
		subject = clean(m.Subject)
	}
	b.WriteString(fold("Subject: "+subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	body := func() {
		if m.Html == nil {
			text(&b, "text/plain", m.Text)
			return
		}
		fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n", alternative)
		b.WriteString("\r\n--" + alternative + "\r\n")
		text(&b, "text/plain", m.Text)
		b.WriteString("\r\n--" + alternative + "\r\n")
		text(&b, "text/html", *m.Html)
		b.WriteString("\r\n--" + alternative + "--\r\n")
	}
	if len(m.Attachments) == 0 {
		body()
		return b.Bytes()
	}
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n", mixed)
	b.WriteString("\r\n--" + mixed + "\r\n")
	body()
	for _, a := range m.Attachments {
		b.WriteString("\r\n--" + mixed + "\r\n")
		if a.MimeType == "" || !printable(a.MimeType) {
			panic(fmt.Sprintf("a mail's mime type is printable ASCII: %q", a.MimeType))
		}
		fmt.Fprintf(&b, "Content-Type: %s\r\nContent-Transfer-Encoding: base64\r\n%s\r\n", a.MimeType, disposition(a.Name))
		for s := base64.StdEncoding.EncodeToString(a.Data); s != ""; {
			n := min(76, len(s))
			b.WriteString(s[:n] + "\r\n")
			s = s[n:]
		}
	}
	b.WriteString("\r\n--" + mixed + "--\r\n")
	return b.Bytes()
}

// disposition gives an attachment's Content-Disposition header: its
// name quoted when it is printable ASCII and fits the line, or else in
// RFC 2231's pieces of at most 60 characters, a line each.
func disposition(name string) string {
	quoted := `filename="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	if printable(name) && len(quoted) <= 45 {
		return "Content-Disposition: attachment; " + quoted + "\r\n"
	}
	var pieces []string
	piece := "UTF-8''"
	for _, c := range []byte(name) {
		next := string(c)
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '.' || c == '-' || c == '_') {
			next = fmt.Sprintf("%%%02X", c)
		}
		if len(piece)+len(next) > 60 {
			pieces, piece = append(pieces, piece), ""
		}
		piece += next
	}
	out := "Content-Disposition: attachment"
	for i, p := range append(pieces, piece) {
		out += fmt.Sprintf(";\r\n filename*%d*=%s", i, p)
	}
	return out + "\r\n"
}

// text writes a text part, quoted-printable: a byte outside printable
// ASCII, a "=", and a space or tab that ends a line are =XX; a line
// break is CR LF; a line passes no 75 characters but by a soft break,
// "=" CR LF; and a text not ending in a line break ends in one.
func text(b *bytes.Buffer, kind, s string) {
	fmt.Fprintf(b, "Content-Type: %s; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", kind)
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		n := 0
		for j := 0; j < len(line); j++ {
			c, next := line[j], line[j:j+1]
			if c == '=' || c < ' ' && c != '\t' || c > '~' || (c == ' ' || c == '\t') && j == len(line)-1 {
				next = fmt.Sprintf("=%02X", c)
			}
			if n+len(next) > 75 {
				b.WriteString("=\r\n")
				n = 0
			}
			b.WriteString(next)
			n += len(next)
		}
		if i < len(lines)-1 {
			b.WriteString("\r\n")
		} else if line != "" || len(lines) == 1 {
			b.WriteString("=\r\n")
		}
	}
}

// addresses writes a header of addresses, one to a line, and nothing
// for none. A name that plain allows is quoted; any other is written
// as encoded words.
func addresses(b *bytes.Buffer, header string, as []Address) {
	for i, a := range as {
		name := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(clean(a.Name)) + `" `
		if a.Name == "" {
			name = ""
		} else if !plain(a.Name) {
			name = words(a.Name) + " "
		}
		line := map[bool]string{true: header + ": ", false: " "}[i == 0] + name + "<" + addrSpec("address", a.Address) + ">"
		if i < len(as)-1 {
			line += ","
		}
		b.WriteString(fold(line) + "\r\n")
	}
}

// plain reports whether a name or subject is written as it is: of
// printable ASCII, holding no "=?" that a reader would decode, and with
// no run of 60 characters without a space, so that folding fits it.
func plain(s string) bool {
	s = clean(s)
	return printable(s) && !strings.Contains(s, "=?") &&
		!slices.ContainsFunc(strings.Split(s, " "), func(w string) bool { return len(w) > 60 })
}

// clean gives s with each control character a space, so that no reader
// gives one back to write into a header of its own.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// fold breaks a header's line before a space wherever it would pass 78
// characters, but the space after the header's name.
func fold(line string) string {
	var out strings.Builder
	n := 0
	for i, w := range strings.Split(line, " ") {
		if i > 1 && n+1+len(w) > 78 { // never before a header's first word
			out.WriteString("\r\n")
			n = 0
		}
		if i > 0 {
			out.WriteString(" ")
			n++
		}
		out.WriteString(w)
		n += len(w)
	}
	return out.String()
}

// printable reports whether s is ASCII's printable characters and
// spaces alone.
func printable(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < ' ' || r > '~' }) < 0
}

// addrSpec gives s, which is written as it is, where one outside
// addressRule could end its header or name another recipient: a bug in
// the caller, who checks a typed address against the rule first
// (spec.md sections 8 and 12).
func addrSpec(what, s string) string {
	if len(s) > 254 || !addressRule.MatchString(s) {
		panic(fmt.Sprintf("a mail's %s is an ASCII addr-spec, as spec.md section 12 states it: %q", what, s))
	}
	return s
}

// words gives s as encoded words (RFC 2047), each the base64 of at
// most 39 bytes of UTF-8 and of whole characters, a space between.
func words(s string) string {
	var out []string
	for s = strings.ToValidUTF8(clean(s), "�"); s != ""; {
		n := 0
		for n < len(s) {
			_, size := utf8.DecodeRuneInString(s[n:])
			if n+size > 39 {
				break
			}
			n += size
		}
		out = append(out, "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(s[:n]))+"?=")
		s = s[n:]
	}
	return strings.Join(out, " ")
}
