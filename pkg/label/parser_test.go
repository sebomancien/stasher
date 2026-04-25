package label

import (
	"testing"
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

	root := ParseTree(input)

	if root.Children["stasher"] == nil {
		t.Fatal("expected 'stasher' child")
	}
	stasher := root.Children["stasher"]
	if stasher.Children["schedule"] == nil || stasher.Children["schedule"].Value != "@daily" {
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
