package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"autohide/ipc"
	"autohide/menubar"

	"github.com/spf13/cobra"
)

func resetRootCommand() {
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	rootCmd.SetArgs(nil)
}

func TestHideAllCommandRegistered(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"hide", "all", "--help"})
	defer resetRootCommand()

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("hide all help failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Hide all eligible background apps") {
		t.Errorf("hide all help missing command description, got: %s", buf.String())
	}
}

func TestRunHideAllPrintsHiddenCount(t *testing.T) {
	old := sendHideAllCmd
	sendHideAllCmd = func() (*ipc.HideAllData, error) {
		return &ipc.HideAllData{Hidden: 2}, nil
	}
	defer func() { sendHideAllCmd = old }()

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	if err := runHideAll(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "Hidden 2 apps." {
		t.Fatalf("output = %q", got)
	}
}

func TestRunHideAllPrintsFailures(t *testing.T) {
	old := sendHideAllCmd
	sendHideAllCmd = func() (*ipc.HideAllData, error) {
		return &ipc.HideAllData{Hidden: 1, Failed: 2}, nil
	}
	defer func() { sendHideAllCmd = old }()

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	if err := runHideAll(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "Hidden 1 app. Failed to hide 2 apps." {
		t.Fatalf("output = %q", got)
	}
}

func TestLaunchedViaBundle(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{"our bundle id", menubar.BundleID, true},
		{"terminal's id", "com.apple.Terminal", false},
		{"unset", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("__CFBundleIdentifier", tc.env)
			if got := launchedViaBundle(); got != tc.want {
				t.Errorf("launchedViaBundle() with %q = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestRootNoArgsOutsideBundlePrintsHelp(t *testing.T) {
	t.Setenv("__CFBundleIdentifier", "com.apple.Terminal")

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{})
	defer resetRootCommand()

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("bare root command failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected help output, got: %s", buf.String())
	}
}

// Finder launches must open the window alongside the daemon — exactly once,
// and spawn failures (UI binary missing in old installs) must not stop the
// daemon from starting.
func TestBundleLaunchSpawnsUIAndRunsDaemon(t *testing.T) {
	t.Setenv("__CFBundleIdentifier", menubar.BundleID)

	spawns, daemonRuns := 0, 0
	origSpawn, origDaemon := spawnUIFn, runDaemonFn
	spawnUIFn = func() error { spawns++; return errors.New("ui binary missing") }
	runDaemonFn = func(cmd *cobra.Command, args []string) error { daemonRuns++; return nil }
	defer func() { spawnUIFn, runDaemonFn = origSpawn, origDaemon }()

	rootCmd.SetArgs([]string{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("bundle launch failed: %v", err)
	}
	if spawns != 1 || daemonRuns != 1 {
		t.Errorf("spawns = %d, daemonRuns = %d; want 1 and 1", spawns, daemonRuns)
	}
}

func TestUICommandRegistered(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if c.Name() == "ui" {
			if c.Short == "" {
				t.Error("ui command needs help text")
			}
			return
		}
	}
	t.Error("ui command not registered")
}
