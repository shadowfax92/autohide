package daemon

import (
	"testing"
	"time"

	"autohide/config"
)

var t0 = time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

func testCfg() *config.Config {
	cfg := config.Default() // 1m default timeout, Finder disabled
	cfg.Apps["NoHide"] = config.AppConfig{Disabled: true}
	return cfg
}

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

func chromeApp() SnapApp   { return SnapApp{Pid: 100, Name: "Google Chrome"} }
func terminalApp() SnapApp { return SnapApp{Pid: 200, Name: "Terminal"} }

func win(id uint32, pid int32, app string) SnapWindow {
	return SnapWindow{ID: id, Pid: pid, App: app, Title: ""}
}

func snap(front SnapApp, focusedID uint32, apps []SnapApp, wins []SnapWindow) *Snapshot {
	return &Snapshot{
		AXTrusted:       true,
		Frontmost:       AppRef{Pid: front.Pid, Name: front.Name},
		FocusedWindowID: focusedID,
		Apps:            apps,
		Windows:         wins,
	}
}

func hasApp(refs []AppRef, name string) bool {
	for _, r := range refs {
		if r.Name == name {
			return true
		}
	}
	return false
}

func hasWin(refs []WindowRef, id uint32) bool {
	for _, r := range refs {
		if r.ID == id {
			return true
		}
	}
	return false
}

// Working in Chrome window 1: stale sibling window 2 minimizes after the
// timeout; the focused window and the frontmost app are never touched.
func TestSiblingOfFrontmostMinimizesAfterTimeout(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}

	dec := tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))
	if len(dec.HideApps) != 0 || len(dec.MinimizeWindows) != 0 {
		t.Fatalf("first tick should be quiet, got %+v", dec)
	}

	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(30))
	if len(dec.MinimizeWindows) != 0 {
		t.Fatalf("nothing expired at 30s, got %+v", dec.MinimizeWindows)
	}

	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(70))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("stale sibling window 2 should minimize")
	}
	if hasWin(dec.MinimizeWindows, 1) {
		t.Error("focused window must never minimize")
	}
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Error("frontmost app must never hide")
	}
}

// Bouncing Terminal<->Chrome keeps Chrome's app timer fresh, but the stale
// Chrome window must still minimize — the core 49"-monitor workflow.
func TestBounceWorkflowStillMinimizesStaleSibling(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{
		win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome"),
		win(10, 200, "Terminal"),
	}

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))
	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(20))
	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(40))

	dec := tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(70))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("window 2 (untouched since t0) should minimize despite app bouncing")
	}
	if hasApp(dec.HideApps, "Google Chrome") || hasApp(dec.HideApps, "Terminal") {
		t.Errorf("no app should hide, got %+v", dec.HideApps)
	}
}

// A background app that was recently used: its stale window minimizes while
// the app survives; once the whole app expires, the app tier takes over and
// its windows are NOT individually minimized (no Dock-stranding).
func TestBackgroundAppWindowTierThenAppTierPrecedence(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	chromeWins := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}
	allWins := append([]SnapWindow{win(10, 200, "Terminal")}, chromeWins...)

	tr.Update(cfg, snap(chromeApp(), 1, apps, allWins), at(0))
	tr.Update(cfg, snap(chromeApp(), 1, apps, allWins), at(10)) // w1 refreshed at 10
	// switch to Terminal
	tr.Update(cfg, snap(terminalApp(), 10, apps, allWins), at(11))

	// t+65: Chrome app last frontmost at 11 (54s, fresh); w2 last at 0 (65s, stale)
	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, allWins), at(65))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("stale window of fresh background app should minimize")
	}
	if hasWin(dec.MinimizeWindows, 1) {
		t.Error("window 1 (55s) not expired yet")
	}
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Error("Chrome app (54s) not expired yet")
	}

	// t+80: Chrome app expired (69s) -> app tier hides it; w1 (70s, also
	// expired) must NOT be minimized — the hide owns it.
	remaining := []SnapWindow{win(10, 200, "Terminal"), win(1, 100, "Google Chrome")}
	dec = tr.Update(cfg, snap(terminalApp(), 10, apps, remaining), at(80))
	if !hasApp(dec.HideApps, "Google Chrome") {
		t.Error("Chrome should hide once the app timer expires")
	}
	if hasWin(dec.MinimizeWindows, 1) {
		t.Error("windows of an app being hidden must not also minimize")
	}
}

