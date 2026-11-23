package check

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Cloud-Limo/kogo/sqlpass"
	"github.com/Cloud-Limo/kogo/syntax"
)

// Workers (doc/work.md, "Workers"): a sketch (DEV-1141), not in
// spec.md. A job and an effect each name top-level funs by their
// part's word, and a work list names the queues a worker serves and
// the fun that alerts.

// A Queue is a job or an effect as the checker read it.
type Queue struct {
	Env      Type             // the environment every fun of the queue takes
	Row      Type             // the row its steps take: what next gives, or the data class read from an effect's table
	Funcs    map[string]*Func // by its part's word
	Prepared Type             // an effect's: what prepare gives and act takes
	Result   Type             // an effect's: the R of act's Act<R>, which record takes
	Attempts int              // an effect's; 0 for unlimited
	Soonest  string           // an effect's column, or ""
	// Select reads an effect's row from its table: built from the
	// row's properties, and checked by Postgres.
	Select       *Query
	Serializable bool   // a job's transaction, as next's first parameter says
	Pkg          string // the package that declares it
}

// A WorkList is a work list as the checker read it.
type WorkList struct {
	Env    Type
	Alert  *Func
	Queues []*syntax.Queue
}

// RunnerColumns are the columns an effect's table has for the runner
// (doc/work.md, "Owed is the runner's rule"), with the Kogo type of
// each: the same list as prelude/kogo/work's Columns.
var RunnerColumns = [][2]string{{"id", ""}, {"attempts", "Int"}, {"next_attempt_at", "Instant"}, {"claimed_at", "Instant"},
	{"last_error", "String"}, {"failed", "Boolean"}, {"done_at", "Instant"}, {"not_after", "Instant"}}

// The words of an effect and of a job, in the order they run.
var (
	effectWords = []string{"soonest", "attempts", "prepare", "act", "record", "failed", "recovery"}
	jobWords    = []string{"next", "run", "failed", "recovery"}
)

// works checks the job and effect forms and the work lists.
func (c *checker) works(program []*syntax.File) {
	queues := map[string]*syntax.Queue{}
	for _, f := range program {
		c.enter(f, c.pkgOf(f.Package.Name))
		for _, d := range f.Queues {
			if queues[d.Name] != nil {
				c.errorf(d, "duplicate", "queue %s is already declared: a work list names a queue by its name alone, so rename one", d.Name)
				continue
			}
			queues[d.Name] = d
			if c.pkgs["kogo.work"] == nil {
				c.errorf(d, "work", "the prelude has no kogo.work")
				continue
			}
			c.info.Queues[d] = c.queue(d)
		}
	}
	listed := map[*syntax.Queue]bool{}
	for _, f := range program {
		c.enter(f, c.pkgOf(f.Package.Name))
		for _, d := range f.Works {
			l := &WorkList{}
			c.info.Works[d] = l
			if d.Alert == nil {
				c.errorf(d, "no-alert", "work %s names no alert: write work %s alertedBy <alert>, an action fun that takes the environment and an Alert and tells someone a row failed for good; one that wants silence names a fun that does nothing", d.Name, d.Name)
			} else if f := c.part(d.Alert, "alert", "a work list's alert", "env: Env, alert: Alert): Either<E, Unit>", "env", "Alert"); f != nil {
				if !f.Action {
					c.errorf(d.Alert, "alert", "%s is a fun, and a work list's alert is an action fun: it runs after the commit that failed the row, with no transaction", d.Alert.Name)
				}
				if e := c.either(f.Result); e == nil || !is(f.Result.(*Named).Args[1], tUnit) {
					c.errorf(d.Alert, "alert", "%s returns %s: a work list's alert returns Either<E, Unit>, its own failure or nothing, which the runner logs", d.Alert.Name, f.Result)
				}
				l.Alert, l.Env = f, f.Params[0].Type
			}
			for _, id := range d.Queues {
				q := queues[id.Name]
				switch info := c.info.Queues[q]; {
				case q == nil:
					c.errorf(id, "work", "%s is no job or effect: a work list names queues, each declared as job %s { ... } or effect %s on <table> { ... }", id.Name, id.Name, id.Name)
				case listed[q] && slices.Contains(l.Queues, q):
					c.errorf(id, "duplicate", "%s is already in work %s: a queue has one worker, so name it once", id.Name, d.Name)
				case info != nil:
					listed[q] = true
					l.Queues = append(l.Queues, q)
					if l.Env == nil {
						l.Env = info.Env
					} else if info.Env != nil && !same(info.Env, l.Env) {
						c.errorf(id, "work", "%s's funs take the environment %s, and work %s's take %s: a list's queues and its alert take one environment type", id.Name, info.Env, d.Name, l.Env)
					}
				}
			}
		}
	}
	for _, f := range program {
		c.enter(f, c.pkgOf(f.Package.Name))
		for _, d := range f.Queues {
			if !listed[d] && queues[d.Name] == d && c.info.Queues[d] != nil {
				c.errorf(d, "unserved", "%s is in no work list, so no worker would serve its rows: add it to a list, as work daemon alertedBy <alert> { %s }", d.Name, d.Name)
			}
		}
	}
}

