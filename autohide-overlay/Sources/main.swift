import AppKit

class AppDelegate: NSObject, NSApplicationDelegate {
    var panel: NSPanel?
    var reader: FocusStateReader?

    func applicationDidFinishLaunching(_ notification: Notification) {
        let statePath = CommandLine.arguments.count > 1
            ? CommandLine.arguments[1]
            : NSString("~/.config/autohide/focus.json").expandingTildeInPath

        reader = FocusStateReader(path: statePath)
        panel = createOverlayWindow(reader: reader!)
        panel?.orderFront(nil)
    }
}

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let delegate = AppDelegate()
app.delegate = delegate
app.run()
