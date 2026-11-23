// Package work is the runner of doc/work.md, "Workers": a sketch
// (DEV-1141), not for merging. A queue is a job or an effect, a work
// list names the queues a worker serves and the function that alerts,
// and each queue is served on its own, a row at a time. The emitter
// writes a queue's entry from its job or effect form, and this Go
// calls the application's functions in order.
//
// It reads the time only from the clock it is handed, and its SQL
// takes the time as a parameter and never says now().
package work

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Cloud-Limo/kogo/prelude/kogo"
	ktime "github.com/Cloud-Limo/kogo/prelude/kogo/time"
	"github.com/Cloud-Limo/kogo/rt"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The types of prelude/work.kg, written here as the emitter would
// write them.

// An EffectKey is the key of one effect's row, <table>-<id>. Only the
// runner makes one.
type EffectKey struct{ text string }

func EffectKey_Text(this EffectKey) string { return this.text }

type Act[R any] interface{}

type Act_Done[R any] struct {
	_      struct{} `kogo:"Done"`
	Result R        `kogo:"result"`
}

type Act_Refused struct {
	_   struct{} `kogo:"Refused"`
	Why string   `kogo:"why"`
}

type Act_NotMade struct {
	_   struct{} `kogo:"NotMade"`
	Why string   `kogo:"why"`
}

type Act_Unknown struct {
	_   struct{} `kogo:"Unknown"`
	Why string   `kogo:"why"`
}

type Failure interface{}

type Failure_Refused struct {
	_   struct{} `kogo:"Refused"`
	Why string   `kogo:"why"`
}

type Failure_NotMade struct {
	_   struct{} `kogo:"NotMade"`
	Why string   `kogo:"why"`
}

type Failure_Exhausted struct {
	_   struct{} `kogo:"Exhausted"`
	Why string   `kogo:"why"`
}

type Failure_Unrecorded struct {
	_   struct{} `kogo:"Unrecorded"`
	Why string   `kogo:"why"`
}

// An Alert is what the runner knows of a row that failed for good.
type Alert struct {
	_        struct{} `kogo:"Alert"`
	Queue    string   `kogo:"queue"`
	Table    *string  `kogo:"table"` // null for a job
	Id       *string  `kogo:"id"`    // null for a job
	Row      string   `kogo:"row"`   // the row's data class as text
	Cause    string   `kogo:"cause"`
	Recovery *string  `kogo:"recovery"`
	Reset    *string  `kogo:"reset"` // null for a job
}

// The runner's constants (decisions.md, "A queue's lease is a constant
// of the runner").
const (
	Lease  = 2 * time.Minute // after which a claimed row is taken for abandoned
	Replay = 20 * time.Hour  // after which an abandoned row is parked, not replayed
)

// backOff gives how long a row waits after its nth attempt gave
// Act.Unknown: 1, 2, 4 and 8 minutes, then every 10.
func backOff(attempts int) time.Duration {
	if attempts >= 1 && attempts <= 4 {
		return time.Duration(1<<(attempts-1)) * time.Minute
	}
	return 10 * time.Minute
}

// Columns are the runner's columns, which an effect's table has: the
// one list, read by the claim here and by the checker's check of each
// effect's table.
var Columns = []string{"id", "attempts", "next_attempt_at", "claimed_at", "last_error", "failed", "done_at", "not_after"}

// An Outcome is what serving one row came to.
type Outcome string

const (
	Idle       Outcome = "idle"       // no row was owed
	Done       Outcome = "done"       // an effect recorded, or a job ran
	Retry      Outcome = "retry"      // Act.Unknown: backed off, to be offered again
	Refused    Outcome = "refused"    // failed, and nobody alerted
	NotMade    Outcome = "not made"   // failed and alerted
	Exhausted  Outcome = "exhausted"  // failed and alerted
	Unrecorded Outcome = "unrecorded" // failed and alerted
	Failed     Outcome = "failed"     // a job's row, failed and alerted
	Stopped    Outcome = "stopped"    // a test's stand-in for a crash after act
	Broken     Outcome = "broken"     // a panic with no row to fail: logged
)

// A Queue is one job or effect as the runner serves it.
type Queue[Env any] struct {
	Name  string
	Table string // an effect's; "" for a job
	pass  func(r *Runner[Env], ctx context.Context) []Outcome
}

// A List is a work list: its queues and the application's alert.
type List[Env any] struct {
	Name   string
	alert  func(Env, Alert) (failure any, failed bool)
	Queues []Queue[Env]
}

