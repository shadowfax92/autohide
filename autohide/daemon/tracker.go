package daemon

import (
	"sort"
	"sync"
	"time"

	"autohide/config"
)

// More than this per tick turns into a genie-animation storm (e.g. right
// after pause->resume when everything expired at once); the remainder stays
// eligible next tick.
const maxMinimizePerTick = 3

type AppState struct {
	Pid        int32
	LastActive time.Time
	Hidden     bool
}

type WindowState struct {
	Pid        int32
	App        string
	Title      string
	LastActive time.Time
	DeferUntil time.Time
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
	mu      sync.RWMutex
	apps    map[string]*AppState
	windows map[uint32]*WindowState
}

func NewTracker() *Tracker {
	return &Tracker{
		apps:    make(map[string]*AppState),
		windows: make(map[uint32]*WindowState),
	}
}

// Update reconciles tracker state with one helper snapshot and decides this
// tick's actions. Two tiers: stale apps hide whole (clean cmd-tab restore),
// stale windows of still-in-use apps minimize individually. Windows of an
// app being hidden are left to the app tier so they don't strand in the
// Dock. Refresh rules: focused window; any window that (re)appears (fresh
// lease for it AND its app — nothing just summoned gets insta-hidden);
// frontmost app. Deliberately NO sibling refresh on app activation, or stale
// windows would never age out while the user bounces between apps.
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
	if snap.FocusedWindowID != 0 {
		if ws, ok := t.windows[snap.FocusedWindowID]; ok {
			ws.LastActive = now
		}
	}

	running := make(map[string]bool, len(snap.Apps))
	for _, a := range snap.Apps {
		running[a.Name] = true
		entry, ok := t.apps[a.Name]
		if !ok {
			entry = &AppState{LastActive: now}
			t.apps[a.Name] = entry
		}
		entry.Pid = a.Pid
		// Mirror reality: a failed hide self-heals (still visible next tick
		// -> re-decided), a user unhide is seen without polling extra state.
		entry.Hidden = a.Hidden
	}
	for name := range t.apps {
		if !running[name] {
			delete(t.apps, name)
		}
	}
	for name := range appeared {
		if entry, ok := t.apps[name]; ok {
			entry.LastActive = now
		}
	}
	if entry, ok := t.apps[snap.Frontmost.Name]; ok {
		entry.LastActive = now
		entry.Hidden = false
	}

	var dec Decisions

	hiding := make(map[string]bool)
	for name, entry := range t.apps {
		// Zero-window apps: hiding them is invisible and re-hide loops
		// after unhide; skip.
		if name == snap.Frontmost.Name || entry.Hidden || winsByApp[name] == 0 {
			continue
		}
		timeout, disabled := cfg.EffectiveTimeout(name)
		if disabled {
			continue
		}
		if now.Sub(entry.LastActive) > timeout {
			dec.HideApps = append(dec.HideApps, AppRef{Pid: entry.Pid, Name: name})
			entry.Hidden = true
			hiding[name] = true
		}
	}
	sort.Slice(dec.HideApps, func(i, j int) bool {
		return dec.HideApps[i].Name < dec.HideApps[j].Name
	})

	// Never minimize blind: a tick without a positively identified focused
	// window (or without AX) makes no window-tier moves.
	if !snap.AXTrusted || snap.FocusedWindowID == 0 {
		return dec
	}

	type candidate struct {
		id uint32
		ws *WindowState
	}
	var cands []candidate
	for id, ws := range t.windows {
		if id == snap.FocusedWindowID || hiding[ws.App] {
			continue
		}
		entry, ok := t.apps[ws.App]
		if !ok || entry.Hidden {
			continue
		}
		timeout, disabled := cfg.EffectiveTimeout(ws.App)
		if disabled {
			continue
		}
		if now.Sub(ws.LastActive) <= timeout || now.Before(ws.DeferUntil) {
			continue
		}
		cands = append(cands, candidate{id, ws})
	}
	sort.Slice(cands, func(i, j int) bool {
		if !cands[i].ws.LastActive.Equal(cands[j].ws.LastActive) {
			return cands[i].ws.LastActive.Before(cands[j].ws.LastActive)
		}
		return cands[i].id < cands[j].id
	})
	if len(cands) > maxMinimizePerTick {
		cands = cands[:maxMinimizePerTick]
	}
	for _, c := range cands {
		dec.MinimizeWindows = append(dec.MinimizeWindows, WindowRef{
			ID: c.id, Pid: c.ws.Pid, App: c.ws.App, Title: c.ws.Title,
		})
	}

	return dec
}

// DeferWindow backs off a window whose minimize failed so it isn't retried
// every tick.
func (t *Tracker) DeferWindow(id uint32, until time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ws, ok := t.windows[id]; ok {
		ws.DeferUntil = until
	}
}

// UpdateLegacy is the pre-window-tracking app-level behavior, used by the
// fallback modes (helper unavailable or window_tracking off).
func (t *Tracker) UpdateLegacy(cfg *config.Config, frontmost string, visible []string, now time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if state, ok := t.apps[frontmost]; ok {
		state.LastActive = now
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
