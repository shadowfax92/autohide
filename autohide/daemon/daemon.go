package daemon

import (
	"context"
	"sync"
	"time"

	"autohide/config"

	"github.com/rs/zerolog"
)

const (
	maxHelperFails  = 3
	minimizeBackoff = 5 * time.Minute
)

type Daemon struct {
	cfgPath   string
	cfg       *config.Config
	tracker   *Tracker
	focus     *FocusManager
	logger    zerolog.Logger

	// helper state is touched only from the tick goroutine.
	helper      *Helper
	helperFails int

	mu           sync.RWMutex
	paused       bool
	resumeAt     *time.Time
	focusMode    bool
	startTime    time.Time
	windowStatus string
}

func New(cfg *config.Config, cfgPath string, logger zerolog.Logger) *Daemon {
	return &Daemon{
		cfgPath:      cfgPath,
		cfg:          cfg,
		tracker:      NewTracker(),
		focus:        NewFocusManager(config.Dir(), logger),
		logger:       logger,
		startTime:    time.Now(),
		windowStatus: "starting",
	}
}

// resolveWindowStatus maps a tick's inputs to the user-facing window-tracking
// mode label shown in `autohide status`.
func resolveWindowStatus(windowTracking, helperFound bool, helperFails int, axTrusted bool) string {
	switch {
	case !windowTracking:
		return "off"
	case !helperFound:
		return "legacy: helper not found"
	case helperFails >= maxHelperFails:
		return "legacy: helper failing"
	case !axTrusted:
		return "app-only: accessibility not granted"
	default:
		return "active"
	}
}

func (d *Daemon) setWindowStatus(status string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.windowStatus != status {
		d.logger.Info().Str("from", d.windowStatus).Str("to", status).Msg("window tracking mode")
		d.windowStatus = status
	}
}

func (d *Daemon) WindowTrackingStatus() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.windowStatus
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

	d.mu.RLock()
	cfg := d.cfg
	focusMode := d.focusMode
	d.mu.RUnlock()

	if d.tickNative(cfg, focusMode) {
		return
	}
	d.tickLegacy(cfg, focusMode)
}

// tickNative runs the helper-driven path (per-window tracking). A false
// return sends the caller to the legacy osascript path: window tracking off,
// helper missing, or helper persistently failing. Transient snapshot errors
// consume the tick instead so the two paths never both act in one tick.
func (d *Daemon) tickNative(cfg *config.Config, focusMode bool) bool {
	if !cfg.General.WindowTracking {
		d.setWindowStatus(resolveWindowStatus(false, true, 0, true))
		return false
	}
	if d.helper == nil {
		path, err := LocateHelper()
		if err != nil {
			d.setWindowStatus(resolveWindowStatus(true, false, 0, true))
			return false
		}
		d.helper = NewHelper(path)
		d.logger.Info().Str("path", path).Msg("autohide-helper found")
	}
	if d.helperFails >= maxHelperFails {
		d.setWindowStatus(resolveWindowStatus(true, true, d.helperFails, true))
		return false
	}

	snap, err := d.helper.Snapshot()
	if err != nil {
		d.helperFails++
		d.logger.Warn().Err(err).Int("consecutive", d.helperFails).Msg("window snapshot failed")
		if d.helperFails >= maxHelperFails {
			d.logger.Error().Msg("helper failing persistently; falling back to app-level autohide")
			d.setWindowStatus(resolveWindowStatus(true, true, d.helperFails, true))
		}
		return true
	}
	d.helperFails = 0
	d.setWindowStatus(resolveWindowStatus(true, true, 0, snap.AXTrusted))

	now := time.Now()
	if focusMode {
		// Focus mode: hide everything except frontmost immediately
		for _, app := range snap.Apps {
			if app.Hidden || app.Name == snap.Frontmost.Name {
				continue
			}
			if _, disabled := cfg.EffectiveTimeout(app.Name); disabled {
				continue
			}
			d.logger.Debug().Str("app", app.Name).Msg("focus mode: hiding app")
			if err := d.helper.Hide(app.Pid); err != nil {
				d.logger.Warn().Err(err).Str("app", app.Name).Msg("failed to hide app")
			}
		}
		return true
	}

	dec := d.tracker.Update(cfg, snap, now)
	for _, app := range dec.HideApps {
		d.logger.Info().Str("app", app.Name).Msg("hiding inactive app")
		if err := d.helper.Hide(app.Pid); err != nil {
			d.logger.Warn().Err(err).Str("app", app.Name).Msg("failed to hide app")
		}
	}
	for _, w := range dec.MinimizeWindows {
		d.logger.Info().Str("app", w.App).Uint32("window", w.ID).Str("title", w.Title).
			Msg("minimizing inactive window")
		if err := d.helper.Minimize(w.Pid, w.ID); err != nil {
			d.logger.Warn().Err(err).Str("app", w.App).Uint32("window", w.ID).
				Msg("minimize failed; backing off")
			d.tracker.DeferWindow(w.ID, now.Add(minimizeBackoff))
		}
	}
	return true
}

// tickLegacy is the pre-helper osascript path, byte-for-byte the old
// behavior: app-level tracking and System Events hides.
func (d *Daemon) tickLegacy(cfg *config.Config, focusMode bool) {
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

	if focusMode {
		// Focus mode: hide everything except frontmost immediately
		for _, name := range visible {
			if name == frontmost {
				continue
			}
			_, disabled := cfg.EffectiveTimeout(name)
			if disabled {
				continue
			}
			d.logger.Debug().Str("app", name).Msg("focus mode: hiding app")
			if err := HideApp(name); err != nil {
				d.logger.Warn().Err(err).Str("app", name).Msg("failed to hide app")
			}
		}
	} else {
		// Normal mode: timeout-based hiding
		toHide := d.tracker.UpdateLegacy(cfg, frontmost, visible, time.Now())
		for _, name := range toHide {
			d.logger.Info().Str("app", name).Msg("hiding inactive app")
			if err := HideApp(name); err != nil {
				d.logger.Warn().Err(err).Str("app", name).Msg("failed to hide app")
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