// NewList is a work list, work name alertedBy alert { queues }.
func NewList[Env, E any](name string, alert func(Env, Alert) kogo.Either[E, rt.Unit], queues ...Queue[Env]) List[Env] {
	return List[Env]{name, func(env Env, a Alert) (any, bool) {
		left, failed := alert(env, a).(kogo.Either_Left[E])
		return left.Value, failed
	}, queues}
}

// A Runner serves a list's queues.
type Runner[Env any] struct {
	Pool  *pgxpool.Pool
	Clock ktime.Clock
	Env   Env
	List  List[Env]
	Log   *slog.Logger
	// Wait waits between passes that found no row; a test hands its
	// own. The clock of kogo.time tells the time and cannot wait.
	Wait func(context.Context, time.Duration)
	// StopAfterAct stands in for a process that dies between act and
	// record: the row stays claimed.
	StopAfterAct bool
}

func (r *Runner[Env]) now() time.Time {
	i := ktime.Clock_Now(r.Clock)
	return time.Unix(i.Seconds, int64(i.Nanos)).UTC()
}

// Pass serves the named queue once: one row, or with over one row for
// each value. It is the entry a test steps a queue by.
func (r *Runner[Env]) Pass(ctx context.Context, queue string) []Outcome {
	for _, q := range r.List.Queues {
		if q.Name == queue {
			return q.pass(r, ctx)
		}
	}
	panic("work list " + r.List.Name + " has no queue " + queue)
}

// Run serves every queue, each in a loop of its own, until ctx ends: a
// row in flight then runs to its record.
func (r *Runner[Env]) Run(ctx context.Context) {
	wait := r.Wait
	if wait == nil {
		wait = func(ctx context.Context, d time.Duration) {
			select {
			case <-ctx.Done():
			case <-time.After(d):
			}
		}
	}
	var loops sync.WaitGroup
	for _, q := range r.List.Queues {
		loops.Go(func() {
			idle := 10 * time.Millisecond
			for ctx.Err() == nil {
				busy := false
				for _, o := range q.pass(r, context.WithoutCancel(ctx)) {
					if o == Broken {
						idle = 10 * time.Second
					} else if o != Idle {
						busy = true
					}
				}
				if busy {
					idle = 10 * time.Millisecond
					continue
				}
				wait(ctx, idle)
				idle = min(2*idle, 200*time.Millisecond)
			}
		})
	}
	loops.Wait()
}

// tx runs block in a transaction of the runner's, and again for as
// long as Postgres ends it with a deadlock or a serialization failure.
func tx[Env, A any](r *Runner[Env], ctx context.Context, serializable bool, block func(kogo.Tx) A) A {
	return kogo.InTx(kogo.NewDb(ctx, r.Pool), serializable, func(conflict *pgconn.PgError) bool {
		r.Log.Info("work: a transaction is run again", "code", conflict.Code)
		return true
	}, block)
}

// guarded runs block in a savepoint and gives what it panicked with,
// rolled back, or nil. A conflict is not caught: its transaction is
// run again.
func guarded[A any](t kogo.Tx, block func() A) (a A, panicked any) {
	defer func() {
		if p := recover(); p != nil {
			var conflict *pgconn.PgError
			if err, ok := p.(error); ok && errors.As(err, &conflict) && (conflict.Code == "40001" || conflict.Code == "40P01") {
				panic(p)
			}
			panicked = p
		}
	}()
	return kogo.Tx_Atomically(t, block), nil
}

// A left is a step's Left on its way out of its savepoint.
type left struct{ why string }

// step runs a step that gives an Either in a savepoint, which a Left
// rolls back as a panic does: what a step wrote before it gave up is
// not kept.
func step[A any](t kogo.Tx, block func() kogo.Either[string, A]) (a A, refused *string, panicked any) {
	a, panicked = guarded(t, func() A {
		switch e := block().(type) {
		case kogo.Either_Right[A]:
			return e.Value
		case kogo.Either_Left[string]:
			panic(left{e.Value})
		default:
			panic(fmt.Sprintf("a step gave a %T, which is no Either", e))
		}
	})
	if l, ok := panicked.(left); ok {
		return a, &l.why, nil
	}
	return a, nil, panicked
}

// caught runs block and gives what it panicked with, or nil.
func caught(block func()) (panicked any) {
	defer func() { panicked = recover() }()
	block()
	return nil
}

