// Command kogo checks Kogo programs and writes their Go and the
// TypeScript of their operations.
//
//	kogo check <dir>        check the .kg files under dir as one program
//	kogo emit <dir> <out>   check them and, when they pass, write their Go under out
//	kogo ts <dir> <out>     check them and, when they pass, write out/<name>.ts for each api list
//
// It prints one JSON object a line for each error (file, line, column,
// code, message) and exits 0 when there are none, 1 when there are
// some, 2 when it could not run. A file that does not parse stops the
// check: the files that parse are only parsed. A program that holds a
// sql literal is checked against the database $DATABASE_URL names, or
// the PG* variables when that is unset; one that holds none, against
// none.
//
// emit writes one Go package for each Kogo package, a.b's at
// <out>/a/b/kogo.go. The Go imports kogo/rt and kogo/prelude/..., and
// its own packages by the import path of <out>, which is read from
// the go.mod above it: so <out> lies in a Go module that requires
// this one. emit refuses, writing nothing, when <out> holds a kogo.go
// that it wrote, by its header, for a package the program no longer
// has; it leaves every other file there alone.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"kogo/check"
	"kogo/emit"
	"kogo/prelude"
	"kogo/sqlpass"
	"kogo/syntax"
	"kogo/ts"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	// JSON as it reads: List<Uuid>, not List<Uuid>.
	out, fatal := json.NewEncoder(stdout), json.NewEncoder(stderr)
	out.SetEscapeHTML(false)
	fatal.SetEscapeHTML(false)
	if !(len(args) == 2 && args[0] == "check" || len(args) == 3 && (args[0] == "emit" || args[0] == "ts")) {
		fatal.Encode(syntax.Error{Code: "usage", Message: "usage: kogo check <dir>, kogo emit <dir> <out>, or kogo ts <dir> <out>"})
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
	info, faults := check.Check(pre, files, sigs)
	if lost != nil {
		fatal.Encode(syntax.Error{Code: "sql-check", Message: lost.Error()})
		return 2
	}
	var written map[string][]byte
	if len(faults) == 0 && args[0] == "emit" {
		root, err := importPath(args[2])
		if err != nil {
			fatal.Encode(syntax.Error{Code: "no-module", Message: err.Error()})
			return 2
		}
		written, faults = emit.Emit(info, pre, files, root, args[2])
		if len(faults) == 0 {
			stale, err := staleFile(args[2], written)
			if err != nil {
				fatal.Encode(syntax.Error{Code: "read", Message: err.Error()})
				return 2
			}
			if stale != "" {
				fatal.Encode(syntax.Error{File: stale, Code: "stale-output", Message: fmt.Sprintf("%s was written by kogo emit for a package this program no longer has, and the Go under %s would build it beside the new: remove it, or emit into another directory", stale, args[2])})
				return 2
			}
		}
	}
	if len(faults) == 0 && args[0] == "ts" {
		written = ts.Files(info)
	}
	for _, f := range faults {
		out.Encode(f)
	}
	if len(faults) > 0 {
		return 1
	}
	for name, text := range written {
		file := filepath.Join(args[2], filepath.FromSlash(name))
		err := os.MkdirAll(filepath.Dir(file), 0o755)
		if err == nil {
			err = os.WriteFile(file, text, 0o644)
		}
		if err != nil {
			fatal.Encode(syntax.Error{Code: "write", Message: err.Error()})
			return 2
		}
	}
	return 0
}

// emitted is how a Go file kogo emit wrote begins.
const emitted = "// Code generated by kogo emit"

// staleFile gives the first file under out, in lexical order, that
// kogo emit wrote and this emit would not: a kogo.go beginning with
// its header, for a package the program no longer has. Every other
// file is the user's. It gives "" when there is none.
func staleFile(out string, written map[string][]byte) (string, error) {
	stale := ""
	err := filepath.WalkDir(out, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			if file == out && errors.Is(err, fs.ErrNotExist) {
				return nil // nothing written there yet
			}
			return err
		}
		if d.IsDir() || d.Name() != "kogo.go" {
			return nil
		}
		rel, err := filepath.Rel(out, file)
		if err != nil {
			return err
		}
		if _, ok := written[filepath.ToSlash(rel)]; ok {
			return nil
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		head := make([]byte, len(emitted))
		if _, err := io.ReadFull(f, head); err != nil || string(head) != emitted {
			return nil // shorter than the header, or not kogo's
		}
		stale = file
		return fs.SkipAll
	})
	return stale, err
}

// importPath gives the import path of the directory out: that of the
// module whose go.mod lies above it, and the way down from there.
func importPath(out string) (string, error) {
	abs, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	for dir := abs; ; dir = filepath.Dir(dir) {
		mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		for _, line := range strings.Split(string(mod), "\n") {
			if name, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok && err == nil {
				rel, err := filepath.Rel(dir, abs)
				return path.Join(strings.Trim(name, `" `), filepath.ToSlash(rel)), err
			}
		}
		if dir == filepath.Dir(dir) {
			return "", fmt.Errorf("no go.mod lies above %s: write the Go inside a Go module that requires kogo, whose rt and prelude it imports", out)
		}
	}
}
