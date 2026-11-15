// Package sqlpass checks a Kogo sql literal against a Postgres database
// and infers its signature: spec.md section 10.
package sqlpass

import (
	"fmt"
	"strings"
)

// An Error is a fault in a literal. Offset is a byte offset in its text.
type Error struct {
	Offset  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// A Param is one bound parameter.
type Param struct {
	Source string // the name or expression as written, without $ or ${}
	Offset int    // of its first occurrence in the literal
	Type   Type
}

// A Span is where one $name or ${expr} of a literal lies, as the
// parser found it: literal[Start:End].
type Span struct{ Start, End int }

// An edit records one renumbering: the parameter is at sql[at:], and
// the text after it lies shift bytes further on in the literal.
type edit struct{ at, shift int }

// rewrite replaces each parameter with a numbered one. A name that
// occurs twice is one parameter; an expression is one parameter per
// occurrence, since evaluating it may have an effect. Any other $ is
// SQL text, but for a numbered parameter, which is refused.
func rewrite(literal string, spans []Span) (string, []Param, []edit, error) {
	var sql strings.Builder
	var params []Param
	var edits []edit
	byName := map[string]int{}
	text := func(from, to int) error {
		for i := from; i+1 < to; i++ {
			if literal[i] == '$' && literal[i+1] >= '0' && literal[i+1] <= '9' {
				return &Error{i, "sql-parameter-syntax", "a numbered parameter is not Kogo: write $name, the value to bind"}
			}
		}
		sql.WriteString(literal[from:to])
		return nil
	}
	prev := 0
	for _, s := range spans {
		if err := text(prev, s.Start); err != nil {
			return "", nil, nil, err
		}
		var n int
		if name := literal[s.Start+1 : s.End]; name[0] == '{' {
			params = append(params, Param{Source: strings.TrimSpace(name[1 : len(name)-1]), Offset: s.Start})
			n = len(params)
		} else if n = byName[name]; n == 0 {
			params = append(params, Param{Source: name, Offset: s.Start})
			n = len(params)
			byName[name] = n
		}
		at := sql.Len()
		fmt.Fprintf(&sql, "$%d", n)
		edits = append(edits, edit{at, s.End - sql.Len()})
		prev = s.End
	}
	if err := text(prev, len(literal)); err != nil {
		return "", nil, nil, err
	}
	return sql.String(), params, edits, nil
}

func isNameByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// origin maps a byte offset in the rewritten SQL back to the literal.
func origin(edits []edit, at int) int {
	shift := 0
	for _, e := range edits {
		if at <= e.at {
			break
		}
		shift = e.shift
	}
	return at + shift
}
