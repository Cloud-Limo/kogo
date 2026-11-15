// The driver of TestEmittedGoRuns: it calls the Go emitted for
// examples 01 to 06 and 20 and prints what each gives. ROOT stands
// for the import path of the directory the Go is written to.
package main

import (
	"fmt"

	"ROOT/cusoon/invoice"
	"ROOT/cusoon/model"
	"ROOT/example"
	ktime "kogo/prelude/kogo/time"
	"kogo/rt"
)

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
}
