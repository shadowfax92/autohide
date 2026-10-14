// Daemon.swift — process entry for `autohide daemon`.
// Wires the pieces together and parks the main thread in the AppKit run loop,
// where every event, timer, and IPC request is then handled.

import AppKit
import AutohideCore

/// flock(paths.lockFile) or exit 0 ("already running"); Log.configure; ConfigStore.loadOrCreate;
/// NSApplication.shared.setActivationPolicy(.accessory); Engine.start; IPCServer.start;
/// MenuBar.install when menubar; SIGTERM/SIGINT → clean stop; NSApp.run().
func runDaemon(paths: ConfigPaths, menubar: Bool) -> Never {
    preconditionFailure("unimplemented")
}
