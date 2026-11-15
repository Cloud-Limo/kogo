// The program TestEmittedGoRuns builds with the Go emitted for
// examples/. It calls those functions in a transaction it rolls back
// and prints what they return.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func main() {
	ctx := context.Background()
	tx := must(must(pgx.Connect(ctx, os.Getenv("DATABASE_URL"))).Begin(ctx))
	defer tx.Rollback(ctx)
	for _, seed := range []string{
		`insert into business (id, slug, legal_name, abn) values
			('00000000-0000-0000-0000-000000000001', 'acme', 'Acme Pty Ltd', '51824753556'),
			('00000000-0000-0000-0000-000000000002', 'bolt', 'Bolt Couriers', null)`,
		`insert into person (id, name, email) values ('00000000-0000-0000-0000-000000000003', 'Ann', 'ann@example.com')`,
		`insert into admin_grant (person_id, business_id) select p.id, b.id from person p, business b`,
		`insert into tenant (id, business_id, slug) select '00000000-0000-0000-0000-000000000004', id, 'acme-store'
			from business where slug = 'acme'`,
		`insert into tenant_driver (tenant_id, driver_id) select t.id, p.id from tenant t, person p`,
		`insert into supply_agreement (operator_id, supplier_id, driver_share_percent) values
			('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 40)`,
		`insert into invoice (id, number, total_cents, status) values
			('00000000-0000-0000-0000-000000000005', 'INV-1', 1000, 'paid'),
			('00000000-0000-0000-0000-000000000006', 'INV-2', null, 'sent')`,
	} {
		must(tx.Exec(ctx, seed))
	}
	acme, bolt, ann := [16]byte{15: 1}, [16]byte{15: 2}, [16]byte{15: 3}

	for _, r := range must(Q10_sql_listL7C27(ctx, tx, &acme)) {
		fmt.Println("supplier:", r.LegalName, r.DriverSharePercent)
	}
	fmt.Println("abn of bolt is null:", must(Q11_sql_one_scalarL6C26(ctx, tx, &bolt))[0].Abn == nil)
	for _, r := range must(Q14_sql_outer_joinL9C28(ctx, tx, &ann)) {
		storefront := "none"
		if r.StorefrontSlug != nil {
			storefront = *r.StorefrontSlug
		}
		fmt.Println("administered:", r.LegalName, "storefront:", storefront)
	}
	id, kind, share, inactive := [16]byte{15: 7}, "outside", int32(25), false
	invite := must(Q13_sql_upsert_returningL17C35(ctx, tx, &id, &acme, &kind, &ann, &ann, nil))[0]
	fmt.Println("invite:", invite.Kind, "share is null:", invite.DriverSharePercent == nil)
	invite = must(Q13_sql_upsert_returningL17C35(ctx, tx, &id, &acme, &kind, &ann, &ann, &share))[0]
	fmt.Println("invite again:", invite.Kind, *invite.DriverSharePercent)
	fmt.Println("drivers, any:", len(must(Q15_sql_optional_filterL7C28(ctx, tx, nil))),
		"inactive:", len(must(Q15_sql_optional_filterL7C28(ctx, tx, &inactive))))
	at, reference := time.Now(), "REF-1"
	fmt.Println("fees accounted:", must(Q16_sql_update_anyL6C20(ctx, tx, &at, &reference, [][16]byte{{15: 5}, {15: 6}, {15: 9}})))
	fmt.Println("paid invoices:", must(Q17_sql_two_statementsL14C26(ctx, tx))[0].N)
	nobody := [16]byte{15: 9}
	fmt.Println("ann administers acme:", must(Q11_sql_one_scalarL12C15(ctx, tx, &ann, &acme)),
		"nobody does:", must(Q11_sql_one_scalarL12C15(ctx, tx, &nobody, &acme)))
	key := int64(822)
	fmt.Println("locked:", must(Q21_sql_voidL6C16(ctx, tx, &key)))
}
