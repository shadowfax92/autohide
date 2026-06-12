import AppKit

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var windowController: MainWindowController?
    private var controller: DaemonController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Light-only app: pin the appearance so the sable-style palette never
        // renders against dark window chrome.
        NSApp.appearance = NSAppearance(named: .aqua)
        NSApp.mainMenu = MainMenu.build()
        setDockIcon()

        let window = MainWindowController()
        windowController = window
        controller = DaemonController(model: window.model)
        controller?.start()
        window.show()
    }

    // The daemon owns all long-lived state; the window is the whole app here.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }

    /// Bare SPM executables get the generic Dock icon; borrow the bundle's
    /// icns when running from Contents/MacOS inside autohide.app.
    private func setDockIcon() {
        let executable = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath()
        let icns =
            executable
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("Resources/autohide.icns")
        if let image = NSImage(contentsOf: icns) {
            NSApp.applicationIconImage = image
        }
    }
}
