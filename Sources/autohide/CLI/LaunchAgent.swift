// Manages the per-user LaunchAgent that keeps the daemon running. With
// KeepAlive{SuccessfulExit=false}, a crash restarts the daemon but a menu Quit
// (exit 0) stays quit until next login.

import Foundation

enum LaunchAgent {
    static let label = "com.autohide.daemon"

    static var plistURL: URL {          // ~/Library/LaunchAgents/com.autohide.daemon.plist
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/LaunchAgents/\(label).plist")
    }

    /// Writes the plist (RunAtLoad, KeepAlive{SuccessfulExit=false}, ProcessType Interactive,
    /// LimitLoadToSessionType Aqua, ThrottleInterval 5, stderr → <config dir>/launchd.log),
    /// then bootout (ignore errors) + bootstrap gui/$UID + kickstart.
    static func install(executable: URL) throws {
        preconditionFailure("unimplemented")
    }

    static func uninstall() throws {
        preconditionFailure("unimplemented")
    }

    static func restart() throws {      // launchctl kickstart -k gui/$UID/label
        preconditionFailure("unimplemented")
    }

    static func isLoaded() -> Bool {
        preconditionFailure("unimplemented")
    }
}
