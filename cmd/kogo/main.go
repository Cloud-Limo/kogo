// Command kogo checks Kogo programs.
//
//	kogo check <dir>   check the .kg files under dir as one program
//
// It prints one JSON object a line for each error (file, line, column,
// code, message) and exits 0 when there are none, 1 when there are
// some, 2 when it could not run. A file that does not parse stops the
// check: the files that parse are only parsed. A program that holds a
// sql literal is checked against the database $DATABASE_URL names, or
// the PG* variables when that is unset; one that holds none, against
// none.
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
	"slices"
	"strings"

	"kogo/check"
	"kogo/prelude"
	"kogo/sqlpass"
	"kogo/syntax"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	// JSON as it reads: List<Uuid>, not List<Uuid>.
	out, fatal := json.NewEncoder(stdout), json.NewEncoder(stderr)
	out.SetEscapeHTML(false)
	fatal.SetEscapeHTML(false)
	if len(args) != 2 || args[0] != "check" {
		fatal.Encode(syntax.Error{Code: "usage", Message: "usage: kogo check <dir>"})
		return 2
	}
	pre, err := prelude.Files()
	if err != nil {
		fatal.Encode(syntax.Error{Code: "prelude", Message: err.Error()})
		return 2
	}
	var files []*syntax.File
	failed := false
	err = filepath.WalkDir(args[1], func(file string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(file, ".kg") {
			return err
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		tree, err := syntax.Parse(file, src)
		var bad *syntax.Error
		if errors.As(err, &bad) { // the other files are still parsed
			failed = true
			return out.Encode(bad)
		}
		files = append(files, tree)
		return err
	})
	if err != nil {
		fatal.Encode(syntax.Error{Code: "read", Message: err.Error()})
		return 2
	}
	if failed {
		// Checked without a file's declarations, the others would show
		// false errors: each name it declares would be unknown.
		return 1
	}
	var sigs check.Signatures
	var lost error // a failure to ask the database, which stops the check
	if slices.ContainsFunc(files, func(f *syntax.File) bool { return f.SQL != nil }) {
		ctx := context.Background()
		db, err := sqlpass.Connect(ctx, os.Getenv("DATABASE_URL"))
		if err != nil {
			fatal.Encode(syntax.Error{Code: "sql-connect", Message: fmt.Sprintf("no development database, set DATABASE_URL or the PG* variables: %v", err)})
			return 2
		}
		defer db.Close(ctx)
		ask := check.Signer(ctx, db)
		sigs = func(lit *syntax.SQL) (*sqlpass.Statement, error) {
			st, err := ask(lit)
			var fault *sqlpass.Error
			if err != nil && !errors.As(err, &fault) && lost == nil {
				lost = err
			}
			return st, err
		}
	}
	_, faults := check.Check(pre, files, sigs)
	if lost != nil {
		fatal.Encode(syntax.Error{Code: "sql-check", Message: lost.Error()})
		return 2
	}
	for _, f := range faults {
		out.Encode(f)
	}
	if len(faults) > 0 {
		return 1
	}
	return 0
}
