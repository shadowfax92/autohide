package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultWindowTrackingOn(t *testing.T) {
	if !Default().General.WindowTracking {
		t.Fatal("Default() should enable window_tracking")
	}
}

func TestLoadWithoutKeyKeepsWindowTrackingOn(t *testing.T) {
	path := writeConfig(t, "[general]\ndefault_timeout = \"2m\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.General.WindowTracking {
		t.Error("absent window_tracking key should keep default true")
	}
	if cfg.General.DefaultTimeout.Duration != 2*time.Minute {
		t.Errorf("default_timeout = %v, want 2m", cfg.General.DefaultTimeout.Duration)
	}
}

func TestLoadWindowTrackingFalse(t *testing.T) {
	path := writeConfig(t, "[general]\nwindow_tracking = false\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.WindowTracking {
		t.Error("window_tracking = false should load as false")
	}
}

func TestSaveLoadRoundtripsWindowTracking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.General.WindowTracking = false
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.General.WindowTracking {
		t.Error("saved false should load as false")
	}
}

func TestDefaultFocusSettings(t *testing.T) {
	cfg := Default()
	if cfg.Focus.KeepRecent != 3 {
		t.Errorf("keep_recent = %d, want 3", cfg.Focus.KeepRecent)
	}
	if cfg.Focus.Grace.Duration != 10*time.Second {
		t.Errorf("grace = %v, want 10s", cfg.Focus.Grace.Duration)
	}
}

func TestLoadFocusSettings(t *testing.T) {
	path := writeConfig(t, "[focus]\nkeep_recent = 5\ngrace = \"30s\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Focus.KeepRecent != 5 {
		t.Errorf("keep_recent = %d, want 5", cfg.Focus.KeepRecent)
	}
	if cfg.Focus.Grace.Duration != 30*time.Second {
		t.Errorf("grace = %v, want 30s", cfg.Focus.Grace.Duration)
	}
}

func TestLoadFocusSettingsKeepsDefaultsWhenSectionMissing(t *testing.T) {
	path := writeConfig(t, "[general]\ndefault_timeout = \"2m\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Focus.KeepRecent != 3 || cfg.Focus.Grace.Duration != 10*time.Second {
		t.Errorf("focus = %+v, want default settings", cfg.Focus)
	}
}

func TestLoadNormalizesFocusLowerBounds(t *testing.T) {
	path := writeConfig(t, "[focus]\nkeep_recent = 0\ngrace = \"-5s\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Focus.KeepRecent != 1 {
		t.Errorf("keep_recent = %d, want lower bound 1", cfg.Focus.KeepRecent)
	}
	if cfg.Focus.Grace.Duration != 0 {
		t.Errorf("grace = %v, want lower bound 0", cfg.Focus.Grace.Duration)
	}
}

func TestEffectiveTimeout(t *testing.T) {
	cfg := Default()
	cfg.General.DefaultTimeout = Duration{1 * time.Minute}
	cfg.Apps["Slack"] = AppConfig{Timeout: Duration{5 * time.Minute}}
	cfg.Apps["Terminal"] = AppConfig{Disabled: true}

	cases := []struct {
		app      string
		timeout  time.Duration
		disabled bool
	}{
		{"Slack", 5 * time.Minute, false},
		{"Terminal", 0, true},
		{"Finder", 0, true}, // disabled via default Apps entry + system_exclude
		{"Safari", 1 * time.Minute, false},
	}
	for _, c := range cases {
		timeout, disabled := cfg.EffectiveTimeout(c.app)
		if timeout != c.timeout || disabled != c.disabled {
			t.Errorf("EffectiveTimeout(%q) = (%v, %v), want (%v, %v)",
				c.app, timeout, disabled, c.timeout, c.disabled)
		}
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "500ms"},
		{30 * time.Second, "30s"},
		{time.Minute, "1m"},
		{2 * time.Minute, "2m"},
		{2*time.Minute + 30*time.Second, "2m30s"},
		{5 * time.Minute, "5m"},
		{time.Hour, "1h0m0s"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.in); got != c.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
