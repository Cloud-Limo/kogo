// Package time is the Go behind Kogo's package kogo.time, in
// prelude/time.kg. Its types are small ones of our own: Go's time
// value carries a location and a monotonic reading, which break
// comparison by value.
package time

import (
	"fmt"
	gotime "time"
)

// A Clock gives the time. A program is handed one: SystemClock in
// use, FixedClock in a test.
type Clock struct{ now func() Instant }

func SystemClock() Clock {
	return Clock{func() Instant { t := gotime.Now(); return Instant{t.Unix(), int32(t.Nanosecond())} }}
}

func FixedClock(at Instant) Clock { return Clock{func() Instant { return at }} }

// An Instant is a moment: seconds since 1970 in UTC, and the
// nanoseconds past that second.
type Instant struct {
	Seconds int64
	Nanos   int32
}

func (i Instant) utc() gotime.Time { return gotime.Unix(i.Seconds, int64(i.Nanos)).UTC() }
func (i Instant) String() string {
	return i.utc().Format("2006-01-02T15:04:05") + fraction(i.Nanos) + "Z"
}

// fraction writes the nanoseconds of a second as java.time does: in
// groups of three digits, as many as are needed.
func fraction(nanos int32) string {
	switch {
	case nanos == 0:
		return ""
	case nanos%1e6 == 0:
		return fmt.Sprintf(".%03d", nanos/1e6)
	case nanos%1e3 == 0:
		return fmt.Sprintf(".%06d", nanos/1e3)
	}
	return fmt.Sprintf(".%09d", nanos)
}

type LocalDate struct{ Year, Month, Day int32 }

func (d LocalDate) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// A ZoneId is a named zone of the tz database.
type ZoneId struct {
	Name string
	loc  *gotime.Location
}

func (z ZoneId) String() string { return z.Name }

// ZoneId_Of is ZoneId.of(name). A name the tz database lacks is a bug.
func ZoneId_Of(name string) ZoneId {
	loc, err := gotime.LoadLocation(name)
	if err != nil || name == "" || name == "Local" {
		panic("unknown time zone " + name)
	}
	return ZoneId{name, loc}
}

type ZonedDateTime struct {
	Instant Instant
	Zone    ZoneId
}

func (z ZonedDateTime) local() gotime.Time { return z.Instant.utc().In(z.Zone.loc) }

// String is java.time's text: seconds are left out when they are zero.
func (z ZonedDateTime) String() string {
	layout := "2006-01-02T15:04"
	if t := z.local(); t.Second() != 0 || z.Instant.Nanos != 0 {
		layout += ":05"
	}
	return z.local().Format(layout) + fraction(z.Instant.Nanos) + z.local().Format("Z07:00") + "[" + z.Zone.Name + "]"
}

func Clock_Now(this Clock) Instant                           { return this.now() }
func Instant_AtZone(this Instant, zone ZoneId) ZonedDateTime { return ZonedDateTime{this, zone} }

// The days since 1970-01-01 of java.time's first and last LocalDate,
// in years -999999999 and 999999999.
const minEpochDay, maxEpochDay = -365243219162, 365241780471

// LocalDate_PlusDays counts across months and years, as java.time does,
// and panics past java.time's range rather than give a wrong date.
func LocalDate_PlusDays(this LocalDate, daysToAdd int64) LocalDate {
	day := gotime.Date(int(this.Year), gotime.Month(this.Month), int(this.Day), 0, 0, 0, 0, gotime.UTC).Unix() / 86400
	if daysToAdd > maxEpochDay-day || daysToAdd < minEpochDay-day {
		panic(fmt.Sprintf("%v plus %d days is outside the range of a LocalDate", this, daysToAdd))
	}
	y, m, d := gotime.Unix((day+daysToAdd)*86400, 0).UTC().Date()
	return LocalDate{int32(y), int32(m), int32(d)}
}

func ZonedDateTime_ToLocalDate(this ZonedDateTime) LocalDate {
	y, m, d := this.local().Date()
	return LocalDate{int32(y), int32(m), int32(d)}
}
