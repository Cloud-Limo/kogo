package emit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Cloud-Limo/kogo/sqlpass"
	"github.com/jackc/pgx/v5"
)

// The types of spec.md 10.4: Postgres's name, Kogo's, a short name
// for the row's funs and domain, and two values at the type's edges,
// as SQL. A composite type, the table's last, is read field by field
// and is no parameter, so its cells are written out in composite.
var shaped = [][5]string{
	{"boolean", "Boolean", "bool", "true", "false"},
	{"integer", "Int", "int", "'-2147483648'", "2147483647"},
	{"bigint", "Long", "long", "'-9223372036854775808'", "9223372036854775807"},
	{"double precision", "Double", "double", "'-0'", "5e-324"},
	{"numeric", "Decimal", "numeric", "0.00", "'NaN'"},
	{"text", "String", "text", "''", `'a"{,}\ NULL é😀'`},
	{"citext", "String", "citext", "'Fold'", "' '"},
	{"varchar", "String", "varchar", "'null'", "'''x'''"},
	{"uuid", "Uuid", "uuid", "'00000000-0000-0000-0000-000000000000'", "'ffffffff-ffff-ffff-ffff-ffffffffffff'"},
	{"timestamptz", "Instant", "timestamptz", "'0001-01-01 00:00:00+00'", "'2026-11-15 14:30:00.123456+00'"},
	{"date", "LocalDate", "date", "'0001-01-01'", "'9999-12-31'"},
	{"time", "LocalTime", "time", "'00:00'", "'23:59:59.999999'"},
	{"interval", "Duration", "interval", "'-26:00:00.5'", "'00:00:00.000001'"},
	{"bytea", "ByteArray", "bytea", `'\x'`, `'\x00ff'`},
	{"invite_kind", "Kind", "kind", "'in_house'", "'outside'"},
}

// The shapes a value of each type is read in: the shape's name, the
// SQL of the value, the Kogo type it is read as, the cast that sends
// it back, and the limit the cell names where Kogo refuses it. %[1]s
// is the type, %[2]s Kogo's, %[3]s the short name, %[4]s and %[5]s
// the values.
var shapes = [][5]string{
	{"Plain", "%[4]s::%[1]s", "%[2]s", "%[1]s", ""},
	{"Null", "null::%[1]s", "%[2]s?", "%[1]s", ""},
	{"Array", "array[%[4]s, %[5]s]::%[1]s[]", "List<%[2]s>", "%[1]s[]", ""},
	{"Nulls", "array[%[4]s, null, %[5]s]::%[1]s[]", "List<%[2]s?>", "%[1]s[]", ""},
	{"NullsAsList", "array[%[4]s, null]::%[1]s[]", "List<%[2]s>", "%[1]s[]", "column v holds an array whose element 1 is NULL"},
	{"TwoDimensions", "array[[%[4]s], [%[5]s]]::%[1]s[]", "List<%[2]s>", "%[1]s[]", "an array of 2 dimensions is not a List"},
	{"Domain", "%[5]s::d_%[3]s", "%[2]s", "%[1]s", ""},
	{"DomainArray", "array[%[4]s, %[5]s]::d_%[3]s[]", "List<%[2]s>", "d_%[3]s[]", ""},
}

