import AppKit
import ApplicationServices

// Minimizes one window of one app, addressed by CGWindowID. Returns an error
// reason or nil on success. Refuses non-standard windows (dialogs, sheets)
// and anything it cannot identify unambiguously — wrong-window minimize is
// worse than no action.
func minimizeWindow(pid: pid_t, windowID: CGWindowID) -> String? {
    guard AXIsProcessTrusted() else { return "accessibility not granted" }

    let appEl = AXUIElementCreateApplication(pid)
    var windowsRef: CFTypeRef?
    guard AXUIElementCopyAttributeValue(appEl, kAXWindowsAttribute as CFString, &windowsRef) == .success,
          let axWindows = windowsRef as? [AXUIElement]
    else { return "cannot list windows of pid \(pid)" }

    guard let target = resolveAXWindow(windowID: windowID, in: axWindows) else {
        return "window \(windowID) not found in pid \(pid)"
    }

    // The user may have clicked this window between snapshot and now (which
    // also makes its app frontmost) — never minimize what they're using.
    // Gated on frontmost: a background app's AXFocusedWindow is merely its
    // key window and must not veto.
    if NSWorkspace.shared.frontmostApplication?.processIdentifier == pid {
        var focusedRef: CFTypeRef?
        if AXUIElementCopyAttributeValue(appEl, kAXFocusedWindowAttribute as CFString, &focusedRef) == .success,
           let focusedRef, CFGetTypeID(focusedRef) == AXUIElementGetTypeID() {
            var focusedID: CGWindowID = 0
            if _AXUIElementGetWindow(focusedRef as! AXUIElement, &focusedID) == .success, focusedID == windowID {
                return "window \(windowID) is currently focused"
            }
        }
    }

    var subroleRef: CFTypeRef?
    guard AXUIElementCopyAttributeValue(target, kAXSubroleAttribute as CFString, &subroleRef) == .success,
          subroleRef as? String == kAXStandardWindowSubrole as String
    else { return "window \(windowID) is not a standard window" }

    var settable = DarwinBoolean(false)
    guard AXUIElementIsAttributeSettable(target, kAXMinimizedAttribute as CFString, &settable) == .success,
          settable.boolValue
    else { return "window \(windowID) cannot be minimized" }

    guard AXUIElementSetAttributeValue(target, kAXMinimizedAttribute as CFString, kCFBooleanTrue) == .success else {
        return "minimize of window \(windowID) failed"
    }
    return nil
}

private func resolveAXWindow(windowID: CGWindowID, in axWindows: [AXUIElement]) -> AXUIElement? {
    for el in axWindows {
        var id: CGWindowID = 0
        if _AXUIElementGetWindow(el, &id) == .success, id == windowID {
            return el
        }
    }
    return matchByFrameAndTitle(windowID: windowID, in: axWindows)
}

// Fallback for _AXUIElementGetWindow breaking: match the CG window's bounds
// (+ title when present) against AX frames. Ambiguity -> no match.
private func matchByFrameAndTitle(windowID: CGWindowID, in axWindows: [AXUIElement]) -> AXUIElement? {
    guard let infos = CGWindowListCopyWindowInfo(.optionIncludingWindow, windowID) as? [[String: Any]],
          let info = infos.first,
          let bounds = info[kCGWindowBounds as String] as? [String: CGFloat],
          let x = bounds["X"], let y = bounds["Y"],
          let w = bounds["Width"], let h = bounds["Height"]
    else { return nil }
    let cgTitle = info[kCGWindowName as String] as? String ?? ""

    var matches: [AXUIElement] = []
    for el in axWindows {
        guard let frame = axFrame(of: el),
              abs(frame.origin.x - x) <= 2, abs(frame.origin.y - y) <= 2,
              abs(frame.size.width - w) <= 2, abs(frame.size.height - h) <= 2
        else { continue }
        if !cgTitle.isEmpty, let axTitle = axTitle(of: el), !axTitle.isEmpty, axTitle != cgTitle {
            continue
        }
        matches.append(el)
    }
    return matches.count == 1 ? matches.first : nil
}

private func axFrame(of el: AXUIElement) -> CGRect? {
    var posRef: CFTypeRef?
    var sizeRef: CFTypeRef?
    var pos = CGPoint.zero
    var size = CGSize.zero
    guard AXUIElementCopyAttributeValue(el, kAXPositionAttribute as CFString, &posRef) == .success,
          let posRef, CFGetTypeID(posRef) == AXValueGetTypeID(),
          AXValueGetValue(posRef as! AXValue, .cgPoint, &pos),
          AXUIElementCopyAttributeValue(el, kAXSizeAttribute as CFString, &sizeRef) == .success,
          let sizeRef, CFGetTypeID(sizeRef) == AXValueGetTypeID(),
          AXValueGetValue(sizeRef as! AXValue, .cgSize, &size)
    else { return nil }
    return CGRect(origin: pos, size: size)
}

private func axTitle(of el: AXUIElement) -> String? {
    var ref: CFTypeRef?
    guard AXUIElementCopyAttributeValue(el, kAXTitleAttribute as CFString, &ref) == .success else { return nil }
    return ref as? String
}

func hideApp(pid: pid_t) -> String? {
    guard let app = NSRunningApplication(processIdentifier: pid) else {
        return "no running application with pid \(pid)"
    }
    // hide() returns false when the app is already hidden (e.g. another
    // autohide tick won the race) — already-hidden is success here.
    if app.isHidden { return nil }
    if app.hide() { return nil }
    if app.isHidden { return nil }
    return "hide request for pid \(pid) was refused"
}