// values gives what a queue serves in turn: over's values, or the one
// value of a queue that names none.
func values[Env, V any](r *Runner[Env], ctx context.Context, name string, over func(kogo.Tx, Env) kogo.List[V]) (vs []V, broken bool) {
	if over == nil {
		return make([]V, 1), false
	}
	if p := caught(func() { vs = tx(r, ctx, false, func(t kogo.Tx) []V { return over(t, r.Env) }) }); p != nil {
		r.Log.Error("work: over panicked", "queue", name, "panic", fmt.Sprint(p))
		return nil, true
	}
	return vs, false
}

// tell calls the application's alert, best effort: a failure or a
// panic of its own is logged.
func (r *Runner[Env]) tell(a *Alert) {
	if a == nil {
		return
	}
	var failure any
	var failed bool
	if p := caught(func() { failure, failed = r.List.alert(r.Env, *a) }); p != nil {
		r.Log.Error("work: the alert panicked", "queue", a.Queue, "row", a.Row, "panic", fmt.Sprint(p))
	} else if failed {
		r.Log.Error("work: the alert failed", "queue", a.Queue, "row", a.Row, "failure", rt.ToString(failure))
	}
}

// A Job is a job's form: next claims one owed row or none, run does it
// in that transaction, and failed marks a row whose run panicked, in a
// transaction of its own, so that next no longer finds it.
type Job[Env, V, Row any] struct {
	Name         string
	Serializable bool                                    // as next's first parameter says
	Over         func(kogo.Tx, Env) kogo.List[V]         // nil where the form names none
	Enter        func(kogo.Tx, Env, V) rt.Unit           // nil where the form names none
	Next         func(kogo.Tx, Env) *Row                 // required
	Run          func(kogo.Tx, Env, Row) rt.Unit         // required
	Failed       func(kogo.Tx, Env, Row, string) rt.Unit // required
	Recovery     func(kogo.Tx, Env, Row, string) *string // nil where the form names none
}

func (j Job[Env, V, Row]) Queue() Queue[Env] {
	return Queue[Env]{Name: j.Name, pass: func(r *Runner[Env], ctx context.Context) []Outcome {
		vs, broken := values(r, ctx, j.Name, j.Over)
		if broken {
			return []Outcome{Broken}
		}
		outs := make([]Outcome, len(vs))
		for i, v := range vs {
			outs[i] = j.serve(r, ctx, v)
		}
		return outs
	}}
}

func (j Job[Env, V, Row]) serve(r *Runner[Env], ctx context.Context, v V) Outcome {
	var row *Row
	panicked := caught(func() {
		tx(r, ctx, j.Serializable, func(t kogo.Tx) rt.Unit {
			if row = nil; j.Enter != nil {
				j.Enter(t, r.Env, v)
			}
			if row = j.Next(t, r.Env); row != nil {
				j.Run(t, r.Env, *row)
			}
			return rt.Unit{}
		})
	})
	switch {
	case panicked == nil && row == nil:
		return Idle
	case panicked == nil:
		return Done
	case row == nil: // enter or next panicked: there is no row to fail
		r.Log.Error("work: a job panicked before it had a row", "queue", j.Name, "panic", fmt.Sprint(panicked))
		return Broken
	}
	why := fmt.Sprint(panicked)
	r.Log.Error("work: a job's run panicked", "queue", j.Name, "row", rt.ToString(*row), "panic", why)
	var words *string
	if p := caught(func() {
		tx(r, ctx, j.Serializable, func(t kogo.Tx) rt.Unit {
			if j.Enter != nil {
				j.Enter(t, r.Env, v)
			}
			if j.Failed(t, r.Env, *row, why); j.Recovery != nil {
				words = j.Recovery(t, r.Env, *row, why)
			}
			return rt.Unit{}
		})
	}); p != nil {
		r.Log.Error("work: a job's failed panicked, and its row is still owed", "queue", j.Name, "row", rt.ToString(*row), "panic", fmt.Sprint(p))
		return Broken
	}
	r.tell(&Alert{Queue: j.Name, Row: rt.ToString(*row), Cause: why, Recovery: words})
	return Failed
}

