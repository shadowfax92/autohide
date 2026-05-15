package daemon

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"autohide/config"
)

type WindowInfo struct {
	ID           string
	AppName      string
	Title        string
	Index        int
	WindowNumber string
	Position     string
	Size         string
	Minimized    bool
}

type WindowState struct {
	Window     WindowInfo
	LastActive time.Time
	Hidden     bool
}

type AppInfo struct {
	Name        string
	WindowID    string
	WindowTitle string
	LastActive  time.Time
	Timeout     time.Duration
	Hidden      bool
	Disabled    bool
}

type Tracker struct {
	mu      sync.RWMutex
	windows map[string]*WindowState
}

func NewTracker() *Tracker {
	return &Tracker{
		windows: make(map[string]*WindowState),
	}
}

func (t *Tracker) UpdateWindows(cfg *config.Config, frontmost WindowInfo, windows []WindowInfo) []WindowInfo {
	return t.UpdateWindowsAt(cfg, frontmost, windows, time.Now())
}

// UpdateWindowsAt refreshes the per-window activity table and returns visible
// windows whose own inactivity timer expired.
func (t *Tracker) UpdateWindowsAt(cfg *config.Config, frontmost WindowInfo, windows []WindowInfo, now time.Time) []WindowInfo {
	t.mu.Lock()
	defer t.mu.Unlock()

	frontmost = normalizeWindow(frontmost)
	if frontmost.ID != "" {
		if state, ok := t.windows[frontmost.ID]; ok {
			state.Window = frontmost
			state.LastActive = now
			state.Hidden = false
		} else {
			t.windows[frontmost.ID] = &WindowState{Window: frontmost, LastActive: now}
		}
	}

	windowSet := make(map[string]WindowInfo, len(windows))
	for _, window := range windows {
		window = normalizeWindow(window)
		if window.ID == "" {
			continue
		}
		windowSet[window.ID] = window
		if state, ok := t.windows[window.ID]; ok {
			state.Window = window
		} else {
			t.windows[window.ID] = &WindowState{Window: window, LastActive: now}
		}
	}

	for id := range t.windows {
		if _, ok := windowSet[id]; !ok {
			delete(t.windows, id)
		}
	}

	var toHide []WindowInfo
	for id, state := range t.windows {
		window, visible := windowSet[id]
		if !visible || id == frontmost.ID || state.Hidden || window.Minimized {
			continue
		}

		timeout, disabled := cfg.EffectiveTimeout(window.AppName)
		if disabled {
			continue
		}

		if now.Sub(state.LastActive) > timeout {
			toHide = append(toHide, window)
			state.Hidden = true
		}
	}

	sort.Slice(toHide, func(i, j int) bool {
		return toHide[i].ID < toHide[j].ID
	})

	return toHide
}

// Prune removes windows owned by apps that are no longer running from the tracker.
func (t *Tracker) Prune(running []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	runSet := make(map[string]bool, len(running))
	for _, name := range running {
		runSet[name] = true
	}
	for id, state := range t.windows {
		if !runSet[state.Window.AppName] {
			delete(t.windows, id)
		}
	}
}

func (t *Tracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.windows)
}

func (t *Tracker) List(cfg *config.Config) []AppInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	apps := make([]AppInfo, 0, len(t.windows))
	for _, state := range t.windows {
		timeout, disabled := cfg.EffectiveTimeout(state.Window.AppName)
		apps = append(apps, AppInfo{
			Name:        state.Window.AppName,
			WindowID:    state.Window.ID,
			WindowTitle: state.Window.Title,
			LastActive:  state.LastActive,
			Timeout:     timeout,
			Hidden:      state.Hidden,
			Disabled:    disabled,
		})
	}

	sort.Slice(apps, func(i, j int) bool {
		if apps[i].Name != apps[j].Name {
			return apps[i].Name < apps[j].Name
		}
		if apps[i].WindowTitle != apps[j].WindowTitle {
			return apps[i].WindowTitle < apps[j].WindowTitle
		}
		return apps[i].WindowID < apps[j].WindowID
	})

	return apps
}

func normalizeWindow(window WindowInfo) WindowInfo {
	if window.ID != "" || window.AppName == "" {
		return window
	}
	if window.WindowNumber != "" {
		window.ID = window.AppName + "\x00" + window.WindowNumber
		return window
	}
	if window.Title != "" || window.Position != "" || window.Size != "" {
		window.ID = window.AppName + "\x00" + window.Title + "\x00" + window.Position + "\x00" + window.Size
		return window
	}
	window.ID = window.AppName + "\x00" + strconv.Itoa(window.Index)
	return window
}
