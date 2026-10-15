# 🫥 autohide

Hides the macOS apps you are not using, so a wide desktop full of side-by-side
windows stays uncluttered.

## What it does

- An app hides (like Cmd-H) **60 s** after you last used it. Cmd-Tab or a Dock
  click brings it back with all its windows, and it starts a fresh 60 s.
- **The frontmost app is never hidden.** The app you were in also stays put
  while a menu-bar popover or Spotlight has focus.
- **Other Spaces are left alone.** Only apps with a window on the current Space
  count. When you switch to a Space, its apps get a fresh timeout.
- Nothing hides while the display sleeps, the screen is locked, autohide is
  paused, or you are on a fullscreen Space. When you come back, every app
  starts a fresh timeout, so nothing vanishes the moment you unlock.
- It hides whole apps only. macOS has no public API to hide a single window
  of another app.

It is one native process: the LaunchAgent daemon, the menu-bar item, and the
CLI are the same binary. It is event-driven and uses ~0% CPU when idle.

## Install

Requires Xcode 26 to build the app (its `actool` compiles the icon); the app
runs on macOS 13+. `make build` and `make test` need only Swift 5.9+.

```bash
make install
```

This builds and signs `/Applications/autohide.app`, symlinks the CLI to
`~/go/bin/autohide`, and installs the LaunchAgent `com.autohide.daemon`. The
agent starts at login, and launchd restarts it if it crashes. **Quit** in the
menu keeps it stopped until the next login or `autohide restart`. Opening
autohide.app from Finder also installs or starts it. `make install` replaces
the old Go version and stops its daemon and helper.

`make uninstall` removes the app and the agent and keeps `~/.config/autohide`.

## Use

Use the menu-bar icon (a window with two fainter windows receding behind it;
while paused, a pause mark replaces them) or the CLI:

```bash
autohide status                  # is it running, and with what settings
autohide list                    # every app, its state, and when it will hide
autohide timeout 2m              # change the 60 s default (menu: Timeout ▸)
autohide exempt add Slack        # never hide Slack (menu: Never hide "<app>")
autohide exempt remove Slack
autohide app-timeout Mail 10m    # per-app timeout; `default` removes it
autohide pause 30m               # or `pause` with no duration, until `resume`
autohide resume
autohide hide-others             # hide everything except the frontmost app now
autohide quit                    # stop it until next login or `autohide restart`
autohide config edit             # open config.json in $EDITOR
```

`status` and `list` take `--json`. Durations look like `45s`, `5m`, or
`1h30m`, and the minimum timeout is 5 s. App names match the name or the
bundle ID, ignoring case.

Settings live in `~/.config/autohide/config.json`. The CLI and menu edit it,
and the daemon picks up hand edits on its own:

```json
{
  "app_timeouts" : { "Mail" : "10m" },
  "exempt" : [ "Finder" ],
  "only_apps" : [ ],
  "timeout" : "1m"
}
```

The old `config.toml` is migrated once. Exemptions and per-app timeouts carry
over; the default timeout becomes 60 s.

**Logs** go to `~/.config/autohide/autohide.log`, with launchd's own output in
`launchd.log` next to it. Set `AUTOHIDE_DEBUG=1` for debug lines.

## Permissions

None. Hiding uses `NSRunningApplication.hide()`, and window visibility comes
from `CGWindowList` (owners and bounds only, never titles). Neither needs a
grant, so autohide never shows a permission prompt.

autohide does not use Accessibility, even if you grant it. An app that
ignores the hide is not forced: it shows as `refused` in `autohide list` and
is tried again after 5 minutes. No app has done so yet.

The bundle is signed with a stable identity (Developer ID, identifier
`com.autohide.daemon`). `make bundle` prints the designated requirement. It
should name a team (`certificate leaf[subject.OU] = "…"`), not a `cdhash`.
With no certificate in the keychain, the Makefile signs ad hoc and warns
loudly. To sign with another identity, pass `SIGN_ID="…"`.

## Test instance

`scripts/test-instance.sh` runs a second autohide beside the real one. It has
its own config directory, no menu-bar item, a 10 s timeout, and an allowlist
(`only_apps`) of throwaway apps: Calculator, TextEdit, Preview, and Font
Book, with Font Book exempt.

```bash
scripts/test-instance.sh start   # write the test config, start the daemon
scripts/test-instance.sh e2e     # open the apps, PASS/FAIL that the right ones hide
scripts/test-instance.sh pause-queue   # PASS/FAIL that a pause drops hides still queued
scripts/test-instance.sh list
scripts/test-instance.sh term    # SIGTERM only: checks exit 0 and that the socket is gone
scripts/test-instance.sh stop    # stop it and quit only the apps the script opened

# The same instance under a throwaway LaunchAgent (no inherited TCC grants,
# real KeepAlive). Label com.autohide.test unless AUTOHIDE_LAUNCHD_LABEL says
# otherwise; com.autohide.daemon is refused.
scripts/test-instance.sh agent-start
scripts/test-instance.sh agent-stop
```

`e2e` reads hides from the daemon log rather than polling `list`, because
every IPC request runs an evaluation and would mask late detection.
`AUTOHIDE_DEBUG=1` logs every event and evaluation.

Every CLI command takes `--config-dir DIR` to talk to a test instance instead.

## Develop

```bash
make build    # swift build -c release
make test     # unit tests for AutohideCore
make bundle   # build/autohide.app, signed and verified
```

`Sources/AutohideCore` is pure: config, IPC types, and every timer rule
(`Policy.swift`), all unit-tested. `Sources/autohide` holds the platform layer
(NSWorkspace events, window probe, hider), the runtime (engine, menu bar,
IPC server), and the CLI.

## Icon

The app icon's source is `assets/AppIcon.icon`, an Icon Composer document:
`icon.json` (background fill, layers, shadow) plus SVG layers in `Assets/` on a
1024×1024 canvas. The design ("Recede") shows the window in use crisp in front,
while the windows behind it step back into the dark corner, smaller and fainter.
Each shape is one SVG; its opacity, glass and dark-mode colour are set in
`icon.json`. Edit it in Icon Composer, which ships with Xcode 26, or by
hand. `make bundle` compiles it with `actool` into `Assets.car`, which macOS 26
draws with the system's glass and dark/tinted styles, plus an `AppIcon.icns`
for macOS 13–15. This needs Xcode 26: without it `make bundle` stops with an
error. `make install` re-registers the app so Finder and the Dock show the new
icon.

The menu-bar glyphs live in `assets/MenuBar.xcassets` as two 18×18 template
SVGs, `menubar-active` and `menubar-paused`, kept as vectors. Template means
macOS uses only their alpha and supplies the colour: the window in use is
opaque, the two behind it are 55% and 25% opaque, and the whole glyph follows
light, dark and tinted menu bars. The same `actool` call compiles them into
`Assets.car`. A binary run outside the
bundle (`.build/release/autohide`) has no `Assets.car` and falls back to the SF
Symbols `eye.slash` and `pause.circle`.
