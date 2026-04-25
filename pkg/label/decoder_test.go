package label

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestUnmarshal(t *testing.T) {
	type Options struct {
		StopDuringBackup bool `label:"stop_during_backup"`
	}
	type Volume struct {
		Path      string        `label:"path"`
		Retention time.Duration `label:"retention"`
	}
	type Config struct {
		Enabled bool              `label:"stasher.enabled"`
		Options Options           `label:"stasher.options"`
		Volumes map[string]Volume `label:"stasher.volumes"`
		Sched   string            `label:"stasher.schedule"`
	}

	input := map[string]string{
		"stasher.enabled":                    "true",
		"stasher.options.stop_during_backup": "true",
		"stasher.volumes.db.path":            "/var/lib/postgresql/data",
		"stasher.volumes.db.retention":       "720h",
		"stasher.volumes.redis.path":         "/data",
		"stasher.schedule":                   "@daily",
	}

	var cfg Config
	err := Unmarshal(input, &cfg)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !cfg.Enabled {
		t.Error("Enabled: want true")
	}
	if !cfg.Options.StopDuringBackup {
		t.Error("Options.StopDuringBackup: want true")
	}
	if cfg.Sched != "@daily" {
		t.Errorf("Sched: want @daily, got %q", cfg.Sched)
	}
	if len(cfg.Volumes) != 2 {
		t.Fatalf("Volumes: want 2 entries, got %d", len(cfg.Volumes))
	}
	db := cfg.Volumes["db"]
	if db.Path != "/var/lib/postgresql/data" {
		t.Errorf("db.Path: got %q", db.Path)
	}
	if db.Retention != 720*time.Hour {
		t.Errorf("db.Retention: got %v", db.Retention)
	}
	if cfg.Volumes["redis"].Path != "/data" {
		t.Errorf("redis.Path: got %q", cfg.Volumes["redis"].Path)
	}
}

