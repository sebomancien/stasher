package cron_test

import (
	"strings"
	"testing"
	"time"

	"stasher/pkg/cron"
)

// from is a fixed reference time: Thursday 2026-01-15 10:30:00 UTC
var from = time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

func mustNext(t *testing.T, schedule string, from time.Time) time.Time {
	t.Helper()
	c, err := cron.NewCron(schedule)
	if err != nil {
		t.Fatalf("invalid cron string: (%q): %v", schedule, err)
	}
	next, err := c.NextOccurrence(from)
	if err != nil {
		t.Fatalf("NextOccurrence(%q): %v", schedule, err)
	}
	return next
}

func TestNextOccurrence_DailyAtHour(t *testing.T) {
	// 03:00 already passed today (from is 10:30), so next is tomorrow
	got := mustNext(t, "0 3 * * *", from)
	want := time.Date(2026, 1, 16, 3, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// 11:00 has not yet passed today
	got = mustNext(t, "0 11 * * *", from)
	want = time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_Aliases(t *testing.T) {
	tests := []struct {
		alias string
		equiv string
	}{
		{"@daily", "0 0 * * *"},
		{"@midnight", "0 0 * * *"},
		{"@hourly", "0 * * * *"},
		{"@weekly", "0 0 * * 0"},
		{"@monthly", "0 0 1 * *"},
		{"@yearly", "0 0 1 1 *"},
		{"@annually", "0 0 1 1 *"},
	}
	for _, tc := range tests {
		t.Run(tc.alias, func(t *testing.T) {
			a := mustNext(t, tc.alias, from)
			b := mustNext(t, tc.equiv, from)
			if !a.Equal(b) {
				t.Errorf("%s (%v) != %s (%v)", tc.alias, a, tc.equiv, b)
			}
		})
	}
}

func TestNextOccurrence_Hourly(t *testing.T) {
	// from is 10:30 → next top-of-hour is 11:00
	got := mustNext(t, "@hourly", from)
	want := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_StepHours(t *testing.T) {
	// hours 0,6,12,18 — from 10:30 next is 12:00
	got := mustNext(t, "0 */6 * * *", from)
	want := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_StepMinutes(t *testing.T) {
	// every 15 minutes: :00,:15,:30,:45 — from 10:30 next is 10:45
	got := mustNext(t, "*/15 * * * *", from)
	want := time.Date(2026, 1, 15, 10, 45, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_WeekdayRestriction(t *testing.T) {
	// Monday at 02:30 — from Thu 2026-01-15, next Mon is 2026-01-19
	got := mustNext(t, "30 2 * * 1", from)
	want := time.Date(2026, 1, 19, 2, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_Weekly(t *testing.T) {
	// @weekly = Sunday 00:00 — from Thu 2026-01-15, next Sun is 2026-01-18
	got := mustNext(t, "@weekly", from)
	want := time.Date(2026, 1, 18, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_Monthly(t *testing.T) {
	// 1st of each month at 00:00 — from Jan 15, next is Feb 1
	got := mustNext(t, "@monthly", from)
	want := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_Yearly(t *testing.T) {
	// Jan 1 at 00:00 — from Jan 15, next is Jan 1 of next year
	got := mustNext(t, "@yearly", from)
	want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_CommaList(t *testing.T) {
	// Mon(1), Wed(3), Fri(5) at midnight — from Thu 2026-01-15 → Fri 2026-01-16
	got := mustNext(t, "0 0 * * 1,3,5", from)
	want := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_RangeField(t *testing.T) {
	// 9-17 on weekdays (Mon-Fri) at :00 — from Thu 10:30 → Thu 11:00
	got := mustNext(t, "0 9-17 * * 1-5", from)
	want := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_MonthRestriction(t *testing.T) {
	// only in March — from Jan 15, next is Mar 1 at 00:00
	got := mustNext(t, "0 0 1 3 *", from)
	want := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_ExactlyAtBoundary(t *testing.T) {
	// from is exactly 10:30:00 — "30 10 * * *" must return tomorrow (strictly after)
	got := mustNext(t, "30 10 * * *", from)
	want := time.Date(2026, 1, 16, 10, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextOccurrence_Invalid(t *testing.T) {
	cases := []string{
		"",
		"invalid",
		"@unknown",
		"* * * *",     // too few fields
		"* * * * * *", // too many fields
		"60 * * * *",  // minute out of range
		"* 24 * * *",  // hour out of range
		"* * 0 * *",   // dom out of range (min 1)
		"* * * 13 *",  // month out of range
		"* * * * 7",   // dow out of range
		"* * * * */0", // step zero
		"abc * * * *", // non-numeric
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			_, err := cron.NewCron(s)
			if err == nil {
				t.Errorf("NextOccurrence(%q): expected error, got nil", s)
			}
		})
	}
}

func TestNextOccurrence_InvalidErrorMessage(t *testing.T) {
	_, err := cron.NewCron("60 * * * *")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "minute") {
		t.Errorf("error should mention field name, got: %v", err)
	}
}

func TestCron_Equal(t *testing.T) {
	mustCron := func(s string) *cron.Cron {
		t.Helper()
		c, err := cron.NewCron(s)
		if err != nil {
			t.Fatalf("NewCron(%q): %v", s, err)
		}
		return c
	}

	equal := [][2]string{
		{"@daily", "0 0 * * *"},
		{"@midnight", "0 0 * * *"},
		{"@hourly", "0 * * * *"},
		{"@weekly", "0 0 * * 0"},
		{"@monthly", "0 0 1 * *"},
		{"@yearly", "0 0 1 1 *"},
		{"@annually", "0 0 1 1 *"},
		{"0 3 * * *", "0 3 * * *"},
		{"*/15 * * * *", "0,15,30,45 * * * *"},
	}
	for _, tc := range equal {
		a, b := mustCron(tc[0]), mustCron(tc[1])
		if !a.Equal(b) {
			t.Errorf("Equal(%q, %q): want true", tc[0], tc[1])
		}
		if !b.Equal(a) {
			t.Errorf("Equal(%q, %q) not symmetric", tc[1], tc[0])
		}
	}

	notEqual := [][2]string{
		{"@daily", "@hourly"},
		{"0 3 * * *", "0 4 * * *"},
		{"0 0 * * 1", "0 0 * * *"}, // weekday restricted vs wildcard
	}
	for _, tc := range notEqual {
		a, b := mustCron(tc[0]), mustCron(tc[1])
		if a.Equal(b) {
			t.Errorf("Equal(%q, %q): want false", tc[0], tc[1])
		}
	}
}
