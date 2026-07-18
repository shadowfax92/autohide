package menubar

import (
	"fmt"
	"os"
	"time"

	"autohide/config"
	"autohide/daemon"

	"github.com/caseymrm/menuet"
)

// BundleID is the app bundle / launchd label identity shared by the menu bar
// app, the .app Info.plist, and the launchd plist.
const BundleID = "com.autohide.daemon"

var dm *daemon.Daemon

// Run owns the startup OS thread and links AppKit termination to daemon shutdown.
func Run(d *daemon.Daemon, shutdown func()) {
	dm = d
	app := menuet.App()
	wg, shutdownCtx := app.GracefulShutdownHandles()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-shutdownCtx.Done()
		shutdown()
	}()
	app.SetMenuState(&menuet.MenuState{Title: menuTitle()})
	app.Children = menuItems
	app.Label = BundleID
	go titleUpdater()
	app.RunApplication()
}

func menuTitle() string {
	paused, _ := dm.IsPaused()
	if paused {
		return "⏸"
	}
	if dm.IsFocusMode() {
		return "🎯"
	}
	return "🫥"
}

func titleUpdater() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
	}
}

func menuItems() []menuet.MenuItem {
	paused, resumeAt := dm.IsPaused()
	focusMode := dm.IsFocusMode()
	cfg := dm.Config()
	tracked := dm.TrackerCount()

	var items []menuet.MenuItem

	items = append(items, menuet.MenuItem{
		Text: "Open autohide…",
		Clicked: func() {
			// stderr lands in daemon.log under launchd.
			if err := daemon.SpawnUI(); err != nil {
				fmt.Fprintf(os.Stderr, "open autohide window: %v\n", err)
			}
		},
	})
	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	statusText := fmt.Sprintf("Active  (%d apps tracked)", tracked)
	if paused {
		statusText = "Paused"
		if resumeAt != nil {
			remaining := time.Until(*resumeAt).Round(time.Second)
			if remaining > 0 {
				statusText = fmt.Sprintf("Paused  (resumes in %s)", remaining)
			}
		}
	} else if focusMode {
		statusText = fmt.Sprintf("Focus Mode  (top %d)", cfg.Focus.KeepRecent)
	}
	items = append(items, menuet.MenuItem{Text: statusText})
	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	if paused {
		items = append(items, menuet.MenuItem{
			Text: "Resume Autohide",
			Clicked: func() {
				dm.Resume()
				menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
			},
		})
	} else {
		items = append(items, menuet.MenuItem{
			Text: "Pause Autohide",
			Clicked: func() {
				dm.Pause(0)
				menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
			},
		})
	}

	focusText := "Focus Mode: Off"
	if focusMode {
		focusText = fmt.Sprintf("Focus Mode: On (top %d)", cfg.Focus.KeepRecent)
	}
	items = append(items, menuet.MenuItem{
		Text:  focusText,
		State: focusMode,
		Clicked: func() {
			dm.SetFocusMode(!focusMode)
			menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
		},
	})

	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	items = append(items, menuet.MenuItem{
		Text:     timeoutSubmenuTitle(cfg),
		Children: func() []menuet.MenuItem { return timeoutItems(cfg) },
	})

	return items
}

func timeoutSubmenuTitle(cfg *config.Config) string {
	return fmt.Sprintf("Timeout: %s", config.FormatDuration(cfg.General.DefaultTimeout.Duration))
}

func timeoutItems(cfg *config.Config) []menuet.MenuItem {
	current := cfg.General.DefaultTimeout.Duration
	presets := cfg.Menubar.TimeoutPresets
	if len(presets) == 0 {
		presets = config.Default().Menubar.TimeoutPresets
	}

	var items []menuet.MenuItem
	for _, p := range presets {
		dur := p.Duration
		items = append(items, menuet.MenuItem{
			Text:  config.FormatDuration(dur),
			State: dur == current,
			Clicked: func() {
				dm.SetDefaultTimeout(dur)
				menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
			},
		})
	}
	return items
}
