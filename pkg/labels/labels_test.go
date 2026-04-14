package labels_test

import (
	"strings"
	"testing"
	"time"

	"stasher/pkg/labels"
)

func TestParseTree(t *testing.T) {
	input := map[string]string{
		"stasher.enabled":                    "true",
		"stasher.options.stop_during_backup": "false",
		"stasher.volumes.db.path":            "/var/lib/postgresql/data",
		"stasher.volumes.db.retention":       "720h",
		"stasher.volumes.redis.path":         "/data",
		"stasher.schedule":                   "@daily",
	}

	root := labels.ParseTree(input)

	if root.Children["stasher"] == nil {
		t.Fatal("expected 'stasher' child")
	}
	stasher := root.Children["stasher"]
	if stasher.Children["schedule"] == nil || *stasher.Children["schedule"].Value != "@daily" {
		t.Errorf("stasher.schedule: want @daily")
	}
	volumes := stasher.Children["volumes"]
	if volumes == nil {
		t.Fatal("expected 'volumes' child under stasher")
	}
	if volumes.Children["db"] == nil || volumes.Children["redis"] == nil {
		t.Error("expected db and redis under volumes")
	}
}

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
	if err := labels.Parse(input, &cfg); err != nil {
		t.Fatalf("Parse: %v", err)
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
	if err := labels.Parse(map[string]string{}, &cfg); err != nil {
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
	err := labels.Parse(map[string]string{"enabled": "maybe"}, &cfg)
	if err == nil {
		t.Fatal("expected error for invalid bool")
	}
}

func TestUnmarshal_InvalidDuration(t *testing.T) {
	type Config struct {
		TTL time.Duration `label:"ttl"`
	}
	var cfg Config
	err := labels.Parse(map[string]string{"ttl": "notaduration"}, &cfg)
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
		if err := labels.Parse(map[string]string{}, &cfg); err != nil {
			t.Fatalf("Parse: %v", err)
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
		if err := labels.Parse(map[string]string{
			"name":      "override",
			"enabled":   "false",
			"retention": "720h",
		}, &cfg); err != nil {
			t.Fatalf("Parse: %v", err)
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
		err := labels.Parse(map[string]string{"name": "foo"}, &cfg)
		if err == nil {
			t.Fatal("expected error for missing required label")
		}
		if !strings.Contains(err.Error(), "path") {
			t.Errorf("error should mention field name, got: %v", err)
		}
	})

	t.Run("no error when required label present", func(t *testing.T) {
		var cfg Config
		if err := labels.Parse(map[string]string{"path": "/data"}, &cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Path != "/data" {
			t.Errorf("Path: got %q", cfg.Path)
		}
	})

	t.Run("required satisfied by default tag", func(t *testing.T) {
		type WithDefault struct {
			Path string `label:"path" required:"true" default:"/default"`
		}
		var cfg WithDefault
		if err := labels.Parse(map[string]string{}, &cfg); err != nil {
			t.Fatalf("default should satisfy required: %v", err)
		}
		if cfg.Path != "/default" {
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
		err := labels.Parse(map[string]string{"volumes.db.name": "foo"}, &cfg)
		if err == nil {
			t.Fatal("expected error for missing required path inside map entry")
		}
		if !strings.Contains(err.Error(), "path") {
			t.Errorf("error should mention field name, got: %v", err)
		}
	})
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
	if err := labels.Parse(input, &cfg, "stasher"); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Enabled {
		t.Error("Enabled: want true")
	}
	if cfg.Volumes["db"].Path != "/data" {
		t.Errorf("db.Path: got %q", cfg.Volumes["db"].Path)
	}

	// Node.At provides the same scoping for callers using Unmarshal directly
	tree := labels.ParseTree(input)
	var cfg2 Config
	if err := labels.Unmarshal(tree.At("stasher"), &cfg2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg2.Enabled != cfg.Enabled || cfg2.Volumes["db"].Path != cfg.Volumes["db"].Path {
		t.Error("Parse and Unmarshal(tree.At) produced different results")
	}
}
