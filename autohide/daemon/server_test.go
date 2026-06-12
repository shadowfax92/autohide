package daemon

import (
	"testing"

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
