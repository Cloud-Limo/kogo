package emit

import "testing"

// What the driver of a program that takes a Db declares: db, over a
// schema of its own in the test database (internal/testdb).
const withDB = `
import (
"context"
"github.com/Cloud-Limo/kogo/internal/testdb"
"github.com/Cloud-Limo/kogo/prelude/kogo"
)
var ctx = context.Background()
var pool, drop = testdb.Open(ctx, "../testdata/schema.sql")
var db = kogo.NewDb(ctx, pool)
`

// between runs the serializable invoiced once more inside its first
// run, so that the two conflict.
const between = `
var runs int32
func between() rt.Unit {
if runs++; runs == 1 {
t.Invoiced(db, func() rt.Unit { return rt.Unit{} })
}
return rt.Unit{}
}
`

const seed = "defer drop()\nif _, err := pool.Exec(ctx, `" + `
insert into business (id, slug, legal_name) values
    ('00000000-0000-0000-0000-000000000001', 'acme', 'Acme'), ('00000000-0000-0000-0000-000000000002', 'bolt', 'Bolt');
insert into tenant (id, business_id, slug) values
    ('00000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', 'Acme-Store');
insert into quote (id, total, deposit) values
    ('00000000-0000-0000-0000-00000000000b', (1500, 'AUD'), null), ('00000000-0000-0000-0000-00000000000c', (2000, 'NZD'), (500, null));
insert into every_type values (true, -7, 9000000000, 2.5, 12.50, 'b', 'Fold', 'c8', '00000000-0000-0000-0000-00000000000a',
    '2026-11-15 14:30:00.123456+00', '2026-02-28', '09:05:00', '1 day 02:00:00.5', 'outside', '{a,b}', 40, null, 'p');
` + "`); err != nil {\npanic(err)\n}\n"

// A transaction commits when its block finishes and rolls back on a
// failure or a panic; a serializable one is run again when Postgres
// refuses it for a conflict; a savepoint rolls back alone (spec.md
// 10.7). The rows that are there afterwards say which happened.
func TestTransactions(t *testing.T) {
	expectWith(t, withDB+between, seed, [][2]string{
		{`t.Failing(db, "ok")`, "Either.Right(1)"},
		{`t.Failing(db, "bad")`, "Either.Left(refused bad)"},
		{`t.Wider(db)`, "Either.Right(1)"},
		{`t.Panicking(db, "bug")`, "panics: broken by bug"},
		{`t.Nested(db, "kept")`, "Either.Right(2)"},
		{`t.Nested(db, "undone")`, "Either.Left(no undone)"},
		{`t.InScope(db, "bad too")`, "Either.Left(no bad too)"},
		{`t.Names(db)`, "[kept, ok, outer of kept, outer of undone, wider]"},
		{`t.Invoiced(db, between)`, "1"},
		{`runs`, "2"},
		{`t.Totals(db)`, "[0, 1]"},
		{`t.Token(db)`, "20 true"},
	}, `package t

// In a fun on Tx, a call is written on this.
fun Tx.add(name: String): Int {
    val id = newId()
    return this.execute(sql"insert into person (id, name, email) values ($id, $name, ${name})")
}

fun names(db: Db): List<String> = db.tx { tx -> tx.query<String>(sql"select name from person order by name") }

// The block of this db.tx is a raise scope: the fun returns an Either.
fun failing(db: Db, name: String): Either<String, Int> = db.tx { tx ->
    val added = tx.add(name)
    ensure(name != "bad") { "refused $name" }
    added
}

// The block gives an Int where db.tx gives an Either<String, Int?>.
fun wider(db: Db): Either<String, Int?> = db.tx { tx -> tx.add("wider") }

fun panicking(db: Db, name: String): Int = db.tx { tx ->
    val added = tx.add(name)
    check(name != "bug") { "broken by $name" }
    added
}

// A failure inside atomically undoes what its block did, and no more.
fun nested(db: Db, name: String): String = db.tx { tx ->
    tx.add("outer of $name")
    val inner = either<String, Int> {
        tx.atomically {
            tx.add(name)
            ensure(name != "undone") { "no $name" }
            2
        }
    }
    "$inner"
}

// A db.tx that is no raise scope, inside one: a failure passes it.
fun inScope(db: Db, name: String): String {
    val r = either<String, Int> {
        db.tx { tx ->
            tx.add(name)
            raise("no $name")
        }
    }
    return "$r"
}

// Two of these that both count before either inserts cannot both
// commit: neither could have run wholly before the other.
fun invoiced(db: Db, between: () -> Unit): Long = db.tx(serializable = true) { tx ->
    val n = tx.querySingle<Long>(sql"""select count(*) as "n!" from invoice""")
    between()
    val id = newId()
    val number = "INV-$id"
    tx.execute(sql"insert into invoice (id, number, total_cents) values ($id, $number, $n)")
    n
}

fun totals(db: Db): List<Long?> = db.tx { tx -> tx.query<Long?>(sql"select total_cents from invoice order by total_cents") }

// A session's token is made, stored and read back the same.
fun token(db: Db): String = db.tx { tx ->
    val token = randomString(20)
    val id = newId()
    tx.execute(sql"insert into person (id, name, email) values ($id, $token, ${"$token@x"})")
    "${token.length} ${tx.querySingle<String>(sql"select name from person where id = $id") == token}"
}
`)
}

