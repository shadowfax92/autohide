# 🫥 autohide

A macOS CLI that automatically hides apps you're not using.

Switch to Chrome, and after 60 seconds Slack, Spotify, and everything else quietly disappear. Switch back and they're right where you left them. Your desktop stays clean without you thinking about it.

Apps you stop using are hidden whole, with the same semantics as Cmd-H — Cmd-Tab brings the app and all of its windows back at once. Anything you summon back — un-hide, switch a Space in — gets a fresh timeout before it's hidden again.

autohide never minimizes windows. macOS has no public API for hiding one window of another app while leaving its other windows visible; the alternatives are minimizing or unsupported private WindowServer APIs that require weakening SIP. Windows are tracked only for activity timers and list display. The hide action is always app-level.

## Install

Requires Go 1.24+ and Swift 5.9+ (for the native helper). No sudo needed (admin account — `/Applications` is admin-writable).

```bash
git clone https://github.com/your-user/mac-auto-hide.git
cd mac-auto-hide
make install
```

This builds everything, installs `/Applications/autohide.app` (menu-bar app + daemon + native helper), starts it via launchd, and symlinks the `autohide` CLI into `$(go env GOPATH)/bin`.

## Quick start

```bash
# make install already started the daemon (auto-starts on login).
# Re-create the launchd service later with:
autohide install

# That's it. Apps now auto-hide after 1 minute of inactivity.
```

Opening the app (Finder double-click or `open /Applications/autohide.app`) starts the 🫥 menu-bar daemon if one isn't running **and opens the autohide window** — grant Accessibility from there and you're done. If the menu-bar daemon is already up, opening the app is a no-op (use the menu-bar's "Open autohide…" or `autohide ui` instead); a headless daemon gets replaced by the menu-bar one.

Every command auto-starts the daemon if it isn't running, so you can also just jump straight in:

```bash
autohide status
```

## Usage

### The window

A clean light-theme control panel for everything below — live status, the
tracked-apps list with window activity detail, pause/focus/timeout controls,
and one-click **Accessibility** granting (it fires the system prompt and
deep-links System Settings).

```bash
autohide ui      # or: menu bar 🫥 → "Open autohide…"
```

### Auto-hiding

```bash
autohide status                # check daemon state (+ window-tracking mode)
autohide list                  # see tracked apps, hide status/reasons, and window counts
autohide list --windows        # expand per-window rows under each app
autohide hide all              # immediately hide all eligible background apps
autohide pause                 # stop hiding (presenting, screen sharing)
autohide pause --duration 1h   # auto-resume after 1 hour
autohide resume                # resume hiding
```

### Focus mode

Focus mode keeps your recent working set visible and hides everything else
more aggressively. By default, the three most recently used apps stay visible;
other eligible apps hide after 10 seconds of inactivity.

```bash
autohide focus on
autohide focus status          # show keep count, grace, and current keep-set
autohide focus off

# Tune the working-set size or grace (changes hot-reload)
autohide config set focus.keep_recent 3
autohide config set focus.grace 30s

# Grace-only behavior: protect just the frontmost app for 30 seconds
autohide config set focus.keep_recent 1
autohide config set focus.grace 30s
```

Disabled apps remain exempt. `autohide hide all` stays a separate immediate
one-shot action and does not use the focus keep-set or grace.

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

## Configuration

Config lives at `~/.config/autohide/config.toml` and is created with defaults on first run.

```toml
[general]
default_timeout = "1m"       # hide apps after this long
check_interval = "5s"        # how often to check
system_exclude = ["Finder"]  # never hide these
window_tracking = true       # track window activity/list detail; hiding stays app-level

[focus]
keep_recent = 3              # frontmost + two next-most-recent apps stay visible
grace = "10s"                # delay before hiding apps outside that working set

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
autohide (CLI)      ── unix socket ──▶  autohide daemon (background)
autohide-ui (window) ── unix socket ──▶       │
                                         ├── polls autohide-helper snapshot every 5s
                                         │     (apps + on-screen windows + focused window)
                                         └── hides apps that exceed their timeout
```

- **App-level hiding.** Every hide is equivalent to Cmd-H: all windows belonging to the app hide and restore together. Individual windows are never minimized, masked, or moved.
- **Native snapshot.** `autohide-helper` (Swift) reads apps and on-screen windows via CGWindowList in milliseconds. Window observations drive activity timers and `autohide list`; they are not independently hidden.
- **Fullscreen handling.** Apps whose visible windows are all fullscreen or in Split View are shown as `unhidable: fullscreen` and skipped until they leave that Space.
- **Graceful fallback.** Helper missing → the daemon runs the legacy osascript app-level path. Accessibility not granted → apps still hide through AppKit, while focused-window and Split View detection are limited. `autohide status` shows which mode you're in.
- **Per-app config.** An app's `timeout`/`disabled` governs whether and when it's hidden.
- **Focus mode.** The tracker keeps a most-recently-used app set visible and applies the shorter focus grace to other eligible apps.
- **Permissions:** **Accessibility** (System Settings > Privacy & Security) enables the synchronous app-hide path plus focused/fullscreen window detection — grant it from the window (`autohide ui` → Settings) or System Settings. Hiding still falls back to AppKit without it. Window *titles* in `list --windows` additionally need Screen Recording (optional, display-only). The legacy osascript path still uses Automation.
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
│   ├── daemon/              # poll loop, app tracker, IPC server
│   └── ipc/                 # unix socket protocol + client
├── autohide-helper/         # Swift app snapshot + hide helper
│   ├── Package.swift
│   └── Sources/
└── autohide-ui/             # Swift status/permissions window (SwiftUI)
    ├── Package.swift
    ├── Sources/
    └── Tests/
```

## License

MIT
