package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"autohide/ipc"

	"github.com/rs/zerolog"
)

type Server struct {
	daemon   *Daemon
	sockPath string
	logger   zerolog.Logger
	listener net.Listener
}

func NewServer(d *Daemon, sockPath string, logger zerolog.Logger) *Server {
	return &Server{
		daemon:   d,
		sockPath: sockPath,
		logger:   logger,
	}
}

func (s *Server) Start() error {
	if _, err := os.Stat(s.sockPath); err == nil {
		conn, err := net.DialTimeout("unix", s.sockPath, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return fmt.Errorf("daemon already running (socket %s is active)", s.sockPath)
		}
		os.Remove(s.sockPath)
	}

	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.sockPath, err)
	}
	s.listener = ln

	go s.accept()
	s.logger.Info().Str("socket", s.sockPath).Msg("IPC server listening")
	return nil
}

func (s *Server) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(s.sockPath)
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
	case "status":
		resp = s.handleStatus()
	case "pause":
		resp = s.handlePause(req)
	case "resume":
		resp = s.handleResume()
	case "list":
		resp = s.handleList()
	case "overlay_start":
		resp = s.handleFocusStart(req)
	case "overlay_stop":
		resp = s.handleFocusStop()
	case "overlay_pause":
		resp = s.handleFocusPause()
	case "overlay_resume":
		resp = s.handleFocusResume()
	case "overlay_status":
		resp = s.handleFocusStatus()
	case "overlay_hide":
		resp = s.handleFocusHide()
	case "overlay_show":
		resp = s.handleFocusShow()
	case "workspace_set_label":
		resp = s.handleWorkspaceSetLabel(req)
	case "focus_on":
		s.daemon.SetFocusMode(true)
		resp = ipc.Response{OK: true, Data: ipc.FocusModeData{Active: true}}
	case "focus_off":
		s.daemon.SetFocusMode(false)
		resp = ipc.Response{OK: true, Data: ipc.FocusModeData{Active: false}}
	case "focus_status":
		resp = ipc.Response{OK: true, Data: ipc.FocusModeData{Active: s.daemon.IsFocusMode()}}
	default:
		resp = ipc.Response{Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}

	s.writeResponse(conn, resp)
}

func (s *Server) handleStatus() ipc.Response {
	paused, resumeAt := s.daemon.IsPaused()
	data := ipc.StatusData{
		Running:      true,
		Paused:       paused,
		FocusMode:    s.daemon.IsFocusMode(),
		Uptime:       s.daemon.Uptime().Round(time.Second).String(),
		TrackedCount: s.daemon.TrackerCount(),
	}
	if resumeAt != nil {
		data.ResumeAt = resumeAt.Format(time.RFC3339)
	}
	return ipc.Response{OK: true, Data: data}
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

func (s *Server) handleList() ipc.Response {
	tracked := s.daemon.TrackerList()
	now := time.Now()

	apps := make([]ipc.AppInfo, 0, len(tracked))
	for _, a := range tracked {
		remaining := a.Timeout - now.Sub(a.LastActive)
		if remaining < 0 || a.Hidden || a.Disabled {
			remaining = 0
		}
		apps = append(apps, ipc.AppInfo{
			Name:          a.Name,
			LastActive:    a.LastActive.Format(time.RFC3339),
			Timeout:       a.Timeout.String(),
			Hidden:        a.Hidden,
			TimeRemaining: remaining.Round(time.Second).String(),
			Disabled:      a.Disabled,
		})
	}

	return ipc.Response{OK: true, Data: ipc.ListData{Apps: apps}}
}

func (s *Server) handleFocusStart(req ipc.Request) ipc.Response {
	task := req.Args["task"]
	if task == "" {
		return ipc.Response{Error: "task name required"}
	}
	durStr := req.Args["duration"]
	if durStr == "" {
		return ipc.Response{Error: "duration required"}
	}
	dur, err := time.ParseDuration(durStr)
	if err != nil {
		return ipc.Response{Error: fmt.Sprintf("invalid duration: %s", durStr)}
	}

	pulseInterval := 60.0
	if v := req.Args["pulse_interval"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			pulseInterval = f
		}
	}
	pulseDuration := 1.5
	if v := req.Args["pulse_duration"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			pulseDuration = f
		}
	}

	if err := s.daemon.Overlay().Start(task, dur, pulseInterval, pulseDuration); err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true, Data: s.focusStatusData()}
}

func (s *Server) handleFocusStop() ipc.Response {
	s.daemon.Overlay().Stop()
	return ipc.Response{OK: true, Data: ipc.OverlayStatusData{Active: false}}
}

func (s *Server) handleFocusPause() ipc.Response {
	if err := s.daemon.Overlay().Pause(); err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true, Data: s.focusStatusData()}
}

func (s *Server) handleFocusResume() ipc.Response {
	if err := s.daemon.Overlay().Resume(); err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true, Data: s.focusStatusData()}
}

func (s *Server) handleFocusStatus() ipc.Response {
	return ipc.Response{OK: true, Data: s.focusStatusData()}
}

func (s *Server) handleFocusHide() ipc.Response {
	if err := s.daemon.Overlay().HideOverlay(); err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true, Data: s.focusStatusData()}
}

func (s *Server) handleFocusShow() ipc.Response {
	if err := s.daemon.Overlay().ShowOverlay(); err != nil {
		return ipc.Response{Error: err.Error()}
	}
	return ipc.Response{OK: true, Data: s.focusStatusData()}
}

func (s *Server) handleWorkspaceSetLabel(req ipc.Request) ipc.Response {
	rawWorkspace := req.Args["workspace"]
	if rawWorkspace == "" {
		return ipc.Response{Error: "workspace required"}
	}

	workspace, err := strconv.Atoi(rawWorkspace)
	if err != nil || workspace < 1 {
		return ipc.Response{Error: fmt.Sprintf("invalid workspace: %s", rawWorkspace)}
	}

	label := NormalizeWorkspaceLabel(req.Args["label"])
	if err := s.daemon.SetWorkspaceLabel(workspace, label); err != nil {
		return ipc.Response{Error: err.Error()}
	}

	return ipc.Response{
		OK: true,
		Data: ipc.WorkspaceLabelData{
			Workspace: workspace,
			Label:     label,
		},
	}
}

func (s *Server) focusStatusData() ipc.OverlayStatusData {
	state, overlayHidden := s.daemon.Overlay().Status()
	if state == nil {
		return ipc.OverlayStatusData{Active: false}
	}
	remaining := time.Duration(state.RemainingSeconds) * time.Second
	return ipc.OverlayStatusData{
		Active:        true,
		Task:          state.Task,
		Duration:      (time.Duration(state.DurationSeconds) * time.Second).String(),
		Remaining:     remaining.String(),
		Paused:        state.Paused,
		OverlayHidden: overlayHidden,
	}
}

func (s *Server) writeResponse(conn net.Conn, resp ipc.Response) {
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	conn.Write(data)
}