// Rows fill R by the checker's facts, and the row count rules of the
// calls hold (spec.md 10.2, 10.3, 10.5): a nullable nested data class
// is null when its columns null only through the outer join are,
// whatever its others hold, a column named "name!" panics on a null,
// a list among them, and a composite type fills a data class by its
// fields, whatever its name.
func TestRows(t *testing.T) {
	sites := "[Site(legalName=Acme, shop=Shop(id=00000000-0000-0000-0000-000000000004, businessId=00000000-0000-0000-0000-000000000001, slug=Acme-Store)), Site(legalName=Bolt, shop=null)]"
	expectWith(t, withDB, seed, [][2]string{
		{`t.Sites(db)`, sites},
		{`t.WholeSites(db)`, sites},
		{`t.HalfSites(db)`, "panics: column shop_slug is null, and fills a property that is not nullable"},
		{`t.Notes(db)`, "[Noted(legalName=Acme, note=Note(id=00000000-0000-0000-0000-000000000004, text=Acme-Store)), Noted(legalName=Bolt, note=null)]"},
		{`t.Largest(db, 1000)`, "2000"},
		{`t.Largest(db, 5000)`, "panics: can't scan into dest[0] (col: m!): cannot scan NULL into *int64"},
		{`t.NoNames(db)`, "panics: column names is null, and fills a property that is not nullable"},
		{`t.Quotes(db)`, "[Quote(id=00000000-0000-0000-0000-00000000000b, total=Amount(cents=1500, currency=AUD), deposit=null), " +
			"Quote(id=00000000-0000-0000-0000-00000000000c, total=Amount(cents=2000, currency=NZD), deposit=Amount(cents=500, currency=null))]"},
		{`t.HeldIn(db, "NZD")`, "Held(quote=Quote(id=00000000-0000-0000-0000-00000000000c, total=Amount(cents=2000, currency=NZD), deposit=Amount(cents=500, currency=null)))"},
		{`t.HeldIn(db, "USD")`, "null"},
		{`t.HeldIn(db, "%")`, "panics: queryOne read more than one row, and reads one or none: select (\"quote\") is not distinct from null, (\"quote\").\"id\", " +
			"((\"quote\").\"total\") is not distinct from null, ((\"quote\").\"total\").\"cents\", ((\"quote\").\"total\").\"currency\", " +
			"((\"quote\").\"deposit\") is not distinct from null, ((\"quote\").\"deposit\").\"cents\", ((\"quote\").\"deposit\").\"currency\" " +
			"from (select q as quote from quote q where (q.total).currency like $1\\n) _"},
		{`t.Nulls(db)`, "[Deposit(n=1, deposit=Amount(cents=null, currency=null)), Deposit(n=2, deposit=null)]"},
		{`t.NoShop(db)`, "panics: column shop is null, and fills a property that is not nullable"},
		{`t.Quoted(db)`, "Priced(price=Price(cents=250))"},
		{`t.Slug(db, "ACME-store")`, "Acme-Store"},
		{`t.Slug(db, "none")`, "panics: querySingle read no row, and reads exactly one: select \"slug\" from (select slug from tenant where slug = $1\\n) _"},
		{`t.Has(db, "acme-store")`, "true"},
		{`t.Has(db, "none")`, "false"},
	}, `package t

data class Shop(val id: Uuid, val businessId: Uuid, val slug: String)
data class Site(val legalName: String, val shop: Shop?)

// A nullable nested data class from the columns of its prefix.
fun sites(db: Db): List<Site> = db.tx { tx ->
    tx.query<Site>(sql"""
        select b.legal_name, t.id as shop_id, t.business_id as shop_business_id, t.slug as shop_slug
        from business b left join tenant t on t.business_id = b.id order by b.legal_name
    """)
}

// The same from a table's whole row, which the outer join nulls.
fun wholeSites(db: Db): List<Site> = db.tx { tx ->
    tx.query<Site>(sql"select b.legal_name, t as shop from business b left join tenant t on t.business_id = b.id order by b.legal_name")
}

// A shop that is there, by its id, with a slug asserted that is not.
fun halfSites(db: Db): List<Site> = db.tx { tx ->
    tx.query<Site>(sql"""
        select b.legal_name, t.id as shop_id, t.business_id as shop_business_id, nullif(t.slug, 'Acme-Store') as "shop_slug!"
        from business b left join tenant t on t.business_id = b.id order by b.legal_name
    """)
}

// A class told there by its id, beside a column that is never null.
data class Note(val id: Uuid, val text: String)
data class Noted(val legalName: String, val note: Note?)

fun notes(db: Db): List<Noted> = db.tx { tx ->
    tx.query<Noted>(sql"""
        select b.legal_name, t.id as note_id, coalesce(t.slug::text, '') as note_text
        from business b left join tenant t on t.business_id = b.id order by b.legal_name
    """)
}

data class Amount(val cents: Long?, val currency: String?)
data class Quote(val id: Uuid, val total: Amount, val deposit: Amount?)
data class Held(val quote: Quote)

fun largest(db: Db, over: Long): Long = db.tx { tx ->
    tx.querySingle<Long>(sql"""select max((total).cents) as "m!" from quote where (total).cents > $over""")
}

fun noNames(db: Db): List<String> = db.tx { tx ->
    tx.querySingle<List<String>>(sql"""select array_agg(name) as "names!" from person where false""")
}

data class Price(val cents: Long?)
data class Priced(val price: Price?)

fun quoted(db: Db): Priced = db.tx { tx -> tx.querySingle<Priced>(sql"""select row(250)::"Price" as price""") }

// A column of a composite type, and a row that holds one.
fun quotes(db: Db): List<Quote> = db.tx { tx -> tx.query<Quote>(sql"select id, total, deposit from quote order by id") }

fun heldIn(db: Db, currency: String): Held? = db.tx { tx ->
    tx.queryOne<Held>(sql"select q as quote from quote q where (q.total).currency like $currency")
}

// A row of nulls, and a null.
data class Deposit(val n: Int, val deposit: Amount?)

fun nulls(db: Db): List<Deposit> = db.tx { tx ->
    tx.query<Deposit>(sql"""select n as "n!", case when n = 1 then row(null, null)::amount end as deposit from generate_series(1, 2) n order by n""")
}

// A whole row asserted there, and absent.
data class Shopped(val shop: Shop)

fun noShop(db: Db): Shopped = db.tx { tx ->
    tx.querySingle<Shopped>(sql"""select t as "shop!" from business b left join tenant t on false where b.slug = 'acme'""")
}

fun slug(db: Db, slug: String): String = db.tx { tx -> tx.querySingle<String>(sql"select slug from tenant where slug = $slug") }
fun has(db: Db, slug: String): Boolean = db.tx { tx -> tx.exists(sql"select 1 from tenant where slug = $slug") }
`)
}

