package daemon

import (
	"bytes"
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

func requireUnhidable(t *testing.T, d *Daemon, name, want string) {
	t.Helper()
	for _, app := range d.TrackerList() {
		if app.Name == name {
			if app.Unhidable != want {
				t.Fatalf("%s list reason = %q, want %q", name, app.Unhidable, want)
			}
			return
		}
	}
	t.Fatalf("%s missing from tracker list", name)
}

const snapshotScript = "#!/bin/sh\ncat <<'JSON'\n" + fullSnapshotJSON + "\nJSON\n"

const hideAllSnapshotJSON = `{
  "ax_trusted": true,
  "frontmost": {"pid": 100, "name": "Google Chrome"},
  "focused_window_id": 42,
  "apps": [
    {"pid": 100, "name": "Google Chrome", "hidden": false, "unhidable": ""},
    {"pid": 200, "name": "NoHide", "hidden": false, "unhidable": ""},
    {"pid": 300, "name": "Slack", "hidden": false, "unhidable": ""},
    {"pid": 400, "name": "Mail", "hidden": false, "unhidable": "fullscreen"},
    {"pid": 500, "name": "Zoom", "hidden": true, "unhidable": ""},
    {"pid": 600, "name": "Calendar", "hidden": false, "unhidable": ""}
  ],
  "windows": [
    {"id": 42, "pid": 100, "app": "Google Chrome", "title": "Docs"},
    {"id": 43, "pid": 300, "app": "Slack", "title": "General"},
    {"id": 44, "pid": 400, "app": "Mail", "title": "Inbox"},
    {"id": 45, "pid": 600, "app": "Calendar", "title": "Week"}
  ]
}`

const focusSnapshotJSON = `{
  "ax_trusted": true,
  "frontmost": {"pid": 100, "name": "Google Chrome"},
  "focused_window_id": 42,
  "apps": [
    {"pid": 100, "name": "Google Chrome", "hidden": false},
    {"pid": 200, "name": "Terminal", "hidden": false},
    {"pid": 300, "name": "Slack", "hidden": false},
    {"pid": 400, "name": "Music", "hidden": false}
  ],
  "windows": [
    {"id": 42, "pid": 100, "app": "Google Chrome", "title": "Docs"},
    {"id": 43, "pid": 200, "app": "Terminal", "title": "Shell"},
    {"id": 44, "pid": 300, "app": "Slack", "title": "General"},
    {"id": 45, "pid": 400, "app": "Music", "title": "Player"}
  ]
}`

func TestFocusTickNativeHidesOnlyOutsideRecentSet(t *testing.T) {
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
printf '%%s\n' "$2" >> %q
;;
esac
`, focusSnapshotJSON, logPath)
	d := testDaemon(t, script)
	d.cfg.Focus.KeepRecent = 3
	d.cfg.Focus.Grace = config.Duration{}

	apps := []SnapApp{chromeApp(), terminalApp(), {Pid: 300, Name: "Slack"}, {Pid: 400, Name: "Music"}}
	wins := []SnapWindow{
		win(42, 100, "Google Chrome"),
		win(43, 200, "Terminal"),
		win(44, 300, "Slack"),
		win(45, 400, "Music"),
	}
	past := time.Now().Add(-time.Second)
	d.tracker.FocusDecisions(d.cfg, snap(terminalApp(), 43, apps, wins), past)
	d.tracker.FocusDecisions(d.cfg, snap(apps[2], 44, apps, wins), past)
	d.tracker.FocusDecisions(d.cfg, snap(chromeApp(), 42, apps, wins), past)

	if !d.tickNative(d.cfg, true) {
		t.Fatal("focus tick should run natively")
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(raw)); len(got) != 1 || got[0] != "400" {
		t.Fatalf("helper hide pids = %v, want only Music pid 400", got)
	}
}

func TestFocusTickLegacyHidesOnlyOutsideRecentSet(t *testing.T) {
	d := testDaemon(t, "")
	d.cfg.Focus.KeepRecent = 3
	d.cfg.Focus.Grace = config.Duration{}
	visible := []string{"Google Chrome", "Terminal", "Slack", "Music"}
	past := time.Now().Add(-time.Second)
	d.tracker.UpdateLegacy(d.cfg, "Terminal", visible, past)
	d.tracker.UpdateLegacy(d.cfg, "Slack", visible, past)
	d.tracker.UpdateLegacy(d.cfg, "Google Chrome", visible, past)
	d.getFrontmostApp = func() (string, error) { return "Google Chrome", nil }
	d.getVisibleApps = func() ([]string, error) { return visible, nil }
	var hidden []string
	d.hideApp = func(name string) error {
		hidden = append(hidden, name)
		return nil
	}

	d.tickLegacy(d.cfg, true)
	if len(hidden) != 1 || hidden[0] != "Music" {
		t.Fatalf("legacy hidden apps = %v, want Music only", hidden)
	}
}

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
if [ "$2" = "600" ]; then
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
		t.Fatalf("helper hide pids = %v, want only Slack pid 300 (Mail skipped, Calendar failed)", got)
	}
	requireUnhidable(t, d, "Mail", "fullscreen")
}

func TestFocusModeSkipsUnhidableApps(t *testing.T) {
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
printf '%%s\n' "$2" >> %q
;;
esac
`, hideAllSnapshotJSON, logPath)
	d := testDaemon(t, script)
	d.cfg.Apps["NoHide"] = config.AppConfig{Disabled: true}
	snap, err := parseSnapshot([]byte(hideAllSnapshotJSON))
	if err != nil {
		t.Fatal(err)
	}
	d.tracker.FocusDecisions(d.cfg, snap, time.Now().Add(-time.Minute))

	if !d.tickNative(d.cfg, true) {
		t.Fatal("focus mode should run through the native helper")
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(raw))
	want := []string{"600", "300"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("focus-mode helper pids = %v, want %v", got, want)
	}
	requireUnhidable(t, d, "Mail", "fullscreen")
}

