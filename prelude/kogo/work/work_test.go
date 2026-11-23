package work

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cloud-Limo/kogo/internal/testdb"
	"github.com/Cloud-Limo/kogo/prelude/kogo"
	ktime "github.com/Cloud-Limo/kogo/prelude/kogo/time"
	"github.com/Cloud-Limo/kogo/rt"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The queues of these tests are written in Go as the emitter writes an
// effect's and a job's entry from examples/32_work.kg: a parcel's
// dispatch books a courier, and a job queues a new parcel's dispatch.

type env struct {
	mu     *sync.Mutex
	keys   *[]string                 // the key of each call of act, in order
	alerts *[]Alert                  // each alert, in order
	act    func(l label) Act[string] // what the courier answers
	alert  func() kogo.Either[string, rt.Unit]
}

type dispatch struct {
	_        struct{}  `kogo:"Dispatch"`
	Id       kogo.Uuid `kogo:"id"`
	ParcelId kogo.Uuid `kogo:"parcelId"`
}

type label struct {
	_       struct{} `kogo:"Label"`
	Address string   `kogo:"address"`
	Grams   int32    `kogo:"grams"`
}

func text(scan kogo.Scan) string { var s string; scan(&s); return s }

func bookCourier(attempts int) Effect[env, string, dispatch, label, string] {
	return Effect[env, string, dispatch, label, string]{
		Name: "bookCourier", Table: "parcel_dispatch", Soonest: "express_since", Attempts: attempts,
		Select: "select id, parcel_id from parcel_dispatch",
		Read:   func(scan kogo.Scan) (d dispatch) { scan(&d.Id, &d.ParcelId); return d },
		Prepare: func(t kogo.Tx, e env, row dispatch) kogo.Either[string, label] {
			l := kogo.Tx_QuerySingle(t, "select address, grams from parcel where id = $1", func(scan kogo.Scan) (l label) {
				scan(&l.Address, &l.Grams)
				return l
			}, row.ParcelId)
			if l.Address == "" {
				return kogo.Either_Left[string]{Value: "The parcel has no address."}
			}
			if l.Grams < 0 {
				panic("a parcel weighs less than nothing")
			}
			return kogo.Either_Right[label]{Value: l}
		},
		Act: func(e env, key EffectKey, l label) Act[string] {
			e.mu.Lock()
			*e.keys = append(*e.keys, EffectKey_Text(key))
			e.mu.Unlock()
			return e.act(l)
		},
		Record: func(t kogo.Tx, e env, row dispatch, tracking string) kogo.Either[string, rt.Unit] {
			kogo.Tx_Execute(t, "update parcel_dispatch set tracking = $2 where id = $1", row.Id, tracking)
			if tracking == "withdrawn" {
				return kogo.Either_Left[string]{Value: "The parcel was withdrawn while the courier was booked."}
			}
			return kogo.Either_Right[rt.Unit]{}
		},
		Failed: func(t kogo.Tx, e env, row dispatch, why Failure) rt.Unit {
			kogo.Tx_Execute(t, "update parcel_dispatch set failed_message = $2 where id = $1", row.Id, strings.TrimPrefix(fmt.Sprintf("%T", why), "work."))
			return rt.Unit{}
		},
		Recovery: func(t kogo.Tx, e env, row dispatch, why Failure) *string {
			if _, exhausted := why.(Failure_Exhausted); exhausted {
				return nil
			}
			return rt.Ptr("Ring the courier, then run the reset statement below.")
		},
	}
}

