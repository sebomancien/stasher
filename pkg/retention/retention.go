package retention

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"stasher/internal/label"
)

// Archive is a backup file with a parsed timestamp.
type Archive struct {
	Name string
	Time time.Time
}

// ParseArchiveTime extracts the UTC timestamp from a filename of the form
// "{archiveName}-YYYYMMdd-HHMMSS.tar.gz". Returns false if the filename does
// not match or the timestamp cannot be parsed.
func ParseArchiveTime(filename, archiveName string) (time.Time, bool) {
	prefix := archiveName + "-"
	const suffix = ".tar.gz"
	if !strings.HasPrefix(filename, prefix) || !strings.HasSuffix(filename, suffix) {
		return time.Time{}, false
	}
	ts := filename[len(prefix) : len(filename)-len(suffix)]
	t, err := time.ParseInLocation("20060102-150405", ts, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Apply returns the names of archives that should be deleted under the given
// policy. If no period in the policy has a value > 0, nil is returned and all
// archives are kept.
//
// For each active period, archives are grouped into calendar buckets. The
// newest archive within each bucket is the representative. The N most recent
// buckets are kept. An archive is deleted only if no period covers it.
func Apply(archives []Archive, policy label.KeepPolicy) []string {
	anyActive := false
	for _, keep := range policy {
		if keep > 0 {
			anyActive = true
			break
		}
	}
	if !anyActive {
		return nil
	}

	kept := make(map[string]bool, len(archives))

	for period, keep := range policy {
		if keep <= 0 {
			continue
		}

		// Group archives into calendar buckets, keeping the newest per bucket.
		buckets := make(map[string]Archive)
		for _, a := range archives {
			key := bucketKey(a.Time, period)
			existing, ok := buckets[key]
			if !ok || a.Time.After(existing.Time) {
				buckets[key] = a
			}
		}

		// Sort buckets newest-representative-first.
		type entry struct {
			key string
			arc Archive
		}
		sorted := make([]entry, 0, len(buckets))
		for k, a := range buckets {
			sorted = append(sorted, entry{k, a})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].arc.Time.After(sorted[j].arc.Time)
		})

		// Mark the top Keep representatives.
		for i, e := range sorted {
			if i >= keep {
				break
			}
			kept[e.arc.Name] = true
		}
	}

	var toDelete []string
	for _, a := range archives {
		if !kept[a.Name] {
			toDelete = append(toDelete, a.Name)
		}
	}
	return toDelete
}

// bucketKey returns the calendar bucket identifier for time t at period p.
func bucketKey(t time.Time, p label.Period) string {
	switch p {
	case label.Days:
		return t.Format("2006-01-02")
	case label.Weeks:
		year, week := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", year, week)
	case label.Months:
		return t.Format("2006-01")
	case label.Years:
		return t.Format("2006")
	default:
		panic(fmt.Sprintf("retention: unknown period %d", p))
	}
}
