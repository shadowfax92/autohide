package daemon

import "testing"

func TestResolveWindowStatus(t *testing.T) {
	cases := []struct {
		name           string
		windowTracking bool
		helperFound    bool
		helperFails    int
		axTrusted      bool
		want           string
	}{
		{"config off wins over everything", false, true, 0, true, "off"},
		{"helper missing", true, false, 0, true, "legacy: helper not found"},
		{"helper failing at threshold", true, true, 3, true, "legacy: helper failing"},
		{"transient failures keep mode", true, true, 2, true, "active"},
		{"no accessibility -> app tier only", true, true, 0, false, "app-only: accessibility not granted"},
		{"all good", true, true, 0, true, "active"},
	}
	for _, c := range cases {
		got := resolveWindowStatus(c.windowTracking, c.helperFound, c.helperFails, c.axTrusted)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
