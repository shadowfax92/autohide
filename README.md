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

Requires Swift 5.9+ (Xcode or the Command Line Tools) and macOS 13+.

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

Use the menu-bar icon (an eye with a slash through it) or the CLI:

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

None are needed. Hiding uses `NSRunningApplication.hide()`, and window
visibility comes from `CGWindowList` (owners and bounds only, never titles).
Neither needs a grant, so autohide never shows a permission prompt.

Accessibility is optional. If you grant it, autohide uses it as a fallback
for the rare app that refuses to hide. The bundle is signed with a stable
identity (Developer ID, identifier `com.autohide.daemon`), so a grant survives
rebuilds. `make bundle` prints the designated requirement. It should name a
team (`certificate leaf[subject.OU] = "…"`), not a `cdhash`. With no
certificate in the keychain, the Makefile signs ad hoc and warns loudly,
because each rebuild would then silently void the grant. To sign with another
identity, pass `SIGN_ID="…"`.

## Test instance

`scripts/test-instance.sh` runs a second autohide beside the real one. It has
its own config directory, no menu-bar item, a 10 s timeout, and an allowlist
(`only_apps`) of throwaway apps: TextEdit, Calculator, Chess, and Preview,
with Preview exempt.

```bash
scripts/test-instance.sh start   # write the test config, start the daemon
scripts/test-instance.sh e2e     # open the apps, PASS/FAIL that the right ones hide
scripts/test-instance.sh list
scripts/test-instance.sh term    # SIGTERM only: checks exit 0 and that the socket is gone
scripts/test-instance.sh stop    # stop it and quit only the apps e2e opened

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
