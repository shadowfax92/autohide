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

func windowCount(infos []AppInfo, name string) int {
	for _, info := range infos {
		if info.Name == name {
			return len(info.Windows)
		}
	}
	return -1
}

func windowLastActive(infos []AppInfo, id uint32) (time.Time, bool) {
	for _, info := range infos {
		for _, w := range info.Windows {
			if w.ID == id {
				return w.LastActive, true
			}
		}
	}
	return time.Time{}, false
}

func appLastActive(infos []AppInfo, name string) (time.Time, bool) {
	for _, info := range infos {
		if info.Name == name {
			return info.LastActive, true
		}
	}
	return time.Time{}, false
}

// --- App-hide tier ---

// A stale background app hides once its timeout passes; the frontmost app is
// never touched.
func TestStaleAppHides(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(0))
	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(30))
	if len(dec.HideApps) != 0 {
		t.Fatalf("nothing should hide at 30s, got %+v", dec.HideApps)
	}

	dec = tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(70))
	if !hasApp(dec.HideApps, "Google Chrome") {
		t.Error("stale Chrome should hide after the timeout")
	}
	if hasApp(dec.HideApps, "Terminal") {
		t.Error("frontmost Terminal must never hide")
	}
}

func TestNeverVisibleZeroWindowAppDoesNotHide(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	terminalOnly := []SnapWindow{win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, terminalOnly), at(0))
	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, terminalOnly), at(70))
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Fatal("a zero-window app never observed with a real window must not hide")
	}
}

func TestSeenAppHidesAfterMovingToAnotherSpace(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	both := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}
	terminalOnly := []SnapWindow{win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, both), at(0))
	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, terminalOnly), at(70))
	if !hasApp(dec.HideApps, "Google Chrome") {
		t.Fatal("a stale app previously seen with a window should hide on another Space")
	}
}

func TestHideOtherSpacesDisabledPreservesZeroWindowSkip(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	cfg.General.HideOtherSpaces = false
	apps := []SnapApp{chromeApp(), terminalApp()}
	both := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}
	terminalOnly := []SnapWindow{win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, both), at(0))
	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, terminalOnly), at(70))
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Fatal("hide_other_spaces=false must preserve the zero-window skip")
	}
}

func TestOtherSpaceFrontmostDisabledAndHiddenAppsStayExempt(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp(), {Pid: 300, Name: "NoHide"}, {Pid: 400, Name: "Mail"}}
	allWindows := []SnapWindow{
		win(1, 100, "Google Chrome"),
		win(10, 200, "Terminal"),
		win(30, 300, "NoHide"),
		win(40, 400, "Mail"),
	}

	tr.Update(cfg, snap(terminalApp(), 10, apps, allWindows), at(0))
	apps[3].Hidden = true
	terminalOnly := []SnapWindow{win(10, 200, "Terminal")}
	dec := tr.Update(cfg, snap(chromeApp(), 0, apps, terminalOnly), at(70))
	if hasApp(dec.HideApps, "Google Chrome") || hasApp(dec.HideApps, "NoHide") || hasApp(dec.HideApps, "Mail") {
		t.Fatalf("other-Space exemptions failed: %+v", dec.HideApps)
	}
}

func TestEventTouchBetweenTicksPreventsEarlyHide(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(0))
	tr.TouchApp("Google Chrome", at(55))
	if dec := tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(70)); hasApp(dec.HideApps, "Google Chrome") {
		t.Fatal("an app touched between polls must get a full timeout from the event")
	}
	if got, _ := appLastActive(tr.List(cfg), "Google Chrome"); !got.Equal(at(55)) {
		t.Fatalf("event touch = %v, want t+55s", got)
	}
	if dec := tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(116)); !hasApp(dec.HideApps, "Google Chrome") {
		t.Fatal("the app should hide after a full timeout from the event")
	}
}

func TestOlderEventCannotMoveLastActiveBackward(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome")}

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(50))
	tr.TouchApp("Google Chrome", at(40))
	if got, _ := appLastActive(tr.List(cfg), "Google Chrome"); !got.Equal(at(50)) {
		t.Fatalf("late event moved LastActive backward to %v", got)
	}
}

func TestShiftLastActiveFreezesAppAndWindowAging(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(0))
	tr.ShiftLastActive(2 * time.Minute)
	if got, _ := appLastActive(tr.List(cfg), "Google Chrome"); !got.Equal(at(120)) {
		t.Fatalf("shifted app lease = %v, want t+120s", got)
	}
	if got, _ := windowLastActive(tr.List(cfg), 1); !got.Equal(at(120)) {
		t.Fatalf("shifted window lease = %v, want t+120s", got)
	}
	if dec := tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(130)); hasApp(dec.HideApps, "Google Chrome") {
		t.Fatal("shifted-away time must not age the app")
	}
	if dec := tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(181)); !hasApp(dec.HideApps, "Google Chrome") {
		t.Fatal("app should hide after a full active timeout following the shift")
	}
}

func TestUnresolvedFrontmostRefreshesLastRegularApp(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(0))
	overlay := snap(SnapApp{Pid: 999}, 0, apps, wins)
	dec := tr.Update(cfg, overlay, at(70))
	if hasApp(dec.HideApps, "Terminal") {
		t.Fatal("the regular app beneath an accessory overlay must stay exempt")
	}
	if got, _ := appLastActive(tr.List(cfg), "Terminal"); !got.Equal(at(70)) {
		t.Fatalf("underlying app lease = %v, want t+70s", got)
	}
}