// Kogo reads a subset of what Postgres can send, and outside it
// refuses (doc/design.md, principle 4; spec.md 10.4). Every type in
// every shape is read by the emitted Go and sent back, and the text
// Postgres makes of what came back is the text it makes of the value
// itself; or the cell names its limit: a panic, for an enum's lists
// a refusal when the program is emitted, and for a composite type's
// one of kogo check.
func TestShapes(t *testing.T) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testURL())
	if err != nil {
		t.Fatalf("no test database, create it with `mise run testdb`: %v", err)
	}
	defer conn.Close(ctx)
	refused := "package t\n\nenum class Kind { in_house, outside }\n" // the cells of an enum's lists
	source := "package t\n\nimport kogo.time.Duration\nimport kogo.time.Instant\nimport kogo.time.LocalDate\nimport kogo.time.LocalTime\n\nenum class Kind { in_house, outside }\n"
	text := func(value string) string { // Postgres's own text of a value
		var text *string
		if err := conn.QueryRow(ctx, "select ("+value+")::text").Scan(&text); err != nil {
			t.Fatalf("%s: %v", value, err)
		} else if text != nil {
			return *text
		}
		return "null"
	}
	cases := [][2]string{
		{"t.AmountPlain(db)", text("(1500, 'AUD')::amount")},
		{"t.AmountNull(db)", text("null::amount")},
		{"t.AmountDomain(db)", text("(null, 'NZD')::d_amount")},
	}
	for _, row := range shaped {
		for _, shape := range shapes {
			args := []any{row[0], row[1], row[2], row[3], row[4]}
			value, kogo, back := fmt.Sprintf(shape[1], args...), fmt.Sprintf(shape[2], args...), fmt.Sprintf(shape[3], args...)
			if row[1] == "Kind" && strings.Contains(kogo, "List") {
				refused += fmt.Sprintf("fun %s%s(tx: Tx): %s = tx.querySingle<%s>(sql\"\"\"select %s as \"v!\" \"\"\")\n", row[2], shape[0], kogo, kogo, value)
				continue
			}
			want := "limit: " + shape[4]
			if shape[4] == "" {
				want = text(value)
			}
			source += fmt.Sprintf("fun %s%s(db: Db): String? = db.tx { tx ->\n    val v = tx.querySingle<%s>(sql\"\"\"select %s as \"v%s\" \"\"\")\n"+
				"    tx.querySingle<String?>(sql\"select ${v}::%s::text as t\")\n}\n", row[2], shape[0], kogo, value,
				map[bool]string{true: "?", false: "!"}[shape[0] == "Null"], back)
			cases = append(cases, [2]string{fmt.Sprintf("t.%s%s(db)", export(row[2]), shape[0]), want})
		}
	}
	_, _, errs := emit(t, "x", refused)
	if lists := strings.Count(refused, "fun "); len(errs) != lists || lists != 5 {
		t.Errorf("%d of an enum's %d lists are refused, want each of 5: %v", len(errs), lists, errs)
	}
	for _, err := range errs {
		if !strings.Contains(err.Message, "which holds a list of an enum") {
			t.Errorf("got %v", err)
		}
	}
	pass := must(sqlpass.Connect(ctx, testURL()))
	defer pass.Close(ctx)
	for _, list := range []string{"array[(1, 'AUD')::amount]", "array[null::amount]", "array[[(1, 'AUD')::amount]]", "array[(1, 'AUD')::d_amount]"} {
		if _, err := pass.Check(ctx, "select "+list+" as v", nil); err == nil || !strings.Contains(err.Error(), "[], which has no Kogo type") {
			t.Errorf("%s\n got %v\nwant a type with no Kogo type", list, err)
		}
	}
	got := run(t, withDB, "defer drop()\n", calls(cases), source, composite)
	if len(got) != len(cases) {
		t.Fatalf("%d lines, want %d:\n%s", len(got), len(cases), strings.Join(got, "\n"))
	}
	for i, c := range cases {
		limit, named := strings.CutPrefix(c[1], "limit: ")
		if named && !(strings.HasPrefix(got[i], "panics: ") && strings.Contains(got[i], limit)) || !named && got[i] != c[1] {
			t.Errorf("%s\n got %s\nwant %s", c[0], got[i], c[1])
		}
	}
	t.Logf("%d cells read or panicking, %d refused", len(cases), len(errs)+4)
}

// The cells of a composite type: alone, null and through a domain. Its
// fields are sent back, since the type is no parameter.
const composite = `package t

data class Amount(val cents: Long?, val currency: String?)
data class Held(val v: Amount?)

fun back(tx: Tx, a: Amount?): String? =
    if (a == null) null else tx.querySingle<String?>(sql"select row(${a.cents}::bigint, ${a.currency}::text)::amount::text as t")
fun amountPlain(db: Db): String? = db.tx { tx -> back(tx, tx.querySingle<Held>(sql"select (1500, 'AUD')::amount as v").v) }
fun amountNull(db: Db): String? = db.tx { tx -> back(tx, tx.querySingle<Held>(sql"select null::amount as v").v) }
fun amountDomain(db: Db): String? = db.tx { tx -> back(tx, tx.querySingle<Held>(sql"select (null, 'NZD')::d_amount as v").v) }
`

