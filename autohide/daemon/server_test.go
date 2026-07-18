package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"autohide/config"
	"autohide/ipc"

	"github.com/rs/zerolog"
)

func seededServer(t *testing.T) *Server {
	t.Helper()
	cfg := testCfg()
	d := New(cfg, "", zerolog.Nop())
	wins := []SnapWindow{
		{ID: 1, Pid: 100, App: "Google Chrome", Title: "Docs"},
		{ID: 2, Pid: 100, App: "Google Chrome", Title: ""},
	}
	d.tracker.Update(cfg, snap(chromeApp(), 1, []SnapApp{chromeApp()}, wins), at(0))
	return NewServer(d, "", zerolog.Nop())
}

func TestHandleListWindowCounts(t *testing.T) {
	s := seededServer(t)

	resp := s.handleList(ipc.Request{Command: "list"})
	if !resp.OK {
		t.Fatalf("list failed: %s", resp.Error)
	}
	data := resp.Data.(ipc.ListData)
	if len(data.Apps) != 1 || data.Apps[0].WindowCount != 2 {
		t.Errorf("expected one app with 2 windows, got %+v", data.Apps)
	}
	if data.Apps[0].Windows != nil {
		t.Error("window detail must be omitted without windows=true")
	}
}

func TestHandleListWindowDetail(t *testing.T) {
	s := seededServer(t)

	resp := s.handleList(ipc.Request{Command: "list", Args: map[string]string{"windows": "true"}})
	data := resp.Data.(ipc.ListData)
	if len(data.Apps) != 1 || len(data.Apps[0].Windows) != 2 {
		t.Fatalf("expected 2 window rows, got %+v", data.Apps)
	}
	titles := map[uint32]string{}
	for _, w := range data.Apps[0].Windows {
		titles[w.ID] = w.Title
		if w.LastActive == "" {
			t.Errorf("window %d missing last-active: %+v", w.ID, w)
		}
	}
	if titles[1] != "Docs" || titles[2] != "" {
		t.Errorf("unexpected titles %+v", titles)
	}
}

func TestHandleListCarriesUnhidableReason(t *testing.T) {
	cfg := testCfg()
	d := New(cfg, "", zerolog.Nop())
	chrome := chromeApp()
	chrome.Unhidable = stringPtr("fullscreen")
	d.tracker.Update(cfg, snap(chrome, 1, []SnapApp{chrome}, []SnapWindow{
		{ID: 1, Pid: 100, App: "Google Chrome", Title: "Docs"},
	}), at(0))

	data := NewServer(d, "", zerolog.Nop()).handleList(ipc.Request{Command: "list"}).Data.(ipc.ListData)
	if len(data.Apps) != 1 || data.Apps[0].Unhidable != "fullscreen" {
		t.Fatalf("list data = %+v, want fullscreen reason", data.Apps)
	}
	if data.Apps[0].TimeRemaining != "0s" {
		t.Errorf("unhidable remaining = %q, want 0s", data.Apps[0].TimeRemaining)
	}
}

func TestHandleStatusCarriesWindowTracking(t *testing.T) {
	s := seededServer(t)
	resp := s.handleStatus()
	data := resp.Data.(ipc.StatusData)
	if data.WindowTracking != "starting" {
		t.Errorf("window_tracking = %q, want starting (pre-first-tick)", data.WindowTracking)
	}
}

func TestFocusModeDataCarriesConfiguredPolicyAndKeepSet(t *testing.T) {
	s := seededServer(t)
	s.daemon.cfg.Focus.KeepRecent = 2
	s.daemon.cfg.Focus.Grace = config.Duration{Duration: 30 * time.Second}

	data := s.focusModeData(true)
	if !data.Active || data.KeepRecent != 2 || data.Grace != "30s" {
		t.Fatalf("focus data = %+v", data)
	}
	want := []string{"Google Chrome"}
	if !equalStrings(data.KeepSet, want) {
		t.Errorf("keep_set = %v, want %v", data.KeepSet, want)
	}
}

