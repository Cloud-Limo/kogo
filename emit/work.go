package emit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Cloud-Limo/kogo/syntax"
)

// Workers (doc/work.md, "Workers"): a sketch (DEV-1141). A job and an
// effect are each written as the work.Queue the runner serves, and a
// work list as the work.List that names them.

// queue writes a job's or an effect's entry: its name, its funs by
// their part, and for an effect its table, its settings and the
// statement and reader of the row its steps take.
func (e *emitter) queue(d *syntax.Queue) {
	defer e.enter(d)()
	e.fn = &scope{names: map[any]string{}, used: map[string]bool{"this_": true}}
	q := e.info.Queues[d]
	work := func(name string) string { return e.qualify("kogo.work", name) }
	fields := []string{fmt.Sprintf("Name: %q", d.Name)}
	if d.Effect {
		fields = append(fields, fmt.Sprintf("Table: %q, Soonest: %q, Attempts: %d", d.Table.Name, q.Soonest, q.Attempts),
			"Select: "+strconv.Quote(q.Select.Stmt.SQL), "Read: "+e.reader(q.Select, q.Row))
	} else {
		fields = append(fields, fmt.Sprintf("Serializable: %t", q.Serializable))
	}
	for _, p := range d.Parts {
		if f := q.Funcs[p.Word]; f != nil {
			fields = append(fields, export(p.Word)+": "+e.qualify(f.Pkg, e.names[f]))
		}
	}
	// A queue that names no over serves one value, of no type.
	types := []string{e.typ(q.Env), "struct{}", e.typ(q.Row)}
	kind := "Job"
	if d.Effect {
		kind, types = "Effect", append(types, e.typ(q.Prepared), e.typ(q.Result))
	}
	e.w("var %s = %s[%s]{%s}.Queue()", e.names[d], work(kind), strings.Join(types, ", "), strings.Join(fields, ", "))
}

// workList writes a work list's work.List: its name, its alert and
// its queues' entries.
func (e *emitter) workList(d *syntax.Work) {
	defer e.enter(d)()
	l := e.info.Works[d]
	args := []string{strconv.Quote(d.Name), e.qualify(l.Alert.Pkg, e.names[l.Alert])}
	for _, q := range l.Queues {
		args = append(args, e.qualify(e.info.Queues[q].Pkg, e.names[q]))
	}
	e.w("var %s = %s(%s)", e.names[d], e.qualify("kogo.work", "NewList"), strings.Join(args, ", "))
}
