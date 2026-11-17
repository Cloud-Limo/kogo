# What cusoon2's Kotlin is made of

Counted on 17 Nov 2026 in cusoon2 at `691aa93cf`, for the question of
how much of what the Kotlin holds by habit Kogo could hold by rule
(`doc/gates.md`, `doc/work.md`). The counts are by regex over the
source and are approximate; the split of functions by what they reach
is the roughest.

Hand-written Kotlin is 18,172 lines: `src/main` is 21,184, of which
3,012 are GraphQL types that are generated and checked in.

## The idioms hold in the core

| Zone | Lines | `override` | open or abstract class | anonymous `object` | `try` | `!!` |
| --- | --- | --- | --- | --- | --- | --- |
| Core: model, pricing, invoice rules, rpc, time | 6,191 | 17 | 1 | 0 | 6 | 42 |
| GraphQL, hand-written | 3,566 | 306 | 16 | 15 | 7 | 25 |
| Daemon | 2,431 | 85 | 1 | 10 | 12 | 31 |
| Outside: Google, Stripe, identity, mail, PDF | 4,380 | 90 | 9 | 2 | 13 | 13 |
| The app's shell | 1,604 | 15 | 0 | 2 | 11 | 3 |

- 97% of `override` is outside the core, most of it where
  graphql-java's generated types force it.
- Nowhere: coroutines, `lateinit`, a top-level `var`, a value class.
- Of the core's 371 functions about 31% have no database handle in
  reach; the rest go through `Model`.
- Where the core is not yet Kogo: `!!`, and `Model` itself, a class of
  about 150 member functions and two properties that change (`clock`
  and `tenantId`).
- Failures are values already: 30 functions take a `Raise`, of
  `String` or `ErrorValue`; there are 64 `bind`s and 103 `raise`s and
  `ensure`s.

## The domain is small

- **Operations:** 93, of which 73 are GraphQL and 20 RPC. A query's
  median is 8 lines, and 22 of 36 have no branch. A mutation's median
  is 22 lines, and 30 of 37 are one `either` in one savepoint. 5 of
  the 73 hold a loop.
- **Where the size is:** four mutations (`createStripeCard`,
  `submitBooking`, `setBookingDetails`, `createStripeCharge`) hold
  about 43% of all the mutations' lines.
- **Gates:** 8 functions on the GraphQL side. 48 fetchers call one;
  25 do not, on purpose: about 16 public reads, 4 bearer links, the 4
  login steps and the logout. All 20 ops operations call one.
- **The access matrix:** 44 rows by 6 callers collapse to 7 patterns
  of allowed and refused, and three of them (an admin; an owner or an
  admin; a payer, an owner or an admin) cover 37 rows.
- **SQL:** about 200 jOOQ statements, 108 of them in one file. About
  20 lines build a condition or a sort when the program runs.
- **Schema:** 44 tables, 27 under row-level security, 399 columns, 17
  enums.
- **Outside the shape:** the daemon and the outside zone are 37% of
  the hand-written lines. Stripe is reached from 2 files, the identity
  service from 1 and Google Maps from 5. A request hands work out by
  writing a row to one of 7 job tables.
- **Tests that hold a habit:** the two access matrix tests are 907
  lines.
