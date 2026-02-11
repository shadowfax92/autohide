package daemon

import (
	"sort"
	"sync"
	"time"

	"autohide/config"
)

type AppState struct {
	LastActive time.Time
	Hidden     bool
}

type AppInfo struct {
	Name       string
	LastActive time.Time
	Timeout    time.Duration
	Hidden     bool
	Disabled   bool
}

type Tracker struct {
	mu   sync.RWMutex
	apps map[string]*AppState
}

func NewTracker() *Tracker {
	return &Tracker{
		apps: make(map[string]*AppState),
	}
}

func (t *Tracker) Update(cfg *config.Config, frontmost string, visible []string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()

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

	return toHide
}

// Prune removes apps that are no longer running from the tracker.
func (t *Tracker) Prune(running []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	runSet := make(map[string]bool, len(running))
	for _, name := range running {
		runSet[name] = true
	}
	for name := range t.apps {
		if !runSet[name] {
			delete(t.apps, name)
		}
	}
}

func (t *Tracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.apps)
}

func (t *Tracker) List(cfg *config.Config) []AppInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	apps := make([]AppInfo, 0, len(t.apps))
	for name, state := range t.apps {
		timeout, disabled := cfg.EffectiveTimeout(name)
		apps = append(apps, AppInfo{
			Name:       name,
			LastActive: state.LastActive,
			Timeout:    timeout,
			Hidden:     state.Hidden,
			Disabled:   disabled,
		})
	}

	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Name < apps[j].Name
	})

	return apps
}
