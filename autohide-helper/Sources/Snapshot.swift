import AppKit
import ApplicationServices

// AX does not publicly expose the CGWindowID needed to join fullscreen state
// to the CGWindowList entry already used by the tracker.
@_silgen_name("_AXUIElementGetWindow")
func _AXUIElementGetWindow(_ element: AXUIElement, _ identifier: inout CGWindowID) -> AXError

struct SnapApp: Encodable {
    let pid: Int32
    let name: String
    let hidden: Bool
    let unhidable: String
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
private let fullscreenTolerance: CGFloat = 2
private let axFullScreenAttribute = "AXFullScreen" as CFString

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

    var windows: [SnapWindow] = []
    var windowIDsByPid: [pid_t: [CGWindowID]] = [:]
    var fullscreenWindowIDs = Set<CGWindowID>()
    let screens = activeDisplayBounds()
    let options: CGWindowListOption = [.optionOnScreenOnly, .excludeDesktopElements]
    if let infos = CGWindowListCopyWindowInfo(options, kCGNullWindowID) as? [[String: Any]] {
        for info in infos {
            guard let layer = info[kCGWindowLayer as String] as? Int, layer == 0,
                  let id = info[kCGWindowNumber as String] as? UInt32,
                  let pid = info[kCGWindowOwnerPID as String] as? Int32,
                  let appName = namesByPid[pid]
            else { continue }
            if let alpha = info[kCGWindowAlpha as String] as? Double, alpha <= 0 { continue }
            guard let values = info[kCGWindowBounds as String] as? [String: CGFloat],
                  let x = values["X"], let y = values["Y"],
                  let width = values["Width"], let height = values["Height"]
            else { continue }
            if width < minWindowDimension || height < minWindowDimension { continue }
            let bounds = CGRect(x: x, y: y, width: width, height: height)
            if screens.contains(where: { approximatelyEqual(bounds, $0) }) {
                fullscreenWindowIDs.insert(id)
            }
            // Title requires Screen Recording permission; display-only, so
            // empty is fine.
            let title = info[kCGWindowName as String] as? String ?? ""
            windows.append(SnapWindow(id: id, pid: pid, app: appName, title: title))
            windowIDsByPid[pid, default: []].append(id)
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
    if trusted {
        fullscreenWindowIDs.formUnion(fullscreenAXWindowIDs(running: running))
    }

    let apps = running.map { app in
        let ids = windowIDsByPid[app.processIdentifier] ?? []
        let unhidable = !ids.isEmpty && ids.allSatisfy(fullscreenWindowIDs.contains)
            ? "fullscreen"
            : ""
        return SnapApp(
            pid: app.processIdentifier,
            name: app.localizedName ?? "",
            hidden: app.isHidden,
            unhidable: unhidable
        )
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

private func activeDisplayBounds() -> [CGRect] {
    var count: UInt32 = 0
    guard CGGetActiveDisplayList(0, nil, &count) == .success, count > 0 else { return [] }
    var displays = [CGDirectDisplayID](repeating: 0, count: Int(count))
    guard CGGetActiveDisplayList(count, &displays, &count) == .success else { return [] }
    return displays.prefix(Int(count)).map(CGDisplayBounds)
}

private func approximatelyEqual(_ lhs: CGRect, _ rhs: CGRect) -> Bool {
    abs(lhs.minX - rhs.minX) <= fullscreenTolerance
        && abs(lhs.minY - rhs.minY) <= fullscreenTolerance
        && abs(lhs.width - rhs.width) <= fullscreenTolerance
        && abs(lhs.height - rhs.height) <= fullscreenTolerance
}

/// Returns CG window IDs whose AX elements report fullscreen, including Split View members.
private func fullscreenAXWindowIDs(running: [NSRunningApplication]) -> Set<CGWindowID> {
    var ids = Set<CGWindowID>()
    for app in running {
        let appElement = AXUIElementCreateApplication(app.processIdentifier)
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(
            appElement,
            kAXWindowsAttribute as CFString,
            &value
        ) == .success, let windows = value as? [AXUIElement] else { continue }

        for window in windows {
            var fullscreenValue: CFTypeRef?
            guard AXUIElementCopyAttributeValue(
                window,
                axFullScreenAttribute,
                &fullscreenValue
            ) == .success, fullscreenValue as? Bool == true else { continue }

            var id: CGWindowID = 0
            if _AXUIElementGetWindow(window, &id) == .success {
                ids.insert(id)
            }
        }
    }
    return ids
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
    return "{\"ax_trusted\": \(trusted), \"screen_recording\": \(CGPreflightScreenCaptureAccess())}"
}

func encodeJSON<T: Encodable>(_ value: T) -> String {
    let encoder = JSONEncoder()
    guard let data = try? encoder.encode(value) else {
        fail("encode snapshot JSON")
    }
    return String(decoding: data, as: UTF8.self)
}
