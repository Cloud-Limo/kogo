package check

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Cloud-Limo/kogo/sqlpass"
	"github.com/Cloud-Limo/kogo/syntax"
)

// The tables of these tests, by name: what the SQL pass would answer
// for the statements the checker builds over an effect's table.
var tables = map[string]string{
	"q": "id: Uuid, attempts: Int, next_attempt_at: Instant, claimed_at: Instant?, last_error: String?, failed: Boolean, " +
		"done_at: Instant?, not_after: Instant?, parcel_id: Uuid, express_since: Instant?, tracking: String?",
	"plain": "id: Uuid, parcel_id: Uuid",
	"odd": "id: Uuid, attempts: String, next_attempt_at: Instant, claimed_at: Instant?, last_error: String?, failed: Boolean, " +
		"done_at: Instant?, not_after: Instant?, parcel_id: Uuid",
}

var selectFrom = regexp.MustCompile(`^select (.*) from (\w+)$`)

// tableSelect answers for a statement of the checker's own, select
// columns from table, over one of tables: a column the table lacks is
// refused as Postgres refuses it.
func tableSelect(lit *syntax.SQL) (*sqlpass.Statement, error, bool) {
	m := selectFrom.FindStringSubmatch(lit.Text)
	if m == nil || tables[m[2]] == "" {
		return nil, nil, false
	}
	st := &sqlpass.Statement{SQL: lit.Text}
	for _, name := range strings.Split(m[1], ", ") {
		_, rest, found := strings.Cut(", "+tables[m[2]]+",", ", "+name+": ")
		if !found {
			return nil, &sqlpass.Error{Code: "sql-rejected", Message: `column "` + name + `" does not exist`}, true
		}
		t, _, _ := strings.Cut(rest, ",")
		base, null := strings.CutSuffix(t, "?")
		st.Columns = append(st.Columns, sqlpass.Column{Name: name, Type: sqlpass.Type{Name: base}, Nullable: null})
	}
	return st, nil, true
}

