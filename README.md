# 🫥 autohide

A macOS CLI that automatically hides app windows you're not using.

Switch to Chrome, and after 60 seconds Slack, Spotify, and everything else quietly disappear. Switch back and they're right where you left them. Your desktop stays clean without you thinking about it.

It works per **window**, not just per app: keep working in one Chrome window and the stale Chrome windows next to it minimize on the same timeout — even while the rest of Chrome stays put. Two tiers:

- **Apps** you stop using entirely are hidden whole (Cmd-Tab brings everything back at once).
- **Windows** you stop using inside an app you're still using are minimized to the Dock individually.

Anything you summon back — un-hide, un-minimize, switch a Space in — gets a fresh timeout before it's touched again.

Also ships with a **floating overlay timer** for focus sessions — a small always-on-top widget that counts down while you work.

## Install

Requires Go 1.24+ and Swift 5.9+ (for the overlay). No sudo needed (admin account — `/Applications` is admin-writable).

```bash
git clone https://github.com/your-user/mac-auto-hide.git
cd mac-auto-hide
make install
```

This builds everything, installs `/Applications/autohide.app` (menu-bar app + daemon + window helper), starts it via launchd, and symlinks the `autohide` CLI into `$(go env GOPATH)/bin`.

## Quick start

```bash
# make install already started the daemon (auto-starts on login).
# Re-create the launchd service later with:
autohide install

# That's it. Apps now auto-hide after 1 minute of inactivity.
```

Opening the app (Finder double-click or `open /Applications/autohide.app`) starts the 🫥 menu-bar daemon if one isn't running. If the menu-bar daemon is already up, opening the app is a no-op; a headless daemon gets replaced by the menu-bar one.

Every command auto-starts the daemon if it isn't running, so you can also just jump straight in:

```bash
autohide status
```

## Usage

### Auto-hiding

```bash
autohide status                # check daemon state (+ window-tracking mode)
autohide list                  # see tracked apps + time-to-hide + window counts
autohide list --windows        # expand per-window rows under each app
autohide pause                 # stop hiding (presenting, screen sharing)
autohide pause --duration 1h   # auto-resume after 1 hour
autohide resume                # resume hiding
```

### Per-app configuration

```bash
# Never hide Terminal
autohide config set-app Terminal disabled true

# Give Slack 5 minutes before hiding
autohide config set-app Slack timeout 5m

# Change the global default to 2 minutes
autohide config set default_timeout 2m

# Or just edit the file directly
autohide config edit
```

### Overlay timer

A floating countdown widget for focus sessions. Stays on top of all windows, visible on every desktop.

<p align="center">
  <img src="assets/overlay.png" alt="autohide overlay timer" width="360">
</p>

```bash
autohide overlay start "API docs" 45m    # start a 45-minute session
autohide overlay pause                    # pause the countdown
autohide overlay resume                   # resume
autohide overlay hide                     # hide widget, timer keeps running
autohide overlay show                     # bring it back
autohide overlay status                   # check remaining time
autohide overlay stop                     # end session, dismiss widget
```

When the timer hits 0:00, the overlay turns red and stays visible until you `stop` or start a new session.

## Configuration

Config lives at `~/.config/autohide/config.toml` and is created with defaults on first run.

```toml
[general]
default_timeout = "1m"       # hide apps / minimize windows after this long
check_interval = "5s"        # how often to check
system_exclude = ["Finder"]  # never hide these
window_tracking = true       # false = legacy app-level behavior only

[apps]
  [apps.Finder]
  disabled = true

  [apps.Slack]
  timeout = "5m"

  [apps.Terminal]
  disabled = true
```

Changes take effect within 5 seconds — the daemon hot-reloads the config file.

## How it works

```
autohide (CLI)  ── unix socket ──▶  autohide daemon (background)
                                         │
                                         ├── polls autohide-helper snapshot every 5s
                                         │     (apps + on-screen windows + focused window)
                                         ├── hides apps that exceed their timeout
                                         ├── minimizes stale windows of apps still in use
                                         └── manages overlay timer + spawns overlay widget
```

- **Native snapshot.** `autohide-helper` (Swift) reads windows via CGWindowList in milliseconds and addresses them by stable window ID — no per-window AppleScript loops, no title/index matching.
- **Graceful fallback.** Helper missing → the daemon runs the legacy osascript app-level path. Accessibility not granted → apps still hide, window minimizing waits for the grant. `autohide status` shows which mode you're in.
- **Per-app config governs both tiers.** An app's `timeout`/`disabled` applies to hiding it and to minimizing its windows.
- **Permissions:** **Accessibility** (System Settings > Privacy & Security) for the helper to read the focused window and minimize windows. Window *titles* in `list --windows` additionally need Screen Recording (optional, display-only). The legacy osascript path still uses Automation.
- **After reinstalling/rebuilding**, macOS may invalidate the Accessibility grant (ad-hoc code signature). If `autohide status` shows `app-only: accessibility not granted`, toggle the grant off and on again in System Settings.
- The daemon runs via `launchd` and restarts automatically.

## Daemon management

```bash
autohide install     # install launchd plist + start on login
autohide uninstall   # remove plist + stop daemon
autohide start       # start via launchd
autohide stop        # stop via launchd
autohide daemon      # run in foreground (for debugging)
```

A starting daemon takes over from any daemon already holding the IPC socket (it asks it to shut down over IPC) — so a stuck or manually-started daemon can't wedge the launchd service.

Logs: `~/.config/autohide/daemon.log`

## Project structure

```
mac-auto-hide/
├── Makefile                 # builds all three targets
├── autohide/                # Go CLI + daemon
│   ├── cmd/                 # cobra commands
│   ├── config/              # TOML config
│   ├── daemon/              # poll loop, two-tier tracker, overlay manager, IPC server
│   └── ipc/                 # unix socket protocol + client
├── autohide-helper/         # Swift window snapshot/minimize/hide helper
│   ├── Package.swift
│   └── Sources/
└── autohide-overlay/        # Swift floating timer widget
    ├── Package.swift
    └── Sources/
```

## License

MIT
