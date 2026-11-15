package time

import (
	"testing"

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
