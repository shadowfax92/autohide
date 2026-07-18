package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fullSnapshotJSON = `{
  "ax_trusted": true,
  "screen_recording": true,
  "frontmost": {"pid": 100, "name": "Google Chrome"},
  "focused_window_id": 42,
  "apps": [
    {"pid": 100, "name": "Google Chrome", "hidden": false},
    {"pid": 200, "name": "Slack", "hidden": true}
  ],
  "windows": [
    {"id": 42, "pid": 100, "app": "Google Chrome", "title": "Docs"},
    {"id": 43, "pid": 100, "app": "Google Chrome", "title": ""}
  ]
}`

func TestParseSnapshotFull(t *testing.T) {
	snap, err := parseSnapshot([]byte(fullSnapshotJSON))
	if err != nil {
		t.Fatal(err)
	}
	if !snap.AXTrusted {
		t.Error("ax_trusted should be true")
	}
	if snap.ScreenRecording == nil || !*snap.ScreenRecording {
		t.Errorf("screen_recording = %v, want true", snap.ScreenRecording)
	}
	if snap.Frontmost.Pid != 100 || snap.Frontmost.Name != "Google Chrome" {
		t.Errorf("frontmost = %+v", snap.Frontmost)
	}
	if snap.FocusedWindowID != 42 {
		t.Errorf("focused_window_id = %d, want 42", snap.FocusedWindowID)
	}
	if len(snap.Apps) != 2 || !snap.Apps[1].Hidden {
		t.Errorf("apps = %+v", snap.Apps)
	}
	if len(snap.Windows) != 2 || snap.Windows[0].ID != 42 || snap.Windows[1].Title != "" {
		t.Errorf("windows = %+v", snap.Windows)
	}
}

func TestParseSnapshotMissingFieldsAreSafe(t *testing.T) {
	snap, err := parseSnapshot([]byte(`{"ax_trusted": false, "apps": [], "windows": []}`))
	if err != nil {
		t.Fatal(err)
	}
	if snap.AXTrusted || snap.FocusedWindowID != 0 || snap.Frontmost.Name != "" {
		t.Errorf("missing fields should zero out, got %+v", snap)
	}
	if snap.ScreenRecording != nil {
		t.Errorf("absent screen_recording must stay unknown (old helper), got %v", *snap.ScreenRecording)
	}
}

func TestParseSnapshotMalformed(t *testing.T) {
	for _, raw := range []string{"", "{not json", "[]"} {
		if _, err := parseSnapshot([]byte(raw)); err == nil {
			t.Errorf("parseSnapshot(%q) should error", raw)
		}
	}
}

func TestLocateHelperPrefersDaemonDir(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeHelper(t, dir, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", "")

	got, err := locateHelper([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestLocateHelperFallsBackToPath(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeHelper(t, dir, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	got, err := locateHelper([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestLocateHelperNotFound(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := locateHelper([]string{t.TempDir()}); err == nil {
		t.Error("expected not-found error")
	}
}

func TestHelperSnapshotParsesOutput(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeHelper(t, dir, "#!/bin/sh\ncat <<'EOF'\n"+fullSnapshotJSON+"\nEOF\n")

	snap, err := NewHelper(path).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.FocusedWindowID != 42 || len(snap.Windows) != 2 {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestHelperTimeoutKillsProcess(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeHelper(t, dir, "#!/bin/sh\nsleep 10\n")

	h := NewHelper(path)
	h.timeout = 100 * time.Millisecond
	start := time.Now()
	_, err := h.Snapshot()
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("helper not killed promptly, took %v", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error %q should mention timeout", err)
	}
}

func TestHelperFailurePropagatesStderr(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeHelper(t, dir, "#!/bin/sh\necho 'boom' >&2\nexit 1\n")

	_, err := NewHelper(path).Snapshot()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q should carry stderr detail", err)
	}
}

func TestHelperWatchStreamsEventsUntilContextCancellation(t *testing.T) {
	dir := t.TempDir()
	closedPath := filepath.Join(dir, "stdin-closed")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"ts":1000,"type":"activate","pid":42,"name":"Slack"}'
printf '%%s\n' '{"ts":1100,"type":"lock"}'
while IFS= read -r line; do :; done
printf closed > %q
`, closedPath)
	h := NewHelper(writeFakeHelper(t, dir, script))
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan WatchEvent, 2)
	done := make(chan error, 1)
	go func() {
		done <- h.Watch(ctx, func(event WatchEvent) { events <- event })
	}()

	first := <-events
	second := <-events
	if first.Type != "activate" || first.Pid != 42 || first.Name != "Slack" || first.TS != 1000 {
		t.Fatalf("first event = %+v", first)
	}
	if second.Type != "lock" || second.Pid != 0 || second.Name != "" || second.TS != 1100 {
		t.Fatalf("second event = %+v", second)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch() on cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch() did not stop after cancellation")
	}
	if _, err := os.Stat(closedPath); err != nil {
		t.Fatalf("watch child did not observe stdin EOF: %v", err)
	}
}

func TestHelperWatchRejectsMalformedEvents(t *testing.T) {
	dir := t.TempDir()
	h := NewHelper(writeFakeHelper(t, dir, "#!/bin/sh\nprintf '%s\\n' '{not-json'\n"))
	if err := h.Watch(context.Background(), func(WatchEvent) {}); err == nil || !strings.Contains(err.Error(), "parse watch event") {
		t.Fatalf("Watch() error = %v, want parse watch event", err)
	}
}

func writeFakeHelper(t *testing.T, dir, script string) string {
	return writeFakeBinary(t, dir, helperName, script)
}

func writeFakeBinary(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocateUIPrefersSiblingDir(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeBinary(t, dir, uiName, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", "")

	got, err := locateBinary(uiName, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestLocateUIFallsBackToPath(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeBinary(t, dir, uiName, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	got, err := locateBinary(uiName, []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestLocateUINotFound(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := locateBinary(uiName, []string{t.TempDir()}); err == nil {
		t.Error("expected not-found error")
	}
}
