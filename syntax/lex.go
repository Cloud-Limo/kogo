package syntax

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// An Error is a fault in a source file, at a line and a column counted
// from 1, with a code a program can match and a message that says what
// to write instead.
type Error struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s: %s", e.File, e.Line, e.Column, e.Code, e.Message)
}

// A source is a file's text and where each of its lines starts.
type source struct {
	name  string
	text  string
	lines []int
}

func newSource(name, text string) *source {
	s := &source{name: name, text: text, lines: []int{0}}
	for i := range len(text) {
		if text[i] == '\n' {
			s.lines = append(s.lines, i+1)
		}
	}
	return s
}

func (s *source) pos(off int) Pos {
	line := sort.Search(len(s.lines), func(i int) bool { return s.lines[i] > off })
	return Pos{off, line, utf8.RuneCountInString(s.text[s.lines[line-1]:off]) + 1}
}

// fail stops the parse of the file with an error at p.
func fail(s *source, p Pos, code, format string, args ...any) {
	panic(&Error{s.name, p.Line, p.Column, code, fmt.Sprintf(format, args...)})
}

type kind int

const (
	tEOF kind = iota
	tIdent
	tNumber // Lit says Int, Long or Double
	tChar
	tString
	tSQL
	tOp
)

type token struct {
	kind    kind
	text    string // as written
	pos     Pos
	nl      bool   // a line ends between the token before and this one
	lit     string // a number's kind
	value   string // a number or a character read; the text of a sql literal
	raw     bool   // a """ string
	textPos Pos    // where a sql literal's text starts
	parts   []part // of a string or a sql literal
}

// A part of a string or a sql literal is text, a $name, or a ${expr}
// whose source lies at [start, end). pos is of the text or of the $.
type part struct {
	text       string
	name       string
	expr       bool
	pos        Pos
	start, end int
}

type lexer struct {
	*source
	off, end int
}

// lex reads the tokens of text[start:end]; the last is tEOF.
func lex(s *source, start, end int) []token {
	l := &lexer{s, start, end}
	var toks []token
	for {
		t := l.next()
		toks = append(toks, t)
		if t.kind == tEOF {
			return toks
		}
	}
}

// Longest first, so that ?. is read before ?.
var operators = strings.Fields(`=== !== ?. ?: :: .. -> == != <= >= && || += -= *= /= %= ++ --
	+ - * / % = < > ! ? . , : ; ( ) [ ] { } @`)

func (l *lexer) next() token {
	nl := l.space()
	start := l.off
	t := token{pos: l.pos(start), nl: nl}
	if l.off >= l.end {
		return t
	}
	c, size := utf8.DecodeRuneInString(l.text[l.off:])
	switch {
	case isLetter(c):
		l.off = l.name(l.off, false)
		t.kind = tIdent
		if l.text[start:l.off] == "sql" && l.off < l.end && l.text[l.off] == '"' {
			l.sql(&t)
		}
	case c >= '0' && c <= '9':
		l.number(&t)
	case c == '"':
		l.str(&t)
	case c == '\'':
		l.char(&t)
	default:
		for _, op := range operators {
			if strings.HasPrefix(l.text[l.off:l.end], op) {
				t.kind = tOp
				l.off += len(op)
				break
			}
		}
		if t.kind != tOp {
			fail(l.source, t.pos, "syntax", "%q is not part of Kogo's syntax", l.text[start:start+size])
		}
	}
	t.text = l.text[start:l.off]
	return t
}

func isLetter(c rune) bool { return c == '_' || unicode.IsLetter(c) }

// name gives the end of the name that starts at off. A sql
// parameter's name is ASCII, as the SQL pass reads it.
func (l *lexer) name(off int, ascii bool) int {
	for first := true; off < l.end; first = false {
		c, size := utf8.DecodeRuneInString(l.text[off:l.end])
		if !isLetter(c) && (first || !unicode.IsDigit(c)) || ascii && c >= utf8.RuneSelf {
			break
		}
		off += size
	}
	return off
}

