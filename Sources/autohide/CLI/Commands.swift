// Argument parsing and dispatch for every `autohide` subcommand. Runs in the
// short-lived CLI process, except `daemon`, which hands the process to runDaemon.
// Live state goes through IPCClient; config edits write config.json directly,
// then send `reload`, so they work even when the daemon is down.

import Foundation
import AutohideCore

enum CLI {
    /// Global flag: --config-dir DIR (sets AUTOHIDE_CONFIG_DIR semantics for this call).
    /// Commands:
    ///   daemon [--no-menubar]           run the agent (launchd uses this)
    ///   status [--json]                 daemon state; "not running" + exit 1 when down
    ///   list [--json]                   per-app state and seconds until hide
    ///   pause [DURATION] | resume
    ///   hide-others
    ///   timeout [DURATION]              show / set default timeout (edits config, sends reload)
    ///   exempt [add|remove APP]         list / change exemptions
    ///   app-timeout APP DURATION|default
    ///   only [APP...] | only --clear    test allowlist
    ///   config [path|show|edit]
    ///   install | uninstall | restart   LaunchAgent management
    ///   version
    /// No args: when stdin is not a TTY (Finder launch) → ensure the LaunchAgent
    /// is installed/loaded and exit 0; when a TTY → status.
    static func run(_ args: [String]) -> Int32 {
        FileHandle.standardError.write(Data("autohide: not implemented yet\n".utf8))
        return 1
    }
}
