// A snapshot of what is on screen right now, taken by the Engine on main at every
// evaluation and heartbeat. It must stay cheap (< 10 ms) and permission-free:
// no Accessibility calls, and no window titles (titles need Screen Recording).

import AppKit
import AutohideCore

/// Builds the Observation that PolicyState.observe reconciles against. Each
/// call reads fresh state from three cheap sources: NSWorkspace (apps,
/// frontmost; its cached properties refresh as the main run loop turns),
/// one CGWindowList call (owner PID, layer, alpha, bounds only), and the
/// window server's session and Space state. Main thread only.
struct WindowProbe {
    /// Smaller layer-0 windows are status widgets, drag handles, and invisible
    /// helpers, not clutter. Counting them would keep an app "visible" forever.
    static let minimumWindowSide: CGFloat = 50

    /// Shared by copies of this struct (a reference), so the Engine's one
    /// probe keeps it across calls.
    private let nonRegular = NonRegularMemo()

    /// Regular, non-terminated apps (excluding our own PID), on-screen layer-0
    /// windows ≥ 50×50 with alpha > 0 grouped by owner PID, frontmost, Space type,
    /// and lock state. Must take < 10 ms; no AX calls.
    func observe() -> Observation {
        let workspace = NSWorkspace.shared
        let ownPID = ProcessInfo.processInfo.processIdentifier
        let withWindows = Self.pidsWithVisibleWindows()

        let running = workspace.runningApplications
        nonRegular.prune(keeping: running)

        var apps: [AppSnapshot] = []
        for app in running {
            if nonRegular.canSkip(app, visiblePIDs: withWindows) { continue }
            let pid = app.processIdentifier
            guard pid > 0, pid != ownPID, app.activationPolicy == .regular, !app.isTerminated else {
                nonRegular.remember(app, pid: pid)
                continue
            }
            // AppKit leads the window server: isHidden flips 5–60 ms before the
            // windows leave CGWindowList (spike.md gotcha 3). A hidden app is
            // never "visible", or the appearance rule would re-arm its timer.
            let isHidden = app.isHidden
            apps.append(AppSnapshot(pid: pid, identity: Self.identity(of: app), isHidden: isHidden,
                                    hasVisibleWindow: !isHidden && withWindows.contains(pid)))
        }

        return Observation(frontmost: workspace.frontmostApplication?.processIdentifier,
                           apps: apps,
                           onFullscreenSpace: SpaceProbe.activeSpaceIsFullscreen(),
                           screenLocked: Self.screenIsLocked())
    }

    /// Config matching uses this name and bundle ID. A nameless app (the
    /// crash trigger in diagnosis R3) still gets a stable, non-empty name so it
    /// is tracked and listed like any other app.
    static func identity(of app: NSRunningApplication) -> AppIdentity {
        let name = meaningful(app.localizedName)
            ?? meaningful(app.bundleURL?.deletingPathExtension().lastPathComponent)
            ?? "pid \(app.processIdentifier)"
        return AppIdentity(name: name, bundleID: meaningful(app.bundleIdentifier))
    }

    /// PIDs owning at least one window that is actually cluttering the current
    /// Space. `.optionOnScreenOnly` drops hidden and minimized windows and lists
    /// only the active Space plus all-Spaces windows (spike.md Q3). The
    /// dictionaries are read as NSDictionary to avoid bridging every key of
    /// every window.
    static func pidsWithVisibleWindows() -> Set<PID> {
        let options: CGWindowListOption = [.optionOnScreenOnly, .excludeDesktopElements]
        guard let windows = CGWindowListCopyWindowInfo(options, kCGNullWindowID) as NSArray? else { return [] }

        var pids = Set<PID>()
        for case let window as NSDictionary in windows {
            guard (window[kCGWindowLayer as String] as? Int) == 0,
                  let pid = window[kCGWindowOwnerPID as String] as? PID,
                  !pids.contains(pid)
            else { continue }
            // A missing alpha means opaque; only an explicit 0 is invisible.
            if let alpha = window[kCGWindowAlpha as String] as? Double, alpha <= 0 { continue }
            guard let boundsInfo = window[kCGWindowBounds as String] as? NSDictionary,
                  let bounds = CGRect(dictionaryRepresentation: boundsInfo as CFDictionary),
                  bounds.width >= minimumWindowSide, bounds.height >= minimumWindowSide
            else { continue }
            pids.insert(pid)
        }
        return pids
    }

    /// The reconcile path for a missed com.apple.screenIsLocked notification.
    /// The key is present (true) only while the lock screen is up.
    static func screenIsLocked() -> Bool {
        guard let session = CGSessionCopyCurrentDictionary() as NSDictionary? else { return false }
        return (session["CGSSessionScreenIsLocked"] as? Bool) ?? false
    }

    /// nil for nil, empty, or a string of only whitespace and invisible format
    /// characters (U+200E and friends), so such a name falls through to the
    /// next candidate. A real name is returned unchanged; AppMatch normalizes.
    private static func meaningful(_ text: String?) -> String? {
        guard let text,
              text.unicodeScalars.contains(where: {
                  !$0.properties.isWhitespace && $0.properties.generalCategory != .format
              })
        else { return nil }
        return text
    }
}

/// Remembers which NSRunningApplication instances were last seen as not
/// regular, so observe() does not touch every process on every call.
///
/// Why: NSRunningApplication drops its cached properties at each main-run-loop
/// turn, and the first read of ANY property (even processIdentifier) on an
/// instance then costs ~37 µs to refetch. This Mac runs ~205 such processes,
/// ~19 of them regular, so touching all of them made observe() ~8.5 ms;
/// touching only the regular ones is ~1–2 ms.
///
/// Safety: a remembered instance is re-checked whenever its PID owns a
/// visible window, so an app that switches to .regular (menu-bar apps do,
/// to show a settings window) is picked up as soon as it can clutter
/// anything. A stale entry can only delay an app's first timer, never hide
/// one. Entries hold the instance strongly, so its ObjectIdentifier cannot be
/// reused by a new instance; prune() drops instances NSWorkspace no longer
/// lists (they churn: ~25% were replaced within 3 s during a build).
private final class NonRegularMemo {
    private var entries: [ObjectIdentifier: (app: NSRunningApplication, pid: PID)] = [:]

    func prune(keeping running: [NSRunningApplication]) {
        guard !entries.isEmpty else { return }
        let live = Set(running.map(ObjectIdentifier.init))
        entries = entries.filter { live.contains($0.key) }
    }

    /// True when `app` is known non-regular and owns no visible window.
    /// Reads no property of `app`.
    func canSkip(_ app: NSRunningApplication, visiblePIDs: Set<PID>) -> Bool {
        let key = ObjectIdentifier(app)
        guard let entry = entries[key] else { return false }
        if visiblePIDs.contains(entry.pid) {
            entries[key] = nil
            return false
        }
        return true
    }

    func remember(_ app: NSRunningApplication, pid: PID) {
        entries[ObjectIdentifier(app)] = (app, pid)
    }
}
