// Package test is the Go behind Kogo's package kogo.test, in
// prelude/test.kg, and the runner of kogo test (spec.md section 16).
package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	"github.com/Cloud-Limo/kogo/rt"
)

// A Test is a sentence saying what it shows, and the block that
// shows it.
type Test[E any] struct {
	sentence string
	body     func(kogo.Tx, E) rt.Unit
}

func Test_String_SerializableTxEToUnit[E any](sentence string, body func(kogo.Tx, E) rt.Unit) Test[E] {
	return Test[E]{sentence, body}
}

// A Refusal is what Postgres said of a statement that tx.refusal ran.
type Refusal = kogo.Refusal

func Refusal_SqlState(this Refusal) string    { return this.SqlState }
func Refusal_Constraint(this Refusal) *string { return this.Constraint }
func Refusal_Message(this Refusal) string     { return this.Message }

// A Placed is a test and where its val is declared.
type Placed[E any] struct {
	Test         Test[E]
	File         string
	Line, Column int
}

// At places a test: the emitter's call, for each name of a list.
func At[E any](t Test[E], file string, line, column int) Placed[E] {
	return Placed[E]{t, file, line, column}
}

// A List is a tests list: its Kogo name, as package.name, and where
// it is declared.
type List[E any] struct {
	Name         string
	File         string
	Line, Column int
	Tests        []Placed[E]
}

// A list registered, with how to run it given the environments.
type registered struct {
	list    List[struct{}] // the list without its tests: name and place
	matches func(env any) bool
	run     func(ctx context.Context, pool *pgxpool.Pool, env any, enc *json.Encoder) (failed int)
}

var lists []registered

// Register makes a list one that Run runs: the emitter's
// initializer of each list's var, so that every list linked into the
// test main runs without the main naming it.
func Register[E any](l List[E]) List[E] {
	lists = append(lists, registered{
		List[struct{}]{Name: l.Name, File: l.File, Line: l.Line, Column: l.Column},
		func(env any) bool { _, ok := env.(E); return ok },
		func(ctx context.Context, pool *pgxpool.Pool, env any, enc *json.Encoder) (failed int) {
			for _, t := range l.Tests {
				if f := run(kogo.NewDb(ctx, pool), env.(E), t); f != nil {
					failed++
					enc.Encode(f)
				}
			}
			return failed
		},
	})
	return l
}

// A Failure is a test that failed, as kogo test prints it: where the
// test is declared, its sentence, the panic that ended it and the
// Kogo line the panic came from.
type Failure struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Code    string `json:"code"`
	Test    string `json:"test"`
	Message string `json:"message"`
	At      string `json:"at"`
}

// Run runs each test of every registered list, with the first of
// envs that is the list's environment, in a transaction of its own,
// which is rolled back however the test ends, and writes each failure
// to out, one JSON object a line. A list no env is the environment of
// fails, as does a list that kogo test named in KOGO_TEST_LISTS and
// the test main does not link. It gives how many tests and lists
// failed, which the application's test main makes its exit status: 1
// for any.
func Run(ctx context.Context, pool *pgxpool.Pool, out io.Writer, envs ...any) (failed int) {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	linked := map[string]bool{}
	for _, l := range lists {
		linked[l.list.Name] = true
		i := slices.IndexFunc(envs, l.matches)
		if i < 0 {
			failed++
			enc.Encode(Failure{File: l.list.File, Line: l.list.Line, Column: l.list.Column, Code: "no-env", Test: l.list.Name,
				Message: fmt.Sprintf("the test main gives test.Run no environment of the type that tests list %s takes: pass one", l.list.Name)})
			continue
		}
		failed += l.run(ctx, pool, envs[i], enc)
	}
	for list := range strings.Lines(os.Getenv("KOGO_TEST_LISTS")) {
		f := strings.Split(strings.TrimSuffix(list, "\n"), "\t") // name, file, line, column
		if !linked[f[0]] && len(f) == 4 {
			name := f[0]
			line, _ := strconv.Atoi(f[2])
			column, _ := strconv.Atoi(f[3])
			failed++
			enc.Encode(Failure{File: f[1], Line: line, Column: column, Code: "not-linked", Test: name,
				Message: fmt.Sprintf("the test main does not import the package of tests list %s, so its tests did not run: import it, as _ if need be", name)})
		}
	}
	return failed
}

// run runs one test. A panic fails it, and only here is one caught:
// the test is over by then.
func run[E any](db kogo.Db, env E, t Placed[E]) (failure *Failure) {
	defer func() {
		if p := recover(); p != nil {
			failure = &Failure{t.File, t.Line, t.Column, "test-failed", t.Test.sentence, fmt.Sprint(p), kogoLine()}
		}
	}()
	kogo.RolledBack(db, func(tx kogo.Tx) { t.Test.body(tx, env) })
	return nil
}

// kogoLine gives the innermost Kogo line of the panic at hand, as
// file:line, from the emitted Go's line directives; "" if none.
func kogoLine() string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.File, ".kg") {
			return fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		if !more {
			return ""
		}
	}
}
