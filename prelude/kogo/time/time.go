// Package time is the Go behind Kogo's package kogo.time, in
// prelude/time.kg. Its types are small ones of our own: Go's time
// value carries a location and a monotonic reading, which break
// comparison by value.
package time

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	gotime "time"

	"github.com/jackc/pgx/v5/pgtype"
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
	t := i.utc()
	return year(t.Year()) + t.Format("-01-02T15:04:05") + fraction(i.Nanos) + "Z"
}

// year writes a year as java.time does: four digits at least, with a
// sign before a negative one and before one of five digits or more.
func year(y int) string {
	switch {
	case y > 9999:
		return "+" + strconv.Itoa(y)
	case y < 0:
		return fmt.Sprintf("-%04d", -y)
	}
	return fmt.Sprintf("%04d", y)
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

func (d LocalDate) String() string {
	return fmt.Sprintf("%s-%02d-%02d", year(int(d.Year)), d.Month, d.Day)
}

// A LocalTime is a time of day: the nanoseconds since midnight.
type LocalTime struct{ NanoOfDay int64 }

// String is java.time's text: seconds are left out when they are zero.
func (t LocalTime) String() string {
	s, nanos := t.NanoOfDay/1e9, int32(t.NanoOfDay%1e9)
	text := fmt.Sprintf("%02d:%02d", s/3600, s/60%60)
	if s%60 != 0 || nanos != 0 {
		text += fmt.Sprintf(":%02d", s%60) + fraction(nanos)
	}
	return text
}

// A Duration is a length of time: seconds, and the nanoseconds past
// that second, from 0 up, as java.time holds one.
type Duration struct {
	Seconds int64
	Nanos   int32
}

// String is java.time's text, PT8H6M12.345S: each part has the sign
// of the whole, and a part that is zero is left out.
func (d Duration) String() string {
	secs, nanos := d.Seconds, int64(d.Nanos)
	if secs < 0 && nanos > 0 {
		secs, nanos = secs+1, nanos-1e9
	}
	text := "PT"
	if h := secs / 3600; h != 0 {
		text += fmt.Sprintf("%dH", h)
	}
	if m := secs % 3600 / 60; m != 0 {
		text += fmt.Sprintf("%dM", m)
	}
	if s := secs % 60; s != 0 || nanos != 0 || text == "PT" {
		if s == 0 && nanos < 0 {
			text += "-"
		}
		text += strconv.FormatInt(s, 10)
		if nanos != 0 { // the digits, exactly: a float64 rounds them
			text += "." + strings.TrimRight(fmt.Sprintf("%09d", max(nanos, -nanos)), "0")
		}
		text += "S"
	}
	return text
}

// To and from Postgres (spec.md 10.4), which keeps microseconds: an
// Instant is a timestamptz, a LocalDate a date, a LocalTime a time
// and a Duration an interval. Postgres's infinity is none of them,
// and an interval with months has no length: each is an error, as a
// null is, which the caller panics with, and so is a time of 24:00,
// which Postgres holds and a LocalTime does not. An interval's day is
// 24 hours, and a Duration is sent as microseconds alone: one too long
// for an int64 of them is an error too.

func (i *Instant) ScanTimestamptz(v pgtype.Timestamptz) error {
	if !v.Valid || v.InfinityModifier != pgtype.Finite {
		return errors.New("null or infinity is not an Instant")
	}
	*i = Instant{v.Time.Unix(), int32(v.Time.Nanosecond())}
	return nil
}

func (i Instant) TimestamptzValue() (pgtype.Timestamptz, error) {
	return pgtype.Timestamptz{Time: i.utc(), Valid: true}, nil
}

func (d *LocalDate) ScanDate(v pgtype.Date) error {
	if !v.Valid || v.InfinityModifier != pgtype.Finite {
		return errors.New("null or infinity is not a LocalDate")
	}
	y, m, day := v.Time.Date()
	*d = LocalDate{int32(y), int32(m), int32(day)}
	return nil
}

func (d LocalDate) DateValue() (pgtype.Date, error) {
	return pgtype.Date{Time: gotime.Date(int(d.Year), gotime.Month(d.Month), int(d.Day), 0, 0, 0, 0, gotime.UTC), Valid: true}, nil
}

func (t *LocalTime) ScanTime(v pgtype.Time) error {
	if !v.Valid || v.Microseconds >= 86400e6 {
		return errors.New("null or 24:00 is not a LocalTime")
	}
	*t = LocalTime{v.Microseconds * 1000}
	return nil
}

func (t LocalTime) TimeValue() (pgtype.Time, error) {
	return pgtype.Time{Microseconds: t.NanoOfDay / 1000, Valid: true}, nil
}

func (d *Duration) ScanInterval(v pgtype.Interval) error {
	if !v.Valid || v.Months != 0 {
		return errors.New("null, or an interval with months, is not a Duration")
	}
	secs, micros := v.Microseconds/1e6, v.Microseconds%1e6
	if micros < 0 {
		secs, micros = secs-1, micros+1e6
	}
	*d = Duration{int64(v.Days)*86400 + secs, int32(micros * 1000)}
	return nil
}

func (d Duration) IntervalValue() (pgtype.Interval, error) {
	if d.Seconds > (math.MaxInt64-int64(d.Nanos)/1000)/1_000_000 || d.Seconds < math.MinInt64/1_000_000 {
		return pgtype.Interval{}, fmt.Errorf("%s is too long for an interval", d)
	}
	return pgtype.Interval{Microseconds: d.Seconds*1e6 + int64(d.Nanos)/1000, Valid: true}, nil
}

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

// String is java.time's text: seconds are left out when they are zero,
// and so are the offset's.
func (z ZonedDateTime) String() string {
	t, layout := z.local(), "-01-02T15:04"
	if t.Second() != 0 || z.Instant.Nanos != 0 {
		layout += ":05"
	}
	return year(t.Year()) + t.Format(layout) + fraction(z.Instant.Nanos) + offset(t) + "[" + z.Zone.Name + "]"
}

// offset writes t's offset from UTC as java.time does: Z for none, and
// the seconds only where there are some, as in a zone's local mean time.
func offset(t gotime.Time) string {
	_, secs := t.Zone()
	if secs == 0 {
		return "Z"
	}
	sign := "+"
	if secs < 0 {
		sign, secs = "-", -secs
	}
	s := fmt.Sprintf("%s%02d:%02d", sign, secs/3600, secs/60%60)
	if secs%60 != 0 {
		s += fmt.Sprintf(":%02d", secs%60)
	}
	return s
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

// LocalDate_AtStartOfDay is the earliest instant of the date in zone,
// as java.time's: where a change of offset skips midnight, the time
// the gap ends; where midnight comes twice, the first.
func LocalDate_AtStartOfDay(this LocalDate, zone ZoneId) Instant {
	y, m, d := int(this.Year), gotime.Month(this.Month), int(this.Day)
	t := gotime.Date(y, m, d, 0, 0, 0, 0, zone.loc)
	if ty, tm, td := t.Date(); ty != y || tm != m || td != d {
		_, t = t.ZoneBounds()
	} else if start, _ := t.ZoneBounds(); !start.IsZero() {
		_, before := start.Add(-1).Zone()
		if p := gotime.Date(y, m, d, 0, 0, 0, 0, gotime.FixedZone("", before)); p.Before(start) {
			t = p
		}
	}
	return Instant{t.Unix(), int32(t.Nanosecond())}
}

// java.time's range of an Instant, in seconds since 1970: the years
// -1000000000 and 1000000000.
const minSecond, maxSecond = -31557014167219200, 31556889864403199

// The arithmetic below is java.time's, step by step, so that it
// overflows where java.time's does and panics with its message.

func addExact(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		panic("long overflow")
	}
	return a + b
}

// floorDiv splits nanos into whole seconds and the nanoseconds past them.
func floorDiv(nanos int64) (int64, int64) {
	s, n := nanos/1e9, nanos%1e9
	if n < 0 {
		s, n = s-1, n+1e9
	}
	return s, n
}

func Instant_OfEpochSecond(epochSecond, nanoAdjustment int64) Instant {
	s, n := floorDiv(nanoAdjustment)
	if s = addExact(epochSecond, s); s < minSecond || s > maxSecond {
		panic("Instant exceeds minimum or maximum instant")
	}
	return Instant{s, int32(n)}
}

func Duration_OfSeconds(seconds, nanoAdjustment int64) Duration {
	s, n := floorDiv(nanoAdjustment)
	return Duration{addExact(seconds, s), int32(n)}
}

func Instant_IsBefore(this, other Instant) bool {
	return this.Seconds < other.Seconds || this.Seconds == other.Seconds && this.Nanos < other.Nanos
}

func Instant_IsAfter(this, other Instant) bool { return Instant_IsBefore(other, this) }

// plus is java.time's Instant.plus(seconds, nanos).
func (i Instant) plus(seconds, nanos int64) Instant {
	if seconds == 0 && nanos == 0 {
		return i
	}
	return Instant_OfEpochSecond(addExact(addExact(i.Seconds, seconds), nanos/1e9), int64(i.Nanos)+nanos%1e9)
}

// Instant_Plus adds the seconds and then the nanoseconds, as java.time
// does, so a sum past the range part way through panics.
func Instant_Plus(this Instant, d Duration) Instant {
	return this.plus(d.Seconds, 0).plus(0, int64(d.Nanos))
}

func Instant_Minus(this Instant, d Duration) Instant {
	if d.Seconds == math.MinInt64 {
		this = this.plus(math.MaxInt64, 0).plus(1, 0)
	} else {
		this = this.plus(-d.Seconds, 0)
	}
	return this.plus(0, -int64(d.Nanos))
}
