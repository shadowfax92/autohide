package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"autohide/config"

	"github.com/rs/zerolog"
)

type synchronizedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestTrackerStateRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 17, 17, 0, 0, 0, time.Local)
	path := filepath.Join(t.TempDir(), "state.json")
	original := NewTracker()
	original.apps["Slack"] = &AppState{
		Pid:        42,
		LastActive: now.Add(-45 * time.Second),
		Hidden:     true,
		DeferUntil: now.Add(time.Minute),
	}
	original.windows[7] = &WindowState{Pid: 42, App: "Slack", Title: "general", LastActive: now}

	if err := saveTrackerState(path, original); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Fatalf("state mode = %o, want 644", got)
	}

	restored := NewTracker()
	count, err := restoreTrackerState(path, restored, now, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("restored count = %d, want 1", count)
	}
	got := restored.apps["Slack"]
	if got == nil || got.Pid != 42 || !got.LastActive.Equal(now.Add(-45*time.Second)) || !got.Hidden || !got.DeferUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("restored app = %+v", got)
	}
	if len(restored.windows) != 0 {
		t.Fatalf("restored %d windows, want none", len(restored.windows))
	}
}

func TestRestoredAppsKeepTimersThroughFirstWindowSnapshot(t *testing.T) {
	tracker := NewTracker()
	tracker.restoreApps(map[string]persistedAppState{
		"Google Chrome": {Pid: 100, LastActive: at(0)},
		"Slack":         {Pid: 200, LastActive: at(0)},
	})
	cfg := testCfg()
	apps := []SnapApp{chromeApp(), {Pid: 200, Name: "Slack"}}
	windows := []SnapWindow{win(1, 100, "Google Chrome"), win(2, 200, "Slack")}

	tracker.Update(cfg, snap(chromeApp(), 1, apps, windows), at(30))
	if got := appLastActive(tracker.List(cfg), "Slack"); !got.Equal(at(0)) {
		t.Fatalf("first snapshot re-leased restored Slack timer: %v", got)
	}

	tracker.Update(cfg, snap(chromeApp(), 1, apps, windows[:1]), at(35))
	tracker.Update(cfg, snap(chromeApp(), 1, apps, windows), at(40))
	if got := appLastActive(tracker.List(cfg), "Slack"); !got.Equal(at(40)) {
		t.Fatalf("real window reappearance did not grant a fresh lease: %v", got)
	}
}

func appLastActive(apps []AppInfo, name string) time.Time {
	for _, app := range apps {
		if app.Name == name {
			return app.LastActive
		}
	}
	return time.Time{}
}

func TestRestoreTrackerStateFreshnessAndInvalidFiles(t *testing.T) {
	now := time.Date(2026, 7, 17, 17, 0, 0, 0, time.Local)

	t.Run("missing", func(t *testing.T) {
		tracker := NewTracker()
		count, err := restoreTrackerState(filepath.Join(t.TempDir(), "missing.json"), tracker, now, 2*time.Minute)
		if err != nil || count != 0 || tracker.Count() != 0 {
			t.Fatalf("restore missing = count %d, err %v, tracker %d", count, err, tracker.Count())
		}
	})

	t.Run("stale", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		original := NewTracker()
		original.apps["Slack"] = &AppState{Pid: 42, LastActive: now.Add(-time.Minute)}
		if err := saveTrackerState(path, original); err != nil {
			t.Fatal(err)
		}
		stale := now.Add(-2*time.Minute - time.Second)
		if err := os.Chtimes(path, stale, stale); err != nil {
			t.Fatal(err)
		}

		tracker := NewTracker()
		count, err := restoreTrackerState(path, tracker, now, 2*time.Minute)
		if err != nil || count != 0 || tracker.Count() != 0 {
			t.Fatalf("restore stale = count %d, err %v, tracker %d", count, err, tracker.Count())
		}
	})

	t.Run("malformed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte("{"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now, now); err != nil {
			t.Fatal(err)
		}

		tracker := NewTracker()
		tracker.apps["Existing"] = &AppState{LastActive: now}
		count, err := restoreTrackerState(path, tracker, now, 2*time.Minute)
		if err == nil || count != 0 || tracker.Count() != 1 {
			t.Fatalf("restore malformed = count %d, err %v, tracker %d", count, err, tracker.Count())
		}
	})
}

func TestDaemonRunRestoresBeforeTickAndSavesOnShutdown(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	now := time.Now()
	path := filepath.Join(t.TempDir(), "state.json")
	original := NewTracker()
	original.apps["Slack"] = &AppState{Pid: 42, LastActive: now.Add(-45 * time.Second)}
	if err := saveTrackerState(path, original); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.General.CheckInterval = config.Duration{Duration: time.Hour}
	var logs synchronizedBuffer
	d := New(cfg, filepath.Join(t.TempDir(), "config.toml"), zerolog.New(&logs))
	d.statePath = path
	d.stateSaveInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for d.TrackerCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if d.TrackerCount() != 1 {
		cancel()
		<-done
		t.Fatal("tracker state was not restored before the first tick")
	}
	if !strings.Contains(logs.String(), `"message":"restored 1 app timers"`) || !strings.Contains(logs.String(), `"count":1`) {
		t.Fatalf("restore log missing count: %s", logs.String())
	}

	d.tracker.mu.Lock()
	d.tracker.apps["Slack"].LastActive = now.Add(-90 * time.Second)
	d.tracker.mu.Unlock()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	restored := NewTracker()
	count, err := restoreTrackerState(path, restored, time.Now(), 2*time.Minute)
	if err != nil || count != 1 {
		t.Fatalf("restore shutdown save = count %d, err %v", count, err)
	}
	if got := restored.apps["Slack"].LastActive; !got.Equal(now.Add(-90 * time.Second)) {
		t.Fatalf("shutdown LastActive = %v", got)
	}
}

func TestDaemonRunSavesOnCadence(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(t.TempDir(), "state.json")
	cfg := config.Default()
	cfg.General.CheckInterval = config.Duration{Duration: time.Hour}
	d := New(cfg, filepath.Join(t.TempDir(), "config.toml"), zerolog.Nop())
	d.statePath = path
	d.stateSaveInterval = 10 * time.Millisecond
	d.tracker.apps["Slack"] = &AppState{Pid: 42, LastActive: time.Now()}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("periodic state save did not create state.json")
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
