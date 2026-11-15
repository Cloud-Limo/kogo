// The driver of TestEmittedGoRuns: it calls the Go emitted for the
// examples and prints what each gives. ROOT stands for the import
// path of the directory the Go is written to.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"

	"ROOT/cusoon/invoice"
	"ROOT/cusoon/model"
	"ROOT/example"
	"ROOT/example/markers"
	"ROOT/example/ops"
	"kogo/internal/testdb"
	"kogo/prelude/kogo"
	"kogo/prelude/kogo/rpc"
	ktime "kogo/prelude/kogo/time"
	"kogo/rt"
)

// The rows that examples 10 to 19, 21 and 22 read.
const seed = `
insert into business (id, slug, legal_name, created_at, abn) values
    ('00000000-0000-0000-0000-000000000001', 'acme', 'Acme Pty Ltd', '2026-01-02 03:04:05+00', '51824753556'),
    ('00000000-0000-0000-0000-000000000002', 'bolt', 'Bolt Couriers', '2026-01-02 03:04:05+00', null);
insert into person (id, name, email) values ('00000000-0000-0000-0000-000000000003', 'Ann', 'ann@example.com');
insert into admin_grant (person_id, business_id) select p.id, b.id from person p, business b;
insert into tenant (id, business_id, slug) values
    ('00000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', 'acme-store');
insert into tenant_driver (tenant_id, driver_id) select t.id, p.id from tenant t, person p;
insert into supply_agreement (operator_id, supplier_id, driver_share_percent) values
    ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 40);
insert into invoice (id, number, total_cents, status) values
    ('00000000-0000-0000-0000-000000000005', 'INV-1', 1000, 'paid'),
    ('00000000-0000-0000-0000-000000000006', 'INV-2', null, 'sent');
`

// in runs f in a transaction of db, as db.tx { } does.
func in[T any](db kogo.Db, f func(kogo.Tx) T) T { return kogo.Db_Tx_Boolean_TxToA(db, nil, f) }

// show prints a result as Kogo's toString() writes it.
func show(what string, value any) { fmt.Println(what+":", rt.ToString(value)) }

// panics prints the panic that f ends in.
func panics(what string, f func()) {
	defer func() { fmt.Println(what, "panics:", recover()) }()
	f()
}

