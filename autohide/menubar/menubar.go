package menubar

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"time"

	"autohide/config"
	"autohide/daemon"

	"github.com/caseymrm/menuet"
)

// workspaceEmojis is the pool of emojis randomly assigned to workspaces.
var workspaceEmojis = []string{
	// nature & elements
	"🔥", "⚡", "🌊", "🌿", "🌸", "🍀", "🌙", "☀️", "❄️", "🌈",
	"🌋", "🍃", "🌻", "🌴", "🍄", "🪵", "🌵", "🌾", "🪷", "🌲",
	"🌺", "🌼", "🦋", "🐝", "🐙", "🦊", "🐺", "🦉", "🦎", "🐢",
	// objects & tools
	"💎", "🎵", "🚀", "💡", "🎨", "📦", "🔧", "📡", "🧪", "🔬",
	"🎯", "🎲", "🎸", "🎹", "📸", "🔮", "💫", "🧲", "⚙️", "🛠️",
	"🧰", "🧠", "⌘", "🖥️", "💻", "📱", "🎛️", "📍", "🗂️", "📝",
	// food & drink
	"🍕", "🍊", "🍋", "🍇", "🍉", "🫐", "🥑", "🌶️", "☕", "🧁",
	"🥐", "🍜", "🍣", "🥥", "🫚", "🍎", "🥨", "🧃", "🍵", "🧋",
	// symbols & misc
	"⭐", "✨", "🏔️", "🏝️", "🎪", "🏗️", "🗿", "🧊", "🪐", "🛸",
	"⚓", "🎭", "🧬", "🎠", "🛡️", "🪄", "🏄", "🧭", "🪩", "🫧",
	"🔆", "🕹️", "🎈", "🪁", "🏁", "🏛️", "🎬", "📚", "🃏", "🎟️",
}

// emojiAssignments caches which emoji was assigned to each workspace number.
var (
	emojiMu          sync.Mutex
	emojiAssignments = map[int]string{}
)

// workspaceEmoji returns a stable random emoji for a given workspace number.
func workspaceEmoji(ws int) string {
	emojiMu.Lock()
	defer emojiMu.Unlock()

	if e, ok := emojiAssignments[ws]; ok {
		return e
	}
	// Pick a deterministic-ish random emoji seeded by workspace number
	idx := ws % len(workspaceEmojis)
	emojiAssignments[ws] = workspaceEmojis[idx]
	return workspaceEmojis[idx]
}

// randomEmoji picks a truly random emoji from the pool.
func randomEmoji() string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(workspaceEmojis))))
	if err != nil {
		return "🔵"
	}
	return workspaceEmojis[n.Int64()]
}

var dm *daemon.Daemon

func Run(d *daemon.Daemon) {
	dm = d
	app := menuet.App()
	if err := registerHotkeys(); err != nil {
		fmt.Fprintf(os.Stderr, "hotkeys unavailable: %v\n", err)
	}
	app.SetMenuState(&menuet.MenuState{Title: menuTitle()})
	app.Children = menuItems
	app.Label = "com.autohide.daemon"
	go titleUpdater()
	app.RunApplication()
}

func menuTitle() string {
	// Determine state emoji
	stateEmoji := "🫥"
	paused, _ := dm.IsPaused()
	if paused {
		stateEmoji = "⏸"
	} else if dm.IsFocusMode() {
		stateEmoji = "🎯"
	}

	// Append workspace emoji + label for the current space
	cfg := dm.Config()
	ws, err := daemon.GetCurrentWorkspaceNumber()
	if err != nil {
		return stateEmoji
	}

	wsEmoji := workspaceEmoji(ws)
	label := cfg.WorkspaceLabel(ws)
	if label != "" {
		return stateEmoji + " " + wsEmoji + " " + label
	}
	// No label — just show emoji + workspace number
	return stateEmoji + " " + wsEmoji + strconv.Itoa(ws)
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
		Text: "Restart Daemon",
		Clicked: func() {
			go restartDaemonFromMenu()
		},
	})

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
		return fmt.Sprintf("%s %d: %s", workspaceEmoji(currentWs), currentWs, label)
	}
	return fmt.Sprintf("%s Workspace %d", workspaceEmoji(currentWs), currentWs)
}