// An Effect is an effect's form: one outside write for one row of
// Table, in three steps. Select is the statement that reads the row's
// data class from the table, which the compiler builds from the
// class's properties and Postgres checks, and Read reads its row.
type Effect[Env, V, Row, P, R any] struct {
	Name     string
	Table    string
	Soonest  string // a column, or "" for the oldest next_attempt_at first
	Attempts int    // the most calls of act for a row; 0 for no limit
	Select   string
	Read     func(kogo.Scan) Row
	Over     func(kogo.Tx, Env) kogo.List[V]                         // nil where the form names none
	Enter    func(kogo.Tx, Env, V) rt.Unit                           // nil where the form names none
	Prepare  func(kogo.Tx, Env, Row) kogo.Either[string, P]          // required
	Act      func(Env, EffectKey, P) Act[R]                          // required
	Record   func(kogo.Tx, Env, Row, R) kogo.Either[string, rt.Unit] // required
	Failed   func(kogo.Tx, Env, Row, Failure) rt.Unit                // nil where the form names none
	Recovery func(kogo.Tx, Env, Row, Failure) *string                // nil where the form names none
}

func (e Effect[Env, V, Row, P, R]) Queue() Queue[Env] {
	return Queue[Env]{Name: e.Name, Table: e.Table, pass: func(r *Runner[Env], ctx context.Context) []Outcome {
		vs, broken := values(r, ctx, e.Name, e.Over)
		if broken {
			return []Outcome{Broken}
		}
		outs := make([]Outcome, len(vs))
		for i, v := range vs {
			if p := caught(func() { outs[i] = e.serve(r, ctx, v) }); p != nil {
				r.Log.Error("work: an effect's row could not be served", "queue", e.Name, "panic", fmt.Sprint(p))
				outs[i] = Broken
			}
		}
		return outs
	}}
}

// held is a row between its transactions.
type held[Row, P any] struct {
	*claimed
	tries    int // the row's attempts, counting this one
	prepared P
}

// claim finds the row the queue owes soonest, locked: not done, not
// failed, not past its not_after, due, and either unclaimed or
// abandoned within the replay window.
func (e Effect[Env, V, Row, P, R]) claim(t kogo.Tx, now time.Time) (c *claimed) {
	order := "next_attempt_at, id" // id, so that rows due at one instant have one order
	if e.Soonest != "" {
		order = e.Soonest + " nulls last, " + order
	}
	return kogo.Tx_QueryOne(t, `select id, id::text as id_text, attempts from `+e.Table+`
		where done_at is null and not failed and (not_after is null or not_after > $1) and next_attempt_at <= $1
		  and (claimed_at is null or claimed_at <= $2 and claimed_at > $3)
		order by `+order+` limit 1 for update skip locked`,
		func(scan kogo.Scan) *claimed {
			c := &claimed{}
			scan(&c.id, &c.text, &c.attempts)
			return c
		}, now, now.Add(-Lease), now.Add(-Replay))
}

// A claimed row: its id as the driver reads it, by which the runner's
// statements find the row through the table's own index, and as
// Postgres writes it as text, for the key and the alert.
type claimed struct {
	id       any
	text     string
	attempts int32
}

func (e Effect[Env, V, Row, P, R]) row(t kogo.Tx, id any) Row {
	return kogo.Tx_QuerySingle(t, e.Select+" where id = $1 for update", e.Read, id)
}

// Reset is the statement that offers a failed row of table again. It
// says no time: the row was due when it failed, so it is due still.
func Reset(table, id string) string {
	return fmt.Sprintf("update %s set failed = false, attempts = 0, claimed_at = null, last_error = null where id = '%s'",
		table, strings.ReplaceAll(id, "'", "''"))
}

// fail marks the row failed, calls the queue's failed and recovery in
// a savepoint, and gives the alert to send once the transaction has
// committed: none for a refusal.
func (e Effect[Env, V, Row, P, R]) fail(r *Runner[Env], t kogo.Tx, c *claimed, row Row, why Failure, cause string, out Outcome) (Outcome, *Alert) {
	kogo.Tx_Execute(t, "update "+e.Table+" set failed = true, claimed_at = null, last_error = $2 where id = $1", c.id, cause)
	words, panicked := guarded(t, func() (words *string) {
		if e.Failed != nil {
			e.Failed(t, r.Env, row, why)
		}
		if e.Recovery != nil {
			words = e.Recovery(t, r.Env, row, why)
		}
		return words
	})
	if panicked != nil {
		cause += fmt.Sprintf("; and the queue's failed or recovery panicked, and wrote nothing: %v", panicked)
	} else if out == Refused {
		return out, nil
	}
	reset := Reset(e.Table, c.text)
	return out, &Alert{Queue: e.Name, Table: &e.Table, Id: &c.text, Row: rt.ToString(row), Cause: cause, Recovery: words, Reset: &reset}
}