func TestDisabledAndExcludedAppsUntouched(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{terminalApp(), {Pid: 300, Name: "NoHide"}, {Pid: 400, Name: "Finder"}}
	wins := []SnapWindow{
		win(10, 200, "Terminal"),
		win(30, 300, "NoHide"),
		win(40, 400, "Finder"),
	}

	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(0))
	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(200))
	if hasApp(dec.HideApps, "NoHide") || hasApp(dec.HideApps, "Finder") {
		t.Errorf("disabled/excluded apps must not hide, got %+v", dec.HideApps)
	}
	if hasWin(dec.MinimizeWindows, 30) || hasWin(dec.MinimizeWindows, 40) {
		t.Errorf("disabled/excluded app windows must not minimize, got %+v", dec.MinimizeWindows)
	}
}

// Hiding an app with no on-screen windows is invisible and risks re-hide
// loops after unhide — the app tier requires at least one snapshot window.
func TestZeroWindowAppNeverHides(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{terminalApp(), {Pid: 500, Name: "Agent"}}
	wins := []SnapWindow{win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(0))
	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(200))
	if hasApp(dec.HideApps, "Agent") {
		t.Error("app with zero on-screen windows must not hide")
	}
}

// A window (re)appearing refreshes its own timer AND its app's timer, so
// nothing just summoned or restored is insta-hidden.
func TestAppearanceGrantsFreshLease(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	base := []SnapWindow{win(10, 200, "Terminal"), win(1, 100, "Google Chrome")}

	tr.Update(cfg, snap(chromeApp(), 1, apps, base), at(0))
	tr.Update(cfg, snap(terminalApp(), 10, apps, base), at(5))

	// t+50: new Chrome window 3 appears while Chrome is background; Chrome's
	// app timer refreshes (would otherwise expire at t+65).
	withNew := append(base, win(3, 100, "Google Chrome"))
	tr.Update(cfg, snap(terminalApp(), 10, apps, withNew), at(50))

	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, withNew), at(80))
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Error("new window at t+50 should have refreshed Chrome's app timer")
	}
	if hasWin(dec.MinimizeWindows, 3) {
		t.Error("window 3 (30s old) must not minimize")
	}
	if !hasWin(dec.MinimizeWindows, 1) {
		t.Error("window 1 (75s stale) should minimize")
	}
}

// After we hide an app, it must not be re-decided while hidden; when the user
// unhides it, its reappearing windows grant a fresh lease.
func TestUnhideDoesNotInstantlyRehide(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	allWins := []SnapWindow{
		win(10, 200, "Terminal"),
		win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome"),
	}

	tr.Update(cfg, snap(chromeApp(), 1, apps, allWins), at(0))
	tr.Update(cfg, snap(terminalApp(), 10, apps, allWins), at(5))

	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, allWins), at(70))
	if !hasApp(dec.HideApps, "Google Chrome") {
		t.Fatal("Chrome should hide at t+70")
	}

	// Hidden: Chrome reported hidden, windows gone from snapshot.
	hiddenApps := []SnapApp{{Pid: 100, Name: "Google Chrome", Hidden: true}, terminalApp()}
	onlyTerm := []SnapWindow{win(10, 200, "Terminal")}
	dec = tr.Update(cfg, snap(terminalApp(), 10, hiddenApps, onlyTerm), at(75))
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Error("hidden app must not be re-decided")
	}

	// User unhides Chrome (cmd-tab): windows reappear, Chrome frontmost.
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, allWins), at(300))
	if hasApp(dec.HideApps, "Google Chrome") || len(dec.MinimizeWindows) != 0 {
		t.Errorf("restored windows must get a fresh timeout, got %+v", dec)
	}

	// And the lease is a real timeout: sibling ages out only after a full
	// minute from restore.
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, allWins), at(330))
	if hasWin(dec.MinimizeWindows, 2) {
		t.Error("sibling restored at t+300 must not minimize at t+330")
	}
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, allWins), at(365))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("sibling should minimize a full timeout after restore")
	}
}