// part finds the one top-level fun that a job, an effect or a work
// list names at id for word, and checks its parameters: kinds are tx,
// env, a type of kogo.work by its name, or "" for any type. It gives
// nil where it reports an error.
func (c *checker) part(id *syntax.Ident, word, what, as string, kinds ...string) *Func {
	var fs []*Func
	for _, f := range c.funcs(id.Name) {
		if f.Kind == Fun && f.Receiver == nil {
			fs = append(fs, f)
		}
	}
	if len(fs) != 1 {
		c.errorf(id, word, "%s is not one top-level fun in scope (%d are): %s is a fun declared in a file, with no receiver, as fun %s(%s", id.Name, len(fs), what, id.Name, as)
		return nil
	}
	f, errs := fs[0], len(c.errs)
	c.info.Uses[id] = f
	if len(f.TypeParams) > 0 {
		c.errorf(id, word, "%s has type parameters: %s has none, so write its types out", id.Name, what)
	}
	if len(f.Params) != len(kinds) {
		c.errorf(id, word, "%s takes %d parameters: %s takes %d, as fun %s(%s", id.Name, len(f.Params), what, len(kinds), id.Name, as)
		return nil
	}
	for i, kind := range kinds {
		t := f.Params[i].Type
		d := named(t)
		switch {
		case t == unknown || kind == "":
		case kind == "tx" && !c.isTx(t):
			c.errorf(id, word, "%s takes a %s as parameter %d: %s takes a Tx there, the transaction the runner calls it in, as fun %s(%s", id.Name, t, i+1, what, id.Name, as)
		case kind == "env" && (d == nil || d.Kind != "data class"):
			c.errorf(id, word, "%s takes a %s as parameter %d: %s takes the environment there, a data class of the application's, as fun %s(%s", id.Name, t, i+1, what, id.Name, as)
		case kind != "tx" && kind != "env" && (d == nil || d.Pkg != "kogo.work" || d.Name != kind):
			c.errorf(id, word, "%s takes a %s as parameter %d: %s takes a %s there, with import kogo.work.%s, as fun %s(%s", id.Name, t, i+1, what, kind, kind, id.Name, as)
		}
	}
	if len(c.errs) > errs {
		return nil
	}
	return f
}