// Workers (doc/work.md, "The list of work"): what the checker holds
// of a job, an effect and a work list.
func TestWork(t *testing.T) {
	head := `package kogo.work
external class EffectKey
sealed interface Act<out R> {
    data class Done<R>(val result: R) : Act<R>
    data class Unknown(val why: String) : Act<Nothing>
}
sealed interface Failure {
    data class Refused(val why: String) : Failure
}
data class Alert(val queue: String)
action external fun ring()
---
import kogo.work.Act
import kogo.work.Alert
import kogo.work.EffectKey
import kogo.work.Failure
import kogo.work.ring
data class Env(val n: Int)
data class Row(val id: Uuid, val parcelId: Uuid)
fun prep(tx: Tx, env: Env, row: Row): Either<String, Int> = either { 1 }
action fun send(env: Env, key: EffectKey, n: Int): Act<String> {
    ring()
    return Act.Done("x")
}
fun rec(tx: Tx, env: Env, row: Row, s: String): Either<String, Unit> = either { }
fun bad(tx: Tx, env: Env, row: Row, why: Failure) { }
fun words(tx: Tx, env: Env, row: Row, why: Failure): String? = null
fun next(tx: Tx, env: Env): Row? = null
fun go(tx: Tx, env: Env, row: Row) { }
fun gone(tx: Tx, env: Env, row: Row, why: String) { }
action fun tell(env: Env, alert: Alert): Either<String, Unit> = either { ring() }
`
	effect := func(parts ...string) string {
		return "effect e on q {\n    " + strings.Join(parts, "\n    ") + "\n}\n"
	}
	steps := []string{"prepare prep", "act send", "record rec"}
	whole := effect(append([]string{"soonest express_since", "attempts 1"}, append(steps, "failed bad", "recovery words")...)...)
	plain := effect(append([]string{"attempts unlimited"}, steps...)...)
	job := "job j {\n    next next\n    run go\n    failed gone\n}\n"
	list := "work w alertedBy tell { e, j }\n"
	passes(t,
		head+whole+job+list,
		head+plain+job+"work w alertedBy tell {\n    e\n    j,\n}\n",
		// A job on a SerializableTx, whose other steps take a Tx or the same.
		head+plain+"fun n2(tx: SerializableTx, env: Env): Row? = null\nfun f2(tx: SerializableTx, env: Env, row: Row, why: String) { }\njob j {\n    next n2\n    run go\n    failed f2\n}\n"+list,
	)
	with := func(decl string) string { return head + decl }
	refused(t, [][3]string{
		{head + "‸" + effect(steps...) + job + list, "part", "effect e states no attempts"},
		{head + effect("attempts 1", "act send", "‸prepare prep", "record rec") + job + list, "part-order", "prepare comes before act"},
		{head + effect("attempts 1", "‸find q", "prepare prep", "act send", "record rec") + job + list, "part", "find is no part of an effect"},
		{head + "‸" + plain + job + "work w alertedBy tell { j }\n", "unserved", "e is in no work list"},
		{head + plain + job + list + "work v alertedBy tell { ‸e }\n", "duplicate", "e is already in a work list"},
		{head + plain + job + "work w alertedBy tell { e, j, ‸k }\n", "work", "k is no job or effect"},
		{head + plain + strings.Replace(strings.Replace(plain, "effect e", "effect e2", 1), "on q", "on ‸q", 1) + job + "work w alertedBy tell { e, e2, j }\n", "duplicate", "effect e is on q too: a table is one effect's"},
		{head + plain + job + "‸work w { e, j }\n", "no-alert", "work w names no alert"},
		{with("fun quiet(env: Env, alert: Alert): Either<String, Unit> = either { }\n") + plain + job + "work w alertedBy ‸quiet { e, j }\n", "alert", "a work list's alert is an action fun"},
		{head + plain + "‸job j {\n    next next\n    run go\n}\n" + list, "part", "job j states no failed"},
		{with("fun plainSend(env: Env, key: EffectKey, n: Int): Act<String> = Act.Done(\"x\")\n") + effect("attempts 1", "prepare prep", "act ‸plainSend", "record rec") + job + list,
			"act", "an effect's act is an action fun"},
		{with("action fun other(env: Env, key: EffectKey, n: String): Act<String> {\n    ring()\n    return Act.Done(n)\n}\n") + effect("attempts 1", "prepare prep", "act ‸other", "record rec") + job + list,
			"act", "other takes a String third, and prepare gives a Int: act takes what prepare gives"},
		{with("fun rec2(tx: Tx, env: Env, row: Row, n: Long): Either<String, Unit> = either { }\n") + effect("attempts 1", "prepare prep", "act send", "record ‸rec2") + job + list,
			"record", "rec2 takes a Long fourth, and act answers an Act<String>: record takes what act answers"},
		{with("fun prep2(tx: SerializableTx, env: Env, row: Row): Either<String, Int> = either { 1 }\n") + effect("attempts 1", "prepare ‸prep2", "act send", "record rec") + job + list,
			"prepare", "prep2 takes a SerializableTx"},
		{with("fun f2(tx: SerializableTx, env: Env, row: Row, why: String) { }\n") + plain + "job j {\n    next next\n    run go\n    failed ‸f2\n}\n" + list,
			"failed", "f2 takes a SerializableTx, and the job's next takes a Tx"},
		{head + strings.Replace(plain, "attempts unlimited", "soonest ‸tracking\n    attempts 1", 1) + job + list, "soonest", "q.tracking is a String, and soonest names a timestamptz"},
		{head + strings.Replace(plain, "on q", "on ‸plain", 1) + job + list, "effect-table", "an effect's table has the runner's columns"},
		{head + strings.Replace(plain, "on q", "on ‸odd", 1) + job + list, "effect-table", "odd.attempts is a String, and the runner's attempts is a Int"},
		{with("data class Wide(val id: Uuid, val weight: Int)\nfun prepW(tx: Tx, env: Env, row: Wide): Either<String, Int> = either { 1 }\nfun recW(tx: Tx, env: Env, row: Wide, s: String): Either<String, Unit> = either { }\n") +
			"effect e on ‸q {\n    attempts 1\n    prepare prepW\n    act send\n    record recW\n}\n" + job + list, "effect-table", `column "weight" does not exist`},
		// A job's row goes from next's transaction into failed's.
		{with("data class HeldRow(val tx: Tx, val id: Uuid)\nfun nextH(tx: Tx, env: Env): HeldRow? = null\nfun goH(tx: Tx, env: Env, row: HeldRow) { }\nfun goneH(tx: Tx, env: Env, row: HeldRow, why: String) { }\n") +
			plain + "job j {\n    next ‸nextH\n    run goH\n    failed goneH\n}\n" + list, "next", "nextH answers HeldRow, which holds a transaction, tx, a property of HeldRow: where run panics"},
		// Nor does prepare hand act its transaction.
		{with("data class Held(val tx: Tx, val n: Int)\nfun prepH(tx: Tx, env: Env, row: Row): Either<String, Held> = either { Held(tx, 1) }\naction fun sendH(env: Env, key: EffectKey, h: Held): Act<String> {\n    ring()\n    return Act.Done(\"x\")\n}\n") +
			effect("attempts 1", "prepare ‸prepH", "act sendH", "record rec") + job + list, "prepare", "prepH answers Held, which holds a transaction, tx, a property of Held: the runner commits prepare's transaction before it calls act"},
		// act reaches no database: its environment holds no Db.
		{"package kogo.work\nexternal class EffectKey\nsealed interface Act<out R> {\n    data class Done<R>(val result: R) : Act<R>\n}\ndata class Alert(val queue: String)\naction external fun ring()\n---\n" +
			"import kogo.work.Act\nimport kogo.work.Alert\nimport kogo.work.EffectKey\nimport kogo.work.ring\ndata class Handles(val db: Db)\ndata class Env(val handles: Handles)\ndata class Row(val id: Uuid)\n" +
			"fun prep(tx: Tx, env: Env, row: Row): Either<String, Int> = either { 1 }\naction fun send(env: Env, key: EffectKey, n: Int): Act<String> {\n    ring()\n    return Act.Done(\"x\")\n}\n" +
			"fun rec(tx: Tx, env: Env, row: Row, s: String): Either<String, Unit> = either { }\naction fun tell(env: Env, alert: Alert): Either<String, Unit> = either { ring() }\n" +
			"effect e on q {\n    attempts 1\n    prepare prep\n    act ‸send\n    record rec\n}\nwork w alertedBy tell { e }\n", "act", "send's environment holds a Db, at Env.handles"},
		{with("fun recO(tx: Tx, env: Env, row: Env, s: String): Either<String, Unit> = either { }\n") + effect("attempts 1", "prepare prep", "act send", "record ‸recO") + job + list,
			"part", "the steps of one queue take one row type"},
	})
}
