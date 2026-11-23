package emit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// DEV-1144, a sketch: each test runs emitted Go against a schema of
// its own and states what it should give; each fails today. The runs
// behind them are in the issue's report.

// What the driver of a stress test declares: db, over a pool of one
// connection, so that a statement prepared before the schema moves is
// met again after, and ddl, which moves it.
const stressDB = `
import (
"context"
"os"
"github.com/jackc/pgx/v5/pgxpool"
"github.com/Cloud-Limo/kogo/prelude/kogo"
)
var ctx = context.Background()
var pool = func() *pgxpool.Pool {
cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
if err != nil {
panic(err)
}
cfg.MaxConns = 1
p, err := pgxpool.NewWithConfig(ctx, cfg)
if err != nil {
panic(err)
}
return p
}()
var db = kogo.NewDb(ctx, pool)
func ddl(s string) string {
if _, err := pool.Exec(ctx, s); err != nil {
panic(err)
}
return "ok"
}
`

// stress makes a schema of the test's own from statements, in the
// test database, has the source's literals checked against it, and
// runs each case's Go expression against it. A case wants its text,
// or with "not: " before it anything but that text: what is wrong
// today, where what is right is a choice still open.
func stress(t *testing.T, statements string, cases [][2]string, source string) {
	t.Helper()
	url := os.Getenv("KOGO_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("no test database, create it with `mise run testdb`: %v", err)
	}
	schema := fmt.Sprintf("stress_emit_%d", os.Getpid())
	if _, err := conn.Exec(ctx, "drop schema if exists "+schema+" cascade; create schema "+schema+
		"; set search_path = "+schema+", public; "+statements); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Exec(ctx, "drop schema "+schema+" cascade")
		conn.Close(ctx)
	})
	t.Setenv("KOGO_TEST_DATABASE_URL", url+"?search_path="+schema+",public")
	var calls []string
	for _, c := range cases {
		calls = append(calls, c[0])
	}
	got := run(t, stressDB, "", calls, source)
	if len(got) != len(cases) {
		t.Fatalf("%d lines, want %d:\n%s", len(got), len(cases), strings.Join(got, "\n"))
	}
	for i, c := range cases {
		if wrong, not := strings.CutPrefix(c[1], "not: "); not && got[i] == wrong {
			t.Errorf("%s\n got %s, which is wrong", c[0], got[i])
		} else if !not && got[i] != c[1] {
			t.Errorf("%s\n got %s\nwant %s", c[0], got[i], c[1])
		}
	}
}

// E: the check matches columns to properties by name, and the reader
// scans by position and never reads the names Postgres sends. A
// column dropped and added again is last, so select * and a whole
// row read each value into the other's property; a column dropped
// for good shifts a whole row's fields along. Reading by name gives
// the first two wants; a panic that names the column is the other
// way to settle it.
func TestStressSchemaMovedUnderPositions(t *testing.T) {
	stress(t, `
create table acct (id integer primary key, owner text not null, payee text not null);
insert into acct values (1, 'alice', 'bob');
create table acct2 (id integer primary key, owner text not null, payee text not null, cents bigint not null);
insert into acct2 values (1, 'alice', 'bob', 500);
`, [][2]string{
		{`t.Star(db)`, "Acct(id=1, owner=alice, payee=bob)"},
		{"ddl(`alter table acct drop column owner; alter table acct add column owner text not null default 'alice'`)", "ok"},
		{`t.Star(db)`, "panics: ERROR: cached plan must not change result type (SQLSTATE 0A000)"},
		{`t.Star(db)`, "Acct(id=1, owner=alice, payee=bob)"},
		{`t.Whole(db)`, "Held(a=Acct(id=1, owner=alice, payee=bob))"},
		{"ddl(`alter table acct2 drop column owner`)", "ok"},
		{`t.Whole2(db)`, "not: Held2(a=Acct2(id=1, owner=bob, payee=500, cents=0))"},
	}, `package t

data class Acct(val id: Int, val owner: String, val payee: String)
data class Held(val a: Acct)
fun star(db: Db): Acct = db.tx { tx -> tx.querySingle<Acct>(sql"select * from acct where id = 1") }
fun whole(db: Db): Held = db.tx { tx -> tx.querySingle<Held>(sql"select acct as a from acct where id = 1") }

data class Acct2(val id: Int, val owner: String, val payee: String, val cents: Long)
data class Held2(val a: Acct2)
fun whole2(db: Db): Held2 = db.tx { tx -> tx.querySingle<Held2>(sql"select acct2 as a from acct2 where id = 1") }
`)
}