// queueParcel is the job: next claims a new parcel, run queues its
// dispatch and marks it, failed marks it so that next stops finding it.
func queueParcel(at time.Time) Job[env, string, kogo.Uuid] {
	return Job[env, string, kogo.Uuid]{
		Name: "queueParcel",
		Next: func(t kogo.Tx, e env) *kogo.Uuid {
			return kogo.Tx_QueryOne(t, "select id from parcel where status = 'new' order by id limit 1 for update skip locked",
				func(scan kogo.Scan) *kogo.Uuid { var id kogo.Uuid; scan(&id); return &id })
		},
		Run: func(t kogo.Tx, e env, id kogo.Uuid) rt.Unit {
			kogo.Tx_Execute(t, "update parcel set status = 'queued' where id = $1", id)
			kogo.Tx_Execute(t, "insert into parcel_dispatch (id, next_attempt_at, parcel_id) values ($1, $2, $1)", id, at)
			if kogo.Tx_QuerySingle(t, "select address from parcel where id = $1", text, id) == "nowhere" {
				panic("no depot serves nowhere")
			}
			return rt.Unit{}
		},
		Failed: func(t kogo.Tx, e env, id kogo.Uuid, why string) rt.Unit {
			kogo.Tx_Execute(t, "update parcel set status = 'failed' where id = $1", id)
			return rt.Unit{}
		},
	}
}

var base = time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)

func clock(at time.Time) ktime.Clock { return ktime.FixedClock(ktime.Instant{Seconds: at.Unix()}) }

// A stage is a test's database, its runner at 09:00 and what the
// queues' functions saw.
type stage struct {
	t      *testing.T
	pool   *pgxpool.Pool
	r      *Runner[env]
	keys   []string
	alerts []Alert
	answer Act[string]
	told   kogo.Either[string, rt.Unit]
	reruns strings.Builder // the SQLSTATE of each transaction run again
}

// codes keeps the code of each transaction the runner ran again.
type codes struct {
	slog.Handler
	to *strings.Builder
}

func (c codes) Enabled(context.Context, slog.Level) bool { return true }
func (c codes) Handle(_ context.Context, r slog.Record) error {
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "code" {
			c.to.WriteString(a.Value.String() + "\n")
		}
		return true
	})
	return nil
}

var ctx = context.Background()

func open(t *testing.T, attempts int, queues ...Queue[env]) *stage {
	url := os.Getenv("KOGO_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
	}
	t.Setenv("DATABASE_URL", url)
	pool, drop := testdb.Open(ctx, "../../../testdata/schema.sql")
	t.Cleanup(drop)
	s := &stage{t: t, pool: pool, answer: Act_Done[string]{Result: "T-1"}, told: kogo.Either_Right[rt.Unit]{}}
	e := env{mu: &sync.Mutex{}, keys: &s.keys, alerts: &s.alerts,
		act:   func(label) Act[string] { return s.answer },
		alert: func() kogo.Either[string, rt.Unit] { return s.told }}
	if queues == nil {
		queues = []Queue[env]{bookCourier(attempts).Queue(), queueParcel(base).Queue()}
	}
	list := NewList("depot", func(e env, a Alert) kogo.Either[string, rt.Unit] {
		e.mu.Lock()
		defer e.mu.Unlock()
		*e.alerts = append(*e.alerts, a)
		return e.alert()
	}, queues...)
	s.r = &Runner[env]{Pool: pool, Clock: clock(base), Env: e, List: list, Log: slog.New(codes{slog.NewTextHandler(io.Discard, nil), &s.reruns})}
	return s
}

func (s *stage) exec(sql string, args ...any) {
	s.t.Helper()
	if _, err := s.pool.Exec(ctx, sql, args...); err != nil {
		s.t.Fatal(err)
	}
}

func id(n int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }

// parcel adds parcel n and, where queued, its dispatch, due at 09:00.
func (s *stage) parcel(n int, address string, queued bool) {
	s.exec("insert into parcel (id, depot, address, grams) values ($1, 'north', $2, $3)", id(n), strings.TrimPrefix(address, "-"), map[bool]int{true: -1, false: 500}[strings.HasPrefix(address, "-")])
	if queued {
		s.exec("insert into parcel_dispatch (id, next_attempt_at, parcel_id) values ($1, $2, $1)", id(n), base)
	}
}