// No focused window identified (or AX untrusted) -> never minimize; the app
// tier still works.
func TestNoMinimizeWhenFocusUnknown(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{
		win(10, 200, "Terminal"),
		win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome"),
	}

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))
	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(5))

	noFocus := snap(terminalApp(), 0, apps, wins)
	dec := tr.Update(cfg, noFocus, at(70))
	if len(dec.MinimizeWindows) != 0 {
		t.Error("focused window unknown -> no minimizing")
	}
	if !hasApp(dec.HideApps, "Google Chrome") {
		t.Error("app tier should still hide stale Chrome")
	}

	tr2 := NewTracker()
	tr2.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))
	untrusted := snap(chromeApp(), 1, apps, wins)
	untrusted.AXTrusted = false
	dec = tr2.Update(cfg, untrusted, at(70))
	if len(dec.MinimizeWindows) != 0 {
		t.Error("AX untrusted -> no minimizing")
	}
}

// More expired windows than the per-tick cap: the oldest go first, the rest
// stay candidates for the next tick.
func TestMinimizeCapOldestFirst(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}

	wins := []SnapWindow{win(10, 200, "Terminal"), win(1, 100, "Google Chrome")}
	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))

	// Windows 2..6 appear at staggered times (2 oldest ... 6 newest).
	for i := uint32(2); i <= 6; i++ {
		wins = append(wins, win(i, 100, "Google Chrome"))
		tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(int(i)))
	}

	dec := tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(120))
	if len(dec.MinimizeWindows) != 3 {
		t.Fatalf("cap is 3 per tick, got %d", len(dec.MinimizeWindows))
	}
	for _, want := range []uint32{2, 3, 4} {
		if !hasWin(dec.MinimizeWindows, want) {
			t.Errorf("oldest window %d should be in the first batch", want)
		}
	}

	// Minimized ones leave the snapshot; the remainder follow next tick.
	rest := []SnapWindow{
		win(10, 200, "Terminal"), win(1, 100, "Google Chrome"),
		win(5, 100, "Google Chrome"), win(6, 100, "Google Chrome"),
	}
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, rest), at(125))
	if !hasWin(dec.MinimizeWindows, 5) || !hasWin(dec.MinimizeWindows, 6) {
		t.Errorf("remaining expired windows should minimize next tick, got %+v", dec.MinimizeWindows)
	}
}

func TestDeferWindowBacksOff(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))
	tr.DeferWindow(2, at(300))

	dec := tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(70))
	if hasWin(dec.MinimizeWindows, 2) {
		t.Error("deferred window must not minimize before the deadline")
	}
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(301))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("deferred window becomes eligible after the deadline")
	}
}

// A window absent from the snapshot is forgotten; if it returns it is new.
func TestWindowPruneAndReappear(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp()}
	both := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}
	only1 := []SnapWindow{win(1, 100, "Google Chrome")}

	tr.Update(cfg, snap(chromeApp(), 1, apps, both), at(0))
	tr.Update(cfg, snap(chromeApp(), 1, apps, only1), at(10))

	if got := windowCount(tr.List(cfg), "Google Chrome"); got != 1 {
		t.Errorf("pruned window still listed: count = %d", got)
	}

	// Window 2 returns at t+20: it must age from t+20, not t0.
	tr.Update(cfg, snap(chromeApp(), 1, apps, both), at(20))
	dec := tr.Update(cfg, snap(chromeApp(), 1, apps, both), at(70))
	if hasWin(dec.MinimizeWindows, 2) {
		t.Error("reappeared window must age from reappearance")
	}
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, both), at(85))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("reappeared window minimizes one timeout after reappearing")
	}
}

func TestListReportsWindows(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{
		win(10, 200, "Terminal"),
		{ID: 1, Pid: 100, App: "Google Chrome", Title: "Docs"},
		{ID: 2, Pid: 100, App: "Google Chrome", Title: ""},
	}
	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))

	infos := tr.List(cfg)
	if windowCount(infos, "Google Chrome") != 2 || windowCount(infos, "Terminal") != 1 {
		t.Errorf("unexpected window counts in %+v", infos)
	}
	for _, info := range infos {
		if info.Name == "Google Chrome" {
			titles := map[string]bool{}
			for _, w := range info.Windows {
				titles[w.Title] = true
			}
			if !titles["Docs"] {
				t.Errorf("expected title Docs in %+v", info.Windows)
			}
		}
	}
}

