package label

import (
	"fmt"
	"stasher/pkg/label"
)

const ROOT = "stasher"

// Period identifies a calendar granularity for backup retention.
type Period int

const (
	Days Period = iota
	Weeks
	Months
	Years
)

// UnmarshalLabel implements pkg/label.Unmarshaler, allowing Period to be used
// as a map key parsed from label strings ("days", "weeks", "months", "years").
func (p *Period) UnmarshalLabel(s string) error {
	switch s {
	case "days":
		*p = Days
	case "weeks":
		*p = Weeks
	case "months":
		*p = Months
	case "years":
		*p = Years
	default:
		return fmt.Errorf("unknown period %q: want days, weeks, months, or years", s)
	}
	return nil
}

// KeepPolicy maps each active retention period to the number of most-recent
// calendar-bucket representatives to keep. Absent keys mean the tier is not
// configured. A value of 0 disables the tier.
type KeepPolicy map[Period]int

type Container struct {
	Enabled bool              `label:"enabled" default:"false"`
	Options Option            `label:"options"`
	Volumes map[string]Volume `label:"volumes"`
}

type Option struct {
	StopDuringBackup bool `label:"stop_during_backup" default:"false"`
}

type Volume struct {
	Path     string     `label:"path"     required:"true"`
	Schedule string     `label:"schedule" default:"@daily"`
	Keep     KeepPolicy `label:"keep"`
}

func Unmarshal(labels map[string]string) (*Container, error) {
	var container Container
	err := label.Unmarshal(labels, &container, ROOT)
	if err != nil {
		return nil, err
	}
	return &container, nil
}
