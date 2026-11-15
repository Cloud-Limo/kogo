package sqlpass

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func connect(t *testing.T) *Checker {
	url := os.Getenv("KOGO_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
	}
	c, err := Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("no test database (create it with `mise run testdb`): %v", err)
	}
	t.Cleanup(func() { c.Close(context.Background()) })
	return c
}

// signature renders a statement as "(name: Type, ...) -> name: Type?, ...".
func signature(st *Statement) string {
	var params, cols []string
	for _, p := range st.Params {
		params = append(params, p.Source+": "+p.Type.String())
	}
	for _, c := range st.Columns {
		s := c.Name + ": " + c.Type.String()
		if c.Nullable {
			s += "?"
		}
		cols = append(cols, s)
	}
	return fmt.Sprintf("(%s) -> %s", strings.Join(params, ", "), strings.Join(cols, ", "))
}

func TestSignatures(t *testing.T) {
	c := connect(t)
	for _, tc := range []struct{ name, literal, want string }{
		{"every row of the type table, as parameter and as column", `
			insert into every_type (flag, small, big, ratio, amount, body, folded, code, id, at, day, clock, span, kind, tags, share)
			values ($flag, $small, $big, $ratio, $amount, $body, $folded, $code, $id, $at, $day, $clock, $span, $kind, $tags, $share)
			returning *`,
			"(flag: Boolean, small: Int, big: Long, ratio: Double, amount: Decimal, body: String, folded: String, " +
				"code: String, id: Uuid, at: Instant, day: LocalDate, clock: LocalTime, span: Duration, " +
				"kind: InviteKind, tags: List<String>, share: Int) -> " +
				"flag: Boolean, small: Int, big: Long, ratio: Double, amount: Decimal, body: String, folded: String, " +
				"code: String, id: Uuid, at: Instant, day: LocalDate, clock: LocalTime, span: Duration, " +
				"kind: InviteKind, tags: List<String>, share: Int, note: String?, pending: String?"},
		{"a NOT NULL column and a nullable one, ordered by a column not selected",
			`select legal_name, abn from business order by created_at`, "() -> legal_name: String, abn: String?"},
		{"a $ that starts no parameter is SQL text",
			`select $$ of $$ || legal_name as "label!" from business`, "() -> label: String"},
		{"a NOT NULL constraint that is NOT VALID proves nothing",
			`select pending from every_type`, "() -> pending: String?"},
		{"both sides of a left join",
			`select b.id, t.id as tenant_id from business b left join tenant t on t.business_id = b.id`,
			"() -> id: Uuid, tenant_id: Uuid?"},
		{"both sides of a right join",
			`select b.id, t.id as tenant_id from business b right join tenant t on t.business_id = b.id`,
			"() -> id: Uuid?, tenant_id: Uuid"},
		{"a full join",
			`select b.id, t.id as tenant_id from business b full join tenant t on t.business_id = b.id`,
			"() -> id: Uuid?, tenant_id: Uuid?"},
		{"a left join with using",
			`select business_id, t.slug from admin_grant g left join tenant t using (business_id)`,
			"() -> business_id: Uuid, slug: String?"},
		{"an aggregate", `select count(*) as n from business`, "() -> n: Long?"},
		{"an expression, coalesce included",
			`select lower(legal_name) as lower, coalesce(abn, '') as abn from business`, "() -> lower: String?, abn: String?"},
		{"both overrides",
			`select count(*) as "n!", max(legal_name) as "name!", id as "id?" from business group by id`,
			"() -> n: Long, name: String, id: Uuid?"},
		{"a grouped column",
			`select legal_name, abn from business group by legal_name, abn`, "() -> legal_name: String, abn: String?"},
		{"a column nulled by a grouping set",
			`select legal_name from business group by rollup (legal_name)`, "() -> legal_name: String?"},
		{"a subquery in from",
			`select s.id, s.tenant_id from (
				select b.id, t.id as tenant_id from business b left join tenant t on t.business_id = b.id) s`,
			"() -> id: Uuid, tenant_id: Uuid?"},
		{"a union takes a column from every branch",
			`select id from business union all select null::uuid`, "() -> id: Uuid?"},
		{"returning, and the old row of an insert",
			`insert into person (id, name, email) values ($id, $name, $email) returning id, old.id as old_id`,
			"(id: Uuid, name: String, email: String) -> id: Uuid, old_id: Uuid?"},
		{"merge can null either side",
			`merge into person p using business b on b.id = p.id when matched then do nothing returning p.id`,
			"() -> id: Uuid?"},
		{"a list parameter, and Postgres's name for an unnamed column",
			`select 1 from business where id = any($ids)`, "(ids: List<Uuid>) -> ?column?: Int?"},
		{"a nullable parameter with a cast, used twice",
			`select body from every_type where ($note::text is null or note = $note)`, "(note: String) -> body: String"},
		{"an expression is a parameter each time it occurs",
			`select body from every_type where id = ${newId()} or id = ${ newId() } or small = ${row.small}`,
			"(newId(): Uuid, newId(): Uuid, row.small: Int) -> body: String"},
		{"a domain parameter and an array of a domain",
			`update supply_agreement set driver_share_percent = $share where driver_share_percent = any($shares::percent[])`,
			"(share: Int, shares: List<Int>) -> "},
	} {
		st, err := c.Check(context.Background(), tc.literal)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
		} else if got := signature(st); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

// exists and execute discard the columns, so they need no Kogo type.
func TestDiscardedColumns(t *testing.T) {
	st, err := connect(t).CheckDiscarding(context.Background(), `select pg_advisory_xact_lock($key)`)
	if err != nil || signature(st) != "(key: Long) -> " {
		t.Errorf("got %v, %v; want (key: Long) -> ", st, err)
	}
}

// A session that does not return the parse tree must not pass every
// column as nullable: that would hide the loss.
func TestNoParseTreeIsAFailure(t *testing.T) {
	c := connect(t)
	if _, err := c.conn.Exec(context.Background(), "set client_min_messages = warning"); err != nil {
		t.Fatal(err)
	}
	st, err := c.Check(context.Background(), `select legal_name from business`)
	var e *Error
	if err == nil || errors.As(err, &e) || !strings.Contains(err.Error(), "no parse tree") {
		t.Errorf("got %v, %v; want a failure to check, not a fault in the literal", st, err)
	}
}

func TestErrors(t *testing.T) {
	c := connect(t)
	// Each literal is wrong at the text between « and », which the test removes.
	for _, tc := range []struct{ name, literal, code, message string }{
		{"a misspelt column, after a parameter and a non-ASCII character on its line",
			`select 'é' from business b where b.id = $businessId and «b.legal_nam = 'x'`,
			"sql-rejected", `column b.legal_nam does not exist. Perhaps you meant to reference the column "b.legal_name". (Postgres 42703)`},
		{"a mistyped comparison",
			"select id from business\nwhere legal_name «= 42", "sql-rejected", "operator does not exist: text = integer"},
		{"an unknown enum label",
			`select id from invoice where status = «'payed'`, "sql-rejected", `invalid input value for enum invoice_status: "payed"`},
		{"a mismatched join",
			`select b.id from business b join tenant t on t.slug «= b.id`, "sql-rejected", "operator does not exist: citext = uuid"},
		{"a parameter Postgres cannot type",
			`select «$note is null as absent`, "sql-untyped-parameter", "could not determine data type of parameter $note: write a cast"},
		{"a fault at a parameter that follows another",
			`select id from business where legal_name = $name and «$flag::integer`, "sql-rejected", "argument of AND must be type boolean"},
		{"a parameter inside a quoted string",
			`select id from business where legal_name = $name and slug = '«${row.slug}'`, "sql-parameter-syntax", "Postgres does not see ${row.slug} as a parameter"},
		{"a parameter inside a quoted string, before one Postgres sees",
			`select id from business where slug = '«$slug' and id = $id`, "sql-parameter-syntax", "Postgres does not see $slug as a parameter"},
		{"a parameter inside a quoted string, where Postgres numbers a parameter of its own",
			`select id from business where (id, slug) = any (select business_id, slug from tenant) and legal_name = '«$name'`,
			"sql-parameter-syntax", "Postgres does not see $name as a parameter"},
		{"a fault at the end of the statement",
			`select id from business where«`, "sql-rejected", "syntax error at end of input"},
		{"a refusal whose detail says why",
			`select «'{1'::int[] as ids`, "sql-rejected", `malformed array literal: "{1". Unexpected end of input.`},
		{"an empty expression", `select «${ } as x`, "sql-parameter-syntax", "${} needs an expression"},
		{"a numbered parameter", `select id from business where id = «$1`, "sql-parameter-syntax", "write $name"},
		{"braces inside an expression", `select «${ids.map { it }}`, "sql-parameter-syntax", "bind the value to a val"},
		{"a type outside the table",
			`«select created_at::timestamp as at from business`, "sql-unmapped-type", `column "at" has the Postgres type timestamp without time zone`},
		{"a function returning void, when the columns are kept",
			`«select pg_advisory_xact_lock(1)`, "sql-unmapped-type", `column "pg_advisory_xact_lock" has the Postgres type void`},
		{"a parameter of a type outside the table",
			`select id from business where created_at::timestamp = «$at`, "sql-unmapped-type", "parameter $at has the Postgres type timestamp without time zone"},
	} {
		offset := strings.Index(tc.literal, "«")
		_, err := c.Check(context.Background(), strings.Replace(tc.literal, "«", "", 1))
		var e *Error
		if !errors.As(err, &e) {
			t.Errorf("%s: got %v, want an *Error", tc.name, err)
		} else if e.Offset != offset || e.Code != tc.code || !strings.Contains(e.Message, tc.message) {
			t.Errorf("%s:\n got %d %s %s\nwant %d %s %s", tc.name, e.Offset, e.Code, e.Message, offset, tc.code, tc.message)
		}
	}
}
