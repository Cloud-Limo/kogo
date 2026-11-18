// Package test is the Go behind Kogo's package kogo.test, in
// prelude/test.kg, and the runner of kogo test (spec.md section 16).
package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"kogo/prelude/kogo"
	"kogo/rt"
)

// A Test is a sentence saying what it shows, and the block that
// shows it.
type Test[E any] struct {
	sentence string
	body     func(kogo.Tx, E) rt.Unit
}

func Test_String_TxEToUnit[E any](sentence string, body func(kogo.Tx, E) rt.Unit) Test[E] {
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

// A List is a tests list.
type List[E any] struct {
	Name  string
	Tests []Placed[E]
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

// Run runs each test of the lists in a transaction of its own, which
// is rolled back however the test ends, and writes each failure to
// out, one JSON object a line. It gives how many tests failed, which
// the application's test main makes its exit status: 1 for any.
func Run[E any](ctx context.Context, pool *pgxpool.Pool, env E, out io.Writer, lists ...List[E]) (failed int) {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for _, l := range lists {
		for _, t := range l.Tests {
			if f := run(kogo.NewDb(ctx, pool), env, t); f != nil {
				failed++
				enc.Encode(f)
			}
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
