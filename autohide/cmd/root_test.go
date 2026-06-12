package cmd

import (
	"bytes"
	"strings"
	"testing"

	"autohide/menubar"
)

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
	defer func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	}()

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("bare root command failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected help output, got: %s", buf.String())
	}
}
