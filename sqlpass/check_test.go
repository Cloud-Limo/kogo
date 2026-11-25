package sqlpass

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
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

// spans finds the parameters of a literal as the parser would: $name,
// and ${expr} up to the brace that closes it.
func spans(literal string) []Span {
	var out []Span
	for i := 0; i+1 < len(literal); i++ {
		j := i + 1
		switch c := literal[j]; {
		case literal[i] != '$':
			continue
		case c == '{':
			for depth := 0; j < len(literal); j++ {
				if literal[j] == '{' {
					depth++
				} else if literal[j] == '}' {
					if depth--; depth == 0 {
						break
					}
				}
			}
			j++
		case isNameByte(c) && (c < '0' || c > '9'):
			for j < len(literal) && isNameByte(literal[j]) {
				j++
			}
		default:
			continue
		}
		out = append(out, Span{i, j})
		i = j - 1
	}
	return out
}

// check checks a literal with the parameters spans finds.
func check(c *Checker, literal string) (*Statement, error) {
	return c.Check(context.Background(), literal, spans(literal))
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
		{"bytea, as parameter and as column",
			`insert into letter (id, eml, draft) values ($id, $eml, $draft) returning eml, draft`,
			"(id: Uuid, eml: ByteArray, draft: ByteArray) -> eml: ByteArray, draft: ByteArray?"},
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
		{"count is not null, sum is",
			`select count(*) as n, count(small) as m, sum(small) as s from every_type`, "() -> n: Long, m: Long, s: Long?"},
		{"exists is not null, in is",
			`select exists (select from tenant) as e, id in (select business_id from tenant) as i from business`,
			"() -> e: Boolean, i: Boolean?"},
		{"a coalesce with an argument not null, a constant or a column, and one without",
			`select coalesce(abn, '') as abn, coalesce(abn, legal_name) as name, coalesce(abn, payid) as payid, lower(legal_name) as lower from business`,
			"() -> abn: String, name: String, payid: String?, lower: String?"},
		{"a relabel, a cast through text and a cast function of a column not null, and a cast of one that may be",
			`select slug::text as slug, id::text as id, created_at::date as made, abn_checked_at::date as checked from business`,
			"() -> slug: String, id: String, made: LocalDate, checked: LocalDate?"},
		{"a cast from jsonb to a number, which gives null for JSON's null, and one through text",
			`select coalesce(abn, 'null')::jsonb::int as n, coalesce(abn, 'null')::jsonb::text as t from business`,
			"() -> n: Int?, t: String"},
		{"a cast by a function of the schema's",
			`select legal_name::amount as a from business`, "() -> a: amount(cents: Long?, currency: String?)?"},
		{"a cast of count, exists and a coalesce, and of sum",
			`select count(*)::int as n, exists (select from tenant)::text as e, coalesce(sum(amount), 0)::bigint as total, sum(amount)::bigint as bare from every_type`,
			"() -> n: Int, e: String, total: Long, bare: Long?"},
		{"count through a subquery in from, not null on an inner join and nullable on an outer one",
			`select i.n, o.n as o, o.n::int as cast, coalesce(o.n, 0) as zero from business b
				join (select business_id, count(*) as n from tenant group by business_id) i on i.business_id = b.id
				left join (select business_id, count(*) as n from tenant group by business_id) o on o.business_id = b.id`,
			"() -> n: Long, o: Long?, cast: Int?, zero: Long"},
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
		{"braces inside an expression, which the parser delimits",
			`select ${ids.map { it }.size}::int as "n!"`, "(ids.map { it }.size: Int) -> n: Int"},
		{"whole rows from a left join: a table's row, null as a whole on the absent side, its fields NOT NULL as its columns are",
			`select b as business, t as storefront from business b left join tenant t on t.business_id = b.id`,
			"() -> business: business(id: Uuid, slug: String, legal_name: String, created_at: Instant, abn: String?, " +
				"abn_checked_at: Instant?, payid: String?, payid_name: String?), storefront: tenant(id: Uuid, business_id: Uuid, slug: String)?"},
		{"columns of a type made with create type, whose fields may all be null",
			`select total, deposit from quote`, "() -> total: amount(cents: Long?, currency: String?), deposit: amount(cents: Long?, currency: String?)?"},
		{"a table's whole row through a subquery, and a row that is not one",
			`select s.t, null::tenant as u from (select t from tenant t) s`,
			"() -> t: tenant(id: Uuid, business_id: Uuid, slug: String), u: tenant(id: Uuid?, business_id: Uuid?, slug: String?)?"},
		{"a domain parameter and an array of a domain",
			`update supply_agreement set driver_share_percent = $share where driver_share_percent = any($shares::percent[])`,
			"(share: Int, shares: List<Int>) -> "},
	} {
		st, err := check(c, tc.literal)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
		} else if got := signature(st); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

