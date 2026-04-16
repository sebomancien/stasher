package retention_test

import (
	"slices"
	"testing"
	"time"

	"stasher/internal/label"
	"stasher/pkg/retention"
)

// TestApply_FullYear tests tiered retention against 731 daily archives spanning
// 2024-01-01 to 2025-12-31 (2024 is a leap year: 366 + 365 = 731 days).
//
// Policy: days=7, weeks=4, months=12, years=2.
//
// Expected kept archives (21 total):
//
//	days=7  → Dec 25–31, 2025 (7 archives)
//	weeks=4 → 4 most recent ISO-week representatives:
//	            2026-W01 (Dec 29–31): rep=Dec 31 — already in days
//	            2025-W52 (Dec 22–28): rep=Dec 28 — already in days
//	            2025-W51 (Dec 15–21): rep=Dec 21 — new
//	            2025-W50 (Dec  8–14): rep=Dec 14 — new
//	months=12 → newest per calendar month for Dec 2025–Jan 2025:
//	            Dec: Dec 31 — already in days; Nov–Jan: month-end dates — new (11)
//	years=2 → newest per year: 2025=Dec 31 (already kept); 2024=Dec 31 — new (1)
//
// Total new: 7 (days) + 2 (weeks) + 11 (months) + 1 (years) = 21 kept, 710 deleted.
func TestApply_FullYear(t *testing.T) {
	const archiveName = "app-data"

	// Build 731 daily archives at noon UTC.
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 31, 12, 0, 0, 0, time.UTC)

	var archives []retention.Archive
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		archives = append(archives, retention.Archive{
			Name: archiveName + "-" + d.Format("20060102-150405") + ".tar.gz",
			Time: d,
		})
	}
	if len(archives) != 731 {
		t.Fatalf("expected 731 archives, got %d", len(archives))
	}

	policy := label.KeepPolicy{
		label.Days:   7,
		label.Weeks:  4,
		label.Months: 12,
		label.Years:  2,
	}

	toDelete := retention.Apply(archives, policy)

	// Resolve kept archives.
	deletedSet := make(map[string]bool, len(toDelete))
	for _, name := range toDelete {
		deletedSet[name] = true
	}
	keptDates := make(map[string]bool)
	for _, a := range archives {
		if !deletedSet[a.Name] {
			keptDates[a.Time.Format("2006-01-02")] = true
		}
	}

	// Expected kept dates with their retention rationale.
	wantKept := map[string]string{
		// days=7
		"2025-12-31": "days + weeks(W01/2026) + months(Dec) + years(2025)",
		"2025-12-30": "days + weeks(W01/2026)",
		"2025-12-29": "days + weeks(W01/2026)",
		"2025-12-28": "days + weeks(W52)",
		"2025-12-27": "days",
		"2025-12-26": "days",
		"2025-12-25": "days",
		// weeks=4 (new - W52 & W01 already covered above)
		"2025-12-21": "weeks(W51)",
		"2025-12-14": "weeks(W50)",
		// months=12 (new — Dec already covered above)
		"2025-11-30": "months(Nov)",
		"2025-10-31": "months(Oct)",
		"2025-09-30": "months(Sep)",
		"2025-08-31": "months(Aug)",
		"2025-07-31": "months(Jul)",
		"2025-06-30": "months(Jun)",
		"2025-05-31": "months(May)",
		"2025-04-30": "months(Apr)",
		"2025-03-31": "months(Mar)",
		"2025-02-28": "months(Feb) — 2025 is not a leap year",
		"2025-01-31": "months(Jan)",
		// years=2 (new — 2025 already covered above)
		"2024-12-31": "years(2024)",
	}

	const wantKeptCount = 21
	const wantDeletedCount = 731 - wantKeptCount // 710

	got := len(keptDates)
	if got != wantKeptCount {
		t.Errorf("kept count: got %d, want %d", got, wantKeptCount)
	}
	got = len(toDelete)
	if got != wantDeletedCount {
		t.Errorf("deleted count: got %d, want %d", got, wantDeletedCount)
	}

	for date, reason := range wantKept {
		if !keptDates[date] {
			t.Errorf("expected %s to be kept (%s) but it was deleted", date, reason)
		}
	}
	for date := range keptDates {
		_, ok := wantKept[date]
		if !ok {
			t.Errorf("unexpectedly kept %s", date)
		}
	}
}

func TestParseArchiveTime(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		archiveName string
		wantOK      bool
		wantTime    time.Time
	}{
		{
			name:        "valid",
			filename:    "postgres-data-20260410-020000.tar.gz",
			archiveName: "postgres-data",
			wantOK:      true,
			wantTime:    time.Date(2026, 4, 10, 2, 0, 0, 0, time.UTC),
		},
		{
			name:        "archiveName with multiple dashes",
			filename:    "my-app-vol1-20260101-120000.tar.gz",
			archiveName: "my-app-vol1",
			wantOK:      true,
			wantTime:    time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:        "wrong prefix",
			filename:    "other-20260410-020000.tar.gz",
			archiveName: "postgres-data",
			wantOK:      false,
		},
		{
			name:        "wrong suffix",
			filename:    "postgres-data-20260410-020000.tar",
			archiveName: "postgres-data",
			wantOK:      false,
		},
		{
			name:        "malformed timestamp",
			filename:    "postgres-data-notadate.tar.gz",
			archiveName: "postgres-data",
			wantOK:      false,
		},
		{
			name:        "empty filename",
			filename:    "",
			archiveName: "postgres-data",
			wantOK:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := retention.ParseArchiveTime(tc.filename, tc.archiveName)
			if ok != tc.wantOK {
				t.Fatalf("ParseArchiveTime(%q, %q): ok=%v, want %v", tc.filename, tc.archiveName, ok, tc.wantOK)
			}
			if tc.wantOK && !got.Equal(tc.wantTime) {
				t.Errorf("time: got %v, want %v", got, tc.wantTime)
			}
		})
	}
}