func TestActivationEventUpdatesOverlayFallback(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(0))
	tr.ActivateApp("Google Chrome", at(55))
	overlay := snap(SnapApp{Pid: 999}, 0, apps, wins)
	dec := tr.Update(cfg, overlay, at(70))
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Fatal("the latest regular activation must become the overlay fallback")
	}
	if got, _ := appLastActive(tr.List(cfg), "Google Chrome"); !got.Equal(at(70)) {
		t.Fatalf("event-selected fallback lease = %v, want t+70s", got)
	}
}

// App-hiding must work with no focused window and AX untrusted — those gated
// only the removed window tier, never hiding.
func TestAppHidesWithoutFocusOrAX(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(10, 200, "Terminal")}

	tr.Update(cfg, snap(terminalApp(), 10, apps, wins), at(0))
	noFocus := snap(terminalApp(), 0, apps, wins)
	noFocus.AXTrusted = false
	dec := tr.Update(cfg, noFocus, at(70))
	if !hasApp(dec.HideApps, "Google Chrome") {
		t.Error("app tier should hide stale Chrome with no focus / AX untrusted")
	}
}

func TestDisabledAndExcludedAppsNeverHide(t *testing.T) {
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

// A new window of a background app refreshes that app's timer, so an app the
// user just opened a window in is not insta-hidden.
func TestNewWindowGrantsAppFreshLease(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	base := []SnapWindow{win(10, 200, "Terminal"), win(1, 100, "Google Chrome")}

	tr.Update(cfg, snap(chromeApp(), 1, apps, base), at(0))
	tr.Update(cfg, snap(terminalApp(), 10, apps, base), at(5))

	// New Chrome window at t+50 refreshes Chrome's app timer (would otherwise
	// expire at t+65).
	withNew := append(base, win(3, 100, "Google Chrome"))
	tr.Update(cfg, snap(terminalApp(), 10, apps, withNew), at(50))

	dec := tr.Update(cfg, snap(terminalApp(), 10, apps, withNew), at(80))
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Error("new window at t+50 should have refreshed Chrome's app timer")
	}
}

// After an app hides it must not be re-decided while hidden; unhiding it
// (windows reappear, app frontmost) grants a fresh lease.
func TestUnhideDoesNotInstantlyRehide(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), terminalApp()}
	allWins := []SnapWindow{
		win(10, 200, "Terminal"),
		win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome"),
	}

	tr.Update(cfg, snap(terminalApp(), 10, apps, allWins), at(0))
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
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Errorf("restored app must get a fresh timeout, got %+v", dec.HideApps)
	}

	// The lease is a real timeout: hidden again only a full minute after restore.
	dec = tr.Update(cfg, snap(terminalApp(), 10, apps, allWins), at(330))
	if hasApp(dec.HideApps, "Google Chrome") {
		t.Error("Chrome restored at t+300 must not hide at t+330")
	}
	dec = tr.Update(cfg, snap(terminalApp(), 10, apps, allWins), at(365))
	if !hasApp(dec.HideApps, "Google Chrome") {
		t.Error("Chrome should hide a full timeout after restore")
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

// --- Window tracking (drives `autohide list`) ---

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

// A window absent from the snapshot is forgotten; if it returns it re-leases
// from the reappearance, not its original time.
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

	// Window 2 returns at t+20: its timer leases from t+20.
	tr.Update(cfg, snap(chromeApp(), 1, apps, both), at(20))
	la, ok := windowLastActive(tr.List(cfg), 2)
	if !ok || !la.Equal(at(20)) {
		t.Errorf("reappeared window should re-lease at t+20, got %v (ok=%v)", la, ok)
	}
}

// The focused window's timer refreshes each tick it holds focus; siblings age.
func TestFocusedWindowRefreshes(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(0))
	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(50)) // window 1 stays focused

	if la, _ := windowLastActive(tr.List(cfg), 1); !la.Equal(at(50)) {
		t.Errorf("focused window 1 should refresh to t+50, got %v", la)
	}
	if la, _ := windowLastActive(tr.List(cfg), 2); !la.Equal(at(0)) {
		t.Errorf("unfocused window 2 should stay at t0, got %v", la)
	}
}

// Granting Accessibility mid-run re-leases every window so `list` doesn't
// suddenly show them all as long idle (focus was unobservable while untrusted).
func TestAXGrantReleasesWindowTimers(t *testing.T) {
	tr := NewTracker()
	cfg := testCfg()
	apps := []SnapApp{chromeApp()}
	wins := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 100, "Google Chrome")}

	untrusted := snap(chromeApp(), 0, apps, wins)
	untrusted.AXTrusted = false
	tr.Update(cfg, untrusted, at(0))
	tr.Update(cfg, untrusted, at(300))

	tr.Update(cfg, snap(chromeApp(), 1, apps, wins), at(305)) // AX granted
	if la, _ := windowLastActive(tr.List(cfg), 2); !la.Equal(at(305)) {
		t.Errorf("window 2 should re-lease at the grant (t+305), got %v", la)
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
	if la, ok := windowLastActive(tr.List(cfg), 2); !ok || !la.Equal(at(70)) {
		t.Errorf("re-registered window should lease at t+70, got %v (ok=%v)", la, ok)
	}
}

// --- Legacy fallback ---

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
