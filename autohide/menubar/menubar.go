package menubar

import (
	"fmt"
	"os"
	"sort"
	"time"

	"autohide/config"
	"autohide/daemon"

	"github.com/caseymrm/menuet"
)

var dm *daemon.Daemon

func Run(d *daemon.Daemon) {
	dm = d
	app := menuet.App()
	app.SetMenuState(&menuet.MenuState{Title: menuTitle()})
	app.Children = menuItems
	app.Label = "com.autohide.daemon"
	go titleUpdater()
	app.RunApplication()
}

func menuTitle() string {
	// Determine state emoji
	emoji := "🫥"
	paused, _ := dm.IsPaused()
	if paused {
		emoji = "⏸"
	} else if dm.IsFocusMode() {
		emoji = "🎯"
	}

	// Append workspace label if one is configured for the current space
	cfg := dm.Config()
	if ws, err := daemon.GetCurrentWorkspaceNumber(); err == nil {
		if label := cfg.WorkspaceLabel(ws); label != "" {
			return emoji + " " + label
		}
	}
	return emoji
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

	// Workspace labels submenu
	items = append(items, menuet.MenuItem{
		Text:     workspaceSubmenuTitle(cfg),
		Children: func() []menuet.MenuItem { return workspaceItems(cfg) },
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
	return fmt.Sprintf("Timeout: %s", formatDuration(cfg.General.DefaultTimeout.Duration))
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
			Text:  formatDuration(dur),
			State: dur == current,
			Clicked: func() {
				dm.SetDefaultTimeout(dur)
				menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
			},
		})
	}
	return items
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return d.String()
}

func workspaceSubmenuTitle(cfg *config.Config) string {
	currentWs, err := daemon.GetCurrentWorkspaceNumber()
	if err != nil {
		return "Workspaces"
	}
	label := cfg.WorkspaceLabel(currentWs)
	if label != "" {
		return fmt.Sprintf("Workspace %d: %s", currentWs, label)
	}
	return fmt.Sprintf("Workspace %d", currentWs)
}

func workspaceItems(cfg *config.Config) []menuet.MenuItem {
	currentWs, _ := daemon.GetCurrentWorkspaceNumber()
	wsMap := cfg.WorkspaceMap()

	var items []menuet.MenuItem

	// Show current workspace at top
	items = append(items, menuet.MenuItem{
		Text: fmt.Sprintf("Current: Workspace %d", currentWs),
	})
	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	// List all configured workspace labels sorted by number
	if len(wsMap) == 0 {
		items = append(items, menuet.MenuItem{Text: "No labels configured"})
		items = append(items, menuet.MenuItem{Text: "Use: autohide workspace set <num> <label>"})
		return items
	}

	nums := make([]int, 0, len(wsMap))
	for n := range wsMap {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	for _, n := range nums {
		text := fmt.Sprintf("%d: %s", n, wsMap[n])
		items = append(items, menuet.MenuItem{
			Text:  text,
			State: n == currentWs,
		})
	}

	return items
}