// Every type of spec.md 10.4 is read from Postgres and sent back to
// it as it was: a row read, inserted again from its properties and
// read again is equal to the first. A T? is sent as NULL and an empty
// List as an empty array.
func TestValues(t *testing.T) {
	expectWith(t, withDB, seed, [][2]string{
		{`t.Everything(db)`, "[Every(flag=true, small=-7, big=9000000000, ratio=2.5, amount=12.50, body=b, folded=Fold, code=c8, " +
			"id=00000000-0000-0000-0000-00000000000a, at=2026-11-15T14:30:00.123456Z, day=2026-02-28, clock=09:05, span=PT26H0.5S, " +
			"kind=outside, tags=[a, b], share=40, note=null, pending=p)]"},
		{`t.Again(db)`, "1"},
		{`func() bool { rows := t.Everything(db); return len(rows) == 2 && rt.Equal(rows[0], rows[1]) }()`, "true"},
		{`t.Untagged(db)`, "2"},
		{`t.Tags(db)`, "[[], []]"},
		{`t.Zero(db)`, "0.00"},
		{`t.Zeros(db)`, "[0.00, 0, 1.50]"},
		{`t.Absent(db)`, "[Listed(n=1, every=null)]"},
	}, `package t

import kogo.time.Duration
import kogo.time.Instant
import kogo.time.LocalDate
import kogo.time.LocalTime

enum class Kind { in_house, outside }

data class Every(
    val flag: Boolean, val small: Int, val big: Long, val ratio: Double, val amount: Decimal, val body: String,
    val folded: String, val code: String, val id: Uuid, val at: Instant, val day: LocalDate, val clock: LocalTime,
    val span: Duration, val kind: Kind, val tags: List<String>, val share: Int, val note: String?, val pending: String?,
)

fun everything(db: Db): List<Every> = db.tx { tx -> tx.query<Every>(sql"select * from every_type") }

// A whole row with a list, absent.
data class Listed(val n: Int, val every: Every?)

fun absent(db: Db): List<Listed> = db.tx { tx ->
    tx.query<Listed>(sql"""select 1 as "n!", e as every from (select 1) o left join every_type e on false""")
}

fun again(db: Db): Int = db.tx { tx ->
    val e = tx.querySingle<Every>(sql"select * from every_type")
    tx.execute(sql"""
        insert into every_type values (${e.flag}, ${e.small}, ${e.big}, ${e.ratio}, ${e.amount}, ${e.body}, ${e.folded}, ${e.code},
            ${e.id}, ${e.at}, ${e.day}, ${e.clock}, ${e.span}, ${e.kind}, ${e.tags}, ${e.share}, ${e.note}, ${e.pending})
    """)
}

fun untagged(db: Db): Int = db.tx { tx ->
    val none = listOf<String>()
    tx.execute(sql"update every_type set tags = $none")
}

fun tags(db: Db): List<List<String>> = db.tx { tx -> tx.query<List<String>>(sql"select tags from every_type") }

// A zero keeps its scale, alone and in a list.
fun zero(db: Db): Decimal = db.tx { tx -> tx.querySingle<Decimal>(sql"""select 0.00::numeric(4, 2) as "z!" """) }
fun zeros(db: Db): List<Decimal> = db.tx { tx -> tx.querySingle<List<Decimal>>(sql"""select array[0.00, 0, 1.50] as "z!" """) }
`)
}

