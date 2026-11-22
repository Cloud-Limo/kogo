package time

import (
	"cmp"
	"context"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Cloud-Limo/kogo/rt"
)

const limit = "ZoneId.of takes a region name, such as Australia/Melbourne; offsets are not supported (spec 12)"

// A date is read in a named zone, and each type writes itself as
// java.time does.
func TestTime(t *testing.T) {
	at := Instant{Seconds: 1794753000, Nanos: 120_000_000} // 2026-11-15T14:30:00.120Z
	sydney := Instant_AtZone(Clock_Now(FixedClock(at)), ZoneId_Of("Australia/Sydney"))
	for _, c := range [][2]string{
		{rt.ToString(at), "2026-11-15T14:30:00.120Z"},
		{rt.ToString(Instant{Seconds: 1794753000}), "2026-11-15T14:30:00Z"},
		{rt.ToString(ZonedDateTime_ToLocalDate(sydney)), "2026-11-16"},
		{rt.ToString(sydney), "2026-11-16T01:30:00.120+11:00[Australia/Sydney]"},
		{rt.ToString(ZonedDateTime_ToInstant(sydney)), "2026-11-15T14:30:00.120Z"},
		{rt.ToString(Instant_AtZone(Instant{Seconds: 1794753000}, ZoneId_Of("UTC"))), "2026-11-15T14:30Z[UTC]"},
		{rt.ToString(LocalDate{Year: 987, Month: 1, Day: 2}), "0987-01-02"},
		// Years past four digits, or before year 0, and an offset in
		// seconds, as Java 21 writes them.
		{rt.ToString(LocalDate{Year: -1, Month: 1, Day: 2}), "-0001-01-02"},
		{rt.ToString(LocalDate{Year: 10000, Month: 1, Day: 2}) + " " + rt.ToString(LocalDate{Year: -10000, Month: 1, Day: 2}), "+10000-01-02 -10000-01-02"},
		{rt.ToString(Instant{Seconds: -62167219201, Nanos: 120_000_000}), "-0001-12-31T23:59:59.120Z"},
		{rt.ToString(LocalDate_AtStartOfDay(LocalDate{Year: -1, Month: 1, Day: 2}, sydney.Zone)), "-0001-01-02T00:00+10:04:52[Australia/Sydney]"},
		{rt.ToString(LocalDate_AtStartOfDay(LocalDate{Year: 10000, Month: 1, Day: 2}, sydney.Zone)), "+10000-01-02T00:00+11:00[Australia/Sydney]"},
		{rt.ToString(Instant_AtZone(Instant{Seconds: 1794753000}, ZoneId_Of("Etc/GMT+5"))), "2026-11-15T09:30-05:00[Etc/GMT+5]"},
		{rt.ToString(LocalDate_PlusDays(LocalDate{Year: 2028, Month: 2, Day: 28}, 1)), "2028-02-29"},
		{rt.ToString(LocalDate_PlusDays(LocalDate{Year: 2026, Month: 12, Day: 30}, 33)), "2027-02-01"},
		{rt.ToString(LocalDate_PlusDays(LocalDate{Year: 2026, Month: 3, Day: 1}, -1)), "2026-02-28"},
		{rt.ToString(Instant_AtZone(Instant{Seconds: 1794753000}, ZoneId_Of("UT"))), "2026-11-15T14:30Z[UT]"},
	} {
		if c[0] != c[1] {
			t.Errorf("got %s, want %s", c[0], c[1])
		}
	}
	if !rt.Equal(ZoneId_Of("UTC"), ZoneId_Of("UTC")) || rt.Equal(at, Instant{Seconds: 1794753000}) {
		t.Error("two zones of one name are equal, and two instants a fraction apart are not")
	}
	if now := Clock_Now(SystemClock()); now.Seconds < at.Seconds-400_000_000 {
		t.Errorf("the system clock reads %v", now)
	}
	for id, want := range map[string]string{"Mars/Olympus": "Unknown time-zone ID: Mars/Olympus", "EST": "Unknown time-zone ID: EST",
		"Factory": "Unknown time-zone ID: Factory", "a b": "Invalid ID for region-based ZoneId, invalid format: a b",
		// Offset IDs are outside the subset (spec 12): Kogo refuses them.
		"Z": limit, "+05:30": limit, "-0930": limit, "GMT+1": limit, "UTC+10": limit, "UT-0": limit} {
		func() {
			defer func() {
				if got := recover(); got != want {
					t.Errorf("ZoneId.of(%q) panics with %v, want %s", id, got, want)
				}
			}()
			ZoneId_Of(id)
		}()
	}
}

