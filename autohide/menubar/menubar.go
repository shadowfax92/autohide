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

func Run(d *daemon.Daemon) {
	dm = d
	app := menuet.App()
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
		statusText = fmt.Sprintf("Focus Mode  (%d apps tracked)", tracked)
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
		focusText = "Focus Mode: On"
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

	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	items = append(items, menuet.MenuItem{
		Text: "Quit",
		Clicked: func() {
			os.Exit(0)
		},
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
