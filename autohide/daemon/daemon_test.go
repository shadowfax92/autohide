package daemon

import (
	"testing"
	"time"

	"autohide/config"

	"github.com/rs/zerolog"
)

func TestResolveWindowStatus(t *testing.T) {
	cases := []struct {
		name           string
		windowTracking bool
		helperFound    bool
		helperFails    int
		axTrusted      bool
		want           string
	}{
		{"config off wins over everything", false, true, 0, true, "off"},
		{"helper missing", true, false, 0, true, "legacy: helper not found"},
		{"helper failing at threshold", true, true, 3, true, "legacy: helper failing"},
		{"transient failures keep mode", true, true, 2, true, "active"},
		{"no accessibility -> app tier only", true, true, 0, false, "app-only: accessibility not granted"},
		{"all good", true, true, 0, true, "active"},
	}
	for _, c := range cases {
		got := resolveWindowStatus(c.windowTracking, c.helperFound, c.helperFails, c.axTrusted)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func testDaemon(t *testing.T, helperScript string) *Daemon {
	t.Helper()
	cfg := config.Default()
	d := New(cfg, "", zerolog.Nop())
	if helperScript != "" {
		d.helper = NewHelper(writeFakeHelper(t, t.TempDir(), helperScript))
	}
	return d
}

const snapshotScript = "#!/bin/sh\ncat <<'JSON'\n" + fullSnapshotJSON + "\nJSON\n"

// Three consecutive snapshot failures latch legacy mode, but a cooldown
// probe recovers without a daemon restart.
func TestHelperFailureLatchAndRecovery(t *testing.T) {
	d := testDaemon(t, "#!/bin/sh\necho boom >&2\nexit 1\n")
	cfg := d.cfg

	if !d.tickNative(cfg, false) || !d.tickNative(cfg, false) {
		t.Fatal("transient failures must consume the tick (no legacy double-poll)")
	}
	if d.tickNative(cfg, false) {
		t.Fatal("third failure must fall back to legacy this tick")
	}
	if got := d.WindowTrackingStatus(); got != "legacy: helper failing" {
		t.Fatalf("status = %q", got)
	}
	if d.tickNative(cfg, false) {
		t.Fatal("inside cooldown the native path must stay off")
	}

	// Cooldown elapsed; helper healthy again -> probe succeeds and resets.
	d.helper = NewHelper(writeFakeHelper(t, t.TempDir(), snapshotScript))
	d.helperRetryAt = time.Now().Add(-time.Second)
	if !d.tickNative(cfg, false) {
		t.Fatal("probe tick should run natively again")
	}
	if d.helperFails != 0 {
		t.Errorf("helperFails = %d after recovery", d.helperFails)
	}
	if got := d.WindowTrackingStatus(); got != "active" {
		t.Errorf("status = %q after recovery", got)
	}
}

// Leaving the native path (config off) clears window state so list data and
// timers cannot rot while unobserved.
func TestExitNativeResetsWindowState(t *testing.T) {
	d := testDaemon(t, snapshotScript)
	cfg := d.cfg

	if !d.tickNative(cfg, false) {
		t.Fatal("native tick should run")
	}
	if got := windowCount(d.tracker.List(cfg), "Google Chrome"); got != 2 {
		t.Fatalf("expected 2 tracked windows, got %d", got)
	}

	off := *cfg
	off.General.WindowTracking = false
	if d.tickNative(&off, false) {
		t.Fatal("window_tracking=false must route to legacy")
	}
	if got := windowCount(d.tracker.List(cfg), "Google Chrome"); got != -1 && got != 0 {
		t.Errorf("window state must be cleared on native exit, count = %d", got)
	}
	if got := d.WindowTrackingStatus(); got != "off" {
		t.Errorf("status = %q", got)
	}
}

// Startup seeding makes permission chips real even when the native tick
// can't run (window_tracking off); SR stays unknown for old helpers that
// don't report it.
func TestSeedPermissionsFromHelperCheck(t *testing.T) {
	d := testDaemon(t, "#!/bin/sh\necho '{\"ax_trusted\": false, \"screen_recording\": true}'\n")
	d.seedPermissions(d.helper)

	ax, sr := d.Permissions()
	if ax == nil || *ax {
		t.Errorf("ax = %v, want false", ax)
	}
	if sr == nil || !*sr {
		t.Errorf("sr = %v, want true", sr)
	}
}

func TestSeedPermissionsOldHelperLeavesSRUnknown(t *testing.T) {
	d := testDaemon(t, "#!/bin/sh\necho '{\"ax_trusted\": true}'\n")
	d.seedPermissions(d.helper)

	ax, sr := d.Permissions()
	if ax == nil || !*ax {
		t.Errorf("ax = %v, want true", ax)
	}
	if sr != nil {
		t.Errorf("sr = %v, want unknown (nil)", sr)
	}
}
