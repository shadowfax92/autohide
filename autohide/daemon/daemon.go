package daemon

import (
	"context"
	"sync"
	"time"

	"autohide/config"
	"autohide/ipc"

	"github.com/rs/zerolog"
)

const (
	maxHelperFails      = 3
	minimizeBackoff     = 5 * time.Minute
	helperRetryCooldown = 5 * time.Minute
)

type Daemon struct {
	cfgPath string
	cfg     *config.Config
	tracker *Tracker
	logger  zerolog.Logger

	actionMu      sync.Mutex
	helper        *Helper
	helperFails   int
	helperRetryAt time.Time
	nativeActive  bool

	mu           sync.RWMutex
	paused       bool
	resumeAt     *time.Time
	focusMode    bool
	startTime    time.Time
	windowStatus string
	// nil until a helper snapshot (or ax_prompt) reports them.
	axTrusted       *bool
	screenRecording *bool

	getFrontmostApp func() (string, error)
	getVisibleApps  func() ([]string, error)
	hideApp         func(string) error
}

func New(cfg *config.Config, cfgPath string, logger zerolog.Logger) *Daemon {
	return &Daemon{
		cfgPath:         cfgPath,
		cfg:             cfg,
		tracker:         NewTracker(),
		logger:          logger,
		startTime:       time.Now(),
		windowStatus:    "starting",
		getFrontmostApp: GetFrontmostApp,
		getVisibleApps:  GetVisibleApps,
		hideApp:         HideApp,
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

func (d *Daemon) setAXTrusted(trusted bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.axTrusted = &trusted
}

func (d *Daemon) setScreenRecording(granted bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.screenRecording = &granted
}

// PromptAccessibility fires the system Accessibility grant dialog from the
// daemon's own process tree so the TCC prompt registers this app's identity
// (a UI-process prompt would register the wrong one). Locates the helper
// per call instead of touching d.helper, which is tick-goroutine-only.
func (d *Daemon) PromptAccessibility() (bool, error) {
	path, err := LocateHelper()
	if err != nil {
		return false, err
	}
	result, err := NewHelper(path).Check(true)
	if err != nil {
		return false, err
	}
	d.applyCheck(result)
	return result.AXTrusted, nil
}

// seedPermissions probes the helper once so permission state is known even
// when the native tick never runs (window_tracking off); no prompt fires.
func (d *Daemon) seedPermissions(h *Helper) {
	result, err := h.Check(false)
	if err != nil {
		d.logger.Debug().Err(err).Msg("permission seed probe failed")
		return
	}
	d.applyCheck(result)
}

func (d *Daemon) applyCheck(result *CheckResult) {
	d.setAXTrusted(result.AXTrusted)
	if result.ScreenRecording != nil {
		d.setScreenRecording(*result.ScreenRecording)
	}
}

// Permissions returns the last-observed accessibility / screen-recording
// state; nil means no helper snapshot has reported yet. Returns copies so
// callers can't race the cache.
func (d *Daemon) Permissions() (axTrusted, screenRecording *bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.axTrusted != nil {
		v := *d.axTrusted
		axTrusted = &v
	}
	if d.screenRecording != nil {
		v := *d.screenRecording
		screenRecording = &v
	}
	return axTrusted, screenRecording
}

func (d *Daemon) Run(ctx context.Context) error {
	interval := d.cfg.General.CheckInterval.Duration
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	d.logger.Info().
		Dur("check_interval", interval).
		Dur("default_timeout", d.cfg.General.DefaultTimeout.Duration).
		Msg("daemon started")

	if path, err := LocateHelper(); err == nil {
		d.seedPermissions(NewHelper(path))
	}

	for {
		select {
		case <-ctx.Done():
			d.logger.Info().Msg("daemon stopping")
			return nil
		case <-ticker.C:
			d.tick()
		}
	}
}

func (d *Daemon) tick() {
	d.actionMu.Lock()
	defer d.actionMu.Unlock()

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

	d.reloadConfig()

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
		d.exitNative()
		return false
	}
	if d.helper == nil {
		path, err := LocateHelper()
		if err != nil {
			d.setWindowStatus(resolveWindowStatus(true, false, 0, true))
			d.exitNative()
			return false
		}
		d.helper = NewHelper(path)
		d.logger.Info().Str("path", path).Msg("autohide-helper found")
	}
	// Persistent failures fall back to legacy, but probe again after a
	// cooldown — a wake-from-sleep stall must not cost window tracking
	// until the next daemon restart.
	if d.helperFails >= maxHelperFails && time.Now().Before(d.helperRetryAt) {
		d.setWindowStatus(resolveWindowStatus(true, true, d.helperFails, true))
		d.exitNative()
		return false
	}

	snap, err := d.helper.Snapshot()
	if err != nil {
		d.helperFails++
		d.logger.Warn().Err(err).Int("consecutive", d.helperFails).Msg("window snapshot failed")
		if d.helperFails >= maxHelperFails {
			d.logger.Error().Msg("helper failing persistently; falling back to app-level autohide")
			d.helperRetryAt = time.Now().Add(helperRetryCooldown)
			d.setWindowStatus(resolveWindowStatus(true, true, d.helperFails, true))
			d.exitNative()
			return false
		}
		return true
	}
	d.helperFails = 0
	d.setWindowStatus(resolveWindowStatus(true, true, 0, snap.AXTrusted))
	d.applyCheck(&CheckResult{AXTrusted: snap.AXTrusted, ScreenRecording: snap.ScreenRecording})
	if !d.nativeActive {
		d.nativeActive = true
		d.tracker.ResetWindows()
	}

	now := time.Now()
	if focusMode {
		// Window timers can't be maintained while focus mode owns the
		// screen; drop them so leaving focus mode re-leases instead of
		// minimize-bursting.
		d.tracker.ResetWindows()
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

func (d *Daemon) reloadConfig() {
	if newCfg, err := config.Load(d.cfgPath); err == nil {
		d.mu.Lock()
		d.cfg = newCfg
		d.mu.Unlock()
	}
}

// HideAll immediately hides eligible background apps without changing focus mode.
func (d *Daemon) HideAll() (ipc.HideAllData, error) {
	d.actionMu.Lock()
	defer d.actionMu.Unlock()

	d.reloadConfig()
	d.mu.RLock()
	cfg := d.cfg
	d.mu.RUnlock()

	if data, ok := d.hideAllNative(cfg); ok {
		return data, nil
	}
	return d.hideAllLegacy(cfg)
}

func (d *Daemon) hideAllNative(cfg *config.Config) (ipc.HideAllData, bool) {
	if !cfg.General.WindowTracking {
		d.setWindowStatus(resolveWindowStatus(false, true, 0, true))
		d.exitNative()
		return ipc.HideAllData{}, false
	}
	if d.helper == nil {
		path, err := LocateHelper()
		if err != nil {
			d.setWindowStatus(resolveWindowStatus(true, false, 0, true))
			d.exitNative()
			return ipc.HideAllData{}, false
		}
		d.helper = NewHelper(path)
		d.logger.Info().Str("path", path).Msg("autohide-helper found")
	}
	if d.helperFails >= maxHelperFails && time.Now().Before(d.helperRetryAt) {
		d.setWindowStatus(resolveWindowStatus(true, true, d.helperFails, true))
		d.exitNative()
		return ipc.HideAllData{}, false
	}

	snap, err := d.helper.Snapshot()
	if err != nil {
		d.helperFails++
		d.logger.Warn().Err(err).Int("consecutive", d.helperFails).Msg("window snapshot failed")
		if d.helperFails >= maxHelperFails {
			d.logger.Error().Msg("helper failing persistently; falling back to app-level autohide")
			d.helperRetryAt = time.Now().Add(helperRetryCooldown)
			d.setWindowStatus(resolveWindowStatus(true, true, d.helperFails, true))
			d.exitNative()
		}
		return ipc.HideAllData{}, false
	}
	d.helperFails = 0
	d.setWindowStatus(resolveWindowStatus(true, true, 0, snap.AXTrusted))
	if !d.nativeActive {
		d.nativeActive = true
		d.tracker.ResetWindows()
	}

	var data ipc.HideAllData
	for _, app := range snap.Apps {
		if app.Hidden || app.Name == snap.Frontmost.Name {
			continue
		}
		if _, disabled := cfg.EffectiveTimeout(app.Name); disabled {
			continue
		}
		d.logger.Debug().Str("app", app.Name).Msg("hide all: hiding app")
		if err := d.helper.Hide(app.Pid); err != nil {
			data.Failed++
			d.logger.Warn().Err(err).Str("app", app.Name).Msg("failed to hide app")
			continue
		}
		data.Hidden++
	}
	return data, true
}

func (d *Daemon) hideAllLegacy(cfg *config.Config) (ipc.HideAllData, error) {
	var data ipc.HideAllData
	frontmost, err := d.getFrontmostApp()
	if err != nil {
		return data, err
	}

	visible, err := d.getVisibleApps()
	if err != nil {
		return data, err
	}

	for _, name := range visible {
		if name == frontmost {
			continue
		}
		_, disabled := cfg.EffectiveTimeout(name)
		if disabled {
			continue
		}
		d.logger.Debug().Str("app", name).Msg("hide all: hiding app")
		if err := d.hideApp(name); err != nil {
			data.Failed++
			d.logger.Warn().Err(err).Str("app", name).Msg("failed to hide app")
			continue
		}
		data.Hidden++
	}

	return data, nil
}

// exitNative drops per-window state when leaving the helper path so list
// data and timers can't rot unobserved; re-entry re-leases via ResetWindows
// + the appearance rule.
func (d *Daemon) exitNative() {
	if d.nativeActive {
		d.nativeActive = false
		d.tracker.ResetWindows()
	}
}

// tickLegacy is the pre-helper osascript path with the old app-level
// semantics: System Events polling and whole-app hides only.
func (d *Daemon) tickLegacy(cfg *config.Config, focusMode bool) {
	frontmost, err := d.getFrontmostApp()
	if err != nil {
		d.logger.Warn().Err(err).Msg("failed to get frontmost app")
		return
	}

	visible, err := d.getVisibleApps()
	if err != nil {
		d.logger.Warn().Err(err).Msg("failed to get visible apps")
		return
	}

	if focusMode {
		for _, name := range visible {
			if name == frontmost {
				continue
			}
			_, disabled := cfg.EffectiveTimeout(name)
			if disabled {
				continue
			}
			d.logger.Debug().Str("app", name).Msg("focus mode: hiding app")
			if err := d.hideApp(name); err != nil {
				d.logger.Warn().Err(err).Str("app", name).Msg("failed to hide app")
			}
		}
	} else {
		toHide := d.tracker.UpdateLegacy(cfg, frontmost, visible, time.Now())
		for _, name := range toHide {
			d.logger.Info().Str("app", name).Msg("hiding inactive app")
			if err := d.hideApp(name); err != nil {
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

// SetDefaultTimeout is copy-on-write: Config() hands the current pointer to
// unlocked readers (status handler, menubar), so the struct must never be
// mutated in place — clone, modify, swap, like the hot-reload path. The
// shallow copy shares the Apps map, which nothing mutates in place.
func (d *Daemon) SetDefaultTimeout(dur time.Duration) error {
	d.mu.Lock()
	cfg := *d.cfg
	cfg.General.DefaultTimeout = config.Duration{Duration: dur}
	d.cfg = &cfg
	cfgPath := d.cfgPath
	d.mu.Unlock()
	return config.Save(&cfg, cfgPath)
}
