// The only code that hides apps. Main thread and non-blocking: the outcome is
// confirmed later by a notification or a run-loop timer, never by sleeping. It
// must never activate, unhide, or prompt for anything (no focus stealing).

import AppKit
import ApplicationServices
import AutohideCore

/// How one hide request ended. `becameFrontmost` is deliberately not a
/// refusal: the user is in that app (or just switched to it), its activation
/// already reset the timer, and backing off would keep it on screen for five
/// minutes after the user leaves it.
enum HideResult: Equatable { case hidden, alreadyHidden, gone, becameFrontmost, refused(String) }

/// Cmd-H for one app, with proof. `NSRunningApplication.hide()` returns false
/// even when it works (spike.md Q1), so its return value is ignored and the
/// hide counts only once `didHideApplication` arrives or `isHidden` flips.
/// Both land on a turn of the main run loop, which is why confirmation is a
/// timer and the caller's main thread must stay free.
///
///     hide(pid) ── hide() ──▶ confirm ≤ 1.0 s ──▶ .hidden / .gone
///                                 │ still visible
///                                 ▼
///                  frontmost now? ── yes ──▶ .becameFrontmost
///                                 │ no
///                  AX trusted? ── no ──▶ .refused
///                                 │ yes: kAXHidden (off main, 0.5 s timeout)
///                                 ▼
///                             confirm ≤ 1.0 s ──▶ .hidden / .gone / .refused
final class Hider {
    static let confirmWindow: TimeInterval = 1.0
    static let pollInterval: TimeInterval = 0.05
    static let accessibilityMessagingTimeout: Float = 0.5

    /// The AX fallback is a synchronous IPC into the target app that can block
    /// for the whole messaging timeout (the app may be hung, which is often why
    /// hide() was ignored). It runs here so main never stalls on it.
    private let accessibilityQueue = DispatchQueue(label: "com.autohide.hider.accessibility")

    /// Main thread, NON-blocking. NSRunningApplication.hide(), then confirm via
    /// didHideApplication or isHidden within 1.0 s (main-run-loop timer, never a
    /// sleep). If still visible and AXIsProcessTrusted(), try kAXHiddenAttribute
    /// (0.5 s messaging timeout) and confirm again. Completion on main.
    ///
    /// The completion fires exactly once and always on a later main-queue turn,
    /// never inside this call, so a caller iterating a hide list can mutate its
    /// state from the completion safely.
    func hide(_ pid: PID, completion: @escaping (HideResult) -> Void) {
        dispatchPrecondition(condition: .onQueue(.main))

        guard let app = NSRunningApplication(processIdentifier: pid), !app.isTerminated else {
            return Self.deliverLater(.gone, completion)
        }
        if app.isHidden {
            return Self.deliverLater(.alreadyHidden, completion)
        }
        // Hiding the active app makes macOS activate another one: focus theft.
        // The Engine already guards this; the one place that hides re-checks.
        if Self.isFrontmost(app) {
            return Self.deliverLater(.becameFrontmost, completion)
        }

        // Observers go in before hide() so an immediate didHide cannot slip by.
        let attempt = HideAttempt(app: app, completion: completion)
        attempt.confirm(within: Self.confirmWindow) { attempt in
            self.fallBackToAccessibility(attempt)
        }
        _ = app.hide()
    }

    /// AXIsProcessTrusted(): reads the current grant and never shows the
    /// Accessibility prompt (that would be AXIsProcessTrustedWithOptions).
    static var accessibilityTrusted: Bool {
        AXIsProcessTrusted()
    }