func workspaceItems(cfg *config.Config) []menuet.MenuItem {
	currentWs, _ := daemon.GetCurrentWorkspaceNumber()
	wsMap := cfg.WorkspaceMap()

	var items []menuet.MenuItem

	// Current workspace header
	currentLabel := cfg.WorkspaceLabel(currentWs)
	if currentLabel != "" {
		items = append(items, menuet.MenuItem{
			Text: fmt.Sprintf("%s %d: %s", workspaceEmoji(currentWs), currentWs, currentLabel),
		})
	} else {
		items = append(items, menuet.MenuItem{
			Text: fmt.Sprintf("%s Workspace %d (no label)", workspaceEmoji(currentWs), currentWs),
		})
	}
	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	// Rename action for current workspace
	items = append(items, menuet.MenuItem{
		Text: "Quick Switch...",
		Clicked: func() {
			go openWorkspaceSwitcher()
		},
	})

	items = append(items, menuet.MenuItem{
		Text: "Set Label for This Workspace...",
		Clicked: func() {
			go promptWorkspaceLabel(currentWs, currentLabel)
		},
	})

	// Clear label for current workspace (only if one exists)
	if currentLabel != "" {
		items = append(items, menuet.MenuItem{
			Text: "Clear Label",
			Clicked: func() {
				dm.SetWorkspaceLabel(currentWs, "")
				menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
			},
		})
	}

	// Randomize emoji for current workspace
	items = append(items, menuet.MenuItem{
		Text: "Shuffle Emoji",
		Clicked: func() {
			emojiMu.Lock()
			emojiAssignments[currentWs] = randomEmoji()
			emojiMu.Unlock()
			menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
		},
	})

	items = append(items, menuet.MenuItem{Type: menuet.Separator})

	// List all configured workspace labels sorted by number
	if len(wsMap) > 0 {
		nums := make([]int, 0, len(wsMap))
		for n := range wsMap {
			nums = append(nums, n)
		}
		sort.Ints(nums)

		for _, n := range nums {
			text := fmt.Sprintf("%s %d: %s", workspaceEmoji(n), n, wsMap[n])
			isCurrent := n == currentWs
			wsNum := n
			items = append(items, menuet.MenuItem{
				Text:  text,
				State: isCurrent,
				Clicked: func() {
					if err := daemon.SwitchToWorkspace(wsNum); err == nil {
						menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
					}
				},
			})
		}
	}

	return items
}

// promptWorkspaceLabel shows a native dialog to set/rename a workspace label.
func promptWorkspaceLabel(ws int, currentLabel string) {
	placeholder := "e.g. Building API"
	if currentLabel != "" {
		placeholder = currentLabel
	}

	response := menuet.App().Alert(menuet.Alert{
		MessageText:     fmt.Sprintf("Label for Workspace %d", ws),
		InformativeText: "Enter a short name for this workspace. It will show in the menu bar.",
		Inputs:          []string{placeholder},
		Buttons:         []string{"Save", "Cancel"},
	})

	// User clicked Save and provided input
	if response.Button == 0 && len(response.Inputs) > 0 {
		label := daemon.NormalizeWorkspaceLabel(response.Inputs[0])
		if label == "" {
			return
		}
		dm.SetWorkspaceLabel(ws, label)
		menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
	}
}

func openWorkspaceSwitcher() {
	err := daemon.PickWorkspaceAndSwitch(dm.Config())
	if err != nil && !errors.Is(err, daemon.ErrWorkspacePickerCanceled) {
		fmt.Fprintf(os.Stderr, "workspace switcher: %v\n", err)
	}
	menuet.App().SetMenuState(&menuet.MenuState{Title: menuTitle()})
}

func restartDaemonFromMenu() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "restart daemon: %v\n", err)
		return
	}
	cmd := exec.Command(exe, "restart")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "restart daemon: %v\n", err)
		return
	}
	go cmd.Wait()
}
