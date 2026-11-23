// The driver of TestEmittedGoRuns: it calls the Go emitted for the
// examples and prints what each gives. ROOT stands for the import
// path of the directory the Go is written to.
package main

import (
	"bytes"
	"context"
	"fmt"
	goio "io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"ROOT/cusoon/invoice"
	"ROOT/cusoon/model"
	"ROOT/example"
	"ROOT/example/depot"
	"ROOT/example/gates"
	"ROOT/example/letters"
	"ROOT/example/markers"
	"ROOT/example/ops"
	"ROOT/example/origin"
	"ROOT/example/parcels"
	"ROOT/example/reply"
	"github.com/Cloud-Limo/kogo/internal/testdb"
	"github.com/Cloud-Limo/kogo/prelude/kogo"
	khttp "github.com/Cloud-Limo/kogo/prelude/kogo/http"
	"github.com/Cloud-Limo/kogo/prelude/kogo/io"
	kmail "github.com/Cloud-Limo/kogo/prelude/kogo/mail"
	"github.com/Cloud-Limo/kogo/prelude/kogo/rpc"
	ktime "github.com/Cloud-Limo/kogo/prelude/kogo/time"
	"github.com/Cloud-Limo/kogo/prelude/kogo/work"
	"github.com/Cloud-Limo/kogo/rt"
	"github.com/jackc/pgx/v5"
)

// The rows that examples 10 to 19, 21, 22, 24, 27 and 28 read.
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
insert into shop (domain) values ('https://shop.example');
insert into session (id, csrf_token, expires_at) values
    ('00000000-0000-0000-0000-00000000000a', 'live token', '2027-01-01 00:00:00+00'),
    ('00000000-0000-0000-0000-00000000000b', 'old token', '2026-01-01 00:00:00+00');
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

// inUse is the console of 24's actions: it prints, before what an
// action prints, how many of the pool's connections are in use then,
// which is none where no transaction is open.
type inUse func() int32

