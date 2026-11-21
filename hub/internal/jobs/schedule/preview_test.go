package schedule

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFromText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"every day at 9", "0 9 * * *"},
		{"Every day at 9:00", "0 9 * * *"},
		{"daily at 9am", "0 9 * * *"},
		{"every day at 5pm", "0 17 * * *"},
		{"every day at 12am", "0 0 * * *"},
		{"every day at 12pm", "0 12 * * *"},
		{"every day at 14:05.", "5 14 * * *"},
		{"every day at noon", "0 12 * * *"},
		{"weekdays at 9:30", "30 9 * * 1-5"},
		{"every weekday at 9", "0 9 * * 1-5"},
		{"weekends at 10", "0 10 * * 0,6"},
		{"every Monday at 8", "0 8 * * 1"},
		{"mondays at 8", "0 8 * * 1"},
		{"every Monday and Friday at 8", "0 8 * * 1,5"},
		{"every sunday at 7:15pm", "15 19 * * 0"},
		{"first of the month at 10", "0 10 1 * *"},
		{"1st of every month at 10", "0 10 1 * *"},
		{"on the 15th of the month at 8:30", "30 8 15 * *"},
	}
	for _, tc := range tests {
		got, err := FromText(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestFromTextErrors(t *testing.T) {
	for _, in := range []string{"", "every day", "every day at 25", "every day at 9:75", "every day at 13pm",
		"every fortnight at 9", "sometimes at 9", "every blursday at 9", "32nd of the month at 9", "last of the month at 9"} {
		if _, err := FromText(in); !errors.Is(err, ErrInvalidText) {
			t.Errorf("%q: got %v", in, err)
		}
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct{ expr, zone, want string }{
		{"0 9 * * *", "Asia/Kathmandu", "Every day at 9:00, Kathmandu time"},
		{"30 9 * * 1-5", "America/New_York", "Every weekday at 9:30, New York time"},
		{"0 8 * * 1", "UTC", "Every Monday at 8:00, UTC"},
		{"0 8 * * 1,5", "UTC", "Every Monday and Friday at 8:00, UTC"},
		{"0 8 * * 1,3,5", "UTC", "Every Monday, Wednesday and Friday at 8:00, UTC"},
		{"0 10 1 * *", "Europe/London", "On the 1st of every month at 10:00, London time"},
		{"0 10 22 * *", "UTC", "On the 22nd of every month at 10:00, UTC"},
		{"*/15 * * * *", "UTC", "Custom schedule (*/15 * * * *), UTC"},
	}
	for _, tc := range tests {
		got, err := Describe(tc.expr, tc.zone)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.expr, got, err, tc.want)
		}
	}
	if _, err := Describe("bad", "UTC"); !errors.Is(err, ErrInvalidCron) {
		t.Error("want cron error")
	}
	if _, err := Describe("0 9 * * *", "Nope"); !errors.Is(err, ErrInvalidZone) {
		t.Error("want zone error")
	}
}

func TestRoundTrip(t *testing.T) {
	for _, in := range []string{"every day at 9", "weekdays at 9:30", "every Monday at 8", "first of the month at 10"} {
		expr, _ := FromText(in)
		s, err := Describe(expr, "Asia/Kathmandu")
		if err != nil || strings.HasPrefix(s, "Custom") {
			t.Errorf("%q: %q %v", in, s, err)
		}
	}
}

func TestPreview(t *testing.T) {
	now := utc("2026-03-05T12:00:00Z") // Thursday
	tests := []struct {
		name, in, zone, cron, sentence string
		first                          string
	}{
		{"text", "weekdays at 9:30", "Asia/Kathmandu", "30 9 * * 1-5", "Every weekday at 9:30, Kathmandu time", "2026-03-06T03:45:00Z"},
		{"cron", "0 9 * * *", "UTC", "0 9 * * *", "Every day at 9:00, UTC", "2026-03-06T09:00:00Z"},
		{"monthly", "first of the month at 10", "UTC", "0 10 1 * *", "On the 1st of every month at 10:00, UTC", "2026-04-01T10:00:00Z"},
		{"monday", "every Monday at 8", "UTC", "0 8 * * 1", "Every Monday at 8:00, UTC", "2026-03-09T08:00:00Z"},
		{"cron extra spaces", "  0  9 * *  * ", "UTC", "0 9 * * *", "Every day at 9:00, UTC", "2026-03-06T09:00:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Preview(tc.in, tc.zone, now)
			if err != nil {
				t.Fatal(err)
			}
			if r.Cron != tc.cron || r.Sentence != tc.sentence || len(r.Next) != 5 || !r.Next[0].Equal(utc(tc.first)) {
				t.Fatalf("got %+v", r)
			}
			for i := 1; i < 5; i++ {
				if !r.Next[i].After(r.Next[i-1]) {
					t.Fatal("times not increasing")
				}
			}
		})
	}
}

func TestPreviewErrors(t *testing.T) {
	now := time.Now()
	tests := []struct {
		in, zone string
		want     error
	}{
		{"", "UTC", ErrInvalidText},
		{"every day", "UTC", ErrInvalidText},
		{"61 9 * * *", "UTC", ErrInvalidCron},
		{"0 9 * * *", "", ErrInvalidZone},
		{"0 9 * * *", "Mars/Base", ErrInvalidZone},
		{"0 0 31 2 *", "UTC", ErrInvalidCron},
	}
	for _, tc := range tests {
		if _, err := Preview(tc.in, tc.zone, now); !errors.Is(err, tc.want) {
			t.Errorf("%q %q: got %v want %v", tc.in, tc.zone, err, tc.want)
		}
	}
}
