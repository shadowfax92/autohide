package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultAllowsFinderToAutoHide(t *testing.T) {
	cfg := Default()

	timeout, disabled := cfg.EffectiveTimeout("Finder")

	if disabled {
		t.Fatal("Finder should not be disabled by the default config")
	}
	if timeout != cfg.General.DefaultTimeout.Duration {
		t.Fatalf("Finder timeout = %s, want default timeout %s", timeout, cfg.General.DefaultTimeout.Duration)
	}
}

func TestLoadMigratesLegacyFinderDefaultExclusion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, path, `
[general]
default_timeout = "1m"
check_interval = "5s"
system_exclude = ["Finder"]

[apps.Finder]
disabled = true
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if containsString(cfg.General.SystemExclude, "Finder") {
		t.Fatal("legacy Finder system exclusion should be removed")
	}
	if _, ok := cfg.Apps["Finder"]; ok {
		t.Fatal("legacy Finder app override should be removed")
	}
	timeout, disabled := cfg.EffectiveTimeout("Finder")
	if disabled {
		t.Fatal("Finder should be enabled after legacy default migration")
	}
	if timeout != time.Minute {
		t.Fatalf("Finder timeout = %s, want 1m", timeout)
	}
}

func TestLoadKeepsExplicitFinderDisable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, path, `
[general]
default_timeout = "1m"
check_interval = "5s"
system_exclude = []

[apps.Finder]
disabled = true
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := cfg.Apps["Finder"]; !ok {
		t.Fatal("explicit Finder app override should be preserved")
	}
	_, disabled := cfg.EffectiveTimeout("Finder")
	if !disabled {
		t.Fatal("explicit Finder disable should still be honored")
	}
}

func writeConfig(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