// at moves the clock to minutes past 09:00 and serves the queue once.
func (s *stage) at(minutes float64, queue string) Outcome {
	s.r.Clock = clock(base.Add(time.Duration(minutes * float64(time.Minute))))
	return s.r.Pass(ctx, queue)[0]
}

// row gives dispatch n's columns as one line.
func (s *stage) row(n int) string {
	s.t.Helper()
	var line string
	err := s.pool.QueryRow(ctx, `select concat_ws(' ', 'attempts', attempts, case when failed then 'failed' end,
		case when done_at is not null then 'done' end, case when claimed_at is not null then 'claimed' end,
		'next +' || extract(epoch from next_attempt_at - $2)::int / 60, 'tracking ' || tracking, failed_message, '(' || last_error || ')')
		from parcel_dispatch where id = $1`, id(n), base).Scan(&line)
	if err != nil {
		s.t.Fatal(err)
	}
	return line
}

func (s *stage) want(what string, got, want any) {
	s.t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		s.t.Errorf("%s:\n got %v\nwant %v", what, got, want)
	}
}

// The runner's cases for an effect, each a sentence (doc/work.md, "How
// it is tested").
func TestEffect(t *testing.T) {
	key := "parcel_dispatch-" + id(1)
	t.Run("act done, record commits: done_at is set and the queue's own result is written", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		s.want("outcome", s.at(0, "bookCourier"), Done)
		s.want("row", s.row(1), "attempts 0 done next +0 tracking T-1")
		s.want("keys", s.keys, []string{key})
		s.want("again", s.at(1, "bookCourier"), Idle)
		s.want("alerts", len(s.alerts), 0)
	})
	t.Run("act unknown: the row backs off 1, 2 and 4 minutes, is offered again under the same key, and at its third attempt is failed as exhausted with an alert", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		s.answer = Act_Unknown{Why: "the courier did not answer"}
		s.want("first", s.at(0, "bookCourier"), Retry)
		s.want("row", s.row(1), "attempts 1 next +1 (the courier did not answer)")
		s.want("too soon", s.at(0.5, "bookCourier"), Idle)
		s.want("second", s.at(1, "bookCourier"), Retry)
		s.want("row", s.row(1), "attempts 2 next +3 (the courier did not answer)")
		s.want("third", s.at(3, "bookCourier"), Exhausted)
		s.want("row", s.row(1), "attempts 3 failed next +3 Failure_Exhausted (the courier did not answer)")
		s.want("keys", s.keys, []string{key, key, key})
		s.want("alerts", len(s.alerts), 1)
		s.want("recovery", s.alerts[0].Recovery, (*string)(nil))
		s.want("failed is not offered", s.at(60, "bookCourier"), Idle)
	})
	t.Run("act unknown with no limit: the row is offered for ever, every 10 minutes after the fourth", func(t *testing.T) {
		s := open(t, 0)
		s.parcel(1, "1 High St", true)
		s.answer = Act_Unknown{Why: "the courier did not answer"}
		for i, minute := range []float64{0, 1, 3, 7, 15, 25, 35} {
			s.want(fmt.Sprint("attempt ", i+1), s.at(minute, "bookCourier"), Retry)
		}
		s.want("row", s.row(1), "attempts 7 next +45 (the courier did not answer)")
	})
	t.Run("act refused: the row is failed, the queue's failed is called and nobody is alerted", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		s.answer = Act_Refused{Why: "The courier does not go there."}
		s.want("outcome", s.at(0, "bookCourier"), Refused)
		s.want("row", s.row(1), "attempts 1 failed next +0 Failure_Refused (The courier does not go there.)")
		s.want("alerts", len(s.alerts), 0)
	})
	t.Run("act not made: the row is failed and the alert carries the queue's words and the runner's reset statement", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		s.answer = Act_NotMade{Why: "the courier's site is closed"}
		s.want("outcome", s.at(0, "bookCourier"), NotMade)
		s.want("row", s.row(1), "attempts 1 failed next +0 Failure_NotMade (the courier's site is closed)")
		s.want("alerts", len(s.alerts), 1)
		a := s.alerts[0]
		s.want("alert", fmt.Sprint(a.Queue, " ", *a.Table, " ", *a.Id, " ", a.Row, " ", a.Cause, " ", *a.Recovery),
			"bookCourier parcel_dispatch "+id(1)+" Dispatch(id="+id(1)+", parcelId="+id(1)+") the courier's site is closed Ring the courier, then run the reset statement below.")
		s.exec(*a.Reset)
		s.want("after the reset", s.row(1)[:15], "attempts 0 next")
	})
	t.Run("prepare refuses: the row is failed, act is never called and nobody is alerted", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "", true)
		s.want("outcome", s.at(0, "bookCourier"), Refused)
		s.want("row", s.row(1), "attempts 1 failed next +0 Failure_Refused (The parcel has no address.)")
		s.want("keys", len(s.keys), 0)
		s.want("alerts", len(s.alerts), 0)
	})
	t.Run("prepare panics: the row is failed as not made, and ops are alerted", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "-1 High St", true)
		s.want("outcome", s.at(0, "bookCourier"), NotMade)
		s.want("row", s.row(1), "attempts 1 failed next +0 Failure_NotMade (prepare panicked: a parcel weighs less than nothing)")
		s.want("keys", len(s.keys), 0)
		s.want("alerts", len(s.alerts), 1)
	})
	t.Run("record gives a Left: the row is failed as unrecorded with an alert, and what record wrote is rolled back", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		s.answer = Act_Done[string]{Result: "withdrawn"}
		s.want("outcome", s.at(0, "bookCourier"), Unrecorded)
		s.want("row", s.row(1), "attempts 1 failed next +0 Failure_Unrecorded (The parcel was withdrawn while the courier was booked.)")
		s.want("alerts", len(s.alerts), 1)
	})
	t.Run("the process dies after act, before record: the row is not claimed again inside the lease, and when the lease ends it is, under the same key: with attempts 1, act is called twice", func(t *testing.T) {
		s := open(t, 1)
		s.parcel(1, "1 High St", true)
		s.r.StopAfterAct = true
		s.want("dies", s.at(0, "bookCourier"), Stopped)
		s.want("row", s.row(1), "attempts 1 claimed next +0")
		s.r.StopAfterAct = false
		s.want("inside the lease", s.at(1.9, "bookCourier"), Idle)
		s.want("the lease ends", s.at(2, "bookCourier"), Done)
		s.want("row", s.row(1), "attempts 0 done next +0 tracking T-1")
		s.want("keys", s.keys, []string{key, key})
	})
	t.Run("the process dies after act, and at the replay act answers unknown: the replay was the row's second attempt of 1, and the row is exhausted", func(t *testing.T) {
		s := open(t, 1)
		s.parcel(1, "1 High St", true)
		s.r.StopAfterAct = true
		s.want("dies", s.at(0, "bookCourier"), Stopped)
		s.r.StopAfterAct = false
		s.answer = Act_Unknown{Why: "the courier did not answer"}
		s.want("the lease ends", s.at(2, "bookCourier"), Exhausted)
		s.want("row", s.row(1), "attempts 2 failed next +0 Failure_Exhausted (the courier did not answer)")
	})
	t.Run("a second worker on the queue takes a slow act's row when its lease ends: act is called twice under one key and record runs twice, the slower over the faster", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		slow, release, first := *s.r, make(chan struct{}), make(chan Outcome)
		slow.Env.act = func(label) Act[string] { <-release; return Act_Done[string]{Result: "T-slow"} }
		go func() { first <- slow.Pass(ctx, "bookCourier")[0] }()
		for s.row(1) != "attempts 1 claimed next +0" {
			time.Sleep(5 * time.Millisecond)
		}
		s.want("inside the lease", s.at(1.9, "bookCourier"), Idle)
		s.want("the second worker", s.at(2, "bookCourier"), Done)
		s.want("row", s.row(1), "attempts 0 done next +0 tracking T-1")
		close(release)
		s.want("the first worker", <-first, Done)
		s.want("row", s.row(1), "attempts 0 done next +0 tracking T-slow")
		s.want("keys", s.keys, []string{key, key})
	})
	t.Run("record deadlocks with another transaction: its transaction is run again, act is not, and the row is done", func(t *testing.T) {
		e := bookCourier(3)
		record, locked := e.Record, make(chan struct{}, 1)
		e.Record = func(t kogo.Tx, v env, row dispatch, tracking string) kogo.Either[string, rt.Unit] {
			kogo.Tx_Execute(t, "select 1 from parcel where id = $1 for update", id(1))
			select {
			case locked <- struct{}{}:
			default:
			}
			kogo.Tx_Execute(t, "select 1 from parcel where id = $1 for update", id(2))
			return record(t, v, row, tracking)
		}
		s := open(t, 3, e.Queue())
		s.parcel(1, "1 High St", true)
		s.parcel(2, "2 High St", false)
		other, err := s.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		other.Exec(ctx, "select 1 from parcel where id = $1 for update", id(2))
		done := make(chan error)
		go func() { // once record waits on parcel 2, wait on parcel 1: record's wait began first, so it is the one Postgres ends
			<-locked
			time.Sleep(300 * time.Millisecond)
			_, err := other.Exec(ctx, "select 1 from parcel where id = $1 for update", id(1))
			if err == nil {
				err = other.Commit(ctx)
			}
			done <- err
		}()
		s.want("outcome", s.at(0, "bookCourier"), Done)
		s.want("the other transaction", <-done, nil)
		s.want("row", s.row(1), "attempts 0 done next +0 tracking T-1")
		s.want("keys", s.keys, []string{key})
		s.want("reruns", s.reruns.String(), "40P01\n")
	})
	t.Run("a row claimed more than 20 hours ago is parked: never replayed, never failed, and nobody is told", func(t *testing.T) {
		s := open(t, 1)
		s.parcel(1, "1 High St", true)
		s.r.StopAfterAct = true
		s.want("dies", s.at(0, "bookCourier"), Stopped)
		s.r.StopAfterAct = false
		s.want("after the window", s.at(20*60, "bookCourier"), Idle)
		s.want("row", s.row(1), "attempts 1 claimed next +0")
		s.want("alerts", len(s.alerts), 0)
	})
	t.Run("a row past its not_after is never acted on, and stays neither done nor failed", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		s.exec("update parcel_dispatch set not_after = $1", base.Add(time.Minute))
		s.want("outcome", s.at(1, "bookCourier"), Idle)
		s.want("row", s.row(1), "attempts 0 next +0")
		s.want("keys", len(s.keys), 0)
	})
	t.Run("soonest: a row with a time in the queue's column goes before an older row with none", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		s.parcel(2, "2 High St", true)
		s.exec("update parcel_dispatch set next_attempt_at = $2 where id = $1", id(1), base.Add(-time.Hour))
		s.exec("update parcel_dispatch set express_since = $2 where id = $1", id(2), base)
		s.at(0, "bookCourier")
		s.at(0, "bookCourier")
		s.want("keys", s.keys, []string{"parcel_dispatch-" + id(2), "parcel_dispatch-" + id(1)})
	})
	t.Run("the alert answers a failure, or panics: the row is failed all the same", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		s.parcel(2, "2 High St", true)
		s.answer = Act_NotMade{Why: "the courier's site is closed"}
		s.told = kogo.Either_Left[string]{Value: "the mail server is down"}
		s.want("outcome", s.at(0, "bookCourier"), NotMade)
		s.r.List.alert = func(env, Alert) (any, bool) { panic("no operators") }
		s.want("outcome", s.at(0, "bookCourier"), NotMade)
		s.want("row", s.row(2)[:17], "attempts 1 failed")
	})
	t.Run("the queue's failed panics: the row is failed all the same, and the alert says so", func(t *testing.T) {
		e := bookCourier(3)
		e.Failed = func(kogo.Tx, env, dispatch, Failure) rt.Unit { panic("failed has a bug") }
		s := open(t, 3, e.Queue())
		s.parcel(1, "", true)
		s.want("outcome", s.at(0, "bookCourier"), Refused)
		s.want("row", s.row(1)[:25], "attempts 1 failed next +0")
		s.want("alerts", len(s.alerts), 1)
	})
	t.Run("act panics: taken for unknown, so the row backs off", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", true)
		s.r.Env.act = func(label) Act[string] { panic("a nil in the client") }
		s.want("outcome", s.at(0, "bookCourier"), Retry)
		s.want("row", s.row(1), "attempts 1 next +1 (act panicked: a nil in the client)")
	})
}

