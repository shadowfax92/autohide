import AppKit
import SwiftUI

class OverlayPanel: NSPanel {
    override var canBecomeKey: Bool { false }
    override var canBecomeMain: Bool { false }
}

func createOverlayWindow(reader: FocusStateReader) -> NSPanel {
    let content = OverlayContentView(reader: reader)
    let hostingView = NSHostingView(rootView: content)
    hostingView.frame = NSRect(x: 0, y: 0, width: 240, height: 45)

    let panel = OverlayPanel(
        contentRect: hostingView.frame,
        styleMask: [.borderless, .nonactivatingPanel],
        backing: .buffered,
        defer: false
    )

    panel.contentView = hostingView
    panel.isOpaque = false
    panel.backgroundColor = .clear
    panel.hasShadow = true
    panel.level = .floating
    panel.collectionBehavior = [.canJoinAllSpaces, .stationary, .fullScreenAuxiliary]
    panel.ignoresMouseEvents = false
    panel.isMovableByWindowBackground = true
    panel.hidesOnDeactivate = false

    positionTopRight(panel)

    return panel
}

private func positionTopRight(_ panel: NSPanel) {
    guard let screen = NSScreen.main else { return }
    let screenFrame = screen.visibleFrame
    let x = screenFrame.maxX - panel.frame.width - 16
    let y = screenFrame.maxY - panel.frame.height - 8
    panel.setFrameOrigin(NSPoint(x: x, y: y))
}

struct OverlayContentView: View {
    @ObservedObject var reader: FocusStateReader

    var body: some View {
        Group {
            if let info = reader.info {
                OverlayView(info: info)
            }
        }
    }
}
