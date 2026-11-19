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

// The boundaries are fixed: every line of a part's body is base64,
// which has no hyphen, so no body holds one.
const mixed, alternative = "kogo-mixed", "kogo-alternative"

// Eml writes the message: the same Mail gives the same bytes.
func Eml(m Mail) kogo.ByteArray {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Message-ID: <%s>\r\n", token("message id", m.MessageId))
	fmt.Fprintf(&b, "Date: %s\r\n", gotime.Unix(m.Date.Seconds, 0).UTC().Format("Mon, 02 Jan 2006 15:04:05 +0000"))
	addresses(&b, "From", []Address{m.From})
	if m.ReplyTo != nil {
		addresses(&b, "Reply-To", []Address{*m.ReplyTo})
	}
	addresses(&b, "To", m.To)
	addresses(&b, "Cc", m.Cc)
	addresses(&b, "Bcc", m.Bcc)
	if s := "Subject: " + m.Subject; len(s) <= 78 && printable(m.Subject) && !strings.Contains(s, "=?") {
		b.WriteString(s + "\r\n")
	} else {
		b.WriteString("Subject: " + words(m.Subject) + "\r\n")
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	body := func() {
		if m.Html == nil {
			part(&b, "text/plain; charset=UTF-8", "", lines(m.Text))
			return
		}
		fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%s\r\n", alternative)
		b.WriteString("\r\n--" + alternative + "\r\n")
		part(&b, "text/plain; charset=UTF-8", "", lines(m.Text))
		b.WriteString("\r\n--" + alternative + "\r\n")
		part(&b, "text/html; charset=UTF-8", "", lines(*m.Html))
		b.WriteString("\r\n--" + alternative + "--\r\n")
	}
	if len(m.Attachments) == 0 {
		body()
		return b.Bytes()
	}
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%s\r\n", mixed)
	b.WriteString("\r\n--" + mixed + "\r\n")
	body()
	for _, a := range m.Attachments {
		b.WriteString("\r\n--" + mixed + "\r\n")
		file := `filename="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(a.Name) + `"`
		if !printable(a.Name) {
			file = "filename*=UTF-8''"
			for _, c := range []byte(a.Name) {
				if c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '.' || c == '-' || c == '_' {
					file += string(c)
				} else {
					file += fmt.Sprintf("%%%02X", c)
				}
			}
		}
		part(&b, token("mime type", a.MimeType), "Content-Disposition: attachment; "+file+"\r\n", a.Data)
	}
	b.WriteString("\r\n--" + mixed + "--\r\n")
	return b.Bytes()
}

// part writes one part's headers and its body in base64, 76 to a line.
func part(b *bytes.Buffer, contentType, more string, body []byte) {
	fmt.Fprintf(b, "Content-Type: %s\r\nContent-Transfer-Encoding: base64\r\n%s\r\n", contentType, more)
	for s := base64.StdEncoding.EncodeToString(body); s != ""; {
		n := min(76, len(s))
		b.WriteString(s[:n] + "\r\n")
		s = s[n:]
	}
}

// lines gives a text with each of its lines ended by CR LF, as a text
// part's are.
func lines(s string) []byte {
	return []byte(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n"))
}

// addresses writes a header of addresses, one to a line, and nothing
// for none. A name of printable ASCII, 60 characters at most, is
// quoted; any other is written as encoded words.
func addresses(b *bytes.Buffer, header string, as []Address) {
	for i, a := range as {
		name := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(a.Name) + `" `
		if a.Name == "" {
			name = ""
		} else if !printable(a.Name) || len(a.Name) > 60 {
			name = words(a.Name) + " "
		}
		b.WriteString(map[bool]string{true: header + ": ", false: ",\r\n "}[i == 0] + name + "<" + token("address", a.Address) + ">")
	}
	if len(as) > 0 {
		b.WriteString("\r\n")
	}
}

// printable reports whether s is ASCII's printable characters and
// spaces alone.
func printable(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < ' ' || r > '~' }) < 0
}

// token gives s, which is written as it is, where a space, a control
// character, an angle bracket or a character outside ASCII would let
// it end its header: a bug in the caller (spec.md section 8).
func token(what, s string) string {
	if s == "" || !printable(s) || strings.ContainsAny(s, " <>") {
		panic(fmt.Sprintf("a mail's %s is printable ASCII with no space or angle bracket: %q", what, s))
	}
	return s
}

// words gives s as encoded words (RFC 2047), each the base64 of at
// most 45 bytes of UTF-8 and of whole characters, one to a line.
func words(s string) string {
	var out []string
	for s = strings.ToValidUTF8(s, "�"); s != ""; {
		n := 0
		for n < len(s) {
			_, size := utf8.DecodeRuneInString(s[n:])
			if n+size > 45 {
				break
			}
			n += size
		}
		out = append(out, "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(s[:n]))+"?=")
		s = s[n:]
	}
	return strings.Join(out, "\r\n ")
}
