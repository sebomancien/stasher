package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Cron struct {
	minutes [60]bool
	hours   [24]bool
	doms    [32]bool // Index starts at 1
	months  [13]bool // Index starts at 1
	dows    [7]bool
	domStar bool // dom field was literally "*"
	dowStar bool // dow field was literally "*"
}

func NewCron(s string) (*Cron, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "@yearly", "@annually":
		s = "0 0 1 1 *"
	case "@monthly":
		s = "0 0 1 * *"
	case "@weekly":
		s = "0 0 * * 0"
	case "@daily", "@midnight":
		s = "0 0 * * *"
	case "@hourly":
		s = "0 * * * *"
	}

	fields := strings.Fields(s)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields, got %d: %q", len(fields), s)
	}

	var err error
	cron := Cron{
		domStar: fields[2] == "*",
		dowStar: fields[4] == "*",
	}

	err = parseField(cron.minutes[:], fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	err = parseField(cron.hours[:], fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	err = parseField(cron.doms[:], fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	err = parseField(cron.months[:], fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	err = parseField(cron.dows[:], fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}

	return &cron, nil
}

// Equal reports whether c and other fire at exactly the same set of times.
// Equivalent expressions such as "@daily" and "0 0 * * *" compare as equal.
func (c *Cron) Equal(other *Cron) bool {
	if other == nil {
		return false
	}
	return c.domStar == other.domStar &&
		c.dowStar == other.dowStar &&
		c.minutes == other.minutes &&
		c.hours == other.hours &&
		c.doms == other.doms &&
		c.months == other.months &&
		c.dows == other.dows
}

// NextOccurrence returns the earliest time strictly after from that satisfies
// the cron expression. Times are in the same timezone as from.
func (c *Cron) NextOccurrence(from time.Time) (time.Time, error) {

	// Search between a minute to 4 years after the requested time
	start := from.Truncate(time.Minute).Add(time.Minute)
	end := start.AddDate(4, 0, 0)

	t := start
	for t.Before(end) {
		// Month check (1-12)
		if !c.months[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}

		// Day check
		domOK := c.doms[t.Day()]
		dowOK := c.dows[int(t.Weekday())]
		var dayOK bool
		switch {
		case c.domStar && c.dowStar:
			dayOK = true
		case c.domStar:
			dayOK = dowOK
		case c.dowStar:
			dayOK = domOK
		default:
			// Both restricted: standard cron OR semantics
			dayOK = domOK || dowOK
		}
		if !dayOK {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}

		// Hour check
		if !c.hours[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}

		// Minute check
		if !c.minutes[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}

		return t, nil
	}

	return time.Time{}, fmt.Errorf("cron %v: no occurrence found in next 4 years", c)
}

// parseField parses one cron field (comma-separated list of ranges/values/steps).
// min and max are the valid value bounds for this field (e.g. 1–31 for dom).
func parseField(values []bool, s string, min, max int) error {
	for part := range strings.SplitSeq(s, ",") {
		vals, err := parseRange(strings.TrimSpace(part), min, max)
		if err != nil {
			return err
		}
		for _, v := range vals {
			values[v] = true
		}
	}
	return nil
}

// parseRange parses a single cron range element: *, n, n-m, */step, n/step, n-m/step.
func parseRange(s string, min, max int) ([]int, error) {
	step := 1
	idx := strings.Index(s, "/")
	if idx >= 0 {
		var err error
		step, err = strconv.Atoi(s[idx+1:])
		if err != nil || step <= 0 {
			return nil, fmt.Errorf("invalid step %q", s[idx+1:])
		}
		s = s[:idx]
	}

	var lo, hi int
	switch {
	case s == "*":
		lo, hi = min, max
	case strings.Contains(s, "-"):
		parts := strings.SplitN(s, "-", 2)
		var err error
		lo, err = strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid range start %q", parts[0])
		}
		hi, err = strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid range end %q", parts[1])
		}
	default:
		var err error
		lo, err = strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("invalid value %q", s)
		}
		if step == 1 {
			hi = lo
		} else {
			hi = max
		}
	}

	if lo < min || hi > max {
		return nil, fmt.Errorf("value %d-%d out of range [%d-%d]", lo, hi, min, max)
	}
	if lo > hi {
		return nil, fmt.Errorf("range start %d > end %d", lo, hi)
	}

	vals := make([]int, 0, (hi-lo)/step+1)
	for v := lo; v <= hi; v += step {
		vals = append(vals, v)
	}
	return vals, nil
}
