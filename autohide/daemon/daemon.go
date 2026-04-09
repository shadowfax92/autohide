package daemon

import (
	"context"
	"sync"
	"time"

	"autohide/config"

	"github.com/rs/zerolog"
)

type Daemon struct {
	cfgPath string
	cfg     *config.Config
	tracker *Tracker
	focus   *FocusManager
	logger  zerolog.Logger

	mu        sync.RWMutex
	paused    bool
	resumeAt  *time.Time
	focusMode bool
	startTime time.Time
}

func New(cfg *config.Config, cfgPath string, logger zerolog.Logger) *Daemon {
	return &Daemon{
		cfgPath:   cfgPath,
		cfg:       cfg,
		tracker:   NewTracker(),
		focus:     NewFocusManager(config.Dir(), logger),
		logger:    logger,
		startTime: time.Now(),
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	d.focus.Cleanup()

	interval := d.cfg.General.CheckInterval.Duration
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	d.logger.Info().
		Dur("check_interval", interval).
		Dur("default_timeout", d.cfg.General.DefaultTimeout.Duration).
		Msg("daemon started")

	for {
		select {
		case <-ctx.Done():
			d.focus.Stop()
			d.logger.Info().Msg("daemon stopping")
			return nil
		case <-ticker.C:
			d.tick()
		}
	}
}

func (d *Daemon) tick() {
	d.mu.RLock()
	paused := d.paused
	resumeAt := d.resumeAt
	d.mu.RUnlock()

	if paused && resumeAt != nil && time.Now().After(*resumeAt) {
		d.mu.Lock()
		d.paused = false
		d.resumeAt = nil
		d.mu.Unlock()
		d.logger.Info().Msg("auto-resumed after pause duration")
		paused = false
	}

	if paused {
		return
	}

	// Hot-reload config
	if newCfg, err := config.Load(d.cfgPath); err == nil {
		d.mu.Lock()
		d.cfg = newCfg
		d.mu.Unlock()
	}

	frontmost, err := GetFrontmostApp()
	if err != nil {
		d.logger.Warn().Err(err).Msg("failed to get frontmost app")
		return
	}

	visible, err := GetVisibleApps()
	if err != nil {
		d.logger.Warn().Err(err).Msg("failed to get visible apps")
		return
	}

	d.mu.RLock()
	cfg := d.cfg
	focusMode := d.focusMode
	d.mu.RUnlock()

	// Both focus mode and normal mode use timeout-based hiding.
	// Focus mode only differs in that it tracks all visible apps (not just
	// previously-seen ones), so newly-opened apps also get auto-hidden
	// once the timeout expires.
	toHide := d.tracker.Update(cfg, frontmost, visible)
	for _, name := range toHide {
		if focusMode {
			d.logger.Debug().Str("app", name).Msg("focus mode: hiding app")
		} else {
			d.logger.Info().Str("app", name).Msg("hiding inactive app")
		}
		if err := HideApp(name); err != nil {
			d.logger.Warn().Err(err).Str("app", name).Msg("failed to hide app")
		}
	}
}

func (d *Daemon) Pause(duration time.Duration) *time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused = true
	if duration > 0 {
		t := time.Now().Add(duration)
		d.resumeAt = &t
		return d.resumeAt
	}
	d.resumeAt = nil
	return nil
}

func (d *Daemon) Resume() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused = false
	d.resumeAt = nil
}

func (d *Daemon) IsPaused() (bool, *time.Time) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.paused, d.resumeAt
}

func (d *Daemon) Uptime() time.Duration {
	return time.Since(d.startTime)
}

func (d *Daemon) Config() *config.Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg
}

func (d *Daemon) TrackerCount() int {
	return d.tracker.Count()
}

func (d *Daemon) TrackerList() []AppInfo {
	d.mu.RLock()
	cfg := d.cfg
	d.mu.RUnlock()
	return d.tracker.List(cfg)
}

func (d *Daemon) Overlay() *FocusManager {
	return d.focus
}

func (d *Daemon) SetFocusMode(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.focusMode = on
	if on {
		d.logger.Info().Msg("focus mode enabled")
	} else {
		d.logger.Info().Msg("focus mode disabled")
	}
}

func (d *Daemon) IsFocusMode() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.focusMode
}

func (d *Daemon) SetDefaultTimeout(dur time.Duration) error {
	d.mu.Lock()
	d.cfg.General.DefaultTimeout = config.Duration{Duration: dur}
	cfg := d.cfg
	cfgPath := d.cfgPath
	d.mu.Unlock()
	return config.Save(cfg, cfgPath)
}

func (d *Daemon) SetWorkspaceLabel(ws int, label string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return UpdateWorkspaceLabel(d.cfg, d.cfgPath, ws, label)
}

func (d *Daemon) CfgPath() string {
	return d.cfgPath
}