// The legacy path (fallback modes) keeps today's app-level semantics.
func TestUpdateLegacy(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	visible := []string{"Terminal", "Slack", "NoHide"}

	if toHide := tr.UpdateLegacy(cfg, "Terminal", visible, at(0)); len(toHide) != 0 {
		t.Fatalf("first tick quiet, got %v", toHide)
	}
	toHide := tr.UpdateLegacy(cfg, "Terminal", visible, at(70))
	if len(toHide) != 1 || toHide[0] != "Slack" {
		t.Errorf("stale Slack should hide (NoHide disabled, Terminal frontmost), got %v", toHide)
	}
	// Hidden apps are not re-decided while not visible.
	if toHide := tr.UpdateLegacy(cfg, "Terminal", []string{"Terminal", "NoHide"}, at(140)); len(toHide) != 0 {
		t.Errorf("nothing left to hide, got %v", toHide)
	}
}

func windowCount(infos []AppInfo, name string) int {
	for _, info := range infos {
		if info.Name == name {
			return len(info.Windows)
		}
	}
	return -1
}

// While AX is untrusted, focus is unobservable and window timers can't
// refresh — granting Accessibility mid-run must NOT minimize-storm windows
// whose staleness is bookkeeping, not observed idleness.
func TestAXGrantDoesNotBurstMinimize(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}

	untrusted := snap(chromeApp(), 0, apps, wins)
	untrusted.AXTrusted = false
	tr.Update(cfg, untrusted, at(0))
	tr.Update(cfg, untrusted, at(300))

	dec := tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(305))
	if len(dec.MinimizeWindows) != 0 {
		t.Fatalf("first trusted tick must not minimize, got %+v", dec.MinimizeWindows)
	}
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(360))
	if hasWin(dec.MinimizeWindows, 2) {
		t.Error("window must age from the grant (55s), not from t0")
	}
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(370))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("window should minimize one full timeout after the grant")
	}
}

func TestWindowTierUsesPerAppTimeout(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	cfg.Apps["Google Chrome"] = config.AppConfig{Timeout: config.Duration{Duration: 5 * time.Minute}}
	apps := []SnapApp{chromeApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))
	dec := tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(120))
	if hasWin(dec.MinimizeWindows, 2) {
		t.Error("2m idle must survive a 5m per-app timeout")
	}
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(310))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("window should minimize after the per-app 5m timeout")
	}
}

// ResetWindows (mode transitions, focus mode) wipes window state; windows
// re-register on the next tick with a fresh lease.
func TestResetWindowsGrantsFreshLease(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))
	tr.ResetWindows()
	if got := windowCount(tr.List(cfg), "Google Chrome"); got != 0 {
		t.Fatalf("windows should be cleared, count = %d", got)
	}

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(70))
	dec := tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(100))
	if hasWin(dec.MinimizeWindows, 2) {
		t.Error("re-registered window must age from re-registration")
	}
	dec = tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(135))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("re-registered window minimizes a full timeout later")
	}
}

// An ineffective hide (app stays visible next snapshot) must not retry every
// tick — it backs off, and a user refocus resets the cycle.
func TestHideRetryBackoff(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	slack := SnapApp{Pid: 300, Name: "Slack"}
	apps := []SnapApp{terminalApp(), slack}
	wins := []SnapWindow{win(10, 200, "Terminal"), win(30, 300, "Slack")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(0))
	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(70))
	if !hasApp(dec.HideApps, "Slack") {
		t.Fatal("Slack should hide at t+70")
	}

	// Hide had no effect: Slack still visible in every following snapshot.
	for _, sec := range []int{75, 80, 200, 360} {
		dec = tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(sec))
		if hasApp(dec.HideApps, "Slack") {
			t.Fatalf("hide must back off, re-decided at t+%d", sec)
		}
	}
	dec = tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(380))
	if !hasApp(dec.HideApps, "Slack") {
		t.Error("hide should retry after the backoff window")
	}

	// User refocuses Slack: backoff cleared, normal timeout cycle resumes.
	tr.Update(cfg, snap(slack, 30, apps, wins), at(385))
	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(390))
	dec = tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(455))
	if !hasApp(dec.HideApps, "Slack") {
		t.Error("refocus must clear the hide backoff (would otherwise wait until t+680)")
	}
}

// Focusing a deferred window clears its backoff: the defer protected a
// failing minimize, but a user touch starts a fresh cycle.
func TestDeferClearedOnFocus(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))
	tr.DeferWindow(2, at(300))

	tr.Update(cfg, snap(chromeApp(), 2, apps, wins), at(100)) // user clicks w2
	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(105))

	dec := tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(170))
	if !hasWin(dec.MinimizeWindows, 2) {
		t.Error("focus must clear the defer; w2 is 70s idle and should minimize")
	}
}
