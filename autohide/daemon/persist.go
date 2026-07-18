package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"autohide/config"
)

const (
	trackerStateVersion      = 1
	defaultStateSaveInterval = 30 * time.Second
	defaultStateMaxAge       = 2 * time.Minute
)

type persistedTrackerState struct {
	Version int                          `json:"version"`
	Apps    map[string]persistedAppState `json:"apps"`
}

type persistedAppState struct {
	Pid        int32     `json:"pid"`
	LastActive time.Time `json:"last_active"`
	Hidden     bool      `json:"hidden"`
	DeferUntil time.Time `json:"defer_until"`
}

func defaultTrackerStatePath() string {
	return filepath.Join(config.Dir(), "state.json")
}

func (t *Tracker) snapshotApps() map[string]persistedAppState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	apps := make(map[string]persistedAppState, len(t.apps))
	for name, state := range t.apps {
		apps[name] = persistedAppState{
			Pid:        state.Pid,
			LastActive: state.LastActive,
			Hidden:     state.Hidden,
			DeferUntil: state.DeferUntil,
		}
	}
	return apps
}

func (t *Tracker) restoreApps(apps map[string]persistedAppState) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.apps = make(map[string]*AppState, len(apps))
	for name, state := range apps {
		t.apps[name] = &AppState{
			Pid:        state.Pid,
			LastActive: state.LastActive,
			Hidden:     state.Hidden,
			DeferUntil: state.DeferUntil,
		}
	}
	return len(t.apps)
}

// saveTrackerState atomically commits the tracker app timers without persisting window leases.
func saveTrackerState(path string, tracker *Tracker) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	state := persistedTrackerState{
		Version: trackerStateVersion,
		Apps:    tracker.snapshotApps(),
	}
	if err := json.NewEncoder(tmp).Encode(state); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// restoreTrackerState loads only a recent, complete snapshot so old timers cannot trigger a mass hide.
func restoreTrackerState(path string, tracker *Tracker, now time.Time, maxAge time.Duration) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if now.Sub(info.ModTime()) > maxAge {
		return 0, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var state persistedTrackerState
	if err := json.NewDecoder(f).Decode(&state); err != nil {
		return 0, fmt.Errorf("decode tracker state: %w", err)
	}
	if state.Version != trackerStateVersion {
		return 0, fmt.Errorf("unsupported tracker state version %d", state.Version)
	}
	return tracker.restoreApps(state.Apps), nil
}

func (d *Daemon) restoreState() {
	count, err := restoreTrackerState(d.statePath, d.tracker, time.Now(), defaultStateMaxAge)
	if err != nil {
		d.logger.Warn().Err(err).Str("path", d.statePath).Msg("failed to restore app timers")
		return
	}
	if count > 0 {
		d.logger.Info().Int("count", count).Msgf("restored %d app timers", count)
	}
}

func (d *Daemon) saveState() {
	if err := saveTrackerState(d.statePath, d.tracker); err != nil {
		d.logger.Warn().Err(err).Str("path", d.statePath).Msg("failed to save app timers")
	}
}
