package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"autohide/config"

	"github.com/spf13/cobra"
)

func TestRunConfigSetFocusValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(config.Default(), path); err != nil {
		t.Fatal(err)
	}
	previous := cfgFile
	cfgFile = path
	defer func() { cfgFile = previous }()

	if err := runConfigSet(&cobra.Command{}, []string{"focus.keep_recent", "4"}); err != nil {
		t.Fatal(err)
	}
	if err := runConfigSet(&cobra.Command{}, []string{"focus.grace", "30s"}); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Focus.KeepRecent != 4 {
		t.Errorf("keep_recent = %d, want 4", loaded.Focus.KeepRecent)
	}
	if loaded.Focus.Grace.Duration != 30*time.Second {
		t.Errorf("grace = %v, want 30s", loaded.Focus.Grace.Duration)
	}
}

func TestRunConfigSetRejectsInvalidFocusValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(config.Default(), path); err != nil {
		t.Fatal(err)
	}
	previous := cfgFile
	cfgFile = path
	defer func() { cfgFile = previous }()

	for _, args := range [][]string{
		{"focus.keep_recent", "0"},
		{"focus.keep_recent", "many"},
		{"focus.grace", "-1s"},
		{"focus.grace", "later"},
	} {
		if err := runConfigSet(&cobra.Command{}, args); err == nil {
			t.Errorf("runConfigSet(%v) succeeded, want error", args)
		}
	}
}