// A column is null only through an outer join where it would be
// non-null were the join inner: a NOT NULL column of the joined table,
// its whole row, a count through a subquery. One nullable in its table,
// or nulled by a grouping set or returning's old, is not.
func TestJoined(t *testing.T) {
	c := connect(t)
	for literal, want := range map[string]string{
		`select b.id, t.id as tenant_id, c.abn, coalesce(t.slug::text, '') as slug, t as tenant, o.n
			from business b left join tenant t on t.business_id = b.id left join business c on c.id = b.id
			left join (select business_id, count(*) as n from tenant group by business_id) o on o.business_id = b.id`: "tenant_id tenant n",
		`select t.id as "tenant_id?", t.id as tid from business b left join tenant t on t.business_id = b.id`: "tid",
		`select legal_name from business group by rollup (legal_name)`:                                        "",
		`insert into person (id, name, email) values ($id, $name, $email) returning old.id as old_id`:         "",
		`select g.person_id from business b full join admin_grant g on g.business_id = b.id`:                  "person_id",
	} {
		st, err := check(c, literal)
		if err != nil {
			t.Fatalf("%s: %v", literal, err)
		}
		var joined []string
		for _, col := range st.Columns {
			if col.Joined {
				joined = append(joined, col.Name)
			}
		}
		if got := strings.Join(joined, " "); got != want {
			t.Errorf("%s\n got %q joined, want %q", literal, got, want)
		}
	}
}

// A column carries the outer joins that can null it on the way,
// nullable in its table or not, each keyed by the subqueries it lies
// in and its range table entry, and a full join by its side too; a
// table's own column, as returning * gives, has none.
func TestJoins(t *testing.T) {
	c := connect(t)
	for literal, want := range map[string]string{
		`select b.id, t.id as tenant_id, t.slug, c.abn from business b left join tenant t on t.business_id = b.id left join business c on c.id = b.id`: "tenant_id[3] slug[3] abn[5]",
		`update business set abn = $abn where id = $id returning *`:                                                                                    "",
		`select t.id as shop_id, d.driver_id as shop_driver_id
			from business b left join tenant t on t.business_id = b.id left join tenant_driver d on d.tenant_id = t.id`: "shop_id[3] shop_driver_id[5]",
		`select s.tid as shop_id, s.did as shop_driver_id from business b left join (select t.business_id, t.id as tid, d.driver_id as did
			from tenant t left join tenant_driver d on d.tenant_id = t.id) s on s.business_id = b.id`: "shop_id[3] shop_driver_id[3 | 2.3]",
		`select g.person_id, b.id from business b full join (admin_grant g join person p on p.id = g.person_id) on g.business_id = b.id`: "person_id[5r] id[5l]",
		`select t.id from tenant_driver d right join tenant t on t.id = d.tenant_id`:                                                     "",
		`select a.id as a_id, t.id as t_id from person x left join (business a full join tenant t on t.business_id = a.id) on true`:      "a_id[4l | 5] t_id[4r | 5]",
		`select j1.tid as a, j2.tid as b
			from (business b1 left join (select t.business_id, t.id as tid from tenant t) t on t.business_id = b1.id) j1
			cross join (business b2 left join (select t.business_id, t.id as tid from tenant t) t on t.business_id = b2.id and false) j2`: "a[3] b[6]",
	} {
		st, err := check(c, literal)
		if err != nil {
			t.Fatalf("%s: %v", literal, err)
		}
		var joins []string
		for _, col := range st.Columns {
			if col.Joins != nil {
				joins = append(joins, col.Name+"["+strings.Join(col.Joins, " | ")+"]")
			}
		}
		if got := strings.Join(joins, " "); got != want {
			t.Errorf("%s\n got %q, want %q", literal, got, want)
		}
	}
}

// An enum carries its constants, as a parameter, as a column and in a
// list, for the checker to match them with a Kogo enum's.
func TestEnumLabels(t *testing.T) {
	st, err := check(connect(t), `select kind, array[kind] as kinds from invite where kind = $kind`)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []Type{st.Params[0].Type, st.Columns[0].Type, st.Columns[1].Type} {
		if strings.Join(typ.Labels, " ") != "in_house outside" {
			t.Errorf("%s has labels %v, want in_house outside", typ, typ.Labels)
		}
	}
}

