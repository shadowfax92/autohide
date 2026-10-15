// The only code that hides apps. Main thread only, and it never sleeps: the
// outcome is confirmed later by a notification or a run-loop timer. It must
// never activate, unhide, or prompt for anything (no focus stealing).

import AppKit
import ApplicationServices
import AutohideCore

/// How one hide request ended. Only `refused` earns the 5-minute backoff.
/// `becameFrontmost` is not a refusal: the user is in that app (or just
/// switched to it), its activation already reset the timer, and backing off
/// would keep it on screen for five minutes after the user leaves it.
/// `withdrawn` means the Engine's eligibility check said no, or the Engine
/// cancelled the attempt (stop, pause); nothing more is done to the app.
enum HideResult: Equatable { case hidden, alreadyHidden, gone, becameFrontmost, withdrawn, refused(String) }

/// Cmd-H for one app, with proof. `NSRunningApplication.hide()` returns false
/// even when it works (spike.md Q1), so its return value is ignored and the
/// hide counts only once `didHideApplication` arrives or `isHidden` flips.
/// Both land on a turn of the main run loop, which is why confirmation is a
/// timer and the caller's main thread must stay free.
///
///     hide(pid) ── frontmost / !stillEligible ──▶ .becameFrontmost / .withdrawn
///         │ hide()
///         ▼
///     confirm ≤ 1.0 s ──▶ .hidden / .gone
///         │ still visible
///         ▼
///     frontmost / !stillEligible ──▶ .becameFrontmost / .withdrawn
///     AX not trusted             ──▶ .refused
///         │ kAXHidden, same main turn as the check (0.25 s messaging timeout)
///         ▼
///     confirm ≤ 1.0 s ──▶ .hidden / .gone / .becameFrontmost / .refused
///
/// The eligibility check is the Engine's (fresh focus, protection, pause,
/// away/lock, fullscreen Space, config), because a second or more can pass
/// between deciding to hide and the AX fallback; only the Engine knows all of
/// that. Each destructive call happens on the same main turn as its check.
final class Hider {
    static let confirmWindow: TimeInterval = 1.0
    static let pollInterval: TimeInterval = 0.05
    /// The AX write is a synchronous IPC into the target app, made on main so
    /// that nothing can run between the eligibility check and the write. A hung
    /// target (often why hide() was ignored) can block main for this long.
    static let accessibilityMessagingTimeout: Float = 0.25

    /// Attempts that have not finished, so the Engine can cancel them.
    private var attempts: [ObjectIdentifier: HideAttempt] = [:]

    /// Main thread. NSRunningApplication.hide(), then confirm via
    /// didHideApplication or isHidden within 1.0 s (main-run-loop timer, never a
    /// sleep). If still visible and AXIsProcessTrusted(), try kAXHiddenAttribute
    /// and confirm again. `stillEligible` is asked right before hide() and right
    /// before the AX write; false ends the attempt as `.withdrawn`.
    ///
    /// The completion fires exactly once, on main, and never inside this call,
    /// so a caller can mutate its state from the completion safely.
    func hide(_ pid: PID, stillEligible: @escaping () -> Bool, completion: @escaping (HideResult) -> Void) {
        dispatchPrecondition(condition: .onQueue(.main))

        guard let app = NSRunningApplication(processIdentifier: pid), !app.isTerminated else {
            return Self.deliverLater(.gone, completion)
        }
        if app.isHidden {
            return Self.deliverLater(.alreadyHidden, completion)
        }
        // Hiding the active app makes macOS activate another one: focus theft.
        if Self.isFrontmost(app) {
            return Self.deliverLater(.becameFrontmost, completion)
        }
        guard stillEligible() else {
            return Self.deliverLater(.withdrawn, completion)
        }

        // Observers go in before hide() so an immediate didHide cannot slip by.
        let attempt = HideAttempt(app: app, stillEligible: stillEligible) { [weak self] attempt, result in
            self?.attempts[ObjectIdentifier(attempt)] = nil
            completion(result)
        }
        attempts[ObjectIdentifier(attempt)] = attempt
        attempt.confirm(within: Self.confirmWindow) { attempt in
            self.fallBackToAccessibility(attempt)
        }
        _ = app.hide()
    }

