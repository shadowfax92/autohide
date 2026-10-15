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

    /// A full rescan of every running process is ~9 ms here, so it runs at most
    /// this often; between scans RegularApps follows the regular apps only.
    static let fullScanInterval: TimeInterval = 60

    /// Shared by copies of this struct (a reference), so the Engine's one
    /// probe keeps it across calls.
    private let regularApps = RegularApps()

    /// Regular, non-terminated apps (excluding our own PID), on-screen layer-0
    /// windows ≥ 50×50 with alpha > 0 grouped by owner PID, frontmost, Space type,
    /// and lock state. Must take < 10 ms; no AX calls.
    func observe() -> Observation {
        let workspace = NSWorkspace.shared
        let withWindows = Self.pidsWithVisibleWindows()
        let frontmost = workspace.frontmostApplication?.processIdentifier

        var candidates = withWindows
        if let frontmost { candidates.insert(frontmost) }

        let apps = regularApps.current(candidates: candidates).map { app -> AppSnapshot in
            // AppKit leads the window server: isHidden flips 5–60 ms before the
            // windows leave CGWindowList (spike.md gotcha 3). A hidden app is
            // never "visible", or the appearance rule would re-arm its timer.
            let pid = app.processIdentifier
            let isHidden = app.isHidden
            return AppSnapshot(pid: pid, identity: Self.identity(of: app), isHidden: isHidden,
                               hasVisibleWindow: !isHidden && withWindows.contains(pid))
        }

        return Observation(frontmost: frontmost,
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

/// The set of regular apps, kept without touching every process on every call.
///
/// Why: NSWorkspace.runningApplications hands out NEW NSRunningApplication
/// instances on every main-run-loop turn, and the first read of any property
/// (even processIdentifier) on an instance costs ~37 µs to fetch. This Mac
/// runs ~205 processes, ~19 of them regular, so scanning them all made
/// observe() ~9–11 ms on every call. Held instances refresh per turn too, so
/// the saving comes from touching only the regular ones (~1 ms).
///
/// How it stays complete: a full scan seeds the set and repeats every
/// `WindowProbe.fullScanInterval`. Between scans, every PID that owns a visible
/// window or is frontmost is checked and admitted if regular. That catches
/// launches and apps that switch to .regular (menu-bar apps do, to show a
/// settings window) as soon as they can clutter anything. Held apps that quit
/// or stop being regular are dropped on the next call. A regular app missing
/// until the next scan has no visible window, so it could not be hidden
/// anyway; the gap can only delay its timer, never cause a hide.
///
/// Main thread only (NSRunningApplication state is a main-run-loop snapshot).
private final class RegularApps {
    private var byPID: [PID: NSRunningApplication] = [:]
    private var lastFullScan: TimeInterval?
    private let ownPID = ProcessInfo.processInfo.processIdentifier

    func current(candidates: Set<PID>) -> [NSRunningApplication] {
        let now = ProcessInfo.processInfo.systemUptime
        if let last = lastFullScan, now - last < WindowProbe.fullScanInterval {
            byPID = byPID.filter { isRegular($0.value) }
            for pid in candidates where byPID[pid] == nil {
                if let app = NSRunningApplication(processIdentifier: pid) { admit(app) }
            }
        } else {
            byPID.removeAll()
            for app in NSWorkspace.shared.runningApplications { admit(app) }
            lastFullScan = now
        }
        return Array(byPID.values)
    }

    private func admit(_ app: NSRunningApplication) {
        if isRegular(app) { byPID[app.processIdentifier] = app }
    }

    private func isRegular(_ app: NSRunningApplication) -> Bool {
        let pid = app.processIdentifier
        return pid > 0 && pid != ownPID && app.activationPolicy == .regular && !app.isTerminated
    }
}