// exists and execute discard the columns, so they need no Kogo type.
func TestDiscardedColumns(t *testing.T) {
	lit := `select pg_advisory_xact_lock($key)`
	st, err := connect(t).CheckDiscarding(context.Background(), lit, spans(lit))
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
	st, err := check(c, `select legal_name from business`)
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
		{"a numbered parameter", `select id from business where id = «$1`, "sql-parameter-syntax", "write $name"},
		{"a type outside the table",
			`«select created_at::timestamp as at from business`, "sql-unmapped-type", `column "at" has the Postgres type timestamp without time zone`},
		{"a function returning void, when the columns are kept",
			`«select pg_advisory_xact_lock(1)`, "sql-unmapped-type", `column "pg_advisory_xact_lock" has the Postgres type void`},
		{"the whole row of a subquery",
			`«select s from (select legal_name from business) s`, "sql-unmapped-type", `column "s" is a row of the anonymous type record`},
		{"a parameter of a composite type",
			`select id from quote where total = «$total::amount`, "sql-unmapped-type", "parameter $total has the composite type amount, which a parameter cannot have"},
		{"a parameter of the anonymous type record",
			`select id from quote where total = «$total`, "sql-unmapped-type", "parameter $total has the composite type record, which a parameter cannot have"},
		{"an array of rows",
			`«select array_agg(t) as tenants from tenant t`, "sql-unmapped-type", `column "tenants" has the Postgres type tenant[]`},
		{"a composite type with a field of a type outside the table",
			`«select null::pg_namespace as n`, "sql-unmapped-type",
			`column "n" has the Postgres type pg_namespace, whose field oid has the Postgres type oid, which has no Kogo type: select its fields as columns`},
		{"an array of a composite type with a field outside the table, named as an array",
			`«select array[null::pg_namespace] as ns`, "sql-unmapped-type", `column "ns" has the Postgres type pg_namespace[], which has no Kogo type`},
		{"an array of a domain over an array, a list of lists, which no List<T> holds",
			`«select array['{a}'::d_tags] as tags`, "sql-unmapped-type", `column "tags" has the Postgres type d_tags[], which has no Kogo type`},
		{"a read whose own with writes, which Postgres takes only at the top level",
			`«with gone as (delete from session returning id) select count(*) from gone`, "sql-writing-with", "move the writing with into a statement of its own"},
		{"two columns of one name, which the wrap cannot name apart",
			`«select b.id, t.id from business b join tenant t on t.business_id = b.id`, "sql-duplicate-column", "two columns are named id"},
		{"an empty literal", `«`, "sql-empty", "this literal holds no statement"},
		{"a literal of a comment alone", "«  -- later\n", "sql-empty", "this literal holds no statement"},
		{"a parameter of a type outside the table",
			`select id from business where created_at::timestamp = «$at`, "sql-unmapped-type", "parameter $at has the Postgres type timestamp without time zone"},
	} {
		offset := strings.Index(tc.literal, "«")
		_, err := check(c, strings.Replace(tc.literal, "«", "", 1))
		var e *Error
		if !errors.As(err, &e) {
			t.Errorf("%s: got %v, want an *Error", tc.name, err)
		} else if e.Offset != offset || e.Code != tc.code || !strings.Contains(e.Message, tc.message) {
			t.Errorf("%s:\n got %d %s %s\nwant %d %s %s", tc.name, e.Offset, e.Code, e.Message, offset, tc.code, tc.message)
		}
	}
}

// The twelve statements of doc/gates.md, "A gate reads and decides":
// what Postgres's tree says each needs of each table, which refuses
// the first seven in a gate and passes the rest. The last two write
// and are not caught: a function's body is not in the tree. Then a
// lock through a view, merge and upsert, and two utility statements,
// whose tree lists no table and gives Postgres's tag instead. Each is
// checked as execute takes it, since a read refuses a with that writes.
func TestTables(t *testing.T) {
	c := connect(t)
	for literal, want := range map[string]string{
		`update person set name = $name where id = $id`:                                             "person update",
		`insert into session (id, csrf_token, expires_at) values ($id, $token, now()) returning id`: "session insert",
		`update people set name = $name`:                                                            "people update",
		`with gone as (delete from session returning id) select count(*) from gone, person`:         "person, session delete",
		`select name from person where id = $id for update`:                                         "person lock",
		`select name from person where id = $id for share`:                                          "person lock",
		`select p.name from person p, session s for key share of s`:                                 "person, session lock",
		`select name from person where id in (select id from session)`:                              "person, session",
		`select name from people`:                                                                   "people",
		`select set_config('app.tenant', $tenant, true)`:                                            "",
		`select rename_all()::text`:                                                                 "",
		`select nextval('ticket')`:                                                                  "",
		`select name from people for update`:                                                        "people lock",
		`merge into person p using session s on p.id = s.id when matched then delete`:               "person delete, session",
		`insert into session (id, csrf_token, expires_at) values ($id, $token, now()) on conflict (id) do update set expires_at = now()`: "session insert update",
		`truncate person`:                    "TRUNCATESTMT",
		`lock table person in share mode`:    "LOCKSTMT",
		`commit`:                             "TRANSACTIONSTMT",
		`set search_path = other`:            "VARIABLESETSTMT",
		`set local statement_timeout = '5s'`: "VARIABLESETSTMT",
		`copy person from stdin`:             "COPYSTMT",
	} {
		st, err := c.CheckDiscarding(context.Background(), literal, spans(literal))
		if err != nil {
			t.Errorf("%s: %v", literal, err)
			continue
		}
		got := []string{st.Utility}[:min(len(st.Utility), 1)]
		for _, table := range st.Tables {
			got = append(got, strings.Join(append([]string{table.Name}, table.Needs...), " "))
		}
		if strings.Join(got, ", ") != want {
			t.Errorf("%s touches %q, want %q", literal, strings.Join(got, ", "), want)
		}
	}
}

