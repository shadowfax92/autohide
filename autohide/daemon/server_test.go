package daemon

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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
		if w.TimeRemaining == "" || w.LastActive == "" {
			t.Errorf("window %d missing remaining/last-active: %+v", w.ID, w)
		}
	}
	if titles[1] != "Docs" || titles[2] != "" {
		t.Errorf("unexpected titles %+v", titles)
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