// queue checks a job or an effect: its words and their order, each
// fun's shape, and that what one step gives is what the next takes.
func (c *checker) queue(d *syntax.Queue) *Queue {
	q := &Queue{Funcs: map[string]*Func{}, Pkg: c.pkg.name}
	kind, words := "job", jobWords
	if d.Effect {
		kind, words = "effect", effectWords
	}
	parts, last, errs := map[string]*syntax.Part{}, -1, len(c.errs)
	for _, p := range d.Parts {
		i := slices.Index(words, p.Word)
		switch {
		case i < 0:
			c.errorf(p, "part", "%s is no part of %s %s: its parts are %s, one a line", p.Word, article(kind), kind, strings.Join(words, ", "))
		case parts[p.Word] != nil:
			c.errorf(p, "part", "%s %s states %s twice: state it once", kind, d.Name, p.Word)
		case i < last:
			c.errorf(p, "part-order", "%s comes before %s: %s %s's parts stand in the order they run, %s", p.Word, words[last], article(kind), kind, strings.Join(words, ", "))
		default:
			last = i
		}
		parts[p.Word] = p
	}
	need := func(word, why string) {
		if parts[word] == nil {
			c.errorf(d, "part", "%s %s states no %s: %s", kind, d.Name, word, why)
		}
	}
	if d.Effect {
		need("attempts", "every effect says how many calls of act a row may have, as attempts 1, or attempts unlimited; there is no default")
		need("prepare", "it reads the row and gives what act takes, or refuses")
		need("act", "it is the effect's one outside write")
		need("record", "it writes the result of act")
	} else {
		need("next", "it claims one owed row, or none")
		need("run", "it does the row's work in next's transaction")
		need("failed", "it marks a row whose run panicked so that next no longer finds it; without it the row is claimed again on every pass")
	}
	if len(c.errs) > errs {
		return nil
	}
	env := func(id *syntax.Ident, f *Func, at int) {
		if t := f.Params[at].Type; q.Env == nil {
			q.Env = t
		} else if !same(t, q.Env) {
			c.errorf(id, "part", "%s takes the environment %s, and %s %s's other funs take %s: the funs of one queue take one environment type", id.Name, t, kind, d.Name, q.Env)
		}
	}
	// row holds each step to the one row type.
	row := func(id *syntax.Ident, t Type) {
		if q.Row == nil {
			q.Row = t
		} else if !same(t, q.Row) {
			c.errorf(id, "part", "%s takes the row as a %s, and %s %s's other steps as a %s: the steps of one queue take one row type", id.Name, t, kind, d.Name, q.Row)
		}
	}
	fun := func(word, what, as string, kinds ...string) *Func {
		p := parts[word]
		if p == nil {
			return nil
		}
		f := c.part(p.Name, word, what, as, kinds...)
		if f != nil {
			q.Funcs[word] = f
			env(p.Name, f, slices.Index(kinds, "env"))
		}
		return f
	}
	gives := func(word string, f *Func, want Type, as string) {
		if f != nil && !same(f.Result, want) {
			c.errorf(parts[word].Name, word, "%s returns %s: %s returns %s", parts[word].Name.Name, f.Result, as, want)
		}
	}
	words2 := nullable(tString)
	if !d.Effect {
		if f := fun("next", "a job's next", "tx: Tx, env: Env): Row?", "tx", "env"); f != nil {
			if _, null := f.Result.(*Nullable); !null {
				c.errorf(parts["next"].Name, "next", "%s returns %s: a job's next returns its row or null, as Row?, with null where no row is owed", f.Name, f.Result)
			}
			q.Row, q.Serializable = nonNull(f.Result), c.serializable(f.Params[0].Type)
		}
		if f := fun("run", "a job's run", "tx: Tx, env: Env, row: Row)", "tx", "env", ""); f != nil {
			row(parts["run"].Name, f.Params[2].Type)
			gives("run", f, tUnit, "a job's run")
		}
		if f := fun("failed", "a job's failed", "tx: Tx, env: Env, row: Row, why: String)", "tx", "env", "", ""); f != nil {
			row(parts["failed"].Name, f.Params[2].Type)
			if !is(f.Params[3].Type, tString) {
				c.errorf(parts["failed"].Name, "failed", "%s takes a %s fourth: a job's failed takes why its run panicked, a String", f.Name, f.Params[3].Type)
			}
			gives("failed", f, tUnit, "a job's failed")
		}
		if f := fun("recovery", "a job's recovery", "tx: Tx, env: Env, row: Row, why: String): String?", "tx", "env", "", ""); f != nil {
			row(parts["recovery"].Name, f.Params[2].Type)
			gives("recovery", f, words2, "a job's recovery")
		}
	} else {
		q.Attempts = parts["attempts"].Attempts
		if f := fun("prepare", "an effect's prepare", "tx: Tx, env: Env, row: Row): Either<String, Prepared>", "tx", "env", ""); f != nil {
			row(parts["prepare"].Name, f.Params[2].Type)
			if e := c.either(f.Result); e == nil || !is(e, tString) {
				c.errorf(parts["prepare"].Name, "prepare", "%s returns %s: an effect's prepare returns Either<String, Prepared>, a refusal in words or what act takes", f.Name, f.Result)
			} else {
				q.Prepared = f.Result.(*Named).Args[1]
			}
		}
		if f := fun("act", "an effect's act", "env: Env, key: EffectKey, prepared: Prepared): Act<Result>", "env", "EffectKey", ""); f != nil {
			id := parts["act"].Name
			if !f.Action {
				c.errorf(id, "act", "%s is a fun, and an effect's act is an action fun: it is the one place a worker acts, with no transaction open", f.Name)
			}
			if q.Prepared != nil && !same(f.Params[2].Type, q.Prepared) {
				c.errorf(id, "act", "%s takes a %s third, and prepare gives a %s: act takes what prepare gives", f.Name, f.Params[2].Type, q.Prepared)
			}
			if n := named(f.Result); n == nil || n.Pkg != "kogo.work" || n.Name != "Act" {
				c.errorf(id, "act", "%s returns %s: an effect's act returns Act<Result>, done with its result, refused, not made or unknown, with import kogo.work.Act", f.Name, f.Result)
			} else {
				q.Result = f.Result.(*Named).Args[0]
			}
		}
		if f := fun("record", "an effect's record", "tx: Tx, env: Env, row: Row, result: Result): Either<String, Unit>", "tx", "env", "", ""); f != nil {
			id := parts["record"].Name
			row(id, f.Params[2].Type)
			if q.Result != nil && !same(f.Params[3].Type, q.Result) {
				c.errorf(id, "record", "%s takes a %s fourth, and act answers an Act<%s>: record takes what act answers", f.Name, f.Params[3].Type, q.Result)
			}
			gives("record", f, &Named{c.prelude("Either"), []Type{tString, tUnit}}, "an effect's record")
		}
		if f := fun("failed", "an effect's failed", "tx: Tx, env: Env, row: Row, why: Failure)", "tx", "env", "", "Failure"); f != nil {
			row(parts["failed"].Name, f.Params[2].Type)
			gives("failed", f, tUnit, "an effect's failed")
		}
		if f := fun("recovery", "an effect's recovery", "tx: Tx, env: Env, row: Row, why: Failure): String?", "tx", "env", "", "Failure"); f != nil {
			row(parts["recovery"].Name, f.Params[2].Type)
			gives("recovery", f, words2, "an effect's recovery")
		}
		if s := parts["soonest"]; s != nil {
			q.Soonest = s.Name.Name
		}
		if len(c.errs) == errs {
			c.table(d, q, parts["soonest"])
		}
	}
	if len(c.errs) > errs {
		return nil
	}
	return q
}