// plusDays reaches java.time's last and first dates and panics past
// them, never wrapping round to a wrong date.
func TestPlusDaysRange(t *testing.T) {
	last, first := LocalDate{999999999, 12, 31}, LocalDate{-999999999, 1, 1}
	if got := LocalDate_PlusDays(LocalDate{999999999, 12, 30}, 1); got != last {
		t.Errorf("the day before the last plus 1 is %v", got)
	}
	if got := LocalDate_PlusDays(LocalDate{-999999999, 1, 2}, -1); got != first {
		t.Errorf("the day after the first minus 1 is %v", got)
	}
	for _, c := range []struct {
		from LocalDate
		days int64
	}{{last, 1}, {first, -1}, {LocalDate{2026, 1, 1}, math.MaxInt64}, {LocalDate{2026, 1, 1}, math.MinInt64}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%v plus %d days does not panic", c.from, c.days)
				}
			}()
			LocalDate_PlusDays(c.from, c.days)
		}()
	}
}

// A day starts at its earliest time in the zone, as java.time's does,
// also where midnight is skipped or comes twice.
func TestAtStartOfDay(t *testing.T) {
	for _, c := range []struct {
		zone string
		day  LocalDate
		want string
	}{
		{"Australia/Sydney", LocalDate{2026, 11, 16}, "2026-11-16T00:00+11:00[Australia/Sydney]"},
		{"America/Santiago", LocalDate{2022, 9, 11}, "2022-09-11T01:00-03:00[America/Santiago]"},   // midnight skipped
		{"America/Sao_Paulo", LocalDate{2018, 11, 4}, "2018-11-04T01:00-02:00[America/Sao_Paulo]"}, // midnight skipped
		{"America/Havana", LocalDate{2022, 3, 13}, "2022-03-13T01:00-04:00[America/Havana]"},       // midnight skipped
		{"America/Havana", LocalDate{2022, 11, 6}, "2022-11-06T00:00-04:00[America/Havana]"},       // midnight twice
		{"Africa/Tunis", LocalDate{1977, 9, 24}, "1977-09-24T00:00+02:00[Africa/Tunis]"},           // midnight twice, Go takes the second
		{"UTC", LocalDate{1969, 12, 31}, "1969-12-31T00:00Z[UTC]"},
	} {
		if got := rt.ToString(LocalDate_AtStartOfDay(c.day, ZoneId_Of(c.zone))); got != c.want {
			t.Errorf("%v.atStartOfDay(%s) is %s, want %s", c.day, c.zone, got, c.want)
		}
	}
}