    /// Runs on main once hide() went unconfirmed for the whole window.
    private func fallBackToAccessibility(_ attempt: HideAttempt) {
        // The user may have switched to this app during the wait. Checked before
        // anything else so that it is never reported as a refusal.
        if Self.isFrontmost(attempt.app) {
            return attempt.finish(.becameFrontmost)
        }
        let window = String(format: "%.1f", Self.confirmWindow)
        guard Self.accessibilityTrusted else {
            return attempt.finish(.refused("still visible \(window)s after hide(); no Accessibility for the fallback"))
        }

        let pid = attempt.app.processIdentifier
        accessibilityQueue.async {
            let element = AXUIElementCreateApplication(pid)
            var error = AXUIElementSetMessagingTimeout(element, Self.accessibilityMessagingTimeout)
            if error == .success {
                error = AXUIElementSetAttributeValue(element, kAXHiddenAttribute as CFString, kCFBooleanTrue)
            }
            // Back to main: the attempt's state and NSRunningApplication are main-only.
            DispatchQueue.main.async {
                guard error == .success else {
                    return attempt.finish(.refused("hide() ignored; AX hide failed (AXError \(error.rawValue))"))
                }
                attempt.confirm(within: Self.confirmWindow) { attempt in
                    attempt.finish(Self.isFrontmost(attempt.app)
                                   ? .becameFrontmost
                                   : .refused("still visible after hide() and AX hide"))
                }
            }
        }
    }

    private static func isFrontmost(_ app: NSRunningApplication) -> Bool {
        app.isActive || NSWorkspace.shared.frontmostApplication?.processIdentifier == app.processIdentifier
    }

    private static func deliverLater(_ result: HideResult, _ completion: @escaping (HideResult) -> Void) {
        DispatchQueue.main.async { completion(result) }
    }
}

/// One in-flight hide. It keeps itself alive through its timer and
/// notification observers (both retain it) and tears all of them down in
/// finish(), so the Hider needs no registry and the completion cannot fire twice.
/// Main thread only.
private final class HideAttempt {
    let app: NSRunningApplication
    private var completion: ((HideResult) -> Void)?
    private var observers: [NSObjectProtocol] = []
    private var timer: Timer?

    init(app: NSRunningApplication, completion: @escaping (HideResult) -> Void) {
        self.app = app
        self.completion = completion
    }

    /// Waits up to `window` for the hide to land, polling `isHidden` and
    /// listening for didHide/didTerminate. On timeout, hands the still-open
    /// attempt to `onTimeout`, which must eventually call finish().
    func confirm(within window: TimeInterval, onTimeout: @escaping (HideAttempt) -> Void) {
        guard completion != nil else { return }   // already finished (e.g. didHide during the AX hop)
        if observers.isEmpty { observeWorkspace() }

        let deadline = ProcessInfo.processInfo.systemUptime + window
        let timer = Timer(timeInterval: Hider.pollInterval, repeats: true) { [self] timer in
            if app.isTerminated {
                finish(.gone)
            } else if app.isHidden {
                finish(.hidden)
            } else if ProcessInfo.processInfo.systemUptime >= deadline {
                timer.invalidate()
                self.timer = nil
                onTimeout(self)
            }
        }
        // Common modes: keep confirming while a menu is being tracked.
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    /// Delivers the result once and releases the timer and observers.
    func finish(_ result: HideResult) {
        guard let completion else { return }
        self.completion = nil
        timer?.invalidate()
        timer = nil
        let center = NSWorkspace.shared.notificationCenter
        for token in observers { center.removeObserver(token) }
        observers.removeAll()
        completion(result)
    }

    private func observeWorkspace() {
        let center = NSWorkspace.shared.notificationCenter
        let pid = app.processIdentifier
        let outcomes: [(NSNotification.Name, HideResult)] = [
            (NSWorkspace.didHideApplicationNotification, .hidden),
            (NSWorkspace.didTerminateApplicationNotification, .gone),
        ]
        for (name, result) in outcomes {
            observers.append(center.addObserver(forName: name, object: nil, queue: .main) { [self] note in
                let subject = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
                guard subject?.processIdentifier == pid else { return }
                finish(result)
            })
        }
    }
}
