import AppKit
import ApplicationServices

// CGWindowID for an AX window element. Private but stable for 10+ years
// (AltTab/yabai depend on it); resolveAXWindow falls back to frame/title
// matching if it ever fails.
@_silgen_name("_AXUIElementGetWindow")
func _AXUIElementGetWindow(_ element: AXUIElement, _ identifier: inout CGWindowID) -> AXError

struct SnapApp: Encodable {
    let pid: Int32
    let name: String
    let hidden: Bool
}

struct SnapWindow: Encodable {
    let id: UInt32
    let pid: Int32
    let app: String
    let title: String
}

struct Frontmost: Encodable {
    let pid: Int32
    let name: String
}

struct SnapshotPayload: Encodable {
    let axTrusted: Bool
    let screenRecording: Bool
    let frontmost: Frontmost
    let focusedWindowId: UInt32
    let apps: [SnapApp]
    let windows: [SnapWindow]

    enum CodingKeys: String, CodingKey {
        case axTrusted = "ax_trusted"
        case screenRecording = "screen_recording"
        case frontmost
        case focusedWindowId = "focused_window_id"
        case apps
        case windows
    }
}

private let minWindowDimension: CGFloat = 50

func regularApps() -> [NSRunningApplication] {
    NSWorkspace.shared.runningApplications.filter {
        $0.activationPolicy == .regular && !$0.isTerminated
    }
}

func makeSnapshotJSON() -> String {
    let running = regularApps()
    let namesByPid = Dictionary(
        running.map { ($0.processIdentifier, $0.localizedName ?? "") },
        uniquingKeysWith: { first, _ in first }
    )

    let apps = running.map {
        SnapApp(pid: $0.processIdentifier, name: $0.localizedName ?? "", hidden: $0.isHidden)
    }

    var windows: [SnapWindow] = []
    let options: CGWindowListOption = [.optionOnScreenOnly, .excludeDesktopElements]
    if let infos = CGWindowListCopyWindowInfo(options, kCGNullWindowID) as? [[String: Any]] {
        for info in infos {
            guard let layer = info[kCGWindowLayer as String] as? Int, layer == 0,
                  let id = info[kCGWindowNumber as String] as? UInt32,
                  let pid = info[kCGWindowOwnerPID as String] as? Int32,
                  let appName = namesByPid[pid]
            else { continue }
            if let alpha = info[kCGWindowAlpha as String] as? Double, alpha <= 0 { continue }
            if let bounds = info[kCGWindowBounds as String] as? [String: CGFloat],
               let w = bounds["Width"], let h = bounds["Height"],
               w < minWindowDimension || h < minWindowDimension { continue }
            // Title requires Screen Recording permission; display-only, so
            // empty is fine.
            let title = info[kCGWindowName as String] as? String ?? ""
            windows.append(SnapWindow(id: id, pid: pid, app: appName, title: title))
        }
    }

    let front = NSWorkspace.shared.frontmostApplication
    let frontmost = Frontmost(
        pid: front?.processIdentifier ?? 0,
        name: front.flatMap { namesByPid[$0.processIdentifier] } ?? ""
    )

    let trusted = AXIsProcessTrusted()
    var focusedID: CGWindowID = 0
    if trusted, let front, front.activationPolicy == .regular {
        focusedID = focusedWindowID(pid: front.processIdentifier)
    }

    let payload = SnapshotPayload(
        axTrusted: trusted,
        screenRecording: CGPreflightScreenCaptureAccess(),
        frontmost: frontmost,
        focusedWindowId: focusedID,
        apps: apps,
        windows: windows
    )
    return encodeJSON(payload)
}

private func focusedWindowID(pid: pid_t) -> CGWindowID {
    let appEl = AXUIElementCreateApplication(pid)
    var focused: CFTypeRef?
    guard AXUIElementCopyAttributeValue(appEl, kAXFocusedWindowAttribute as CFString, &focused) == .success,
          let focused, CFGetTypeID(focused) == AXUIElementGetTypeID()
    else { return 0 }
    var id: CGWindowID = 0
    guard _AXUIElementGetWindow(focused as! AXUIElement, &id) == .success else { return 0 }
    return id
}

func checkJSON(prompt: Bool) -> String {
    let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: prompt] as CFDictionary
    let trusted = AXIsProcessTrustedWithOptions(options)
    return "{\"ax_trusted\": \(trusted)}"
}

func encodeJSON<T: Encodable>(_ value: T) -> String {
    let encoder = JSONEncoder()
    guard let data = try? encoder.encode(value) else {
        fail("encode snapshot JSON")
    }
    return String(decoding: data, as: UTF8.self)
}