// A ByteArray is read from a bytea and sent to one as it was, a zero
// byte and none at all included; a ByteArray? is NULL, and a NULL is
// refused where a ByteArray is read (spec.md 10.4).
func TestBytes(t *testing.T) {
	expectWith(t, withDB, "defer drop()\n", [][2]string{
		{`t.Keep(db, kogo.NewId(), kogo.ByteArray("a\x00\xff"), nil)`, "1"},
		{`t.Keep(db, kogo.NewId(), kogo.ByteArray(nil), rt.Ptr(kogo.ByteArray("d")))`, "1"}, // as a binding may give none
		{`func() string { var s string; for _, l := range t.Kept(db) { s += fmt.Sprintf("%q %v;", l.Eml, l.Draft) }; return s }()`, `"" &[100];"a\x00\xff" <nil>;`},
		{`t.Drafts(db)`, "panics: can't scan into dest[0] (col: draft!): null is not a ByteArray"},
	}, `package t

data class Letter(val eml: ByteArray, val draft: ByteArray?)

fun keep(db: Db, id: Uuid, eml: ByteArray, draft: ByteArray?): Int = db.tx { tx ->
    tx.execute(sql"insert into letter (id, eml, draft) values ($id, $eml, $draft)")
}

fun kept(db: Db): List<Letter> = db.tx { tx -> tx.query<Letter>(sql"select eml, draft from letter order by eml") }

fun drafts(db: Db): List<ByteArray> = db.tx { tx -> tx.query<ByteArray>(sql"""select draft as "draft!" from letter""") }
`)
}

