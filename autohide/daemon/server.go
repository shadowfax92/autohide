package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"autohide/config"
	"autohide/ipc"

	"github.com/rs/zerolog"
)

var ErrAlreadyRunning = errors.New("daemon already running")

type Server struct {
	daemon       *Daemon
	sockPath     string
	logger       zerolog.Logger
	listener     net.Listener
	boundInfo    os.FileInfo
	onShutdown   func()
	shutdownOnce sync.Once
}

func NewServer(d *Daemon, sockPath string, logger zerolog.Logger) *Server {
	return &Server{
		daemon:   d,
		sockPath: sockPath,
		logger:   logger,
	}
}

// SetOnShutdown registers the callback fired when an IPC "shutdown" request
// arrives. Must be called before Start.
func (s *Server) SetOnShutdown(f func()) {
	s.onShutdown = f
}

func (s *Server) Start() error {
	if _, err := os.Stat(s.sockPath); err == nil {
		conn, err := net.DialTimeout("unix", s.sockPath, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return fmt.Errorf("%w (socket %s is active)", ErrAlreadyRunning, s.sockPath)
		}
		os.Remove(s.sockPath)
	}

	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.sockPath, err)
	}
	// Stop unlinks explicitly (and only when it still owns the path) —
	// auto-unlink on Close would race a takeover poller that has already
	// bound a fresh socket at this path.
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	s.boundInfo, _ = os.Stat(s.sockPath)
	s.listener = ln

	go s.accept()
	s.logger.Info().Str("socket", s.sockPath).Msg("IPC server listening")
	return nil
}

// StartTakeover binds like Start, but a live socket holder is asked to shut
// down over IPC and the bind is retried until timeout. Lets a (re)starting
// daemon displace headless ensureDaemon spawns instead of KeepAlive-thrashing.
func (s *Server) StartTakeover(timeout time.Duration) error {
	err := s.Start()
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		return err
	}

	resp, serr := ipc.NewClient(s.sockPath).Send(ipc.Request{Command: "shutdown"})
	if serr == nil && !resp.OK {
		return fmt.Errorf("takeover: holder refused shutdown: %s", resp.Error)
	}
	if serr != nil {
		// Holder vanished between probe and send — the bind retry below settles it.
		s.logger.Info().Err(serr).Msg("socket holder gone mid-takeover, retrying bind")
	} else {
		s.logger.Info().Str("socket", s.sockPath).Msg("asked running daemon to shut down")
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		err := s.Start()
		if err == nil {
			s.logger.Info().Msg("took over socket from previous daemon")
			return nil
		}
		if !errors.Is(err, ErrAlreadyRunning) {
			return err
		}
	}
	return fmt.Errorf("takeover of %s timed out after %s", s.sockPath, timeout)
}

func (s *Server) Stop() {
	if s.listener == nil {
		return
	}
	// Remove only the socket file this server bound — by the time a displaced
	// daemon stops, a takeover poller may already own the path.
	if cur, err := os.Stat(s.sockPath); err == nil && s.boundInfo != nil && os.SameFile(cur, s.boundInfo) {
		os.Remove(s.sockPath)
	}
	s.listener.Close()
}

func (s *Server) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}

	var req ipc.Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		s.writeResponse(conn, ipc.Response{Error: "invalid request"})
		return
	}

	var resp ipc.Response
	switch req.Command {
	case "shutdown":
		// Reply before firing the hook — the hook typically exits the process.
		s.logger.Info().Msg("shutdown requested via IPC")
		s.writeResponse(conn, ipc.Response{OK: true})
		if s.onShutdown != nil {
			go s.shutdownOnce.Do(s.onShutdown)
		}
		return
	case "status":
		resp = s.handleStatus()
	case "pause":
		resp = s.handlePause(req)
	case "resume":
		resp = s.handleResume()
	case "list":
		resp = s.handleList(req)
	case "ax_prompt":
		resp = s.handleAXPrompt()
	case "set_timeout":
		resp = s.handleSetTimeout(req)
	case "hide_all":
		resp = s.handleHideAll()
	case "focus_on":
		s.daemon.SetFocusMode(true)
		resp = ipc.Response{OK: true, Data: s.focusModeData(true)}
	case "focus_off":
		s.daemon.SetFocusMode(false)
		resp = ipc.Response{OK: true, Data: s.focusModeData(false)}
	case "focus_status":
		resp = ipc.Response{OK: true, Data: s.focusModeData(s.daemon.IsFocusMode())}
	default:
		resp = ipc.Response{Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}

	s.writeResponse(conn, resp)
}

