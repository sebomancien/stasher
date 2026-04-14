package label

import "time"

const ROOT = "stasher"

type Container struct {
	Enabled bool              `label:"enabled" default:"false"`
	Options Option            `label:"options"`
	Volumes map[string]Volume `label:"volumes"`
}

type Option struct {
	StopDuringBackup bool `label:"stop_during_backup" default:"false"`
}

type Volume struct {
	Path      string        `label:"path" required:"true"`
	Schedule  string        `label:"schedule"  default:"@daily"`
	Retention time.Duration `label:"retention" default:"168h"`
}