func TestHandleStatusCarriesFocusKeepRecent(t *testing.T) {
	s := seededServer(t)
	s.daemon.cfg.Focus.KeepRecent = 4
	data := s.handleStatus().Data.(ipc.StatusData)
	if data.FocusKeepRecent != 4 {
		t.Errorf("focus_keep_recent = %d, want 4", data.FocusKeepRecent)
	}
}

func TestHandleStatusPermissionsUnknownBeforeFirstTick(t *testing.T) {
	s := seededServer(t)
	data := s.handleStatus().Data.(ipc.StatusData)
	if data.AXTrusted != nil || data.ScreenRecording != nil {
		t.Errorf("permissions must be unknown pre-tick, got ax=%v sr=%v", data.AXTrusted, data.ScreenRecording)
	}
	if data.DefaultTimeout != "1m" {
		t.Errorf("default_timeout = %q, want 1m", data.DefaultTimeout)
	}
	want := []string{"30s", "1m", "2m", "5m"}
	if len(data.TimeoutPresets) != len(want) {
		t.Fatalf("timeout_presets = %v, want %v", data.TimeoutPresets, want)
	}
	for i, p := range want {
		if data.TimeoutPresets[i] != p {
			t.Errorf("timeout_presets[%d] = %q, want %q", i, data.TimeoutPresets[i], p)
		}
	}
}

const mixedPermissionsSnapshotJSON = `{
  "ax_trusted": true,
  "screen_recording": false,
  "frontmost": {"pid": 100, "name": "Google Chrome"},
  "focused_window_id": 42,
  "apps": [{"pid": 100, "name": "Google Chrome", "hidden": false}],
  "windows": [{"id": 42, "pid": 100, "app": "Google Chrome", "title": "Docs"}]
}`

func TestHandleStatusPermissionsAfterSnapshot(t *testing.T) {
	d := testDaemon(t, "#!/bin/sh\ncat <<'JSON'\n"+mixedPermissionsSnapshotJSON+"\nJSON\n")
	if !d.tickNative(d.cfg, false) {
		t.Fatal("native tick should run")
	}

	s := NewServer(d, "", zerolog.Nop())
	data := s.handleStatus().Data.(ipc.StatusData)
	if data.AXTrusted == nil || !*data.AXTrusted {
		t.Errorf("ax_trusted = %v, want true", data.AXTrusted)
	}
	if data.ScreenRecording == nil || *data.ScreenRecording {
		t.Errorf("screen_recording = %v, want false", data.ScreenRecording)
	}
}

// tempSock avoids t.TempDir(): test names push the path past the 104-char
// unix socket limit on macOS.
func tempSock(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ah")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "d.sock")
}

func liveServer(t *testing.T, sock string) *Server {
	t.Helper()
	return NewServer(New(testCfg(), "", zerolog.Nop()), sock, zerolog.Nop())
}

func TestShutdownRepliesOKThenFiresHook(t *testing.T) {
	sock := tempSock(t)
	srv := liveServer(t, sock)
	fired := make(chan struct{})
	srv.SetOnShutdown(func() { close(fired) })
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	resp, err := ipc.NewClient(sock).Send(ipc.Request{Command: "shutdown"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("shutdown not OK: %s", resp.Error)
	}

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown hook never fired")
	}
}