func (e Effect[Env, V, Row, P, R]) enter(t kogo.Tx, env Env, v V) {
	if e.Enter != nil {
		e.Enter(t, env, v)
	}
}

func (e Effect[Env, V, Row, P, R]) serve(r *Runner[Env], ctx context.Context, v V) Outcome {
	now := r.now()
	// The first transaction: claim, prepare, and mark the row claimed.
	var h *held[Row, P]
	var out Outcome
	var alert *Alert
	tx(r, ctx, false, func(t kogo.Tx) rt.Unit {
		h, out, alert = nil, Idle, nil
		e.enter(t, r.Env, v)
		c := e.claim(t, now)
		if c == nil {
			return rt.Unit{}
		}
		// An attempt is counted at the claim, a replay's too, before
		// prepare; the row is marked claimed once prepare has given
		// what act takes.
		attempts := int(c.attempts) + 1
		kogo.Tx_Execute(t, "update "+e.Table+" set attempts = $2 where id = $1", c.id, attempts)
		row := e.row(t, c.id)
		prepared, refused, panicked := step(t, func() kogo.Either[string, P] { return e.Prepare(t, r.Env, row) })
		if panicked != nil {
			cause := fmt.Sprintf("prepare panicked: %v", panicked)
			out, alert = e.fail(r, t, c, row, Failure_NotMade{Why: cause}, cause, NotMade)
		} else if refused != nil {
			out, alert = e.fail(r, t, c, row, Failure_Refused{Why: *refused}, *refused, Refused)
		} else {
			kogo.Tx_Execute(t, "update "+e.Table+" set claimed_at = $2 where id = $1", c.id, now)
			h = &held[Row, P]{c, attempts, prepared}
		}
		return rt.Unit{}
	})
	if r.tell(alert); h == nil {
		return out
	}
	// act, with no transaction open. A panic there is taken for
	// Act.Unknown: the write may have been made.
	var result Act[R]
	start := time.Now()
	if p := caught(func() {
		result = e.Act(r.Env, EffectKey{e.Table[strings.LastIndexByte(e.Table, '.')+1:] + "-" + h.text}, h.prepared)
	}); p != nil {
		result = Act_Unknown{Why: fmt.Sprintf("act panicked: %v", p)}
	}
	r.Log.Info("work: act", "queue", e.Name, "id", h.text, "result", fmt.Sprintf("%T", result), "duration", time.Since(start))
	if r.StopAfterAct {
		return Stopped
	}
	// The second transaction: record, back off or fail.
	now = r.now()
	tx(r, ctx, false, func(t kogo.Tx) rt.Unit {
		alert = nil
		e.enter(t, r.Env, v)
		row := e.row(t, h.id)
		switch a := result.(type) {
		case Act_Done[R]:
			_, refused, panicked := step(t, func() kogo.Either[string, rt.Unit] { return e.Record(t, r.Env, row, a.Result) })
			if panicked != nil {
				cause := fmt.Sprintf("the act was made, and record panicked: %v", panicked)
				out, alert = e.fail(r, t, h.claimed, row, Failure_Unrecorded{Why: cause}, cause, Unrecorded)
			} else if refused != nil {
				out, alert = e.fail(r, t, h.claimed, row, Failure_Unrecorded{Why: *refused}, *refused, Unrecorded)
			} else {
				kogo.Tx_Execute(t, "update "+e.Table+" set done_at = $2, attempts = 0, claimed_at = null, last_error = null where id = $1", h.id, now)
				out = Done
			}
		case Act_Refused:
			out, alert = e.fail(r, t, h.claimed, row, Failure_Refused{Why: a.Why}, a.Why, Refused)
		case Act_NotMade:
			out, alert = e.fail(r, t, h.claimed, row, Failure_NotMade{Why: a.Why}, a.Why, NotMade)
		case Act_Unknown:
			if e.Attempts > 0 && h.tries >= e.Attempts {
				out, alert = e.fail(r, t, h.claimed, row, Failure_Exhausted{Why: a.Why}, a.Why, Exhausted)
			} else {
				kogo.Tx_Execute(t, "update "+e.Table+" set claimed_at = null, next_attempt_at = $2, last_error = $3 where id = $1",
					h.id, now.Add(backOff(h.tries)), a.Why)
				out = Retry
			}
		default:
			panic(fmt.Sprintf("act gave a %T, which is no Act", result))
		}
		return rt.Unit{}
	})
	r.tell(alert)
	return out
}
