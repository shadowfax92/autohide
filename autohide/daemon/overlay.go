package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type FocusState struct {
	Task             string  `json:"task"`
	DurationSeconds  int     `json:"duration_seconds"`
	RemainingSeconds int     `json:"remaining_seconds"`
	Paused           bool    `json:"paused"`
	PulseInterval    float64 `json:"pulse_interval"`
	PulseDuration    float64 `json:"pulse_duration"`
	UpdatedAt        string  `json:"updated_at"`
}

type focusSession struct {
	task          string
	duration      time.Duration
	startedAt     time.Time
	pausedAt      *time.Time
	pausedDur     time.Duration
	pulseInterval float64
	pulseDuration float64
}

func (s *focusSession) remaining() time.Duration {
	elapsed := time.Since(s.startedAt) - s.pausedDur
	if s.pausedAt != nil {
		elapsed -= time.Since(*s.pausedAt)
	}
	r := s.duration - elapsed
	if r < 0 {
		return 0
	}
	return r
}

type FocusManager struct {
	mu            sync.Mutex
	session       *focusSession
	overlayCmd    *exec.Cmd
	overlayHidden bool
	statePath     string
	logger        zerolog.Logger
	syncDone      chan struct{}
}

func NewFocusManager(configDir string, logger zerolog.Logger) *FocusManager {
	return &FocusManager{
		statePath: filepath.Join(configDir, "focus.json"),
		logger:    logger,
	}
}

func (fm *FocusManager) Cleanup() {
	os.Remove(fm.statePath)
	exec.Command("pkill", "-x", "autohide-overlay").Run()
}

func (fm *FocusManager) Start(task string, duration time.Duration, pulseInterval, pulseDuration float64) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.session != nil {
		fm.stopLocked()
	}

	fm.session = &focusSession{
		task:          task,
		duration:      duration,
		startedAt:     time.Now(),
		pulseInterval: pulseInterval,
		pulseDuration: pulseDuration,
	}
	fm.overlayHidden = false

	fm.writeStateLocked()
	fm.spawnOverlayLocked()
	fm.startSyncLocked()

	fm.logger.Info().Str("task", task).Dur("duration", duration).Msg("focus session started")
	return nil
}

func (fm *FocusManager) Stop() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if fm.session == nil {
		return
	}
	fm.stopLocked()
	fm.logger.Info().Msg("focus session stopped")
}

func (fm *FocusManager) stopLocked() {
	fm.stopSyncLocked()
	fm.killOverlayLocked()
	os.Remove(fm.statePath)
	fm.session = nil
	fm.overlayHidden = false
}

func (fm *FocusManager) Pause() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.session == nil {
		return fmt.Errorf("no active focus session")
	}
	if fm.session.pausedAt != nil {
		return fmt.Errorf("already paused")
	}

	now := time.Now()
	fm.session.pausedAt = &now
	fm.writeStateLocked()
	fm.logger.Info().Msg("focus session paused")
	return nil
}

func (fm *FocusManager) Resume() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.session == nil {
		return fmt.Errorf("no active focus session")
	}
	if fm.session.pausedAt == nil {
		return fmt.Errorf("not paused")
	}

	fm.session.pausedDur += time.Since(*fm.session.pausedAt)
	fm.session.pausedAt = nil
	fm.writeStateLocked()
	fm.logger.Info().Msg("focus session resumed")
	return nil
}

func (fm *FocusManager) HideOverlay() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.session == nil {
		return fmt.Errorf("no active focus session")
	}

	fm.killOverlayLocked()
	fm.overlayHidden = true
	fm.logger.Info().Msg("overlay hidden")
	return nil
}

func (fm *FocusManager) ShowOverlay() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.session == nil {
		return fmt.Errorf("no active focus session")
	}

	fm.writeStateLocked()
	fm.spawnOverlayLocked()
	fm.overlayHidden = false
	fm.logger.Info().Msg("overlay shown")
	return nil
}

func (fm *FocusManager) Status() (*FocusState, bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.session == nil {
		return nil, false
	}
	return fm.buildStateLocked(), fm.overlayHidden
}

func (fm *FocusManager) buildStateLocked() *FocusState {
	r := fm.session.remaining()
	return &FocusState{
		Task:             fm.session.task,
		DurationSeconds:  int(fm.session.duration.Seconds()),
		RemainingSeconds: int(r.Seconds()),
		Paused:           fm.session.pausedAt != nil,
		PulseInterval:    fm.session.pulseInterval,
		PulseDuration:    fm.session.pulseDuration,
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
	}
}

func (fm *FocusManager) writeStateLocked() {
	state := fm.buildStateLocked()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		fm.logger.Warn().Err(err).Msg("failed to marshal focus state")
		return
	}
	if err := os.MkdirAll(filepath.Dir(fm.statePath), 0755); err != nil {
		fm.logger.Warn().Err(err).Msg("failed to create state directory")
		return
	}
	if err := os.WriteFile(fm.statePath, data, 0644); err != nil {
		fm.logger.Warn().Err(err).Msg("failed to write focus state")
	}
}

func (fm *FocusManager) findOverlayBin() string {
	// Look next to our own binary first (both installed via `make install`)
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "autohide-overlay")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Fall back to PATH
	if bin, err := exec.LookPath("autohide-overlay"); err == nil {
		return bin
	}
	return ""
}

func (fm *FocusManager) spawnOverlayLocked() {
	bin := fm.findOverlayBin()
	if bin == "" {
		fm.logger.Warn().Msg("autohide-overlay not found, skipping overlay")
		return
	}
	cmd := exec.Command(bin)
	if err := cmd.Start(); err != nil {
		fm.logger.Warn().Err(err).Str("bin", bin).Msg("failed to spawn overlay")
		return
	}
	fm.logger.Info().Str("bin", bin).Int("pid", cmd.Process.Pid).Msg("overlay spawned")
	fm.overlayCmd = cmd
	go cmd.Wait()
}

func (fm *FocusManager) killOverlayLocked() {
	if fm.overlayCmd != nil && fm.overlayCmd.Process != nil {
		fm.overlayCmd.Process.Kill()
		fm.overlayCmd = nil
	}
}

func (fm *FocusManager) startSyncLocked() {
	fm.syncDone = make(chan struct{})
	go fm.syncLoop(fm.syncDone)
}

func (fm *FocusManager) stopSyncLocked() {
	if fm.syncDone != nil {
		close(fm.syncDone)
		fm.syncDone = nil
	}
}

func (fm *FocusManager) syncLoop(done chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			fm.mu.Lock()
			if fm.session != nil {
				fm.writeStateLocked()
			}
			fm.mu.Unlock()
		}
	}
}