func article(kind string) string {
	if kind == "effect" {
		return "an"
	}
	return "a"
}

// table checks an effect's table by Postgres's answer: it has the
// runner's columns, soonest names a timestamptz of it, and the row's
// properties are its columns. Each is a statement the SQL pass
// prepares and never runs.
func (c *checker) table(d *syntax.Queue, q *Queue, soonest *syntax.Part) {
	table := d.Table.Name
	// asked gives the signature of a select of cols from the table, or
	// nil with the fault reported at at.
	asked := func(at syntax.Node, cols, what string) (*syntax.SQL, *sqlpass.Statement) {
		lit := &syntax.SQL{Pos: at.At(), Call: "querySingle", Text: "select " + cols + " from " + table, TextPos: at.At()}
		if c.signer == nil {
			c.errorf(at, "sql-check", "there is no database to check effect %s's table against", d.Name)
			return nil, nil
		}
		st, err := c.signer(lit)
		var fault *sqlpass.Error
		if errors.As(err, &fault) {
			c.errorf(at, "effect-table", "%s: Postgres refuses select %s from %s: %s", what, cols, table, fault.Message)
			return nil, nil
		} else if err != nil {
			c.errorf(at, "sql-check", "effect %s's table could not be checked: %v", d.Name, err)
			return nil, nil
		}
		return lit, st
	}
	var names []string
	for _, col := range RunnerColumns {
		names = append(names, col[0])
	}
	if _, st := asked(d.Table, strings.Join(names, ", "), fmt.Sprintf("an effect's table has the runner's columns, %s", strings.Join(names, ", "))); st != nil {
		for i, col := range st.Columns {
			if want := RunnerColumns[i][1]; want != "" && (col.Type.Name != want || col.Type.List) {
				c.errorf(d.Table, "effect-table", "%s.%s is a %s, and the runner's %s is a %s", table, col.Name, col.Type, col.Name, want)
			}
		}
	}
	if soonest != nil {
		if _, st := asked(soonest.Name, soonest.Name.Name, "soonest names a column of the effect's table"); st != nil && (st.Columns[0].Type.Name != "Instant" || st.Columns[0].Type.List) {
			c.errorf(soonest.Name, "soonest", "%s.%s is a %s, and soonest names a timestamptz: rows with a time there go first, the soonest first", table, soonest.Name.Name, st.Columns[0].Type)
		}
	}
	rowAt := syntax.Node(d.Table)
	cls := dataClass(q.Row)
	if cls == nil || q.Row != nonNull(q.Row) {
		c.errorf(rowAt, "effect-row", "effect %s's steps take the row as a %s: the row is a data class the application declares, whose properties are columns of %s, which the runner reads by id and locks", d.Name, q.Row, table)
		return
	}
	var cols []string
	for _, p := range cls.Decl.Props {
		cols = append(cols, Snake(p.Name))
	}
	if lit, st := asked(rowAt, strings.Join(cols, ", "), fmt.Sprintf("the properties of %s, the row effect %s's steps take, are columns of its table", q.Row, d.Name)); st != nil {
		q.Select = &Query{Stmt: st, Row: q.Row, Args: []syntax.Expr{}}
		c.row(lit, q.Select)
	}
}
