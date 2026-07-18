package daemon

import (
	"sort"
	"sync"
	"time"

	"autohide/config"
)

// An ineffective hide (e.g. a Split View member: the request is accepted but
// nothing hides) would otherwise be re-decided every tick via the reality
// mirror.
const hideRetryBackoff = 5 * time.Minute

type AppState struct {
	Pid             int32
	LastActive      time.Time
	Hidden          bool
	DeferUntil      time.Time
	SeenWithWindows bool
}

type WindowState struct {
	Pid        int32
	App        string
	Title      string
	LastActive time.Time
}

type WindowDetail struct {
	ID         uint32
	Title      string
	LastActive time.Time
}

type AppInfo struct {
	Name       string
	LastActive time.Time
	Timeout    time.Duration
	Hidden     bool
	Disabled   bool
	Windows    []WindowDetail
}

type Tracker struct {
	mu                   sync.RWMutex
	apps                 map[string]*AppState
	windows              map[uint32]*WindowState
	axTrustedPrev        bool
	lastRegularFrontmost string
	activationCandidates map[string]time.Time
}

// TouchApp advances an app lease using an activity event timestamp.
func (t *Tracker) TouchApp(name string, at time.Time) {
	if name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.touchApp(name, at)
}

// ActivateApp touches a known regular app and remembers it as frontmost.
func (t *Tracker) ActivateApp(name string, at time.Time) {
	if name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if previous, ok := t.activationCandidates[name]; !ok || at.After(previous) {
		t.activationCandidates[name] = at
	}
	entry, known := t.apps[name]
	if !known {
		return
	}
	if at.After(entry.LastActive) {
		entry.LastActive = at
	}
	entry.Hidden = false
	entry.DeferUntil = time.Time{}
	t.lastRegularFrontmost = name
}

func (t *Tracker) touchApp(name string, at time.Time) {
	entry, ok := t.apps[name]
	if !ok {
		return
	}
	if at.After(entry.LastActive) {
		entry.LastActive = at
	}
}

// ShiftLastActive freezes every app and window lease for an away interval.
func (t *Tracker) ShiftLastActive(delta time.Duration) {
	if delta <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, entry := range t.apps {
		entry.LastActive = entry.LastActive.Add(delta)
	}
	for _, window := range t.windows {
		window.LastActive = window.LastActive.Add(delta)
	}
}

func NewTracker() *Tracker {
	return &Tracker{
		apps:                 make(map[string]*AppState),
		windows:              make(map[uint32]*WindowState),
		activationCandidates: make(map[string]time.Time),
	}
}

// Update reconciles tracker state with one helper snapshot and decides which
// stale apps to hide whole (clean cmd-tab restore). Windows are tracked, not
// acted on: their state powers the fresh-lease rule (a window (re)appearing
// re-leases it AND its app, so nothing just summoned gets insta-hidden) and
// the per-window rows in `autohide list`. Refresh rules: focused window; any
// window that (re)appears; frontmost app. Deliberately NO sibling refresh on
// app activation, or windows would never age out while the user bounces
// between apps.
func (t *Tracker) Update(cfg *config.Config, snap *Snapshot, now time.Time) Decisions {
	t.mu.Lock()
	defer t.mu.Unlock()

	present := make(map[uint32]bool, len(snap.Windows))
	appeared := make(map[string]bool)
	winsByApp := make(map[string]int)
	for _, w := range snap.Windows {
		present[w.ID] = true
		winsByApp[w.App]++
		if ws, ok := t.windows[w.ID]; ok {
			ws.Pid = w.Pid
			ws.Title = w.Title
		} else {
			t.windows[w.ID] = &WindowState{
				Pid: w.Pid, App: w.App, Title: w.Title, LastActive: now,
			}
			appeared[w.App] = true
		}
	}
	for id := range t.windows {
		if !present[id] {
			delete(t.windows, id)
		}
	}
	// While AX was untrusted, focus was unobservable and no window timer
	// could refresh — their staleness is bookkeeping, not idleness. Re-lease
	// everything on the grant so `list` doesn't show every window as long
	// idle. (Focused-id-0 blips and desktop idling keep aging deliberately:
	// there the user really wasn't using the windows.)
	if snap.AXTrusted && !t.axTrustedPrev {
		for _, ws := range t.windows {
			ws.LastActive = now
		}
	}
	t.axTrustedPrev = snap.AXTrusted

	if snap.FocusedWindowID != 0 {
		if ws, ok := t.windows[snap.FocusedWindowID]; ok {
			ws.LastActive = now
		}
	}

	appCounts := make(map[string]int, len(snap.Apps))
	for _, app := range snap.Apps {
		appCounts[app.Name]++
	}
	running := make(map[string]bool, len(snap.Apps))
	for _, a := range snap.Apps {
		running[a.Name] = true
		entry, ok := t.apps[a.Name]
		if !ok || appCounts[a.Name] == 1 && entry.Pid != 0 && entry.Pid != a.Pid {
			entry = &AppState{LastActive: now}
			t.apps[a.Name] = entry
		}
		entry.Pid = a.Pid
		if winsByApp[a.Name] > 0 {
			entry.SeenWithWindows = true
		}
		// Mirror reality: a failed hide self-heals (still visible next tick
		// -> re-decided), a user unhide is seen without polling extra state.
		entry.Hidden = a.Hidden
	}
	for name := range t.apps {
		if !running[name] {
			delete(t.apps, name)
		}
	}
	if !running[t.lastRegularFrontmost] {
		t.lastRegularFrontmost = ""
	}
	for name := range appeared {
		if entry, ok := t.apps[name]; ok {
			if now.After(entry.LastActive) {
				entry.LastActive = now
			}
		}
	}
	frontmost := snap.Frontmost.Name
	if _, ok := t.apps[frontmost]; ok {
		t.lastRegularFrontmost = frontmost
	} else if frontmost == "" && snap.Frontmost.Pid != 0 {
		var latest time.Time
		for name, activatedAt := range t.activationCandidates {
			if running[name] && (latest.IsZero() || activatedAt.After(latest)) {
				frontmost = name
				latest = activatedAt
			}
		}
		if frontmost == "" {
			frontmost = t.lastRegularFrontmost
		} else {
			t.lastRegularFrontmost = frontmost
		}
	}
	clear(t.activationCandidates)
	if entry, ok := t.apps[frontmost]; ok {
		if now.After(entry.LastActive) {
			entry.LastActive = now
		}
		entry.Hidden = false
		entry.DeferUntil = time.Time{}
	}

	var dec Decisions

	for name, entry := range t.apps {
		if name == frontmost || entry.Hidden {
			continue
		}
		// Never hide a zero-window app until it has proven it owns a real
		// window; this avoids menu-bar apps and unhide/re-hide loops.
		if winsByApp[name] == 0 && (!cfg.General.HideOtherSpaces || !entry.SeenWithWindows) {
			continue
		}
		if now.Before(entry.DeferUntil) {
			continue
		}
		timeout, disabled := cfg.EffectiveTimeout(name)
		if disabled {
			continue
		}
		if now.Sub(entry.LastActive) > timeout {
			dec.HideApps = append(dec.HideApps, AppRef{Pid: entry.Pid, Name: name})
			entry.Hidden = true
			entry.DeferUntil = now.Add(hideRetryBackoff)
		}
	}
	sort.Slice(dec.HideApps, func(i, j int) bool {
		return dec.HideApps[i].Name < dec.HideApps[j].Name
	})

	return dec
}