// A time and a length of time come from Postgres's microseconds and
// write themselves as java.time does. What Postgres holds and Kogo
// does not is an error: a null, infinity, an interval with months,
// 24:00; and so is a Duration too long to send.
func TestPostgres(t *testing.T) {
	var span Duration
	var clock LocalTime
	if span.ScanInterval(pgtype.Interval{Days: 1, Microseconds: -500_000, Valid: true}) != nil ||
		clock.ScanTime(pgtype.Time{Microseconds: 32_700_250_000, Valid: true}) != nil {
		t.Fatal("an interval or a time is not read")
	}
	for _, c := range [][2]string{
		{rt.ToString(span), "PT23H59M59.5S"},
		{rt.ToString(Duration{}), "PT0S"},
		{rt.ToString(Duration{Seconds: -90}), "PT-1M-30S"},
		{rt.ToString(Duration{Seconds: -3600, Nanos: 500_000_000}), "PT-59M-59.5S"},
		{rt.ToString(Duration{Seconds: 7200}), "PT2H"},
		{rt.ToString(Duration{Seconds: -2, Nanos: 4}), "PT-1.999999996S"},
		{rt.ToString(Duration{Seconds: -1, Nanos: 999_999_999}), "PT-0.000000001S"},
		{rt.ToString(clock), "09:05:00.250"},
		{rt.ToString(LocalTime{NanoOfDay: 18 * 3600e9}), "18:00"},
		{rt.ToString(LocalTime{NanoOfDay: 86399e9}), "23:59:59"},
	} {
		if c[0] != c[1] {
			t.Errorf("got %s, want %s", c[0], c[1])
		}
	}
	if back, _ := span.IntervalValue(); back != (pgtype.Interval{Microseconds: 86_399_500_000, Valid: true}) {
		t.Errorf("a Duration is sent as %v", back)
	}
	for what, err := range map[string]error{
		"a null timestamptz":       new(Instant).ScanTimestamptz(pgtype.Timestamptz{}),
		"infinity":                 new(Instant).ScanTimestamptz(pgtype.Timestamptz{InfinityModifier: pgtype.Infinity, Valid: true}),
		"a null date":              new(LocalDate).ScanDate(pgtype.Date{}),
		"a date of -infinity":      new(LocalDate).ScanDate(pgtype.Date{InfinityModifier: pgtype.NegativeInfinity, Valid: true}),
		"a null time":              new(LocalTime).ScanTime(pgtype.Time{}),
		"a time of 24:00":          new(LocalTime).ScanTime(pgtype.Time{Microseconds: 86400e6, Valid: true}),
		"a null interval":          new(Duration).ScanInterval(pgtype.Interval{}),
		"an interval with a month": new(Duration).ScanInterval(pgtype.Interval{Months: 1, Valid: true}),
	} {
		if err == nil {
			t.Errorf("%s is read as a value", what)
		}
	}
	const most, least = math.MaxInt64 / 1_000_000, math.MinInt64 / 1_000_000
	for _, d := range []Duration{{Seconds: most, Nanos: 999_999_999}, {Seconds: least - 1, Nanos: 999_999_999}} {
		if _, err := d.IntervalValue(); err == nil {
			t.Errorf("%v is sent as an interval", d)
		}
	}
	for _, d := range []Duration{{Seconds: most, Nanos: 775_807_000}, {Seconds: least}} {
		if _, err := d.IntervalValue(); err != nil {
			t.Errorf("the longest Duration Postgres holds is not sent: %v", err)
		}
	}
}

// Postgres holds a timestamptz from -4713-11-24 to +294276-12-31, and
// a date to +5874897-12-31: the last value at each end is stored as itself,
// and the first past it is an error with nothing stored, never a
// time the driver's microseconds or days wrapped round to.
func TestPostgresRange(t *testing.T) {
	ctx, url := context.Background(), cmp.Or(os.Getenv("KOGO_TEST_DATABASE_URL"), "postgres://kogo:kogo@127.0.0.1/kogo_test")
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	conn.Exec(ctx, "create temp table at (i timestamptz, d date)")
	for v, held := range map[any]bool{
		Instant{-210866803200, 0}: true, Instant{9224318015999, 999_999_000}: true,
		LocalDate{-4713, 11, 24}: true, LocalDate{5874897, 12, 31}: true,
		Instant{-210866803201, 999_999_999}: false, Instant{9224318016000, 0}: false,
		Instant{1700000000000000, 0}: false, Instant{minSecond, 0}: false,
		LocalDate{-4713, 11, 23}: false, LocalDate{5874898, 1, 1}: false, LocalDate{11994002, 9, 10}: false,
		LocalDate{-10000000, 1, 1}: false,
	} {
		sql, back := "insert into at (i) values ($1) returning i", any(new(Instant))
		if _, ok := v.(LocalDate); ok {
			sql, back = "insert into at (d) values ($1) returning d", new(LocalDate)
		}
		err := conn.QueryRow(ctx, sql, v).Scan(back)
		if stored := reflect.ValueOf(back).Elem().Interface(); held != (err == nil) || held && stored != v {
			t.Errorf("%v is stored as %v (%v)", v, stored, err)
		}
	}
	var n int
	if conn.QueryRow(ctx, "select count(*) from at").Scan(&n); n != 4 {
		t.Errorf("%d values are stored, want the 4 Postgres holds", n)
	}
}
