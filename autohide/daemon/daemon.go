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

	frontmost, windows, err := GetWindowSnapshot()
	if err != nil {
		d.logger.Warn().Err(err).Msg("failed to get window snapshot")
		return
	}

	d.mu.RLock()
	cfg := d.cfg
	focusMode := d.focusMode
	d.mu.RUnlock()

	if focusMode {
		// Focus mode: hide everything except frontmost immediately
		for _, window := range windows {
			if window.ID == frontmost.ID || window.Minimized {
				continue
			}
			_, disabled := cfg.EffectiveTimeout(window.AppName)
			if disabled {
				continue
			}
			d.logger.Debug().
				Str("app", window.AppName).
				Str("window", window.Title).
				Msg("focus mode: hiding window")
			if err := HideWindow(window); err != nil {
				d.logger.Warn().
					Err(err).
					Str("app", window.AppName).
					Str("window", window.Title).
					Msg("failed to hide window")
			}
		}
	} else {
		// Normal mode: timeout-based hiding
		toHide := d.tracker.UpdateWindows(cfg, frontmost, windows)
		for _, window := range toHide {
			d.logger.Info().
				Str("app", window.AppName).
				Str("window", window.Title).
				Msg("hiding inactive window")
			if err := HideWindow(window); err != nil {
				d.logger.Warn().
					Err(err).
					Str("app", window.AppName).
					Str("window", window.Title).
					Msg("failed to hide window")
			}
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
