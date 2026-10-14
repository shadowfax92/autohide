// The only code that hides apps. Main thread and non-blocking: the outcome is
// confirmed later by a notification or a run-loop timer, never by sleeping. It
// must never activate, unhide, or prompt for anything (no focus stealing).

import AppKit
import ApplicationServices
import AutohideCore

enum HideResult: Equatable { case hidden, alreadyHidden, gone, refused(String) }

final class Hider {
    /// Main thread, NON-blocking. NSRunningApplication.hide(), then confirm via
    /// didHideApplication or isHidden within 1.0 s (main-run-loop timer, never a
    /// sleep). If still visible and AXIsProcessTrusted(), try kAXHiddenAttribute
    /// (0.5 s messaging timeout) and confirm again. Completion on main.
    func hide(_ pid: PID, completion: @escaping (HideResult) -> Void) {
        preconditionFailure("unimplemented")
    }

    static var accessibilityTrusted: Bool {   // AXIsProcessTrusted(), never prompts
        preconditionFailure("unimplemented")
    }
}
