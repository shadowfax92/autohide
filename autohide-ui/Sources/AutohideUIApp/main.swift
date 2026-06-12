import AppKit

// Single instance: the window is a transient control panel, so a relaunch
// should surface the existing one instead of stacking windows.
let currentExecutable = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath()
let twin = NSWorkspace.shared.runningApplications.first {
    $0.processIdentifier != ProcessInfo.processInfo.processIdentifier
        && $0.executableURL?.resolvingSymlinksInPath() == currentExecutable
}
if let twin {
    twin.activate(options: [])
    exit(0)
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
// Regular policy gives the bare SPM executable a Dock presence and key window.
app.setActivationPolicy(.regular)
app.run()