// The runner's cases for a job.
func TestJob(t *testing.T) {
	t.Run("run commits: the parcel is queued with its dispatch, and next finds no more", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "1 High St", false)
		s.want("outcome", s.at(0, "queueParcel"), Done)
		s.want("row", s.row(1), "attempts 0 next +0")
		s.want("again", s.at(0, "queueParcel"), Idle)
	})
	t.Run("run panics: its writes are rolled back, failed marks the row in a transaction of its own, ops are alerted with the row, and next no longer finds it", func(t *testing.T) {
		s := open(t, 3)
		s.parcel(1, "nowhere", false)
		s.want("outcome", s.at(0, "queueParcel"), Failed)
		var status string
		var dispatches int
		s.pool.QueryRow(ctx, "select status, (select count(*) from parcel_dispatch) from parcel").Scan(&status, &dispatches)
		s.want("status", status, "failed")
		s.want("dispatches", dispatches, 0)
		s.want("alerts", len(s.alerts), 1)
		s.want("alert", fmt.Sprint(s.alerts[0].Queue, " ", s.alerts[0].Table, " ", s.alerts[0].Row, " ", s.alerts[0].Cause), "queueParcel <nil> "+id(1)+" no depot serves nowhere")
		s.want("again", s.at(0, "queueParcel"), Idle)
	})
	t.Run("failed does not stop next finding the row: the job is claimed, fails and alerts on every pass", func(t *testing.T) {
		j := queueParcel(base)
		j.Failed = func(kogo.Tx, env, kogo.Uuid, string) rt.Unit { return rt.Unit{} }
		s := open(t, 3, j.Queue())
		s.parcel(1, "nowhere", false)
		for range 3 {
			s.want("outcome", s.at(0, "queueParcel"), Failed)
		}
		s.want("alerts", len(s.alerts), 3)
	})
	t.Run("a job whose next takes a SerializableTx runs at SERIALIZABLE, and its failed too", func(t *testing.T) {
		j, levels := queueParcel(base), []string{}
		j.Serializable = true
		run, failed := j.Run, j.Failed
		j.Run = func(t kogo.Tx, e env, id kogo.Uuid) rt.Unit {
			levels = append(levels, kogo.Tx_QuerySingle(t, "show transaction_isolation", text))
			return run(t, e, id)
		}
		j.Failed = func(t kogo.Tx, e env, id kogo.Uuid, why string) rt.Unit {
			levels = append(levels, kogo.Tx_QuerySingle(t, "show transaction_isolation", text))
			return failed(t, e, id, why)
		}
		s := open(t, 3, j.Queue())
		s.parcel(1, "nowhere", false)
		s.want("outcome", s.at(0, "queueParcel"), Failed)
		s.want("levels", levels, []string{"serializable", "serializable"})
	})
	t.Run("next panics: there is no row to fail, so nothing is marked and nobody is alerted", func(t *testing.T) {
		j := queueParcel(base)
		j.Next = func(kogo.Tx, env) *kogo.Uuid { panic("next has a bug") }
		s := open(t, 3, j.Queue())
		s.parcel(1, "1 High St", false)
		s.want("outcome", s.at(0, "queueParcel"), Broken)
		s.want("alerts", len(s.alerts), 0)
	})
}