// ResetWindows drops all per-window state. Called when the native path
// stops observing (mode fallback, focus mode) so timers and list data can't
// rot; on re-entry windows re-register via the appearance rule (fresh lease).
func (t *Tracker) ResetWindows() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.windows) > 0 {
		t.windows = make(map[uint32]*WindowState)
	}
}

// UpdateLegacy is the pre-window-tracking app-level behavior, used by the
// fallback modes (helper unavailable or window_tracking off).
func (t *Tracker) UpdateLegacy(cfg *config.Config, frontmost string, visible []string, now time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if state, ok := t.apps[frontmost]; ok {
		if now.After(state.LastActive) {
			state.LastActive = now
		}
		state.Hidden = false
	} else {
		t.apps[frontmost] = &AppState{LastActive: now}
	}

	for _, name := range visible {
		if _, ok := t.apps[name]; !ok {
			t.apps[name] = &AppState{LastActive: now}
		}
	}

	visibleSet := make(map[string]bool, len(visible))
	for _, v := range visible {
		visibleSet[v] = true
	}

	var toHide []string
	for name, state := range t.apps {
		if name == frontmost || state.Hidden || !visibleSet[name] {
			continue
		}

		timeout, disabled := cfg.EffectiveTimeout(name)
		if disabled {
			continue
		}

		if now.Sub(state.LastActive) > timeout {
			toHide = append(toHide, name)
			state.Hidden = true
		}
	}

	sort.Strings(toHide)
	return toHide
}

func (t *Tracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.apps)
}

func (t *Tracker) List(cfg *config.Config) []AppInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	windowsByApp := make(map[string][]WindowDetail)
	for id, ws := range t.windows {
		windowsByApp[ws.App] = append(windowsByApp[ws.App], WindowDetail{
			ID: id, Title: ws.Title, LastActive: ws.LastActive,
		})
	}

	apps := make([]AppInfo, 0, len(t.apps))
	for name, state := range t.apps {
		timeout, disabled := cfg.EffectiveTimeout(name)
		windows := windowsByApp[name]
		sort.Slice(windows, func(i, j int) bool {
			if !windows[i].LastActive.Equal(windows[j].LastActive) {
				return windows[i].LastActive.After(windows[j].LastActive)
			}
			return windows[i].ID < windows[j].ID
		})
		apps = append(apps, AppInfo{
			Name:       name,
			LastActive: state.LastActive,
			Timeout:    timeout,
			Hidden:     state.Hidden,
			Disabled:   disabled,
			Windows:    windows,
		})
	}

	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Name < apps[j].Name
	})

	return apps
}