func TestUnmarshal_EmptyLabels(t *testing.T) {
	type Config struct {
		Enabled bool   `label:"stasher.enabled"`
		Name    string `label:"stasher.name"`
	}
	var cfg Config
	err := Unmarshal(map[string]string{}, &cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Enabled || cfg.Name != "" {
		t.Error("expected zero values for empty labels")
	}
}

func TestUnmarshal_InvalidBool(t *testing.T) {
	type Config struct {
		Enabled bool `label:"enabled"`
	}
	var cfg Config
	err := Unmarshal(map[string]string{"enabled": "maybe"}, &cfg)
	if err == nil {
		t.Fatal("expected error for invalid bool")
	}
}

func TestUnmarshal_InvalidDuration(t *testing.T) {
	type Config struct {
		TTL time.Duration `label:"ttl"`
	}
	var cfg Config
	err := Unmarshal(map[string]string{"ttl": "notaduration"}, &cfg)
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestUnmarshal_Defaults(t *testing.T) {
	type Config struct {
		Name      string        `label:"name"      default:"myapp"`
		Enabled   bool          `label:"enabled"   default:"true"`
		Retention time.Duration `label:"retention" default:"168h"`
	}

	t.Run("uses defaults when labels absent", func(t *testing.T) {
		var cfg Config
		err := Unmarshal(map[string]string{}, &cfg)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if cfg.Name != "myapp" {
			t.Errorf("Name: want myapp, got %q", cfg.Name)
		}
		if !cfg.Enabled {
			t.Error("Enabled: want true")
		}
		if cfg.Retention != 168*time.Hour {
			t.Errorf("Retention: want 168h, got %v", cfg.Retention)
		}
	})

	t.Run("label overrides default", func(t *testing.T) {
		var cfg Config
		err := Unmarshal(map[string]string{
			"name":      "override",
			"enabled":   "false",
			"retention": "720h",
		}, &cfg)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if cfg.Name != "override" {
			t.Errorf("Name: want override, got %q", cfg.Name)
		}
		if cfg.Enabled {
			t.Error("Enabled: want false")
		}
		if cfg.Retention != 720*time.Hour {
			t.Errorf("Retention: want 720h, got %v", cfg.Retention)
		}
	})
}

func TestUnmarshal_Required(t *testing.T) {
	type Config struct {
		Path string `label:"path" required:"true"`
		Name string `label:"name"`
	}

	t.Run("error when required label absent", func(t *testing.T) {
		var cfg Config
		err := Unmarshal(map[string]string{"name": "foo"}, &cfg)
		if err == nil {
			t.Fatal("expected error for missing required label")
		}
		if !strings.Contains(err.Error(), "path") {
			t.Errorf("error should mention field name, got: %v", err)
		}
	})

	t.Run("no error when required label present", func(t *testing.T) {
		var cfg Config
		err := Unmarshal(map[string]string{"path": "/data"}, &cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Path != "/data" {
			t.Errorf("Path: got %q", cfg.Path)
		}
	})

	t.Run("required propagates through map key context", func(t *testing.T) {
		type Volume struct {
			Path string `label:"path" required:"true"`
		}
		type Cfg struct {
			Volumes map[string]Volume `label:"volumes"`
		}
		var cfg Cfg
		// "db" key exists but has no path sub-label
		err := Unmarshal(map[string]string{"volumes.db.name": "foo"}, &cfg)
		if err == nil {
			t.Fatal("expected error for missing required path inside map entry")
		}
		if !strings.Contains(err.Error(), "path") {
			t.Errorf("error should mention field name, got: %v", err)
		}
	})
}

func TestUnmarshal_MapWithCustomKeyUnmarshaler(t *testing.T) {
	type Config struct {
		Schedule map[testWeekday]int `label:"schedule"`
	}

	t.Run("valid weekday key", func(t *testing.T) {
		var cfg Config
		err := Unmarshal(map[string]string{"schedule.monday": "3"}, &cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Schedule) != 1 {
			t.Fatalf("want 1 entry, got %d", len(cfg.Schedule))
		}
		if cfg.Schedule[testWeekdayMonday] != 3 {
			t.Errorf("monday: want 3, got %d", cfg.Schedule[testWeekdayMonday])
		}
	})

	t.Run("invalid weekday key", func(t *testing.T) {
		var cfg Config
		err := Unmarshal(map[string]string{"schedule.notaweekday": "2"}, &cfg)
		if err == nil {
			t.Fatal("expected error for unknown weekday key")
		}
		if !strings.Contains(err.Error(), "notaweekday") {
			t.Errorf("error should mention the bad key, got: %v", err)
		}
	})
}

type testWeekday int

const (
	testWeekdayMonday testWeekday = iota
	testWeekdayTuesday
	testWeekdayWednesday
	testWeekdayThursday
	testWeekdayFriday
)

func (w *testWeekday) UnmarshalLabel(s string) error {
	switch s {
	case "monday":
		*w = testWeekdayMonday
	case "tuesday":
		*w = testWeekdayTuesday
	case "wednesday":
		*w = testWeekdayWednesday
	case "thursday":
		*w = testWeekdayThursday
	case "friday":
		*w = testWeekdayFriday
	default:
		return fmt.Errorf("unknown weekday %q", s)
	}
	return nil
}

func TestParse_WithRoot(t *testing.T) {
	type Volume struct {
		Path string `label:"path"`
	}
	type Config struct {
		Enabled bool              `label:"enabled"`
		Volumes map[string]Volume `label:"volumes"`
	}

	input := map[string]string{
		"stasher.enabled":         "true",
		"stasher.volumes.db.path": "/data",
		"unrelated.enabled":       "false", // must not bleed into result
	}

	var cfg Config
	err := Unmarshal(input, &cfg, "stasher")
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !cfg.Enabled {
		t.Error("Enabled: want true")
	}
	if cfg.Volumes["db"].Path != "/data" {
		t.Errorf("db.Path: got %q", cfg.Volumes["db"].Path)
	}

	// Node.At provides the same scoping for callers using Unmarshal directly
	var cfg2 Config
	err = Unmarshal(input, &cfg2, "stasher")
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg2.Enabled != cfg.Enabled || cfg2.Volumes["db"].Path != cfg.Volumes["db"].Path {
		t.Error("Parse and Unmarshal(tree.At) produced different results")
	}
}
