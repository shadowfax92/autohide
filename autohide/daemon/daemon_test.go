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

func TestResetTickerInterval(t *testing.T) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	if got := resetTickerInterval(ticker, time.Hour, 5*time.Millisecond); got != 5*time.Millisecond {
		t.Fatalf("reset interval = %v, want 5ms", got)
	}
	select {
	case <-ticker.C:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("ticker did not adopt the reloaded interval")
	}
	if got := resetTickerInterval(ticker, 5*time.Millisecond, 0); got != 5*time.Millisecond {
		t.Fatalf("invalid interval changed ticker to %v", got)
	}
}

func TestHandleWatchEventTouchesAppsAndTracksAwayState(t *testing.T) {
	d := testDaemon(t, "")
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}
	d.tracker.Update(d.cfg, snap(terminalApp(), 10, apps, wins), at(0))

	d.handleWatchEvent(WatchEvent{TS: at(20).UnixMilli(), Type: "activate", Pid: 100, Name: "Google Chrome"})
	if got, _ := appLastActive(d.tracker.List(d.cfg), "Google Chrome"); !got.Equal(at(20)) {
		t.Fatalf("activate touch = %v, want t+20s", got)
	}
	d.handleWatchEvent(WatchEvent{TS: at(25).UnixMilli(), Type: "deactivate", Pid: 100, Name: "Google Chrome"})
	if got, _ := appLastActive(d.tracker.List(d.cfg), "Google Chrome"); !got.Equal(at(25)) {
		t.Fatalf("deactivate touch = %v, want t+25s", got)
	}

	d.handleWatchEvent(WatchEvent{TS: at(30).UnixMilli(), Type: "lock"})
	if !d.locked {
		t.Fatal("lock event must set locked state")
	}
	d.handleWatchEvent(WatchEvent{TS: at(31).UnixMilli(), Type: "unlock"})
	if d.locked {
		t.Fatal("unlock event must clear locked state")
	}
	d.handleWatchEvent(WatchEvent{TS: at(32).UnixMilli(), Type: "sleep"})
	if !d.sleeping {
		t.Fatal("sleep event must set sleeping state")
	}
	d.handleWatchEvent(WatchEvent{TS: at(33).UnixMilli(), Type: "wake"})
	if d.sleeping {
		t.Fatal("wake event must clear sleeping state")
	}
}

func TestSuspendSizedTickGapFreezesAging(t *testing.T) {
	d := testDaemon(t, "")
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}
	d.tracker.Update(d.cfg, snap(terminalApp(), 10, apps, wins), at(0))

	d.beginAgingTick(at(0), 5*time.Second)
	delta, shifted := d.beginAgingTick(at(20), 5*time.Second)
	if delta != 20*time.Second || shifted != 20*time.Second {
		t.Fatalf("gap result = (%v, %v), want (20s, 20s)", delta, shifted)
	}
	if got, _ := appLastActive(d.tracker.List(d.cfg), "Google Chrome"); !got.Equal(at(20)) {
		t.Fatalf("gap-shifted lease = %v, want t+20s", got)
	}
}

func TestLockedAndIdleTicksFreezeOnce(t *testing.T) {
	d := testDaemon(t, "")
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}
	d.tracker.Update(d.cfg, snap(terminalApp(), 10, apps, wins), at(0))
	d.beginAgingTick(at(0), 5*time.Second)
	d.handleWatchEvent(WatchEvent{TS: at(0).UnixMilli(), Type: "lock"})

	delta, shifted := d.beginAgingTick(at(5), 5*time.Second)
	idle := 30.0
	shifted = d.freezeIdleTick(&Snapshot{IdleSeconds: &idle}, 5*time.Second, delta, shifted)
	if shifted != 5*time.Second {
		t.Fatal("locked/idle tick must freeze aging")
	}
	if got, _ := appLastActive(d.tracker.List(d.cfg), "Google Chrome"); !got.Equal(at(5)) {
		t.Fatalf("combined gates shifted more than once: lease = %v, want t+5s", got)
	}

	d.handleWatchEvent(WatchEvent{TS: at(5).UnixMilli(), Type: "unlock"})
	delta, shifted = d.beginAgingTick(at(10), 5*time.Second)
	shifted = d.freezeIdleTick(&Snapshot{IdleSeconds: &idle}, 5*time.Second, delta, shifted)
	if shifted != 5*time.Second {
		t.Fatal("idle tick must freeze aging")
	}
	if got, _ := appLastActive(d.tracker.List(d.cfg), "Google Chrome"); !got.Equal(at(10)) {
		t.Fatalf("idle-shifted lease = %v, want t+10s", got)
	}
}

func TestSleepingTickFreezesAging(t *testing.T) {
	d := testDaemon(t, "")
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}
	d.tracker.Update(d.cfg, snap(terminalApp(), 10, apps, wins), at(0))
	d.beginAgingTick(at(0), 5*time.Second)
	d.handleWatchEvent(WatchEvent{TS: at(1).UnixMilli(), Type: "sleep"})

	if _, shifted := d.beginAgingTick(at(5), 5*time.Second); shifted != 4*time.Second {
		t.Fatal("sleeping tick must freeze aging")
	}
	if got, _ := appLastActive(d.tracker.List(d.cfg), "Google Chrome"); !got.Equal(at(4)) {
		t.Fatalf("sleep-shifted lease = %v, want t+4s", got)
	}
}

func TestLockIntervalBetweenTicksFreezesExactDuration(t *testing.T) {
	d := testDaemon(t, "")
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}
	d.tracker.Update(d.cfg, snap(terminalApp(), 10, apps, wins), at(0))
	d.beginAgingTick(at(0), 5*time.Second)
	d.handleWatchEvent(WatchEvent{TS: at(1).UnixMilli(), Type: "lock"})
	d.handleWatchEvent(WatchEvent{TS: at(3).UnixMilli(), Type: "unlock"})

	if _, shifted := d.beginAgingTick(at(5), 5*time.Second); shifted != 2*time.Second {
		t.Fatalf("between-tick lock shifted %v, want 2s", shifted)
	}
	if got, _ := appLastActive(d.tracker.List(d.cfg), "Google Chrome"); !got.Equal(at(2)) {
		t.Fatalf("lock-shifted lease = %v, want t+2s", got)
	}
}

func TestIdleGateUsesIntervalThatDroveTick(t *testing.T) {
	d := testDaemon(t, "")
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}
	d.tracker.Update(d.cfg, snap(terminalApp(), 10, apps, wins), at(0))
	d.beginAgingTick(at(0), time.Minute)
	delta, shifted := d.beginAgingTick(at(60), time.Minute)
	idle := 6.0
	shifted = d.freezeIdleTick(&Snapshot{IdleSeconds: &idle}, time.Minute, delta, shifted)
	if shifted != 0 {
		t.Fatalf("newly reloaded shorter interval shifted the prior tick by %v", shifted)
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
