// Command kogo-sql checks the sql literals of .kg files against Postgres.
// A file that does not parse is reported and its literals are not checked.
//
//	kogo-sql check <dir>   print each literal's signature, or its error, one JSON object a line
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
	"strings"
	"unicode/utf8"

	"kogo/check"
	"kogo/sqlpass"
	"kogo/syntax"
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

func run(args []string, stdout, stderr io.Writer) int {
	fatal := func(code string, err error) int {
		encoder(stderr).Encode(diagnostic{Code: code, Message: err.Error()})
		return 2
	}
	if len(args) != 2 || args[0] != "check" {
		return fatal("usage", errors.New("usage: kogo-sql check <dir>"))
	}
	ctx := context.Background()
	checker, err := sqlpass.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return fatal("sql-connect", fmt.Errorf("no development database, set DATABASE_URL or the PG* variables: %w", err))
	}
	defer checker.Close(ctx)
	ask := check.Signer(ctx, checker)

	out := encoder(stdout)
	failed := false
	err = filepath.WalkDir(args[1], func(file string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(file, ".kg") {
			return err
		}
		bytes, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		tree, err := syntax.Parse(file, bytes)
		var bad *syntax.Error
		if errors.As(err, &bad) {
			failed = true
			out.Encode(diagnostic{file, bad.Line, bad.Column, bad.Code, bad.Message})
			return nil
		}
		for _, lit := range tree.SQL {
			line, column := lit.Line, lit.Column
			st, err := ask(lit)
			var fault *sqlpass.Error
			switch {
			case errors.As(err, &fault):
				failed = true
				line, column = position(string(bytes), lit.TextPos.Offset+fault.Offset)
				out.Encode(diagnostic{file, line, column, fault.Code, fault.Message})
			case err != nil:
				return err
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
	return 0
}

// position gives the line and column, both from 1, of a byte offset.
func position(src string, offset int) (line, column int) {
	before := src[:offset]
	start := strings.LastIndexByte(before, '\n') + 1
	return strings.Count(before, "\n") + 1, utf8.RuneCountInString(before[start:]) + 1
}