func TestApply(t *testing.T) {
	// Helper to build Archive slices from compact (name, RFC3339) pairs.
	arc := func(pairs ...string) []retention.Archive {
		var out []retention.Archive
		for i := 0; i < len(pairs); i += 2 {
			ts, err := time.Parse(time.RFC3339, pairs[i+1])
			if err != nil {
				t.Fatalf("bad test time %q: %v", pairs[i+1], err)
			}
			out = append(out, retention.Archive{Name: pairs[i], Time: ts})
		}
		return out
	}

	tests := []struct {
		name     string
		archives []retention.Archive
		policy   label.KeepPolicy
		want     []string // names expected to be deleted; nil = no deletions
	}{
		{
			name:     "no tiers — keep everything",
			archives: arc("a", "2026-01-01T00:00:00Z", "b", "2026-01-02T00:00:00Z"),
			policy:   label.KeepPolicy{label.Days: 0},
			want:     nil,
		},
		{
			name:     "empty archive list",
			archives: nil,
			policy:   label.KeepPolicy{label.Days: 7},
			want:     nil,
		},
		{
			name: "days: keep 3, 5 distinct days",
			archives: arc(
				"d5", "2026-01-05T12:00:00Z",
				"d4", "2026-01-04T12:00:00Z",
				"d3", "2026-01-03T12:00:00Z",
				"d2", "2026-01-02T12:00:00Z",
				"d1", "2026-01-01T12:00:00Z",
			),
			policy: label.KeepPolicy{label.Days: 3},
			want:   []string{"d2", "d1"}, // oldest two days deleted
		},
		{
			name: "days: multiple archives same day, newest kept",
			archives: arc(
				"d1-late", "2026-01-01T23:00:00Z",
				"d1-early", "2026-01-01T01:00:00Z",
				"d2", "2026-01-02T12:00:00Z",
			),
			// Both days kept; d1-early is not the bucket representative (d1-late is),
			// so d1-early is deleted even though its day is covered.
			policy: label.KeepPolicy{label.Days: 2},
			want:   []string{"d1-early"},
		},
		{
			name: "weeks: keep 2",
			archives: arc(
				"w3", "2026-01-19T12:00:00Z", // week 4
				"w2", "2026-01-12T12:00:00Z", // week 3
				"w1", "2026-01-05T12:00:00Z", // week 2
				"w0", "2025-12-29T12:00:00Z", // week 1
			),
			policy: label.KeepPolicy{label.Weeks: 2},
			want:   []string{"w1", "w0"},
		},
		{
			name: "months: keep 2",
			archives: arc(
				"m3", "2026-03-15T12:00:00Z",
				"m2", "2026-02-15T12:00:00Z",
				"m1", "2026-01-15T12:00:00Z",
				"m0", "2025-12-15T12:00:00Z",
			),
			policy: label.KeepPolicy{label.Months: 2},
			want:   []string{"m1", "m0"},
		},
		{
			name: "years: keep 2",
			archives: arc(
				"y3", "2026-06-01T00:00:00Z",
				"y2", "2025-06-01T00:00:00Z",
				"y1", "2024-06-01T00:00:00Z",
				"y0", "2023-06-01T00:00:00Z",
			),
			policy: label.KeepPolicy{label.Years: 2},
			want:   []string{"y1", "y0"},
		},
		{
			name: "multi-tier: archive covered by multiple tiers is kept",
			// "recent" is covered by both days=1 and months=1; "old" is covered by neither.
			archives: arc(
				"recent", "2026-04-15T12:00:00Z",
				"old", "2025-01-01T12:00:00Z",
			),
			policy: label.KeepPolicy{
				label.Days:   1, // keep only the most recent day
				label.Months: 1, // keep only the most recent month
			},
			want: []string{"old"},
		},
		{
			name: "GFS staircase",
			// Build 14 archives: daily for 2 weeks
			archives: arc(
				"d14", "2026-04-15T12:00:00Z",
				"d13", "2026-04-14T12:00:00Z",
				"d12", "2026-04-13T12:00:00Z",
				"d11", "2026-04-12T12:00:00Z",
				"d10", "2026-04-11T12:00:00Z",
				"d09", "2026-04-10T12:00:00Z",
				"d08", "2026-04-09T12:00:00Z",
				"d07", "2026-04-08T12:00:00Z",
				"d06", "2026-04-07T12:00:00Z",
				"d05", "2026-04-06T12:00:00Z",
				"d04", "2026-04-05T12:00:00Z",
				"d03", "2026-04-04T12:00:00Z",
				"d02", "2026-04-03T12:00:00Z",
				"d01", "2026-04-02T12:00:00Z",
			),
			// days keeps d14..d08; weeks(2) keeps the two most recent ISO-week
			// representatives. Both fall within d14..d08, so d07..d01 are deleted.
			policy: label.KeepPolicy{
				label.Days:  7,
				label.Weeks: 2,
			},
			want: []string{"d07", "d06", "d05", "d04", "d03", "d02", "d01"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := retention.Apply(tc.archives, tc.policy)

			if tc.want == nil {
				if len(got) != 0 {
					t.Errorf("expected no deletions, got %v", got)
				}
				return
			}

			slices.Sort(got)
			want := slices.Clone(tc.want)
			slices.Sort(want)

			if !slices.Equal(got, want) {
				t.Errorf("deleted:\n  got  %v\n  want %v", got, want)
			}
		})
	}
}
