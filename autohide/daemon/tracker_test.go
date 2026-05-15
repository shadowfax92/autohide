package daemon

import (
	"reflect"
	"testing"
	"time"

	"autohide/config"
)

func TestTrackerTracksSameAppWindowsIndependently(t *testing.T) {
	cfg := config.Default()
	cfg.General.DefaultTimeout = config.Duration{Duration: time.Second}
	tracker := NewTracker()
	now := time.Unix(100, 0)

	docs := WindowInfo{ID: "chrome-docs", AppName: "Google Chrome", Title: "Docs"}
	mail := WindowInfo{ID: "chrome-mail", AppName: "Google Chrome", Title: "Mail"}
	calendar := WindowInfo{ID: "chrome-calendar", AppName: "Google Chrome", Title: "Calendar"}
	visible := []WindowInfo{docs, mail, calendar}

	tracker.UpdateWindowsAt(cfg, docs, visible, now)
	tracker.UpdateWindowsAt(cfg, mail, visible, now.Add(500*time.Millisecond))
	toHide := tracker.UpdateWindowsAt(cfg, calendar, visible, now.Add(1100*time.Millisecond))

	if got, want := windowIDs(toHide), []string{"chrome-docs"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("windows to hide = %v, want %v", got, want)
	}
}

func windowIDs(windows []WindowInfo) []string {
	ids := make([]string, 0, len(windows))
	for _, window := range windows {
		ids = append(ids, window.ID)
	}
	return ids
}
