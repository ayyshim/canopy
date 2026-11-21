// Package schedule parses cron and plain-text schedules for jobs.
// It has no database use.
package schedule

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // zones work without system tzdata
)

// ErrInvalidCron is wrapped by every cron parse error.
var ErrInvalidCron = errors.New("invalid cron")

// ErrInvalidZone is wrapped when a time zone name is unknown.
var ErrInvalidZone = errors.New("invalid time zone")

// Cron is a parsed 5-field cron expression.
type Cron struct {
	expr                string
	min, hour, dom, mon uint64
	dow                 uint64
	domStar, dowStar    bool
}

type fieldSpec struct {
	name   string
	lo, hi int
	names  []string // names[i] is the name of value lo+i
}

var (
	monthNames = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
	dayNames   = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

	specs = [5]fieldSpec{
		{"minute", 0, 59, nil},
		{"hour", 0, 23, nil},
		{"day of month", 1, 31, nil},
		{"month", 1, 12, monthNames},
		{"day of week", 0, 7, dayNames},
	}
)

// LoadZone loads an IANA zone such as Asia/Kathmandu.
func LoadZone(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: zone is empty", ErrInvalidZone)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a known zone (use a name like Asia/Kathmandu)", ErrInvalidZone, name)
	}
	return loc, nil
}

// ParseCron parses "minute hour day-of-month month day-of-week".
func ParseCron(expr string) (*Cron, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, fmt.Errorf("%w: need 5 fields (minute hour day month weekday), got %d", ErrInvalidCron, len(f))
	}
	var bits [5]uint64
	var star [5]bool
	for i, s := range f {
		b, st, err := parseField(s, specs[i])
		if err != nil {
			return nil, fmt.Errorf("%w: %s field %q: %v", ErrInvalidCron, specs[i].name, s, err)
		}
		bits[i], star[i] = b, st
	}
	// Sunday may be 0 or 7
	if bits[4]&(1<<7) != 0 {
		bits[4] = bits[4]&^(1<<7) | 1
	}
	return &Cron{
		expr: strings.Join(f, " "),
		min:  bits[0], hour: bits[1], dom: bits[2], mon: bits[3], dow: bits[4],
		domStar: star[2], dowStar: star[4],
	}, nil
}

// String returns the cleaned expression.
func (c *Cron) String() string { return c.expr }

func parseField(s string, sp fieldSpec) (bits uint64, star bool, err error) {
	if s == "*" || s == "?" {
		star = true
	}
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			return 0, false, errors.New("empty item")
		}
		rng, stepS, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			step, err = strconv.Atoi(stepS)
			if err != nil || step < 1 {
				return 0, false, fmt.Errorf("bad step %q", stepS)
			}
		}
		lo, hi := sp.lo, sp.hi
		switch {
		case rng == "*" || rng == "?":
			if sp.name == "day of week" {
				hi = 6
			}
		case strings.Contains(rng, "-"):
			a, b, _ := strings.Cut(rng, "-")
			if lo, err = parseValue(a, sp); err != nil {
				return 0, false, err
			}
			if hi, err = parseValue(b, sp); err != nil {
				return 0, false, err
			}
			if lo > hi {
				return 0, false, fmt.Errorf("range %q goes backwards", rng)
			}
		default:
			if lo, err = parseValue(rng, sp); err != nil {
				return 0, false, err
			}
			hi = lo
			if hasStep {
				hi = sp.hi // "5/15" means 5 through the end
			}
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, star, nil
}

func parseValue(s string, sp fieldSpec) (int, error) {
	if sp.names != nil {
		for i, n := range sp.names {
			if strings.EqualFold(s, n) {
				return sp.lo + i, nil
			}
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if v < sp.lo || v > sp.hi {
		return 0, fmt.Errorf("%d is out of range %d-%d", v, sp.lo, sp.hi)
	}
	return v, nil
}

func (c *Cron) dayMatches(t time.Time) bool {
	d := c.dom&(1<<uint(t.Day())) != 0
	w := c.dow&(1<<uint(t.Weekday())) != 0
	if !c.domStar && !c.dowStar {
		return d || w // both set: either one
	}
	return d && w
}

// nextWall finds the first matching wall-clock minute at or after t.
// t is a plain wall time held in UTC, so there are no zone jumps in the math.
func (c *Cron) nextWall(t time.Time) (time.Time, bool) {
	limit := t.AddDate(8, 0, 0) // covers Feb 29 on leap cycles
	for t.Before(limit) {
		switch {
		case c.mon&(1<<uint(t.Month())) == 0:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		case !c.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
		case c.hour&(1<<uint(t.Hour())) == 0:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
		case c.min&(1<<uint(t.Minute())) == 0:
			t = t.Add(time.Minute)
		default:
			return t, true
		}
	}
	return time.Time{}, false
}

// Next returns the first run strictly after the instant, in UTC.
// The bool is false when the expression never matches (such as 31 Feb).
//
// Daylight saving: a wall time that does not exist runs at the moment the
// clock jumps over it. A wall time that happens twice runs on its first pass.
func (c *Cron) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	l := after.In(loc)
	w := time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute(), 0, 0, time.UTC).Add(time.Minute)
	for {
		m, ok := c.nextWall(w)
		if !ok {
			return time.Time{}, false
		}
		if at := wallToInstant(m, loc); at.After(after) {
			return at.UTC(), true
		}
		w = m.Add(time.Minute)
	}
}

// wallToInstant maps a wall time (held in UTC) to an instant in loc.
func wallToInstant(w time.Time, loc *time.Location) time.Time {
	same := func(i time.Time) bool {
		l := i.In(loc)
		return l.Year() == w.Year() && l.Month() == w.Month() && l.Day() == w.Day() &&
			l.Hour() == w.Hour() && l.Minute() == w.Minute()
	}
	_, offBefore := w.Add(-36 * time.Hour).In(loc).Zone()
	_, offAfter := w.Add(36 * time.Hour).In(loc).Zone()
	a := w.Add(-time.Duration(offBefore) * time.Second)
	b := w.Add(-time.Duration(offAfter) * time.Second)
	okA, okB := same(a), same(b)
	switch {
	case okA && okB:
		if a.Before(b) {
			return a
		}
		return b
	case okA:
		return a
	case okB:
		return b
	}
	// In a gap: find the first instant whose wall time is at or after w.
	lo, hi := w.Add(-36*time.Hour), w.Add(36*time.Hour)
	for hi.Sub(lo) > time.Second {
		mid := lo.Add(hi.Sub(lo) / 2)
		m := mid.In(loc)
		mw := time.Date(m.Year(), m.Month(), m.Day(), m.Hour(), m.Minute(), m.Second(), 0, time.UTC)
		if mw.Before(w) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi.Truncate(time.Second)
}