func TestShutdownHookFiresOnce(t *testing.T) {
	sock := tempSock(t)
	srv := liveServer(t, sock)
	var fires atomic.Int32
	srv.SetOnShutdown(func() { fires.Add(1) })
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	for range 3 {
		if _, err := ipc.NewClient(sock).Send(ipc.Request{Command: "shutdown"}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if n := fires.Load(); n != 1 {
		t.Fatalf("shutdown hook fired %d times, want 1", n)
	}
}

func TestShutdownWithoutHookKeepsServing(t *testing.T) {
	sock := tempSock(t)
	srv := liveServer(t, sock)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	resp, err := ipc.NewClient(sock).Send(ipc.Request{Command: "shutdown"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("shutdown not OK: %s", resp.Error)
	}

	resp, err = ipc.NewClient(sock).Send(ipc.Request{Command: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("server stopped serving after hook-less shutdown: %s", resp.Error)
	}
}

func TestUnknownCommandStillErrors(t *testing.T) {
	sock := tempSock(t)
	srv := liveServer(t, sock)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	resp, err := ipc.NewClient(sock).Send(ipc.Request{Command: "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error == "" {
		t.Fatalf("expected unknown-command error, got %+v", resp)
	}
}

// The overlay timer was removed; its IPC verbs must answer like any other
// unknown command rather than keep a vestigial handler surface.
func TestRemovedOverlayCommandsAreUnknown(t *testing.T) {
	sock := tempSock(t)
	srv := liveServer(t, sock)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	resp, err := ipc.NewClient(sock).Send(ipc.Request{Command: "overlay_start"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || !strings.Contains(resp.Error, "unknown command: overlay_start") {
		t.Fatalf("expected unknown-command error for overlay_start, got %+v", resp)
	}
}

func TestStartTakeoverDisplacesLiveHolder(t *testing.T) {
	sock := tempSock(t)

	holder := liveServer(t, sock)
	holderDown := make(chan struct{})
	holder.SetOnShutdown(func() {
		holder.Stop()
		close(holderDown)
	})
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}

	usurper := liveServer(t, sock)
	if err := usurper.StartTakeover(3 * time.Second); err != nil {
		t.Fatalf("takeover failed: %v", err)
	}
	defer usurper.Stop()

	select {
	case <-holderDown:
	case <-time.After(2 * time.Second):
		t.Fatal("holder never shut down")
	}

	resp, err := ipc.NewClient(sock).Send(ipc.Request{Command: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("new server not serving after takeover: %s", resp.Error)
	}
}

func TestStartTakeoverRecoversStaleSocket(t *testing.T) {
	sock := tempSock(t)
	if err := os.WriteFile(sock, nil, 0644); err != nil {
		t.Fatal(err)
	}

	srv := liveServer(t, sock)
	if err := srv.StartTakeover(time.Second); err != nil {
		t.Fatalf("stale socket not recovered: %v", err)
	}
	srv.Stop()
}

func TestStartTakeoverTimesOutOnStubbornHolder(t *testing.T) {
	sock := tempSock(t)

	holder := liveServer(t, sock)
	holder.SetOnShutdown(func() {}) // acknowledges but never exits
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer holder.Stop()

	usurper := liveServer(t, sock)
	start := time.Now()
	err := usurper.StartTakeover(700 * time.Millisecond)
	if err == nil {
		usurper.Stop()
		t.Fatal("expected takeover to fail against stubborn holder")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("takeover did not respect timeout, took %s", time.Since(start))
	}
}

// Simulates the takeover window: the path is freed while the dying holder's
// listener is still open, a new server binds, then the holder stops. The
// holder must not unlink the new server's socket (neither via its explicit
// remove nor via UnixListener auto-unlink-on-close).
func TestStopDoesNotUnlinkAReboundSocket(t *testing.T) {
	sock := tempSock(t)

	holder := liveServer(t, sock)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	os.Remove(sock)

	usurper := liveServer(t, sock)
	if err := usurper.Start(); err != nil {
		t.Fatal(err)
	}
	defer usurper.Stop()

	holder.Stop()

	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("dying holder unlinked the new daemon's socket: %v", err)
	}
	resp, err := ipc.NewClient(sock).Send(ipc.Request{Command: "status"})
	if err != nil || !resp.OK {
		t.Fatalf("new server unreachable after holder stopped: %v %+v", err, resp)
	}
}

func TestStartStillStrictAgainstLiveHolder(t *testing.T) {
	sock := tempSock(t)

	holder := liveServer(t, sock)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer holder.Stop()

	second := liveServer(t, sock)
	if err := second.Start(); err == nil {
		second.Stop()
		t.Fatal("plain Start must refuse a live socket")
	}
}

// The stub validates its argv so a wrong helper invocation fails the test.
const axPromptScript = "#!/bin/sh\n[ \"$1\" = \"check\" ] && [ \"$2\" = \"--prompt\" ] || { echo bad args >&2; exit 1; }\necho '{\"ax_trusted\": true}'\n"

func TestAXPromptRunsHelperAndRefreshesCache(t *testing.T) {
	dir := t.TempDir()
	writeFakeHelper(t, dir, axPromptScript)
	t.Setenv("PATH", dir)

	s := NewServer(testDaemon(t, ""), "", zerolog.Nop())
	resp := s.handleAXPrompt()
	if !resp.OK {
		t.Fatalf("ax_prompt failed: %s", resp.Error)
	}
	if data := resp.Data.(ipc.AXPromptData); !data.AXTrusted {
		t.Errorf("ax_trusted = false, want true")
	}

	status := s.handleStatus().Data.(ipc.StatusData)
	if status.AXTrusted == nil || !*status.AXTrusted {
		t.Errorf("status ax_trusted = %v after prompt, want true", status.AXTrusted)
	}
}

func TestAXPromptWithoutHelperErrors(t *testing.T) {
	t.Setenv("PATH", "")

	s := NewServer(testDaemon(t, ""), "", zerolog.Nop())
	resp := s.handleAXPrompt()
	if resp.OK || !strings.Contains(resp.Error, "autohide-helper") {
		t.Fatalf("expected helper-not-found error, got %+v", resp)
	}
}

func TestSetTimeoutPersistsConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	d := New(config.Default(), cfgPath, zerolog.Nop())
	s := NewServer(d, "", zerolog.Nop())

	resp := s.handleSetTimeout(ipc.Request{Command: "set_timeout", Args: map[string]string{"duration": "2m"}})
	if !resp.OK {
		t.Fatalf("set_timeout failed: %s", resp.Error)
	}
	if got := d.Config().General.DefaultTimeout.Duration; got != 2*time.Minute {
		t.Errorf("in-memory timeout = %v, want 2m", got)
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.General.DefaultTimeout.Duration; got != 2*time.Minute {
		t.Errorf("persisted timeout = %v, want 2m", got)
	}
	if status := s.handleStatus().Data.(ipc.StatusData); status.DefaultTimeout != "2m" {
		t.Errorf("status default_timeout = %q, want 2m", status.DefaultTimeout)
	}
}

func TestSetTimeoutRejectsInvalid(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	d := New(config.Default(), cfgPath, zerolog.Nop())
	s := NewServer(d, "", zerolog.Nop())

	for _, args := range []map[string]string{nil, {"duration": "nope"}, {"duration": ""}} {
		resp := s.handleSetTimeout(ipc.Request{Command: "set_timeout", Args: args})
		if resp.OK || resp.Error == "" {
			t.Errorf("set_timeout(%v) should error, got %+v", args, resp)
		}
	}
	if got := d.Config().General.DefaultTimeout.Duration; got != time.Minute {
		t.Errorf("timeout changed to %v on invalid input", got)
	}
}

// set_timeout must not mutate the config struct that Config() has already
// handed to unlocked readers (status handler, menubar). Fails under -race
// if SetDefaultTimeout writes in place instead of copy-on-write.
func TestSetTimeoutDoesNotRaceStatusReaders(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	d := New(config.Default(), cfgPath, zerolog.Nop())
	s := NewServer(d, "", zerolog.Nop())

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.handleStatus()
		}()
		go func() {
			defer wg.Done()
			s.handleSetTimeout(ipc.Request{Command: "set_timeout", Args: map[string]string{"duration": "2m"}})
		}()
	}
	wg.Wait()
}

// An old helper whose snapshot omits screen_recording must leave the cache
// unknown rather than stamping "denied".
func TestSnapshotWithoutSRKeepsCacheUnknown(t *testing.T) {
	noSR := `{
  "ax_trusted": true,
  "frontmost": {"pid": 100, "name": "Google Chrome"},
  "focused_window_id": 42,
  "apps": [{"pid": 100, "name": "Google Chrome", "hidden": false}],
  "windows": []
}`
	d := testDaemon(t, "#!/bin/sh\ncat <<'JSON'\n"+noSR+"\nJSON\n")
	if !d.tickNative(d.cfg, false) {
		t.Fatal("native tick should run")
	}

	data := NewServer(d, "", zerolog.Nop()).handleStatus().Data.(ipc.StatusData)
	if data.AXTrusted == nil || !*data.AXTrusted {
		t.Errorf("ax_trusted = %v, want true", data.AXTrusted)
	}
	if data.ScreenRecording != nil {
		t.Errorf("screen_recording = %v, want unknown (absent)", *data.ScreenRecording)
	}
}