// space skips blanks and comments and reports whether a line ended
// outside a comment. As in Kotlin, a line end inside a block comment
// does not count, and block comments nest.
func (l *lexer) space() bool {
	nl := false
	for l.off < l.end {
		switch rest := l.text[l.off:l.end]; {
		case rest[0] == '\n':
			nl = true
			l.off++
		case rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r':
			l.off++
		case strings.HasPrefix(rest, "//"):
			if i := strings.IndexByte(rest, '\n'); i >= 0 {
				l.off += i
			} else {
				l.off = l.end
			}
		case strings.HasPrefix(rest, "/*"):
			start, depth := l.off, 0
			for {
				rest = l.text[l.off:l.end]
				switch {
				case rest == "":
					fail(l.source, l.pos(start), "syntax", "this comment has no closing */")
				case strings.HasPrefix(rest, "/*"):
					depth++
					l.off += 2
				case strings.HasPrefix(rest, "*/"):
					depth--
					l.off += 2
				default:
					l.off++
				}
				if depth == 0 {
					break
				}
			}
		default:
			return nl
		}
	}
	return nl
}

// number reads 1, 1L, 1.0 or 10_000.
func (l *lexer) number(t *token) {
	bad := func() { fail(l.source, t.pos, "number-syntax", "write a number as 1, 1L, 1.0 or 10_000") }
	digits := func() {
		for l.off < l.end && (l.text[l.off] >= '0' && l.text[l.off] <= '9' || l.text[l.off] == '_') {
			l.off++
		}
		if l.text[l.off-1] == '_' {
			bad()
		}
	}
	start := l.off
	digits()
	t.kind, t.lit = tNumber, "Int"
	if l.off+1 < l.end && l.text[l.off] == '.' && l.text[l.off+1] >= '0' && l.text[l.off+1] <= '9' {
		l.off++
		digits()
		t.lit = "Double"
	}
	t.value = strings.ReplaceAll(l.text[start:l.off], "_", "")
	if t.lit == "Int" && l.off < l.end && l.text[l.off] == 'L' {
		l.off++
		t.lit = "Long"
	}
	if c, _ := utf8.DecodeRuneInString(l.text[l.off:l.end]); isLetter(c) || unicode.IsDigit(c) {
		bad()
	}
}

// escape reads the escape at l.off, a backslash: \t \b \n \r \' \" \\
// \$ and \uXXXX, as in Kotlin. A surrogate is written as a pair, which
// gives one rune: Go text cannot hold half of one.
func (l *lexer) escape(b *strings.Builder) {
	rest := l.text[l.off:l.end]
	if len(rest) >= 2 {
		if i := strings.IndexByte(`tbnr'"\$`, rest[1]); i >= 0 {
			b.WriteByte("\t\b\n\r'\"\\$"[i])
			l.off += 2
			return
		}
		if r, ok := unit(rest); ok {
			n := 6
			if utf16.IsSurrogate(r) {
				lo, ok := unit(rest[6:])
				if r = utf16.DecodeRune(r, lo); !ok || r == unicode.ReplacementChar {
					fail(l.source, l.pos(l.off), "string-escape", `\uD800 to \uDFFF are halves of a pair, written together in a string: "\uD83D\uDE00"`)
				}
				n = 12
			}
			b.WriteRune(r)
			l.off += n
			return
		}
	}
	fail(l.source, l.pos(l.off), "string-escape", `the escapes are \t \b \n \r \' \" \\ \$ and \uXXXX`)
}

// unit reads the \uXXXX that s begins with, if it does.
func unit(s string) (rune, bool) {
	if len(s) < 6 || s[:2] != `\u` || strings.ContainsAny(s[2:6], "+-_") {
		return 0, false
	}
	r, err := strconv.ParseUint(s[2:6], 16, 16)
	return rune(r), err == nil
}

// char reads 'c' or an escape: one UTF-16 code unit.
func (l *lexer) char(t *token) {
	l.off++
	var b strings.Builder
	if l.off < l.end && l.text[l.off] == '\\' {
		l.escape(&b)
	} else if c, size := utf8.DecodeRuneInString(l.text[l.off:l.end]); size > 0 && c != '\'' && c != '\n' && c <= 0xFFFF {
		b.WriteRune(c)
		l.off += size
	}
	if r, _ := utf8.DecodeRuneInString(b.String()); b.Len() == 0 || r > 0xFFFF || l.off >= l.end || l.text[l.off] != '\'' {
		fail(l.source, t.pos, "char-syntax", "a Char is one character in single quotes, as 'a' or '\\n'")
	}
	l.off++
	t.kind, t.value = tChar, b.String()
}