// With over, a queue names a function that gives the values to serve,
// and the runner enters each before it looks: one row a value in a
// pass, under row-level security that hides every other value's rows.
func TestOver(t *testing.T) {
	// over runs before any value is entered, so it reads a table with
	// no row-level security, as a tenant table is.
	depots := func(t kogo.Tx, e env) kogo.List[string] {
		return kogo.Tx_Query(t, "select distinct depot from parcel_depot order by depot", text)
	}
	enter := func(t kogo.Tx, e env, depot string) rt.Unit {
		kogo.Tx_Execute(t, "select set_config('depot.name', $1, true)", depot)
		return rt.Unit{}
	}
	e, j := bookCourier(3), queueParcel(base)
	e.Over, e.Enter, j.Over, j.Enter = depots, enter, depots, enter
	s := open(t, 3, e.Queue(), j.Queue())
	for n, depot := range []string{"north", "north", "north", "south", "west", "west", "south"} {
		s.parcel(n+1, map[bool]string{true: "nowhere", false: "1 High St"}[n == 6], false)
		s.exec("update parcel set depot = $2 where id = $1", id(n+1), depot)
	}
	s.exec(`create table parcel_depot as select id, depot from parcel;
		alter table parcel enable row level security; alter table parcel force row level security;
		create policy own on parcel using (depot = current_setting('depot.name', true));
		alter table parcel_dispatch enable row level security; alter table parcel_dispatch force row level security;
		create policy own on parcel_dispatch using
			((select depot from parcel_depot d where d.id = parcel_id) = current_setting('depot.name', true))`)
	var seen int
	s.pool.QueryRow(ctx, "select count(*) from parcel").Scan(&seen)
	s.want("parcels seen unbound", seen, 0)
	s.want("the job's first pass", s.r.Pass(ctx, "queueParcel"), []Outcome{Done, Done, Done})
	// South's second parcel is one run panics on: failed is called
	// with south entered again, or under row-level security it would
	// mark no row and the parcel would fail on every pass.
	s.want("its second", s.r.Pass(ctx, "queueParcel"), []Outcome{Done, Failed, Done})
	s.want("its third", s.r.Pass(ctx, "queueParcel"), []Outcome{Done, Idle, Idle})
	s.want("alerts", len(s.alerts), 1)
	s.want("the effect's first pass", s.r.Pass(ctx, "bookCourier"), []Outcome{Done, Done, Done})
	s.want("keys", s.keys, []string{"parcel_dispatch-" + id(1), "parcel_dispatch-" + id(4), "parcel_dispatch-" + id(5)})
	s.want("its second", s.r.Pass(ctx, "bookCourier"), []Outcome{Done, Idle, Done})
	s.want("its third", s.r.Pass(ctx, "bookCourier"), []Outcome{Done, Idle, Idle})
}

// Run serves every queue in a loop of its own until its context ends:
// the job feeds the effect, and every parcel is booked once.
func TestRun(t *testing.T) {
	s := open(t, 3)
	for n := 1; n <= 20; n++ {
		s.parcel(n, "1 High St", false)
	}
	ctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { s.r.Run(ctx); close(done) }()
	deadline := time.Now().Add(20 * time.Second)
	for booked := 0; booked < 20 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		s.pool.QueryRow(ctx, "select count(*) from parcel_dispatch where done_at is not null").Scan(&booked)
	}
	stop()
	<-done
	s.r.Env.mu.Lock()
	defer s.r.Env.mu.Unlock()
	keys := slices.Clone(s.keys)
	slices.Sort(keys)
	s.want("calls of act", len(keys), 20)
	s.want("each once", len(slices.Compact(keys)), 20)
}