func (n inUse) Write(b []byte) (int, error) {
	fmt.Printf("with %d transactions open, %s", n(), b)
	return len(b), nil
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
	var h http.Handler = rpc.NewHandler(ops.Ops, pool, ops.OpsEnv{MaxLooks: 2}, slog.New(slog.DiscardHandler))
	cookie := ""
	post := func(what, op, body string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/"+op, strings.NewReader(body))
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: cookie, Value: "1"})
		}
		h.ServeHTTP(w, r)
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

	// 24: a checked list of gated rows, each stage refusing in turn.
	// The address has two looks, which its gate refuses; hooks, an
	// unchecked list, is a value that no handler takes.
	var _ rpc.Unserved[gates.DeskEnv] = gates.Hooks
	console := io.NewConsole(os.Stdin, inUse(func() int32 { return pool.Stat().AcquiredConns() }))
	desk := func(printer string) {
		h = rpc.NewHandler(gates.Desk, pool, gates.DeskEnv{Printer: printer, Console: console}, slog.New(slog.DiscardHandler))
	}
	desk("")
	post("refused by the check", "unpaid", "")
	post("the check before the name", "cancel", "")
	cookie = "desk"
	post("refused by a shared gate", "unpaid", "")
	post("ungated, after the check", "looks", "")
	if _, err := pool.Exec(ctx, "delete from look; update invoice set status = 'sent' where number = 'INV-1'"); err != nil {
		panic(err)
	}
	post("unpaid", "unpaid", "")
	post("refused by the work's step", "markPaid", `{"number": "INV-2"}`)
	post("refused by the work", "statusOf", `{"number": "INV-9"}`)
	post("refused by work that is an action", "testPrinter", "")
	desk("front")
	post("sent", "sendInvoice", `{"number": "INV-1"}`)
	post("mark paid", "markPaid", `{"number": "INV-1"}`)
	post("mark paid again", "markPaid", `{"number": "INV-1"}`)
	post("status", "statusOf", `{"number": "INV-1"}`)
	post("work that is an action", "testPrinter", "")

	// 25: an answer that sets cookies, and refusals that set none.
	h2 := rpc.NewHandler(reply.Session, pool, reply.SessionEnv{Secure: true}, slog.New(slog.DiscardHandler))
	cookies := func(what, op, cookie, body string) {
		w, r := httptest.NewRecorder(), httptest.NewRequest("POST", "/"+op, strings.NewReader(body))
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		h2.ServeHTTP(w, r)
		fmt.Printf("%s: %d %q %s\n", what, w.Code, w.Header().Values("Set-Cookie"), strings.TrimSpace(w.Body.String()))
	}
	cookies("sign in", "signIn", "", `{"name": "Ann"}`)
	cookies("sign in refused", "signIn", "", `{"name": " "}`)
	cookies("sign out", "signOut", "session=Ann", "")
	cookies("sign out refused", "signOut", "", "")

	// 27 and 28: a list's check of the Origin and the session's
	// token, each request with the headers named: Sec-Fetch-Site,
	// Origin, the session's cookie and X-CSRF-Token, "" for none.
	h3 := rpc.NewHandler(origin.Counter, pool, origin.CounterEnv{Clock: clock, Settings: origin.Settings{
		AdminOrigin: "https://admin.example", ExtraAllowedOrigins: kogo.ListOf("http://localhost:5173")}}, slog.New(slog.DiscardHandler))
	op := "whoAmI"
	from := func(what, site, from, sid, token string) {
		w, r := httptest.NewRecorder(), httptest.NewRequest("POST", "/"+op, nil)
		for name, value := range map[string]string{"Sec-Fetch-Site": site, "Origin": from, "Cookie": sid, "X-CSRF-Token": token} {
			if value != "" {
				r.Header.Set(name, value)
			}
		}
		h3.ServeHTTP(w, r)
		fmt.Printf("%s: %d %s\n", what, w.Code, strings.TrimSpace(w.Body.String()))
	}
	const live, old = "SID=00000000-0000-0000-0000-00000000000a", "SID=00000000-0000-0000-0000-00000000000b"
	from("no browser", "", "", "", "")
	from("a shop's page", "same-origin", "https://shop.example", "", "")
	from("the admin's page, typed in", "none", "https://admin.example", "", "")
	from("an extra origin, with a slash", "", "http://localhost:5173/", "", "")
	from("an old browser on a shop's page", "", "https://shop.example", "", "")
	from("another of the site's hosts", "same-site", "https://shop.example", "", "")
	from("another site", "cross-site", "https://shop.example", "", "")
	from("same-origin, from an origin not ours", "same-origin", "https://other.example", "", "")
	from("an origin not ours", "", "https://other.example", "", "")
	from("a prefix of ours", "", "https://shop.exampl", "", "")
	from("null", "", "null", "", "")
	from("null, same-origin", "same-origin", "null", "", "")
	from("null, none", "none", "null", "", "")
	from("a session with its token", "same-origin", "https://shop.example", live, "live token")
	from("a session with no token", "same-origin", "https://shop.example", live, "")
	from("a session with another's token", "same-origin", "https://shop.example", live, "old token")
	from("no browser, with a session and no token", "", "", live, "")
	from("the Origin before the token", "cross-site", "", live, "")
	from("an expired session with no token", "same-origin", "https://shop.example", old, "")
	from("an expired session with its token", "same-origin", "https://shop.example", old, "old token")
	from("a cookie that is no UUID", "same-origin", "https://shop.example", "SID=live token", "")
	from("a cookie of no session", "same-origin", "https://shop.example", "SID=00000000-0000-0000-0000-00000000000c", "")
	op = "signOut"
	from("sign out", "same-origin", "https://shop.example", live, "live token")
	op = "whoAmI"
	from("an ended session with no token", "same-origin", "https://shop.example", live, "")

	// 29: a lookup and an action, against a local server that prints
	// what it is sent, through a handle made as main makes one.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := goio.ReadAll(r.Body)
		fmt.Printf("the depot is sent: %s %s?%s [%s %s] %s\n", r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("X-Shop"), body)
		switch {
		case r.URL.Path == "/v1/prices" && r.URL.Query().Get("item") == "anvil":
			fmt.Fprint(w, `{"status": "REQUEST_DENIED"}`)
		case r.URL.Path == "/v1/prices":
			fmt.Fprint(w, `{"status": "OK", "cents": 250}`)
		case r.Method == "GET":
			fmt.Fprintf(w, `{"item": %q, "count": 9007199254740993}`, r.URL.Query().Get("item"))
		case strings.Contains(r.URL.Path, "anvil"):
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"reason": "sold out"}`)
		case strings.Contains(r.URL.Path, "ghost"):
			fmt.Fprint(w, `{"reserved": true}`)
		default:
			fmt.Fprint(w, `{"reservation": {"id": "r-1"}}`)
		}
	}))
	defer server.Close()
	service := khttp.Service{Address: server.URL, Encoding: khttp.JSON, MaxAnswer: 1 << 10, Timeout: 5 * time.Second,
		Headers: map[string]string{"Authorization": "Bearer of main"}, Log: slog.New(slog.DiscardHandler)}
	theDepot, err := khttp.New(service)
	if err != nil {
		panic(err)
	}
	show("stock", depot.StockOf(theDepot, "bolt"))
	show("reserved", depot.Reserved(theDepot, "acme", "nut/5?", 2))
	show("sold out", kogo.Either_MapLeft[khttp.HttpFailure, string](depot.Reserved(theDepot, "acme", "anvil", 1), depot.Said))
	show("no id", kogo.Either_MapLeft[khttp.HttpFailure, string](depot.Reserved(theDepot, "acme", "ghost", 1), depot.Said))
	// A refusal inside a 200 leaves one line, written here without its
	// time, and a price the client accepts leaves none.
	service.Log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		return map[bool]slog.Attr{true: {}, false: a}[a.Key == slog.TimeKey]
	}}))
	priced, _ := khttp.New(service)
	show("price", depot.PriceOf(priced, "bolt"))
	show("price refused", depot.PriceOf(priced, "anvil"))
	service.Address, service.Log = "http://127.0.0.1:1", slog.New(slog.DiscardHandler)
	nowhere, _ := khttp.New(service)
	show("nowhere", kogo.Either_MapLeft[khttp.HttpFailure, string](depot.Reserved(nowhere, "acme", "bolt", 1), func(f khttp.HttpFailure) bool { return rt.Is[khttp.HttpFailure_NotSent](f) }))

	// 30: a message built, kept in a bytea column and read back as
	// the bytes eml gave, of which the lines that say its shape.
	receipt := letters.Receipt("7", kmail.Address{Name: "Ann", Address: "ann@a.example"}, ktime.Instant{Seconds: 1794839400}, kogo.ByteArray("paid\n"))
	letter := kogo.NewId()
	show("queued", in(db, func(tx kogo.Tx) int32 { return letters.Queue(tx, letter, receipt) }))
	back := in(db, func(tx kogo.Tx) *kogo.ByteArray { return letters.Queued(tx, letter) })
	show("read back as eml wrote it", bytes.Equal(*back, kmail.Eml(receipt)))
	for _, line := range strings.Split(string(*back), "\r\n") {
		if strings.Contains(line, ": ") && !strings.HasPrefix(line, "Content-Transfer") {
			fmt.Println("  " + line)
		}
	}
	show("no letter", in(db, func(tx kogo.Tx) *kogo.ByteArray { return letters.Queued(tx, kogo.NewId()) }))

	// 32: a worker's two queues, stepped a pass at a time through the
	// runner, against a courier that is a local server: it books the
	// first parcel, answers 503 once for the second, and refuses the
	// third. The fourth is one the job's run panics on.
	busy := true
	courier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := goio.ReadAll(r.Body)
		fmt.Printf("the courier is sent: %s [%s] %s\n", r.URL.Path, r.Header.Get("Idempotency-Key"), body)
		switch {
		case bytes.Contains(body, []byte("2 Low Rd")) && busy:
			busy = false
			w.WriteHeader(http.StatusServiceUnavailable)
		case bytes.Contains(body, []byte("the moon")):
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"reason": "too far"}`)
		default:
			fmt.Fprintf(w, `{"tracking": "T-%d"}`, len(body))
		}
	}))
	defer courier.Close()
	if _, err := pool.Exec(ctx, `insert into parcel (id, depot, address, grams) values
		('00000000-0000-0000-0000-0000000000a1', 'north', '1 High St', 500),
		('00000000-0000-0000-0000-0000000000a2', 'north', '2 Low Rd', 900),
		('00000000-0000-0000-0000-0000000000a3', 'north', 'the moon', 100),
		('00000000-0000-0000-0000-0000000000a4', 'north', 'nowhere', 100)`); err != nil {
		panic(err)
	}
	theCourier, err := khttp.New(khttp.Service{Address: courier.URL, Encoding: khttp.JSON, MaxAnswer: 1 << 10, Timeout: 5 * time.Second, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		panic(err)
	}
	nine := ktime.Instant{Seconds: 1795424400} // 2026-11-23T09:00:00Z
	runner := &work.Runner[parcels.DepotEnv]{Pool: pool, Clock: ktime.FixedClock(nine), List: parcels.Depot, Log: slog.New(slog.DiscardHandler),
		Env: parcels.DepotEnv{Courier: theCourier, Clock: ktime.FixedClock(nine), Console: io.NewConsole(os.Stdin, os.Stdout)}}
	for range 5 {
		show("queueParcel", runner.Pass(ctx, "queueParcel"))
	}
	dispatches := func(when string) {
		rows, _ := pool.Query(ctx, `select concat_ws(' ', right(p.id::text, 2), p.status, 'attempts ' || attempts, case when failed then 'failed' end,
			case when done_at is not null then 'done' end, 'next ' || to_char(next_attempt_at at time zone 'UTC', 'HH24:MI'), tracking, failed_message,
			'(' || last_error || ')') from parcel p left join parcel_dispatch d on d.parcel_id = p.id order by p.id`)
		lines, _ := pgx.CollectRows(rows, pgx.RowTo[string])
		fmt.Printf("%s:\n  %s\n", when, strings.Join(lines, "\n  "))
	}
	dispatches("after the job")
	for range 4 {
		show("bookCourier at 09:00", runner.Pass(ctx, "bookCourier"))
	}
	dispatches("at 09:00")
	runner.Clock = ktime.FixedClock(ktime.Instant{Seconds: nine.Seconds + 60})
	show("bookCourier at 09:01", runner.Pass(ctx, "bookCourier"))
	show("bookCourier at 09:01", runner.Pass(ctx, "bookCourier"))
	dispatches("at 09:01")
}
