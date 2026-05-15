package daemon

import (
	"reflect"
	"testing"
	"time"

	"autohide/config"
)

func TestTrackerTracksAppsIndependently(t *testing.T) {
	cfg := config.Default()
	cfg.General.DefaultTimeout = config.Duration{Duration: time.Second}
	tracker := NewTracker()
	now := time.Unix(100, 0)

	visible := []string{"Google Chrome", "Slack"}

	tracker.UpdateAt(cfg, "Google Chrome", visible, now)
	toHide := tracker.UpdateAt(cfg, "Google Chrome", visible, now.Add(1100*time.Millisecond))

	if got, want := toHide, []string{"Slack"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("apps to hide = %v, want %v", got, want)
	}

	toHide = tracker.UpdateAt(cfg, "Slack", visible, now.Add(2200*time.Millisecond))
	if got, want := toHide, []string{"Google Chrome"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("apps to hide = %v, want %v", got, want)
	}
}
