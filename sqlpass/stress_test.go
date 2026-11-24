package sqlpass

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// DEV-1144, a sketch: each test states what the pass should answer
// and fails today. The runs behind them are in the issue's report,
// under each finding's number.

// stressed gives a Checker over a schema of the test's own, made
// from statements in the test database and dropped after.
func stressed(t *testing.T, statements string) *Checker {
	t.Helper()
	url := os.Getenv("KOGO_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("no test database (create it with `mise run testdb`): %v", err)
	}
	schema := fmt.Sprintf("stress_sqlpass_%d", os.Getpid())
	if _, err := conn.Exec(ctx, "drop schema if exists "+schema+" cascade; create schema "+schema+
		"; set search_path = "+schema+", public; "+statements); err != nil {
		t.Fatal(err)
	}
	c, err := Connect(ctx, url+"?search_path="+schema+",public")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.Close(ctx)
		conn.Exec(ctx, "drop schema "+schema+" cascade")
		conn.Close(ctx)
	})
	return c
}

// wantSignature fails unless the literal checks to the signature.
func wantSignature(t *testing.T, c *Checker, literal, want string) {
	t.Helper()
	st, err := check(c, literal)
	if err != nil {
		t.Errorf("%s\n got error %v\nwant %s", literal, err, want)
	} else if got := signature(st); got != want {
		t.Errorf("%s\n got %s\nwant %s", literal, got, want)
	}
}

// wantCode fails unless the literal is refused with the code.
func wantCode(t *testing.T, c *Checker, literal, code string) {
	t.Helper()
	st, err := check(c, literal)
	var fault *Error
	switch {
	case err == nil:
		t.Errorf("%s\n got %s\nwant %s", literal, signature(st), code)
	case !errors.As(err, &fault):
		t.Fatal(err)
	case fault.Code != code:
		t.Errorf("%s\n got %s: %s\nwant %s", literal, fault.Code, fault.Message, code)
	}
}

// Finding 1: a name written twice is one parameter, so the one
// Postgres sees outside a quoted string hides the one inside it,
// whose text is then sent as $1: 'costs $1', an interval of '$1
// days', which Postgres reads as one day, a pattern that looks for
// the text $1, and a jsonpath that looks for a variable named 1.
func TestStressNameInsideAString(t *testing.T) {
	c := stressed(t, `create table t (id integer primary key, name text not null, at timestamptz not null, j jsonb not null);`)
	wantCode(t, c, `select 'costs $x' as label from t where id = $x`, "sql-parameter-syntax")
	wantCode(t, c, `select name from t where at > now() - interval '$n days' and id < $n`, "sql-parameter-syntax")
	wantCode(t, c, `select name from t where name like '%$q%' or name = $q`, "sql-parameter-syntax")
	wantCode(t, c, `select 1 from t where jsonb_path_exists(j, '$.tags[*] ? (@ == $tag)', jsonb_build_object('tag', $tag::text))`,
		"sql-parameter-syntax")
}

// Finding 9: Postgres 18 sets attnotnull for a NOT NULL constraint
// that is NO INHERIT, and a child's rows, which the parent's scan
// gives, may hold a null. Only `from only` keeps the column non-null.
func TestStressNotNullNoInherit(t *testing.T) {
	c := stressed(t, `
		create table par (id integer primary key, v text, constraint par_v_nn not null v no inherit);
		create table kid () inherits (par);`)
	wantSignature(t, c, `select id, v from par`, "() -> id: Int, v: String?")
	wantSignature(t, c, `select p as row from par p`, "() -> row: par(id: Int, v: String?)")
	wantSignature(t, c, `select id, v from only par`, "() -> id: Int, v: String")
}

// Finding 10: the tree Postgres logs is the statement before its
// rules are applied, and a rule replaces returning's list with its
// own.
func TestStressRuleRewritesReturning(t *testing.T) {
	c := stressed(t, `
		create table r (id integer primary key, v text not null);
		create table r_log (id integer, v text);
		create rule r_ins as on insert to r do instead insert into r_log values (new.id, null) returning id, v;`)
	wantSignature(t, c, `insert into r values (1, 'x') returning v`, "() -> v: String?")
}

// Finding 11, ruled 24 Nov 2026: a dollar-quoted string, and $word or
// $5 as text in a string, stay refused, since a $name in a string must
// stay refused when it is also a parameter, and each message says how
// a $ is written as text: 'in ' || chr(36) || 'AUD' passes.
func TestStressDollarText(t *testing.T) {
	c := stressed(t, `create table t (id integer primary key, amount numeric not null);`)
	for _, literal := range []string{
		`select $q$hello$q$ as "s!"`,
		`select $$it's$$ as "s!"`,
		`select 'costs $5' as "s!"`,
		`select 'in $AUD' as "s!"`,
	} {
		_, err := check(c, literal)
		if fault := (*Error)(nil); !errors.As(err, &fault) || !strings.Contains(fault.Message, "chr(36)") {
			t.Errorf("%s\n got %v\nwant a refusal that says how to write a $ as text", literal, err)
		}
	}
	wantSignature(t, c, `select 'in ' || chr(36) || 'AUD' as "s!"`, "() -> s: String")
}

// Finding 12: Postgres types $x from `id = $x`, then finds `$x is
// null` untyped and says so with SQLSTATE 42P08, not 42P18, so the
// message lacks "write a cast after it" for the mistake 10.6's idiom
// invites.
func TestStressOptionalFilterWithoutCast(t *testing.T) {
	c := stressed(t, `create table t (id integer primary key, name text not null);`)
	wantCode(t, c, `select name from t where ($x is null or id = $x)`, "sql-untyped-parameter")
}
