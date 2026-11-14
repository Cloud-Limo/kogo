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

// An edit records one renumbering: the parameter is at sql[at:], and
// the text after it lies shift bytes further on in the literal.
type edit struct{ at, shift int }

// rewrite replaces each $name and ${expr} with a numbered parameter.
// A name that occurs twice is one parameter; an expression is one
// parameter per occurrence, since evaluating it may have an effect.
func rewrite(literal string) (string, []Param, []edit, error) {
	var sql strings.Builder
	var params []Param
	var edits []edit
	byName := map[string]int{}
	for i := 0; i < len(literal); {
		if literal[i] != '$' || i+1 == len(literal) {
			sql.WriteByte(literal[i])
			i++
			continue
		}
		j, n := i+1, 0
		switch c := literal[j]; {
		case c == '{':
			end := strings.IndexAny(literal[j+1:], "{}")
			if end < 0 || literal[j+1+end] == '{' {
				return "", nil, nil, &Error{i, "sql-parameter-syntax",
					"${ needs a closing } and no braces inside: bind the value to a val and write $name"}
			}
			j += end + 2
			source := strings.TrimSpace(literal[i+2 : j-1])
			if source == "" {
				return "", nil, nil, &Error{i, "sql-parameter-syntax",
					"${} needs an expression between the braces, as in ${row.id}"}
			}
			params = append(params, Param{Source: source, Offset: i})
			n = len(params)
		case isNameByte(c) && (c < '0' || c > '9'):
			for j < len(literal) && isNameByte(literal[j]) {
				j++
			}
			name := literal[i+1 : j]
			if n = byName[name]; n == 0 {
				params = append(params, Param{Source: name, Offset: i})
				n = len(params)
				byName[name] = n
			}
		case c >= '0' && c <= '9':
			return "", nil, nil, &Error{i, "sql-parameter-syntax",
				"a numbered parameter is not Kogo: write $name, the value to bind"}
		default: // a lone $, as in dollar quoting
			sql.WriteByte('$')
			i++
			continue
		}
		at := sql.Len()
		fmt.Fprintf(&sql, "$%d", n)
		edits = append(edits, edit{at, j - sql.Len()})
		i = j
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