// A read sends its statement as written, under a select of its
// columns by name: a select inside a subquery, any other in a with. A
// -- comment at its end stays a comment. exists and execute send it
// as written.
func TestWrap(t *testing.T) {
	c := connect(t)
	for literal, want := range map[string]string{
		"select slug from tenant -- by slug":  `select "slug" from (select slug from tenant -- by slug` + "\n) _",
		`select count(*) as "n!" from tenant`: `select "n!" from (select count(*) as "n!" from tenant` + "\n) _",
		"select slug from tenant; -- by slug": `select "slug" from (select slug from tenant` + "\n) _",
		`merge into account a using (values (1)) v(id) on a.id = v.id when matched then delete returning a.id`: `with _ as (merge into account a ` +
			"using (values (1)) v(id) on a.id = v.id when matched then delete returning a.id\n) select \"id\" from _",
		// A whole row in a write's returning, with a composite inside it.
		`update quote q set deposit = null where false returning q`: "with _ as (update quote q set deposit = null where false returning q\n) " +
			`select ("q") is not distinct from null, ("q")."id", (("q")."total") is not distinct from null, ` +
			`(("q")."total")."cents", (("q")."total")."currency", (("q")."deposit") is not distinct from null, ` +
			`(("q")."deposit")."cents", (("q")."deposit")."currency" ` +
			`from _`,
	} {
		if st, err := check(c, literal); err != nil || st.SQL != want {
			t.Errorf("%s: got %v, %v; want %s", literal, st, err, want)
		}
	}
	lit := `select slug from tenant`
	if st, err := c.CheckDiscarding(context.Background(), lit, nil); err != nil || st.SQL != lit {
		t.Errorf("exists and execute: got %v, %v; want %s", st, err, lit)
	}
}

// Postgres does not promise that a select keeps the order of the
// subquery it wraps; Postgres 18's planner keeps it. Under the
// settings that force a parallel plan, with and without Gather Merge,
// this fails if a later Postgres loses it.
func TestWrapKeepsOrder(t *testing.T) {
	c, ctx := connect(t), context.Background()
	// The second reads a whole row by its fields.
	for _, sql := range []string{`select id, k from ranked order by k`, `select r as ranked from ranked r order by r.k`} {
		st, err := check(c, sql)
		if err != nil {
			t.Fatal(err)
		}
		for _, setting := range []string{"on", "off"} {
			tx, err := c.conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx, "set local parallel_setup_cost = 0; set local parallel_tuple_cost = 0; "+
				"set local min_parallel_table_scan_size = 0; set local min_parallel_index_scan_size = 0; "+
				"set local max_parallel_workers_per_gather = 8; set local parallel_leader_participation = off; "+
				"set local enable_gathermerge = "+setting); err != nil {
				t.Fatal(err)
			}
			rows, _ := tx.Query(ctx, "explain (costs off) "+st.SQL)
			lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
			plan := strings.Join(lines, " / ")
			t.Logf("enable_gathermerge = %s: %s", setting, plan)
			if err != nil || !strings.Contains(plan, "Gather") {
				t.Errorf("enable_gathermerge = %s: no parallel plan, so the order is not tested: %s %v", setting, plan, err)
			}
			rows, _ = tx.Query(ctx, st.SQL)
			ks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (k int, err error) {
				dest := make([]any, len(r.FieldDescriptions())) // k is the last column
				dest[len(dest)-1] = &k
				return k, r.Scan(dest...)
			})
			if err != nil || len(ks) != 5000 || !slices.IsSorted(ks) {
				t.Errorf("%s, enable_gathermerge = %s: %d rows, sorted %v, %v", sql, setting, len(ks), slices.IsSorted(ks), err)
			}
			tx.Rollback(ctx)
		}
	}
}
