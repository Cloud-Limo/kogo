// Command kogo checks Kogo programs.
//
//	kogo check <dir>   check the .kg files under dir as one program
//
// It prints one JSON object a line for each error (file, line, column,
// code, message) and exits 0 when there are none, 1 when there are
// some, 2 when it could not run.
package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"kogo/check"
	"kogo/prelude"
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
	files, err := prelude.Files()
	if err != nil {
		fatal.Encode(syntax.Error{Code: "prelude", Message: err.Error()})
		return 2
	}
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
		if errors.As(err, &bad) { // the other files are still checked
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
	_, faults := check.Check(files)
	for _, f := range faults {
		out.Encode(f)
	}
	if failed || len(faults) > 0 {
		return 1
	}
	return 0
}
