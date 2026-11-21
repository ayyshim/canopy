package schedule

import (
	"errors"
	"testing"
	"time"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := LoadZone(name)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNext(t *testing.T) {
	tests := []struct {
		name, expr, zone, after, want string
	}{
		{"daily utc", "0 9 * * *", "UTC", "2026-03-01T08:59:59Z", "2026-03-01T09:00:00Z"},
		{"strictly after", "0 9 * * *", "UTC", "2026-03-01T09:00:00Z", "2026-03-02T09:00:00Z"},
		{"zone offset", "30 9 * * *", "Asia/Tokyo", "2026-03-01T00:00:00Z", "2026-03-01T00:30:00Z"},
		{"kathmandu +05:45", "0 9 * * *", "Asia/Kathmandu", "2026-03-01T00:00:00Z", "2026-03-01T03:15:00Z"},
		{"kathmandu next day", "0 9 * * *", "Asia/Kathmandu", "2026-03-01T03:15:00Z", "2026-03-02T03:15:00Z"},
		{"weekdays skip weekend", "0 9 * * 1-5", "UTC", "2026-03-06T10:00:00Z", "2026-03-09T09:00:00Z"},
		{"sunday as 7", "0 8 * * 7", "UTC", "2026-03-01T09:00:00Z", "2026-03-08T08:00:00Z"},
		{"names", "0 8 * * mon", "UTC", "2026-03-01T00:00:00Z", "2026-03-02T08:00:00Z"},
		{"first of month", "0 10 1 * *", "UTC", "2026-03-01T10:00:00Z", "2026-04-01T10:00:00Z"},
		{"steps", "*/15 * * * *", "UTC", "2026-03-01T00:07:00Z", "2026-03-01T00:15:00Z"},
		{"dom or dow", "0 9 13 * 5", "UTC", "2026-03-01T00:00:00Z", "2026-03-06T09:00:00Z"},
		{"leap day", "0 0 29 2 *", "UTC", "2026-03-01T00:00:00Z", "2028-02-29T00:00:00Z"},
		// New York springs forward 2026-03-08 02:00 -> 03:00
		{"spring forward missing time", "30 2 * * *", "America/New_York", "2026-03-08T00:00:00Z", "2026-03-08T07:00:00Z"},
		{"spring forward next day normal", "30 2 * * *", "America/New_York", "2026-03-08T07:00:00Z", "2026-03-09T06:30:00Z"},
		{"spring forward other times ok", "30 4 * * *", "America/New_York", "2026-03-08T00:00:00Z", "2026-03-08T08:30:00Z"},
		// New York falls back 2026-11-01 02:00 EDT -> 01:00 EST
		{"fall back first pass", "30 1 * * *", "America/New_York", "2026-11-01T00:00:00Z", "2026-11-01T05:30:00Z"},
		{"fall back runs once", "30 1 * * *", "America/New_York", "2026-11-01T05:30:00Z", "2026-11-02T06:30:00Z"},
		{"fall back after first run, in second pass", "30 1 * * *", "America/New_York", "2026-11-01T06:10:00Z", "2026-11-02T06:30:00Z"},
		{"fall back hourly skips repeated hour", "0 * * * *", "America/New_York", "2026-11-01T05:30:00Z", "2026-11-01T07:00:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseCron(tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := c.Next(utc(tc.after), mustZone(t, tc.zone))
			if !ok {
				t.Fatal("no next time")
			}
			if !got.Equal(utc(tc.want)) || got.Location() != time.UTC {
				t.Fatalf("got %s want %s", got.Format(time.RFC3339), tc.want)
			}
		})
	}
}

func TestNextGapRunsOnce(t *testing.T) {
	c, _ := ParseCron("*/30 2 * * *")
	loc := mustZone(t, "America/New_York")
	first, _ := c.Next(utc("2026-03-08T06:00:00Z"), loc)
	if !first.Equal(utc("2026-03-08T07:00:00Z")) {
		t.Fatalf("first %s", first)
	}
	second, _ := c.Next(first, loc)
	if !second.Equal(utc("2026-03-09T06:00:00Z")) {
		t.Fatalf("second %s", second)
	}
}

func TestNextNever(t *testing.T) {
	c, _ := ParseCron("0 0 31 2 *")
	if _, ok := c.Next(utc("2026-01-01T00:00:00Z"), time.UTC); ok {
		t.Fatal("31 Feb should never run")
	}
}

func TestParseCronErrors(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "a * * * *", "5-1 * * * *", "*/0 * * * *", "1,,2 * * * *"} {
		if _, err := ParseCron(expr); !errors.Is(err, ErrInvalidCron) {
			t.Errorf("%q: want ErrInvalidCron, got %v", expr, err)
		}
	}
}

func TestLoadZoneError(t *testing.T) {
	for _, z := range []string{"", "Mars/Base", "kathmandu"} {
		if _, err := LoadZone(z); !errors.Is(err, ErrInvalidZone) {
			t.Errorf("%q: got %v", z, err)
		}
	}
}
