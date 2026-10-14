// A snapshot of what is on screen right now, taken by the Engine on main at every
// evaluation and heartbeat. It must stay cheap (< 10 ms) and permission-free:
// no Accessibility calls, and no window titles (titles need Screen Recording).

import AppKit
import AutohideCore

struct WindowProbe {
    /// Regular, non-terminated apps (excluding our own PID), on-screen layer-0
    /// windows ≥ 50×50 with alpha > 0 grouped by owner PID, frontmost, Space type,
    /// and lock state. Must take < 10 ms; no AX calls.
    func observe() -> Observation {
        preconditionFailure("unimplemented")
    }

    static func identity(of app: NSRunningApplication) -> AppIdentity {
        preconditionFailure("unimplemented")
    }
}