    /// Ends every unfinished attempt as `.withdrawn`. A finished attempt never
    /// writes, so no AX fallback runs after this. A native hide() already sent
    /// cannot be recalled; if it lands, WorkspaceEvents reports it as usual.
    func cancelAll() {
        for attempt in Array(attempts.values) { attempt.finish(.withdrawn) }
    }

    /// AXIsProcessTrusted(): reads the current grant and never shows the
    /// Accessibility prompt (that would be AXIsProcessTrustedWithOptions).
    static var accessibilityTrusted: Bool {
        AXIsProcessTrusted()
    }

    /// Runs on main once hide() went unconfirmed for the whole window. Every
    /// check below and the AX write run in this one main-thread call, so no
    /// activation, pause, or config change can be processed in between.
    private func fallBackToAccessibility(_ attempt: HideAttempt) {
        guard attempt.isActive else { return }
        // The user may have switched to this app during the wait. Checked before
        // anything else so that it is never reported as a refusal.
        if Self.isFrontmost(attempt.app) {
            return attempt.finish(.becameFrontmost)
        }
        guard attempt.stillEligible() else {
            return attempt.finish(.withdrawn)
        }
        let window = String(format: "%.1f", Self.confirmWindow)
        guard Self.accessibilityTrusted else {
            return attempt.finish(.refused("still visible \(window)s after hide(); no Accessibility for the fallback"))
        }

        let element = AXUIElementCreateApplication(attempt.app.processIdentifier)
        var error = AXUIElementSetMessagingTimeout(element, Self.accessibilityMessagingTimeout)
        if error == .success {
            error = AXUIElementSetAttributeValue(element, kAXHiddenAttribute as CFString, kCFBooleanTrue)
        }
        guard error == .success else {
            return attempt.finish(.refused("hide() ignored; AX hide failed (AXError \(error.rawValue))"))
        }
        attempt.confirm(within: Self.confirmWindow) { attempt in
            attempt.finish(Self.isFrontmost(attempt.app)
                           ? .becameFrontmost
                           : .refused("still visible after hide() and AX hide"))
        }
    }

    private static func isFrontmost(_ app: NSRunningApplication) -> Bool {
        app.isActive || NSWorkspace.shared.frontmostApplication?.processIdentifier == app.processIdentifier
    }

    private static func deliverLater(_ result: HideResult, _ completion: @escaping (HideResult) -> Void) {
        DispatchQueue.main.async { completion(result) }
    }
}

/// One in-flight hide. Its timer and notification observers retain it, and
/// the Hider's registry holds it until finish(), which tears all of them down,
/// so the completion cannot fire twice. Main thread only.
private final class HideAttempt {
    let app: NSRunningApplication
    let stillEligible: () -> Bool
    private var completion: ((HideAttempt, HideResult) -> Void)?
    private var observers: [NSObjectProtocol] = []
    private var timer: Timer?

    init(app: NSRunningApplication, stillEligible: @escaping () -> Bool,
         completion: @escaping (HideAttempt, HideResult) -> Void) {
        self.app = app
        self.stillEligible = stillEligible
        self.completion = completion
    }

    /// False once finished or cancelled; such an attempt must not touch the app.
    var isActive: Bool { completion != nil }

    /// Waits up to `window` for the hide to land, polling `isHidden` and
    /// listening for didHide/didTerminate. On timeout, hands the still-open
    /// attempt to `onTimeout`, which must eventually call finish().
    func confirm(within window: TimeInterval, onTimeout: @escaping (HideAttempt) -> Void) {
        guard isActive else { return }   // already finished (e.g. didHide arrived first)
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
        completion(self, result)
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
