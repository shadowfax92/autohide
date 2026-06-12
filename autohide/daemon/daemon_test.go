package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"autohide/config"
	"autohide/ipc"

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

const hideAllSnapshotJSON = `{
  "ax_trusted": true,
  "frontmost": {"pid": 100, "name": "Google Chrome"},
  "focused_window_id": 42,
  "apps": [
    {"pid": 100, "name": "Google Chrome", "hidden": false},
    {"pid": 200, "name": "NoHide", "hidden": false},
    {"pid": 300, "name": "Slack", "hidden": false},
    {"pid": 400, "name": "Mail", "hidden": false},
    {"pid": 500, "name": "Zoom", "hidden": true}
  ],
  "windows": [
    {"id": 42, "pid": 100, "app": "Google Chrome", "title": "Docs"},
    {"id": 43, "pid": 300, "app": "Slack", "title": "General"},
    {"id": 44, "pid": 400, "app": "Mail", "title": "Inbox"}
  ]
}`

func TestHideAllNativeSkipsIneligibleAppsAndCountsFailures(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/hide.log"
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
snapshot)
cat <<'JSON'
%s
JSON
;;
hide)
if [ "$2" = "400" ]; then
  exit 1
fi
printf '%%s\n' "$2" >> %q
;;
esac
`, hideAllSnapshotJSON, logPath)
	d := testDaemon(t, script)
	d.cfg.Apps["NoHide"] = config.AppConfig{Disabled: true}
	d.SetFocusMode(true)

	data, err := d.HideAll()
	if err != nil {
		t.Fatal(err)
	}
	if data.Hidden != 1 || data.Failed != 1 {
		t.Fatalf("HideAll() = %+v, want 1 hidden and 1 failed", data)
	}
	if !d.IsFocusMode() {
		t.Fatal("hide all must not disable focus mode")
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(raw))
	if len(got) != 1 || got[0] != "300" {
		t.Fatalf("helper hide pids = %v, want only Slack pid 300", got)
	}
}

func TestHideAllFallsBackToLegacyWhenWindowTrackingOff(t *testing.T) {
	d := testDaemon(t, "")
	d.cfg.General.WindowTracking = false
	d.cfg.Apps["NoHide"] = config.AppConfig{Disabled: true}
	d.getFrontmostApp = func() (string, error) { return "Terminal", nil }
	d.getVisibleApps = func() ([]string, error) {
		return []string{"Terminal", "Slack", "NoHide"}, nil
	}
	var hidden []string
	d.hideApp = func(name string) error {
		hidden = append(hidden, name)
		return nil
	}

	data, err := d.HideAll()
	if err != nil {
		t.Fatal(err)
	}
	if data.Hidden != 1 || data.Failed != 0 {
		t.Fatalf("HideAll() = %+v, want 1 hidden and 0 failed", data)
	}
	if len(hidden) != 1 || hidden[0] != "Slack" {
		t.Fatalf("legacy hidden apps = %v, want Slack only", hidden)
	}
}

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

func TestServerHideAllIPC(t *testing.T) {
	sock := tempSock(t)
	d := New(testCfg(), "", zerolog.Nop())
	d.cfg.General.WindowTracking = false
	d.getFrontmostApp = func() (string, error) { return "Terminal", nil }
	d.getVisibleApps = func() ([]string, error) { return []string{"Terminal", "Slack"}, nil }
	d.hideApp = func(name string) error { return nil }
	srv := NewServer(d, sock, zerolog.Nop())
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	resp, err := ipc.NewClient(sock).Send(ipc.Request{Command: "hide_all"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("hide_all failed: %s", resp.Error)
	}
	raw, _ := json.Marshal(resp.Data)
	var data ipc.HideAllData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if data.Hidden != 1 || data.Failed != 0 {
		t.Fatalf("hide_all data = %+v, want 1 hidden and 0 failed", data)
	}
}