func main() {
	// 01: what a job is worth and what the supplier is paid.
	extras := []model.BookingExtraCharge{{Amount: 500}}
	booking := model.Booking{
		Price: &model.Price{Amount: 10000}, SharePercentWhenGiven: rt.Ptr[int32](80),
		HadCardWhenGiven: rt.Ptr(true), StripeCardId: rt.Ptr("card_1"),
	}
	show("worth by card", model.JobWorth(booking, extras, true))
	show("worth", model.JobWorth(booking, extras, false))
	show("worth with no price", model.JobWorth(model.Booking{}, extras, true))
	show("share", model.ShareOf(10237, 80))
	show("pay of a share", model.SupplierPay(rt.Ptr[int64](10237), 80, nil))
	show("pay when fixed", model.SupplierPay(rt.Ptr[int64](10237), 80, rt.Ptr[int64](5000)))
	show("pay of nothing", model.SupplierPay(nil, 80, nil))
	show("booking's pay", model.Booking_SupplierPay(booking, extras))
	noCard := booking
	noCard.HadCardWhenGiven = nil
	show("booking's pay with no card", model.Booking_SupplierPay(noCard, extras))
	noShare := booking
	noShare.SharePercentWhenGiven = nil
	show("booking's pay with no share", model.Booking_SupplierPay(noShare, extras))

	// 02: fixed-point money and a data class.
	show("discounted", invoice.DiscountedCents(10000, 2.5))
	show("stack", invoice.Stack([]int64{10000, 5000}, 2.5, "Card"))
	show("stack of nothing off", invoice.Stack([]int64{10000}, 0, "None"))

	// 03: strings, a regex and list functions.
	show("abn", model.IsValidAbn("51824753556"))
	show("abn off by one", model.IsValidAbn("51824753557"))
	show("abn of letters", model.IsValidAbn("abc"))
	show("formatted", model.FormatAbn("51824753556"))
	show("too short to format", model.FormatAbn("123"))

	// 04 and 05: a sealed interface and an enum.
	show("ok", example.Describe(example.Outcome_Ok[int32]{Value: 7}))
	show("refused", example.Describe(example.Outcome_Refused{Message: "late"}))
	show("issued can pay", example.CanPay(example.InvoiceStatus_issued))
	show("paid can pay", example.CanPay(example.InvoiceStatus_paid))

	// 06: absence.
	show("nobody", example.Contact(nil))
	show("ann", example.Contact(&example.Person{Name: "Ann", Mobile: rt.Ptr("  0400 111 222 ")}))
	show("blank mobile", example.Contact(&example.Person{Name: "Bob", Mobile: rt.Ptr("   ")}))
	show("no mobile", example.Contact(&example.Person{Name: "Cy"}))
	show("mobile", example.MobileOf(example.Person{Name: "Ann", Mobile: rt.Ptr("0400")}))
	panics("mobile of none", func() { example.MobileOf(example.Person{Name: "Cy"}) })

	// 07: either { }, ensure, raise and bind; the first failure wins.
	show("signup", example.Message(example.Signup(" Ann@Example.COM ", 30)))
	show("signup with a bad email", example.Message(example.Signup("ann", 30)))
	show("signup too young", example.Message(example.Signup("a@b", 17)))
	show("signup with both", example.Message(example.Signup("ann", 17)))
	show("checked email", example.CheckedEmail("A@B"))

	// 09: a failure converted at each layer.
	show("load", example.Load(1))
	show("load of a missing item", example.Load(2))
	show("title of a missing item", example.Title(2))
	show("title with missing items after", example.Title(1))

	// 20: an interface, a generic fun, a loop and time.
	var card example.Card = example.FlatCard{Label: "flat", Cents: 500}
	show("card", card)
	show("fee", card.Fee(12))
	show("label", card.GetLabel())
	show("first free", example.FirstFree([]int32{1, 2, 3}, func(n int32) bool { return n < 3 }))
	show("none free", example.FirstFree([]int32{1, 2}, func(int32) bool { return true }))
	taken := map[string]bool{"acme": true, "acme-2": true}
	show("slug", example.FreeSlug("acme", func(s string) bool { return taken[s] }))
	clock := ktime.FixedClock(ktime.Instant{Seconds: 1794753000}) // 2026-11-15T14:30:00Z
	show("today in Sydney", example.TodayIn(clock, "Australia/Sydney"))
	show("today in New York", example.TodayIn(clock, "America/New_York"))

	// 10 to 19, 21 and 22: SQL, against a schema of its own in the test
	// database, through a Db made as a server would make one.
	ctx := context.Background()
	pool, drop := testdb.Open(ctx, "../../testdata/schema.sql")
	defer drop()
	if _, err := pool.Exec(ctx, seed); err != nil {
		panic(err)
	}
	db := kogo.NewDb(ctx, pool)
	acme, bolt, ann, nobody := kogo.Uuid{15: 1}, kogo.Uuid{15: 2}, kogo.Uuid{15: 3}, kogo.Uuid{15: 9}
	paid, sent := kogo.Uuid{15: 5}, kogo.Uuid{15: 6}
	in(db, func(tx kogo.Tx) rt.Unit {
		show("suppliers", example.SuppliersOf(tx, acme))
		show("abn of acme", example.AbnOf(tx, acme))
		show("abn of bolt", example.AbnOf(tx, bolt))
		show("abn of nobody", example.AbnOf(tx, nobody))
		show("legal name", example.LegalName(tx, bolt))
		show("ann administers acme", example.Administers(tx, ann, acme))
		show("nobody administers acme", example.Administers(tx, nobody, acme))
		show("business", example.BusinessBySlug(tx, "ACME"))
		show("business of no slug", example.BusinessBySlug(tx, "none"))
		show("administered", example.Administered_Tx_Uuid(tx, ann))
		show("drivers", example.LinkedDrivers(tx, nil))
		show("inactive drivers", example.LinkedDrivers(tx, rt.Ptr(false)))
		show("invoice", example.Invoice_Tx_Uuid_Boolean(tx, paid, false))
		show("invoice for update", example.Invoice_Tx_Uuid_Boolean(tx, sent, true))
		show("no invoice", example.Invoice_Tx_Uuid_Boolean(tx, nobody, true))
		show("paid invoices", example.PaidCount(tx))
		show("business by id", markers.Business_Tx_Uuid(tx, acme))
		show("held", markers.LegalNameOf(*markers.Hold(tx, acme)))
		show("hold of nobody", markers.Hold(tx, nobody))
		show("locked", example.LockInvoice(tx, 822))
		return rt.Unit{}
	})
	panics("legal name of nobody", func() { in(db, func(tx kogo.Tx) string { return example.LegalName(tx, nobody) }) })

	// 16: the check that fails is a panic, which rolls its update back.
	account := func(ids ...kogo.Uuid) rt.Unit {
		return in(db, func(tx kogo.Tx) rt.Unit {
			return example.AccountFees(tx, ids, "REF-1", ktime.Instant{Seconds: 1794753000})
		})
	}
	panics("fees of a missing invoice", func() { account(paid, sent, nobody) })
	show("fees, the first rolled back", account(paid, sent))

	// 13 and 18: an upsert, and a transaction that fails as a value.
	invite := func(share *int32) example.Invite {
		return in(db, func(tx kogo.Tx) example.Invite {
			return example.UpsertInvite(tx, acme, example.InviteKind_outside, ann, ann, share)
		})
	}
	first := invite(nil)
	show("invite's kind", first.Kind)
	show("invite's share", first.DriverSharePercent)
	show("accept with no share", example.Accept(db, first.Id, bolt))
	second := invite(rt.Ptr[int32](25))
	show("invite again", second.DriverSharePercent)
	show("the same invite", second.Id == first.Id)
	show("accept", example.Accept(db, first.Id, bolt))
	show("accept again", example.Accept(db, first.Id, bolt))
	show("suppliers after", in(db, func(tx kogo.Tx) []example.SupplierRow { return example.SuppliersOf(tx, acme) }))

	// 22: the table of an api list, served as main serves it, each
	// request from one address.
	h := rpc.NewHandler(ops.Ops, pool, ops.OpsEnv{MaxLooks: 2}, slog.New(slog.DiscardHandler))
	post := func(what, op, body string) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/"+op, strings.NewReader(body)))
		fmt.Println(strings.TrimSpace(fmt.Sprintf("%s: %d %s", what, w.Code, w.Body)))
	}
	post("invoices", "invoices", "")
	post("paid with no total", "setStatus", `{"number": "INV-2", "status": "paid"}`)
	post("invoices after the refusal", "invoices", "")
	post("back to a draft", "setStatus", `{"number": "INV-2", "status": "draft"}`)
	post("no status", "setStatus", `{"number": "INV-2"}`)
	post("not a status", "setStatus", `{"number": "INV-2", "status": "void"}`)
	post("no such operation", "cancel", "{}")
	post("look up", "lookUp", `{"number": "INV-1"}`)
	post("look up again", "lookUp", `{"number": "INV-1"}`)
	post("look up a missing invoice", "lookUp", `{"number": "INV-9"}`)
	post("look up another missing invoice", "lookUp", `{"number": "INV-8"}`)
	post("look up after two misses", "lookUp", `{"number": "INV-1"}`)
}