// closing reports whether a literal's closing quote is at l.off. As
// in Kotlin, quotes before the three that close a raw literal belong
// to its text: extra counts them.
func (l *lexer) closing(triple bool) (ok bool, extra int) {
	rest := l.text[l.off:l.end]
	if !triple {
		return strings.HasPrefix(rest, `"`), 0
	}
	q := len(rest) - len(strings.TrimLeft(rest, `"`))
	return q >= 3, q - 3
}

func quotes(triple bool) int {
	if triple {
		return 3
	}
	return 1
}

// template reads the $name or ${expr} at l.off, if there is one.
func (l *lexer) template(ascii bool) (part, bool) {
	rest := l.text[l.off:l.end]
	p := part{pos: l.pos(l.off)}
	if rest[0] != '$' {
		return p, false
	}
	if strings.HasPrefix(rest, "${") {
		p.expr, p.start = true, l.off+2
		p.end = l.closeBrace(p.start, p.pos)
		l.off = p.end + 1
		return p, true
	}
	end := l.name(l.off+1, ascii)
	if end == l.off+1 {
		return p, false // a lone $ is text
	}
	p.name = l.text[l.off+1 : end]
	l.off = end
	return p, true
}

// closeBrace gives the offset of the } that closes a ${ whose
// expression starts at start. Strings in it are read whole, so they
// may hold braces and templates of their own.
func (l *lexer) closeBrace(start int, dollar Pos) int {
	sub := &lexer{l.source, start, l.end}
	for depth := 0; ; {
		t := sub.next()
		switch {
		case t.kind == tEOF:
			fail(l.source, dollar, "syntax", "this ${ has no closing }")
		case t.kind == tOp && t.text == "{":
			depth++
		case t.kind == tOp && t.text == "}":
			if depth == 0 {
				return t.pos.Offset
			}
			depth--
		}
	}
}

// str reads "..." or """...""" into text parts and templates.
func (l *lexer) str(t *token) {
	t.kind, t.raw = tString, strings.HasPrefix(l.text[l.off:l.end], `"""`)
	l.off += quotes(t.raw)
	var b strings.Builder
	textPos := l.pos(l.off)
	flush := func() {
		if b.Len() > 0 {
			t.parts = append(t.parts, part{text: b.String(), pos: textPos})
			b.Reset()
		}
	}
	for {
		if l.off >= l.end || !t.raw && l.text[l.off] == '\n' {
			fail(l.source, t.pos, "string-unterminated", "this string has no closing quote")
		}
		if ok, extra := l.closing(t.raw); ok {
			b.WriteString(l.text[l.off : l.off+extra])
			l.off += extra + quotes(t.raw)
			flush()
			return
		}
		at := l.off
		if p, ok := l.template(false); ok {
			flush()
			t.parts = append(t.parts, p)
			textPos = l.pos(l.off)
		} else if !t.raw && l.text[l.off] == '\\' {
			l.escape(&b)
		} else {
			_, size := utf8.DecodeRuneInString(l.text[l.off:l.end])
			b.WriteString(l.text[at : at+size])
			l.off += size
		}
	}
}

// sql reads the literal after the word sql at l.off: no escapes, and
// $name or ${expr} as parameters. A name is ASCII, as the SQL pass
// reads it.
func (l *lexer) sql(t *token) {
	triple := strings.HasPrefix(l.text[l.off:l.end], `"""`)
	l.off += quotes(triple)
	t.kind, t.textPos = tSQL, l.pos(l.off)
	for {
		if l.off >= l.end || !triple && l.text[l.off] == '\n' {
			fail(l.source, t.pos, "sql-unterminated", "this sql literal has no closing quote")
		}
		if ok, extra := l.closing(triple); ok {
			t.value = l.text[t.textPos.Offset : l.off+extra]
			l.off += extra + quotes(triple)
			return
		}
		if p, ok := l.template(true); ok {
			t.parts = append(t.parts, p)
		} else {
			l.off++
		}
	}
}
