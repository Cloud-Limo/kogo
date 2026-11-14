// Command kogo-sql checks the sql literals of .kg files against Postgres.
//
//	kogo-sql check <dir>          print each literal's signature, or its error, one JSON object a line
//	kogo-sql emit <dir> <package> print a Go file with one function per literal
//
// The database is $DATABASE_URL, or the PG* variables when that is unset.
// It exits 0 when every literal passes, 1 when one fails, 2 when it could not run.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"kogo/sqlpass"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

type diagnostic struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type signature struct {
	File    string  `json:"file"`
	Line    int     `json:"line"`
	Column  int     `json:"column"`
	Params  []named `json:"params"`
	Columns []named `json:"columns"`
}

type named struct {
	Name string `json:"name"`
	Type string `json:"type"` // as Kogo writes it: List<Uuid>, String?
}

// encoder writes JSON as it reads: List<Uuid>, not List\u003cUuid\u003e.
func encoder(w io.Writer) *json.Encoder {
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	return e
}

var nonWord, wordByte = regexp.MustCompile(`\W`), regexp.MustCompile(`\w`)

// funcName names the Go function of the literal at line and column of
// a file, rel being its path from the directory without .kg. Mapping
// the path to a Go name can make two files' names alike; used holds
// the names given, and a name already given gets a number added.
func funcName(rel string, line, column int, used map[string]bool) string {
	base := fmt.Sprintf("Q%sL%dC%d", nonWord.ReplaceAllString(rel, "_"), line, column)
	name := base
	for n := 2; used[name]; n++ {
		name = fmt.Sprintf("%s_%d", base, n)
	}
	used[name] = true
	return name
}

func run(args []string, stdout, stderr io.Writer) int {
	fatal := func(code string, err error) int {
		encoder(stderr).Encode(diagnostic{Code: code, Message: err.Error()})
		return 2
	}
	emit := len(args) == 3 && args[0] == "emit"
	if !emit && (len(args) != 2 || args[0] != "check") {
		return fatal("usage", errors.New("usage: kogo-sql check <dir>, or kogo-sql emit <dir> <package>"))
	}
	ctx := context.Background()
	checker, err := sqlpass.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return fatal("sql-connect", fmt.Errorf("no development database, set DATABASE_URL or the PG* variables: %w", err))
	}
	defer checker.Close(ctx)

	out, faults := encoder(stdout), encoder(stdout)
	if emit {
		faults = encoder(stderr)
	}
	var funcs []sqlpass.Func
	names := map[string]bool{}
	failed := false
	err = filepath.WalkDir(args[1], func(file string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(file, ".kg") {
			return err
		}
		bytes, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		src := string(bytes)
		for _, lit := range find(src) {
			line, column := position(src, lit.at)
			if lit.open {
				failed = true
				faults.Encode(diagnostic{file, line, column, "sql-unterminated", "this sql literal has no closing quote"})
				continue
			}
			st, err := checker.Check(ctx, lit.body)
			var fault *sqlpass.Error
			switch {
			case errors.As(err, &fault):
				failed = true
				line, column = position(src, lit.text+fault.Offset)
				faults.Encode(diagnostic{file, line, column, fault.Code, fault.Message})
			case err != nil:
				return err
			case emit:
				rel, _ := filepath.Rel(args[1], strings.TrimSuffix(file, ".kg"))
				funcs = append(funcs, sqlpass.Func{Name: funcName(rel, line, column, names), Stmt: st})
			default:
				sig := signature{file, line, column, []named{}, []named{}}
				for _, p := range st.Params {
					sig.Params = append(sig.Params, named{p.Source, p.Type.String()})
				}
				for _, c := range st.Columns {
					t := c.Type.String()
					if c.Nullable {
						t += "?"
					}
					sig.Columns = append(sig.Columns, named{c.Name, t})
				}
				out.Encode(sig)
			}
		}
		return nil
	})
	if err != nil {
		return fatal("sql-check", err)
	}
	if failed {
		return 1
	}
	if emit {
		src, err := sqlpass.Emit(args[2], funcs)
		if err != nil {
			return fatal("sql-emit", err)
		}
		stdout.Write(src)
	}
	return 0
}

// A literal is a sql"..." or sql"""...""" in a source file: where it
// starts, where its text starts, and the text. An open one never closes.
type literal struct {
	at, text int
	body     string
	open     bool
}

// find stands in for the parser: it reads no Kogo, so a literal in a
// comment or a string is found too. Neither form has escapes.
func find(src string) []literal {
	var lits []literal
	for i := 0; ; {
		at := strings.Index(src[i:], `sql"`)
		if at < 0 {
			return lits
		}
		at += i
		i = at + len(`sql"`)
		if at > 0 && wordByte.MatchString(src[at-1:at]) {
			continue // the end of a longer name
		}
		quote := `"`
		if strings.HasPrefix(src[i:], `""`) {
			quote, i = `"""`, i+2
		}
		end := strings.Index(src[i:], quote)
		if end < 0 || quote == `"` && strings.Contains(src[i:i+end], "\n") {
			lits = append(lits, literal{at: at, open: true})
			continue
		}
		lits = append(lits, literal{at: at, text: i, body: src[i : i+end]})
		i += end + len(quote)
	}
}

// position gives the line and column, both from 1, of a byte offset.
func position(src string, offset int) (line, column int) {
	before := src[:offset]
	start := strings.LastIndexByte(before, '\n') + 1
	return strings.Count(before, "\n") + 1, utf8.RuneCountInString(before[start:]) + 1
}