// A ByteArray where check cannot see it compared, in a contains, an in,
// a Set or behind a type parameter, panics, since Kotlin compares it by
// identity and the bytes would answer otherwise; inside a data class's
// text it is Kotlin's signed bytes (spec.md 3; kotlinc 2.4.21 gives
// F(name=x, data=[97, 0, -1])).
func TestBytesIdentity(t *testing.T) {
	const bytes = "\nimport \"github.com/Cloud-Limo/kogo/prelude/kogo\"\nvar a, b = kogo.ByteArray(\"a\\x00\\xff\"), kogo.ByteArray(\"a\\x00\\xff\")\n"
	identity := "panics: a ByteArray cannot be compared: Kotlin compares it by identity"
	expectWith(t, bytes, "", [][2]string{
		{"t.InList(a, b)", identity},
		{"t.Contains(a, b)", identity},
		{"t.InSet(a, b)", identity},
		{"t.Same(a, b)", identity},
		{"t.Boxed(a, b)", identity},
		{"t.Text(a)", "F(name=x, data=[97, 0, -1])"},
	}, `package t

data class F(val name: String, val data: ByteArray)

fun <T> same(a: T, b: T): Boolean = a == b

fun inList(a: ByteArray, b: ByteArray): Boolean = b in listOf(a)
fun contains(a: ByteArray, b: ByteArray): Boolean = listOf(a).contains(b)
fun inSet(a: ByteArray, b: ByteArray): Boolean = b in setOf(a)
fun boxed(a: ByteArray, b: ByteArray): Boolean = same(F("x", a), F("x", b))
fun text(a: ByteArray): String = "${F("x", a)}"
`)
}

// A read takes each column by the name kogo check saw, so a table
// whose columns the database served orders otherwise, as after a
// column is dropped and added again, or which has one more, is read
// right (spec.md 10.3): read by position, the first read after the
// move would panic on its cached plan and the second swap owner and
// payee. Each alter runs on the schema the program reads, not the one
// checked.
func TestMovedColumns(t *testing.T) {
	alter := `
func alter(sql string) string {
if _, err := pool.Exec(ctx, sql); err != nil {
panic(err)
}
return "altered"
}
`
	row := "[Account(id=1, owner=alice, payee=bob)]"
	held := "[Held(account=Account(id=1, owner=alice, payee=bob))]"
	maybe := "[Maybe(n=1, account=Account(id=1, owner=alice, payee=bob)), Maybe(n=2, account=null)]"
	expectWith(t, withDB+alter, "defer drop()\n", [][2]string{
		{`t.Open(db)`, "Account(id=1, owner=alice, payee=bob)"},
		{`t.All(db)`, row},
		{`t.Whole(db)`, held},
		{`t.Joined(db)`, maybe},
		{`alter("alter table account add column was text; update account set was = owner; " +
			"alter table account drop column owner; alter table account rename column was to owner")`, "altered"},
		{`t.All(db)`, row},
		{`t.All(db)`, row},
		{`t.Whole(db)`, held},
		{`t.Joined(db)`, maybe},
		{`alter("alter table account add column note text")`, "altered"},
		{`t.All(db)`, row},
		{`t.Pay(db, "carol")`, "Account(id=1, owner=alice, payee=carol)"},
	}, `package t

data class Account(val id: Int, val owner: String, val payee: String)

fun open(db: Db): Account = db.tx { tx ->
    tx.querySingle<Account>(sql"insert into account (id, owner, payee) values (1, 'alice', 'bob') returning *")
}

fun all(db: Db): List<Account> = db.tx { tx -> tx.query<Account>(sql"select * from account order by id") }

fun pay(db: Db, payee: String): Account = db.tx { tx ->
    tx.querySingle<Account>(sql"update account set payee = $payee where id = 1 returning *")
}

// A whole row as one column, there and absent.
data class Held(val account: Account)
data class Maybe(val n: Int, val account: Account?)

fun whole(db: Db): List<Held> = db.tx { tx -> tx.query<Held>(sql"select a as account from account a order by a.id") }

fun joined(db: Db): List<Maybe> = db.tx { tx ->
    tx.query<Maybe>(sql"""select n as "n!", a as account from generate_series(1, 2) n left join account a on a.id = n order by n""")
}
`)
}