// F: a literal that ends the transaction passes check, and what
// follows it in db.tx runs outside any transaction: a panic rolls
// nothing back (spec.md 10.7). Where check refuses the literals
// instead, this test moves to check's.
func TestStressCommitInsideTx(t *testing.T) {
	stress(t, `create table p (id integer primary key, name text not null);`, [][2]string{
		{`t.Sneaky(db)`, "panics: boom"},
		{`t.Undone(db)`, "panics: boom"},
		{`t.Names(db)`, "[]"},
	}, `package t

fun sneaky(db: Db): Int = db.tx { tx ->
    tx.execute(sql"insert into p values (1, 'before commit')")
    tx.execute(sql"commit")
    tx.execute(sql"insert into p values (2, 'after commit')")
    check(false) { "boom" }
    1
}

fun undone(db: Db): Int = db.tx { tx ->
    tx.execute(sql"insert into p values (3, 'before rollback')")
    tx.execute(sql"rollback")
    tx.execute(sql"insert into p values (4, 'after rollback')")
    check(false) { "boom" }
    1
}

fun names(db: Db): List<String> = db.tx { tx -> tx.query<String>(sql"select name from p order by id") }
`)
}

// B, D: Postgres's int4[] says neither how many dimensions a value
// has nor whether an element is null. Two dimensions read as one
// flat list; a null element fills a List<Int> and is the driver's
// panic. C: an array of a domain is a type the driver does not know,
// and only Go's own types are read from its text.
func TestStressArrays(t *testing.T) {
	stress(t, `
create domain d_num as numeric(10, 2);
create domain d_uuid as uuid;
create table a (ints integer[] not null, nums d_num[] not null, uuids d_uuid[] not null);
insert into a values ('{1,null,3}', '{0.00,1.50}', '{00000000-0000-0000-0000-000000000002}');
`, [][2]string{
		{`t.Grid(db)`, "not: [1, 2, 3, 4]"},
		{`t.Ints(db)`, "not: panics: can't scan into dest[0] (col: ints): failed to scan array element 1: cannot scan NULL into *int32"},
		{`t.Nums(db)`, "[0.00, 1.50]"},
		{`t.Uuids(db)`, "[00000000-0000-0000-0000-000000000002]"},
	}, `package t

fun grid(db: Db): List<Int> = db.tx { tx -> tx.querySingle<List<Int>>(sql"""select '{{1,2},{3,4}}'::int[] as "x!" """) }
fun ints(db: Db): List<Int> = db.tx { tx -> tx.querySingle<List<Int>>(sql"select ints from a") }
fun nums(db: Db): List<Decimal> = db.tx { tx -> tx.querySingle<List<Decimal>>(sql"select nums from a") }
fun uuids(db: Db): List<Uuid> = db.tx { tx -> tx.querySingle<List<Uuid>>(sql"select uuids from a") }
`)
}

// G: a String holds U+0000 and Postgres's text does not; spec.md 10.4
// lists the edges between the two sides' types and not this one.
func TestStressZeroByteInAString(t *testing.T) {
	stress(t, `create table p (id integer primary key, name text not null);`, [][2]string{
		{`t.Keep(db, "a\x00b")`, `not: panics: ERROR: invalid byte sequence for encoding "UTF8": 0x00 (SQLSTATE 22021)`},
	}, `package t

fun keep(db: Db, name: String): Int = db.tx { tx -> tx.execute(sql"insert into p values (9, $name)") }
`)
}

// H: execute gives an Int (spec.md 10.2) and Postgres counts rows in
// 64 bits: 2,200,000,000 of them come back as -2094967296. It runs
// for over two minutes, so only when KOGO_STRESS_SLOW is set.
func TestStressRowsAffectedPastInt(t *testing.T) {
	if os.Getenv("KOGO_STRESS_SLOW") == "" {
		t.Skip("set KOGO_STRESS_SLOW=1 to run it")
	}
	stress(t, ``, [][2]string{
		{`t.Many(db)`, "not: -2094967296"},
	}, `package t

fun many(db: Db): Int = db.tx { tx -> tx.execute(sql"select from generate_series(1, 50000) a, generate_series(1, 44000) b") }
`)
}
