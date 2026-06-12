import AppKit
import SwiftUI

/// Hosts the SwiftUI shell inside an AppKit window, sable-style.
@MainActor
final class MainWindowController: NSWindowController {
    let model = MainWindowModel()

    init() {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 900, height: 640),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        window.title = "autohide"
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.contentMinSize = NSSize(width: 800, height: 540)
        window.center()

        super.init(window: window)

        window.contentView = NSHostingView(rootView: MainView().environmentObject(model))
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    func show() {
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
}
