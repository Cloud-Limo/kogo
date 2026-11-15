package time

import (
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"kogo/rt"
)

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
		{rt.ToString(Instant_AtZone(Instant{Seconds: 1794753000}, ZoneId_Of("UTC"))), "2026-11-15T14:30Z[UTC]"},
		{rt.ToString(LocalDate{Year: 987, Month: 1, Day: 2}), "0987-01-02"},
		{rt.ToString(LocalDate_PlusDays(LocalDate{Year: 2028, Month: 2, Day: 28}, 1)), "2028-02-29"},
		{rt.ToString(LocalDate_PlusDays(LocalDate{Year: 2026, Month: 12, Day: 30}, 33)), "2027-02-01"},
		{rt.ToString(LocalDate_PlusDays(LocalDate{Year: 2026, Month: 3, Day: 1}, -1)), "2026-02-28"},
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
	defer func() {
		if recover() == nil {
			t.Error("a zone the tz database lacks does not panic")
		}
	}()
	ZoneId_Of("Mars/Olympus")
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

// A time and a length of time come from Postgres's microseconds and
// write themselves as java.time does. What Postgres holds and Kogo
// does not is an error: a null, infinity, an interval with months.
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
		{rt.ToString(clock), "09:05:00.250"},
		{rt.ToString(LocalTime{NanoOfDay: 18 * 3600e9}), "18:00"},
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
		"a null interval":          new(Duration).ScanInterval(pgtype.Interval{}),
		"an interval with a month": new(Duration).ScanInterval(pgtype.Interval{Months: 1, Valid: true}),
	} {
		if err == nil {
			t.Errorf("%s is read as a value", what)
		}
	}
}