func (s *Server) handleStatus() ipc.Response {
	paused, resumeAt := s.daemon.IsPaused()
	axTrusted, screenRecording := s.daemon.Permissions()
	cfg := s.daemon.Config()
	presets := cfg.Menubar.TimeoutPresets
	if len(presets) == 0 {
		presets = config.Default().Menubar.TimeoutPresets
	}
	presetLabels := make([]string, 0, len(presets))
	for _, p := range presets {
		presetLabels = append(presetLabels, config.FormatDuration(p.Duration))
	}
	data := ipc.StatusData{
		Running:         true,
		Paused:          paused,
		FocusMode:       s.daemon.IsFocusMode(),
		FocusKeepRecent: cfg.Focus.KeepRecent,
		Uptime:          s.daemon.Uptime().Round(time.Second).String(),
		TrackedCount:    s.daemon.TrackerCount(),
		WindowTracking:  s.daemon.WindowTrackingStatus(),
		AXTrusted:       axTrusted,
		ScreenRecording: screenRecording,
		DefaultTimeout:  config.FormatDuration(cfg.General.DefaultTimeout.Duration),
		TimeoutPresets:  presetLabels,
	}
	if resumeAt != nil {
		data.ResumeAt = resumeAt.Format(time.RFC3339)
	}
	return ipc.Response{OK: true, Data: data}
}

func (s *Server) focusModeData(active bool) ipc.FocusModeData {
	cfg := s.daemon.Config()
	return ipc.FocusModeData{
		Active:     active,
		KeepRecent: cfg.Focus.KeepRecent,
		Grace:      config.FormatDuration(cfg.Focus.Grace.Duration),
		KeepSet:    s.daemon.tracker.RecentApps(cfg.Focus.KeepRecent),
	}
}

func (s *Server) handlePause(req ipc.Request) ipc.Response {
	var dur time.Duration
	if d, ok := req.Args["duration"]; ok && d != "" {
		var err error
		dur, err = time.ParseDuration(d)
		if err != nil {
			return ipc.Response{Error: fmt.Sprintf("invalid duration: %s", d)}
		}
	}
	resumeAt := s.daemon.Pause(dur)
	data := ipc.PauseData{Paused: true}
	if resumeAt != nil {
		data.ResumeAt = resumeAt.Format(time.RFC3339)
	}
	s.logger.Info().Dur("duration", dur).Msg("daemon paused")
	return ipc.Response{OK: true, Data: data}
}

func (s *Server) handleResume() ipc.Response {
	s.daemon.Resume()
	s.logger.Info().Msg("daemon resumed")
	return ipc.Response{OK: true, Data: ipc.PauseData{Paused: false}}
}

func (s *Server) handleAXPrompt() ipc.Response {
	trusted, err := s.daemon.PromptAccessibility()
	if err != nil {
		return ipc.Response{Error: err.Error()}
	}
	s.logger.Info().Bool("ax_trusted", trusted).Msg("accessibility prompt requested")
	return ipc.Response{OK: true, Data: ipc.AXPromptData{AXTrusted: trusted}}
}

func (s *Server) handleSetTimeout(req ipc.Request) ipc.Response {
	raw := req.Args["duration"]
	if raw == "" {
		return ipc.Response{Error: "missing duration"}
	}
	dur, err := time.ParseDuration(raw)
	if err != nil || dur <= 0 {
		return ipc.Response{Error: fmt.Sprintf("invalid duration: %s", raw)}
	}
	if err := s.daemon.SetDefaultTimeout(dur); err != nil {
		return ipc.Response{Error: fmt.Sprintf("saving config: %s", err)}
	}
	s.logger.Info().Dur("timeout", dur).Msg("default timeout updated")
	return ipc.Response{OK: true}
}

func (s *Server) handleHideAll() ipc.Response {
	data, err := s.daemon.HideAll()
	if err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true, Data: data}
}

func (s *Server) handleList(req ipc.Request) ipc.Response {
	withWindows := req.Args["windows"] == "true"
	tracked := s.daemon.TrackerList()
	now := time.Now()

	apps := make([]ipc.AppInfo, 0, len(tracked))
	for _, a := range tracked {
		remaining := a.Timeout - now.Sub(a.LastActive)
		if remaining < 0 || a.Hidden || a.Disabled {
			remaining = 0
		}
		info := ipc.AppInfo{
			Name:          a.Name,
			LastActive:    a.LastActive.Format(time.RFC3339),
			Timeout:       a.Timeout.String(),
			Hidden:        a.Hidden,
			TimeRemaining: remaining.Round(time.Second).String(),
			Disabled:      a.Disabled,
			WindowCount:   len(a.Windows),
		}
		if withWindows {
			for _, w := range a.Windows {
				info.Windows = append(info.Windows, ipc.WindowInfo{
					ID:         w.ID,
					Title:      w.Title,
					LastActive: w.LastActive.Format(time.RFC3339),
				})
			}
		}
		apps = append(apps, info)
	}

	return ipc.Response{OK: true, Data: ipc.ListData{Apps: apps}}
}

func (s *Server) writeResponse(conn net.Conn, resp ipc.Response) {
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	conn.Write(data)
}
