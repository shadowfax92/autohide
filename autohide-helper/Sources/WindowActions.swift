import AppKit
import ApplicationServices

private let hidePollAttempts = 20
private let hidePollInterval: TimeInterval = 0.05

/// Hides one whole app, falling back to a bounded visibility poll when AX is unavailable.
func hideApp(pid: pid_t) -> String? {
    guard let app = NSRunningApplication(processIdentifier: pid) else {
        return "no running application with pid \(pid)"
    }
    if app.isHidden { return nil }

    var axError = AXError.apiDisabled
    if AXIsProcessTrusted() {
        let appElement = AXUIElementCreateApplication(pid)
        axError = AXUIElementSetAttributeValue(
            appElement,
            kAXHiddenAttribute as CFString,
            kCFBooleanTrue
        )
        if axError == .success { return nil }
    }

    _ = app.hide()
    for _ in 0..<hidePollAttempts {
        if app.isHidden || app.isTerminated { return nil }
        Thread.sleep(forTimeInterval: hidePollInterval)
    }
    if app.isHidden || app.isTerminated { return nil }

    let grace = Double(hidePollAttempts) * hidePollInterval
    return "app pid \(pid) still visible after \(String(format: "%.1f", grace))s grace (AXError \(axError.rawValue))"
}
