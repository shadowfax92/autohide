package daemon

import (
	"sort"
	"sync"
	"time"

	"autohide/config"
)

// An accepted hide that remains ineffective for an unknown reason would
// otherwise be re-decided every tick via the reality mirror.
const hideRetryBackoff = 5 * time.Minute

type AppState struct {
	Pid             int32
	LastActive      time.Time
	Hidden          bool
	Unhidable       string
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
	Unhidable  string
	Disabled   bool
	Windows    []WindowDetail
}

type activationCandidate struct {
	Pid int32
	At  time.Time
}

type Tracker struct {
	mu                   sync.RWMutex
	apps                 map[string]*AppState
	windows              map[uint32]*WindowState
	restoredApps         map[string]int32
	recent               []string
	axTrustedPrev        bool
	lastRegularFrontmost string
	activationCandidates map[string]activationCandidate
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
func (t *Tracker) ActivateApp(pid int32, name string, at time.Time) {
	if name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if previous, ok := t.activationCandidates[name]; !ok || at.After(previous.At) {
		t.activationCandidates[name] = activationCandidate{Pid: pid, At: at}
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
	if entry.Pid != 0 && entry.Pid != pid {
		return
	}
	entry.Pid = pid
	t.lastRegularFrontmost = name
	t.recordFrontmostLocked(name)
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

// FreezeLastActive removes an exact away interval from every overlapping lease.
func (t *Tracker) FreezeLastActive(start, end time.Time) {
	if !end.After(start) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, entry := range t.apps {
		entry.LastActive = freezeLease(entry.LastActive, start, end)
	}
	for _, window := range t.windows {
		window.LastActive = freezeLease(window.LastActive, start, end)
	}
}

// ShiftLastActiveBefore applies an inferred gap only to leases that predate it.
func (t *Tracker) ShiftLastActiveBefore(delta time.Duration, cutoff time.Time) {
	if delta <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, entry := range t.apps {
		if !entry.LastActive.After(cutoff) {
			entry.LastActive = entry.LastActive.Add(delta)
		}
	}
	for _, window := range t.windows {
		if !window.LastActive.After(cutoff) {
			window.LastActive = window.LastActive.Add(delta)
		}
	}
}

func freezeLease(lastActive, start, end time.Time) time.Time {
	if !lastActive.Before(end) {
		return lastActive
	}
	overlapStart := start
	if lastActive.After(start) {
		overlapStart = lastActive
	}
	return lastActive.Add(end.Sub(overlapStart))
}

func NewTracker() *Tracker {
	return &Tracker{
		apps:                 make(map[string]*AppState),
		windows:              make(map[uint32]*WindowState),
		activationCandidates: make(map[string]activationCandidate),
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

	frontmost := t.observeAppsLocked(snap, appeared, winsByApp, now)
	// Window state is intentionally ephemeral; the first snapshot must not
	// mistake every restored app's existing windows for a real reappearance.
	t.restoredApps = nil

	var dec Decisions

	for name, entry := range t.apps {
		if name == frontmost || entry.Hidden || entry.Unhidable != "" {
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

// ReconcileApps mirrors snapshot app state without scheduling hide decisions.
func (t *Tracker) ReconcileApps(snap *Snapshot, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.observeAppsLocked(snap, nil, nil, now)
}

// FocusDecisions keeps the running MRU working set visible and applies the
// focus grace to every other eligible app.
func (t *Tracker) FocusDecisions(cfg *config.Config, snap *Snapshot, now time.Time) Decisions {
	t.mu.Lock()
	defer t.mu.Unlock()

	winsByApp := make(map[string]int, len(snap.Windows))
	for _, w := range snap.Windows {
		winsByApp[w.App]++
	}
	t.observeAppsLocked(snap, nil, winsByApp, now)

	keepRecent := cfg.Focus.KeepRecent
	if keepRecent < 1 {
		keepRecent = 1
	}
	recent := t.recentAppsLocked(keepRecent)
	keep := make(map[string]bool, len(recent))
	for _, name := range recent {
		keep[name] = true
	}

	grace := cfg.Focus.Grace.Duration
	if grace < 0 {
		grace = 0
	}
	var dec Decisions
	for name, entry := range t.apps {
		if keep[name] || entry.Hidden || entry.Unhidable != "" {
			continue
		}
		if winsByApp[name] == 0 && (!cfg.General.HideOtherSpaces || !entry.SeenWithWindows) {
			continue
		}
		if now.Before(entry.DeferUntil) {
			continue
		}
		if _, disabled := cfg.EffectiveTimeout(name); disabled {
			continue
		}
		if now.Sub(entry.LastActive) > grace {
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

// RecentApps returns up to n running apps in most-recently-used order.
func (t *Tracker) RecentApps(n int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.recentAppsLocked(n)
}

func (t *Tracker) observeAppsLocked(
	snap *Snapshot,
	appeared map[string]bool,
	winsByApp map[string]int,
	now time.Time,
) string {
	if winsByApp == nil {
		winsByApp = make(map[string]int, len(snap.Windows))
		for _, window := range snap.Windows {
			winsByApp[window.App]++
		}
	}
	appCounts := make(map[string]int, len(snap.Apps))
	runningPIDs := make(map[string]map[int32]bool, len(snap.Apps))
	for _, app := range snap.Apps {
		appCounts[app.Name]++
		if runningPIDs[app.Name] == nil {
			runningPIDs[app.Name] = make(map[int32]bool)
		}
		runningPIDs[app.Name][app.Pid] = true
	}
	running := make(map[string]bool, len(snap.Apps))
	for _, a := range snap.Apps {
		running[a.Name] = true
		entry, ok := t.apps[a.Name]
		replaced := ok && appCounts[a.Name] == 1 && entry.Pid != 0 && entry.Pid != a.Pid
		if !ok || replaced {
			entry = &AppState{LastActive: now}
			t.apps[a.Name] = entry
			if replaced {
				delete(t.restoredApps, a.Name)
				if t.lastRegularFrontmost == a.Name {
					t.lastRegularFrontmost = ""
				}
			}
		}
		entry.Pid = a.Pid
		entry.Unhidable = ""
		if a.Unhidable != nil {
			entry.Unhidable = *a.Unhidable
		}
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
	pruned := t.recent[:0]
	for _, name := range t.recent {
		if running[name] {
			pruned = append(pruned, name)
		}
	}
	t.recent = pruned
	for name := range appeared {
		if entry, ok := t.apps[name]; ok {
			if _, restored := t.restoredApps[name]; !restored {
				if now.After(entry.LastActive) {
					entry.LastActive = now
				}
			}
		}
	}
	frontmost := snap.Frontmost.Name
	var latestCandidate time.Time
	for name, candidate := range t.activationCandidates {
		newerThanSnapshot := snap.StartedAt.IsZero() || !candidate.At.Before(snap.StartedAt)
		seedsUnknownFrontmost := snap.Frontmost.Name == "" && t.lastRegularFrontmost == ""
		if runningPIDs[name][candidate.Pid] && (newerThanSnapshot || seedsUnknownFrontmost) &&
			(latestCandidate.IsZero() || candidate.At.After(latestCandidate)) {
			frontmost = name
			latestCandidate = candidate.At
		}
	}
	if _, ok := t.apps[frontmost]; ok {
		t.lastRegularFrontmost = frontmost
	} else if frontmost == "" && snap.Frontmost.Pid != 0 {
		frontmost = t.lastRegularFrontmost
	}
	clear(t.activationCandidates)
	if entry, ok := t.apps[frontmost]; ok {
		if now.After(entry.LastActive) {
			entry.LastActive = now
		}
		entry.Hidden = false
		entry.DeferUntil = time.Time{}
		t.recordFrontmostLocked(frontmost)
	}
	return frontmost
}

func (t *Tracker) recordFrontmostLocked(name string) {
	if name == "" || len(t.recent) > 0 && t.recent[0] == name {
		return
	}
	recent := make([]string, 1, len(t.recent)+1)
	recent[0] = name
	for _, existing := range t.recent {
		if existing != name {
			recent = append(recent, existing)
		}
	}
	t.recent = recent
}

func (t *Tracker) recentAppsLocked(n int) []string {
	if n <= 0 {
		return nil
	}
	result := make([]string, 0, min(n, len(t.recent)))
	for _, name := range t.recent {
		if _, running := t.apps[name]; running {
			result = append(result, name)
			if len(result) == n {
				break
			}
		}
	}
	return result
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

	frontmost = normalizeLegacyAppName(frontmost)
	if frontmost != "" {
		if state, ok := t.apps[frontmost]; ok {
			if now.After(state.LastActive) {
				state.LastActive = now
			}
			state.Hidden = false
		} else {
			t.apps[frontmost] = &AppState{LastActive: now}
		}
	}
	t.recordFrontmostLocked(frontmost)

	visibleSet := make(map[string]bool, len(visible))
	for _, rawName := range visible {
		name := normalizeLegacyAppName(rawName)
		if name == "" {
			continue
		}
		visibleSet[name] = true
		if _, ok := t.apps[name]; !ok {
			t.apps[name] = &AppState{LastActive: now}
		}
	}

	for name := range t.apps {
		if normalizeLegacyAppName(name) == "" {
			delete(t.apps, name)
		}
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
			Unhidable:  state.Unhidable,
			Disabled:   disabled,
			Windows:    windows,
		})
	}

	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Name < apps[j].Name
	})

	return apps
}