func TestFocusModeLogsStillVisibleAsWarning(t *testing.T) {
	script := `#!/bin/sh
case "$1" in
snapshot)
cat <<'JSON'
{
  "ax_trusted": true,
  "frontmost": {"pid": 100, "name": "Google Chrome"},
  "focused_window_id": 42,
  "apps": [
    {"pid": 100, "name": "Google Chrome", "hidden": false, "unhidable": ""},
    {"pid": 300, "name": "Slack", "hidden": false, "unhidable": ""}
  ],
  "windows": [
    {"id": 42, "pid": 100, "app": "Google Chrome", "title": "Docs"},
    {"id": 43, "pid": 300, "app": "Slack", "title": "General"}
  ]
}
JSON
;;
hide)
echo 'hide: app pid 300 still visible after 1.0s grace (AXError -25211)' >&2
exit 1
;;
esac
`
	var logs bytes.Buffer
	d := New(config.Default(), "", zerolog.New(&logs))
	d.helper = NewHelper(writeFakeHelper(t, t.TempDir(), script))
	snap, err := parseSnapshot([]byte(`{
  "ax_trusted": true,
  "frontmost": {"pid": 100, "name": "Google Chrome"},
  "apps": [
    {"pid": 100, "name": "Google Chrome", "hidden": false, "unhidable": ""},
    {"pid": 300, "name": "Slack", "hidden": false, "unhidable": ""}
  ],
  "windows": [
    {"id": 42, "pid": 100, "app": "Google Chrome", "title": "Docs"},
    {"id": 43, "pid": 300, "app": "Slack", "title": "General"}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	d.tracker.FocusDecisions(d.cfg, snap, time.Now().Add(-time.Minute))

	if !d.tickNative(d.cfg, true) {
		t.Fatal("focus mode should run through the native helper")
	}
	got := logs.String()
	if !strings.Contains(got, `"level":"warn"`) ||
		!strings.Contains(got, "still visible after 1.0s grace") {
		t.Fatalf("expected still-visible warning, logs:\n%s", got)
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

func TestLegacyFocusNeverHidesMissingValues(t *testing.T) {
	d := testDaemon(t, "")
	d.cfg.Focus.KeepRecent = 1
	d.cfg.Focus.Grace = config.Duration{}
	d.tracker.apps["Slack"] = &AppState{LastActive: time.Now().Add(-time.Minute)}
	d.getFrontmostApp = func() (string, error) { return "MISSING VALUE", nil }
	d.getVisibleApps = func() ([]string, error) {
		return []string{"", "missing value", "Slack"}, nil
	}
	var hidden []string
	d.hideApp = func(name string) error {
		hidden = append(hidden, name)
		return nil
	}

	d.tickLegacy(d.cfg, true)

	if len(hidden) != 1 || hidden[0] != "Slack" {
		t.Fatalf("legacy focus hidden apps = %v, want Slack only", hidden)
	}
}

func TestHideAllLegacyNeverHidesMissingValues(t *testing.T) {
	d := testDaemon(t, "")
	d.getFrontmostApp = func() (string, error) { return "", nil }
	d.getVisibleApps = func() ([]string, error) {
		return []string{"missing value", "Slack"}, nil
	}
	var hidden []string
	d.hideApp = func(name string) error {
		hidden = append(hidden, name)
		return nil
	}

	data, err := d.hideAllLegacy(d.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 1 || hidden[0] != "Slack" || data.Hidden != 1 || data.Failed != 0 {
		t.Fatalf("hide all = hidden %v, data %+v", hidden, data)
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