func testURL() string {
	if url := os.Getenv("KOGO_TEST_DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://kogo:kogo@127.0.0.1/kogo_test"
}

func calls(cases [][2]string) (calls []string) {
	for _, c := range cases {
		calls = append(calls, c[0])
	}
	return calls
}

// The limits of spec.md 10.4 that the table above does not hold, and
// the cases of DEV-1144's findings 4 to 8: each is the value Postgres
// holds or a panic that names the limit, never another value.
func TestLimits(t *testing.T) {
	zero := "panics: a String that holds U+0000 cannot be sent to Postgres, whose text holds none (spec.md 10.4)"
	cases := [][2]string{
		{`t.Grid(db)`, "panics: can't scan into dest[0] (col: x!): an array of 2 dimensions is not a List, which has one (spec.md 10.4)"},
		{`t.Ints(db)`, "panics: column ids holds an array whose element 0 is NULL, and fills a List whose elements are not nullable " +
			"(spec.md 10.4): read it as a List<T?>, or keep the nulls out in the statement"},
		{`t.IntsOrNull(db)`, "[null]"},
		{`t.Lower(db)`, "[1, 2]"}, // a lower bound is not kept
		{`t.Nums(db)`, "[0.00, 1.50]"},
		{`t.Uuids(db)`, "[00000000-0000-0000-0000-000000000002]"},
		{`t.Tags(db)`, "[a, b] [Fold] null"},
		{`t.Cardinals(db)`, "[1, 2]"},
		{`t.TagSets(db)`, "TagSet(tags=[a, b], names=[c])"},
		{`t.SendDates(db)`, "2"}, // d_date[] is read by no case before it
		{`t.Keep(db, "a\x00b", nil)`, zero},
		{`t.Keep(db, "a", []string{"b", "\x00"})`, zero},
		{`t.KeepOrNull(db, []*string{nil, rt.Ptr("\x00")})`, zero},
		{`t.Keep(db, "a\u0001b", []string{""})`, "2"},
	}
	if os.Getenv("KOGO_STRESS_SLOW") != "" { // two minutes: Postgres counts 2,200,000,000 rows
		cases = append(cases, [2]string{`t.Many(db)`, "panics: execute changed 2200000000 rows, more than the Int it gives can hold (spec.md 10.4)"})
	}
	expectWith(t, withDB, "defer drop()\n", cases, `package t

import kogo.time.LocalDate

fun grid(db: Db): List<Int> = db.tx { tx -> tx.querySingle<List<Int>>(sql"""select '{{1,2},{3,4}}'::int[] as "x!" """) }
fun ints(db: Db): List<Int> = db.tx { tx ->
    tx.querySingle<List<Int>>(sql"""select array_agg(t.id) as "ids!" from (select 1) one left join account t on false""")
}
fun intsOrNull(db: Db): List<Int?> = db.tx { tx ->
    tx.querySingle<List<Int?>>(sql"""select array_agg(t.id) as "ids!" from (select 1) one left join account t on false""")
}
fun lower(db: Db): List<Int> = db.tx { tx -> tx.querySingle<List<Int>>(sql"""select '[5:6]={1,2}'::int[] as "x!" """) }
fun nums(db: Db): List<Decimal> = db.tx { tx -> tx.querySingle<List<Decimal>>(sql"""select '{0.00,1.50}'::d_numeric[] as "x!" """) }
fun uuids(db: Db): List<Uuid> = db.tx { tx ->
    tx.querySingle<List<Uuid>>(sql"""select '{00000000-0000-0000-0000-000000000002}'::d_uuid[] as "x!" """)
}

// A domain over an array, an array of citext, and a NULL for a List?.
data class Tagged(val tags: List<String>, val folded: List<String>, val none: List<String>?)
fun tags(db: Db): String = db.tx { tx ->
    val t = tx.querySingle<Tagged>(sql"""select '{a,b}'::d_tags as "tags!", '{Fold}'::citext[] as "folded!", null::text[] as none""")
    "${t.tags} ${t.folded} ${t.none}"
}

// A composite type whose fields are such arrays, which come as columns.
data class TagSet(val tags: List<String>?, val names: List<String>?)
data class Sets(val set: TagSet)
fun tagSets(db: Db): TagSet = db.tx { tx ->
    tx.querySingle<Sets>(sql"""select row('{a,b}', '{c}')::tag_set as "set!" """).set
}

// An array of a domain initdb makes, below Postgres's first oid.
fun cardinals(db: Db): List<Int> = db.tx { tx ->
    tx.querySingle<List<Int>>(sql"""select '{1,2}'::information_schema.cardinal_number[] as "x!" """)
}
fun sendDates(db: Db): Int = db.tx { tx ->
    val ds = tx.querySingle<List<LocalDate>>(sql"""select '{2026-01-02,2026-01-03}'::date[] as "x!" """)
    tx.execute(sql"select from unnest($ds::d_date[])")
}

fun keep(db: Db, name: String, more: List<String>): Int = db.tx { tx ->
    tx.execute(sql"select from unnest(array[$name] || $more::text[])")
}
fun keepOrNull(db: Db, more: List<String?>): Int = db.tx { tx -> tx.execute(sql"select from unnest($more::text[])") }
fun many(db: Db): Int = db.tx { tx -> tx.execute(sql"select from generate_series(1, 50000) a, generate_series(1, 44000) b") }
`)
}
