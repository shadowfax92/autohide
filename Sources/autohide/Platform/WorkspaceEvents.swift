// WorkspaceEvents.swift — NSWorkspace + DistributedNotificationCenter → PolicyEvent, delivered on main.
// Activation of non-regular apps is NOT forwarded. Never throws/aborts on odd payloads (nil names are fine).
// The old helper tore down its whole event stream on one nameless app (diagnosis R3).

import AppKit
import AutohideCore

/// The Engine's only source of push events. Observers are registered on the
/// main queue, so `onEvent` always runs on main and may touch Engine state.
///
/// Mapping: didActivateApplication→.activated (regular only), didLaunchApplication→.launched,
/// didTerminateApplication→.terminated, didHideApplication→.hidden, didUnhideApplication→.unhidden,
/// activeSpaceDidChange→.spaceChanged, willSleep/didWake→.away/.returned(.sleep),
/// screensDidSleep/screensDidWake→(.displaySleep), com.apple.screenIsLocked/Unlocked→(.lock),
/// sessionDidResignActive/BecomeActive→(.session).
///
/// A payload is only ever inspected for its PID (and, for activation, its
/// activation policy). A notification that carries no usable PID is skipped on
/// its own; the observers stay registered and the Engine's heartbeat observe
/// reconciles whatever that one notification would have said.
final class WorkspaceEvents {
    private let onEvent: (PolicyEvent) -> Void
    private var workspaceObservers: [NSObjectProtocol] = []
    private var lockRelay: LockRelay?

    init(onEvent: @escaping (PolicyEvent) -> Void) {
        self.onEvent = onEvent
    }

    deinit {
        stop()
    }

    /// Idempotent: a second start() while running registers nothing new.
    func start() {
        guard workspaceObservers.isEmpty, lockRelay == nil else { return }

        let center = NSWorkspace.shared.notificationCenter
        for (name, translate) in Self.workspaceMapping {
            // `[weak self]`: the center retains this block until stop(); a
            // WorkspaceEvents dropped without stop() must not be kept alive by it.
            let token = center.addObserver(forName: name, object: nil, queue: .main) { [weak self] note in
                let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
                guard let self, let event = translate(app) else { return }
                self.onEvent(event)
            }
            workspaceObservers.append(token)
        }

        lockRelay = LockRelay { [weak self] event in self?.onEvent(event) }
    }

    /// Removes every observer from both centers. Safe to call when not started.
    func stop() {
        let center = NSWorkspace.shared.notificationCenter
        for token in workspaceObservers { center.removeObserver(token) }
        workspaceObservers.removeAll()
        lockRelay?.invalidate()
        lockRelay = nil
    }

    private static let workspaceMapping: [(NSNotification.Name, (NSRunningApplication?) -> PolicyEvent?)] = [
        (NSWorkspace.didActivateApplicationNotification, { app in
            // Menu-bar extras, Spotlight, and other accessory apps activate too.
            // Forwarding them would unprotect the regular app the user is in.
            guard let app, app.activationPolicy == .regular, let pid = usablePID(app) else { return nil }
            return .activated(pid)
        }),
        (NSWorkspace.didLaunchApplicationNotification, { usablePID($0).map(PolicyEvent.launched) }),
        (NSWorkspace.didTerminateApplicationNotification, { usablePID($0).map(PolicyEvent.terminated) }),
        (NSWorkspace.didHideApplicationNotification, { usablePID($0).map(PolicyEvent.hidden) }),
        (NSWorkspace.didUnhideApplicationNotification, { usablePID($0).map(PolicyEvent.unhidden) }),
        (NSWorkspace.activeSpaceDidChangeNotification, { _ in .spaceChanged }),
        (NSWorkspace.willSleepNotification, { _ in .away(.sleep) }),
        (NSWorkspace.didWakeNotification, { _ in .returned(.sleep) }),
        (NSWorkspace.screensDidSleepNotification, { _ in .away(.displaySleep) }),
        (NSWorkspace.screensDidWakeNotification, { _ in .returned(.displaySleep) }),
        (NSWorkspace.sessionDidResignActiveNotification, { _ in .away(.session) }),
        (NSWorkspace.sessionDidBecomeActiveNotification, { _ in .returned(.session) }),
    ]

    private static func usablePID(_ app: NSRunningApplication?) -> PID? {
        guard let pid = app?.processIdentifier, pid > 0 else { return nil }
        return pid
    }
}

/// Screen lock and unlock arrive only as distributed notifications from
/// loginwindow. NSApplication suspends distributed delivery while the app is
/// inactive, and an accessory agent is practically never active, so the
/// default (coalescing) registration would hold these until the user next
/// clicked our menu. `.deliverImmediately` exists only on the selector-based
/// API, hence this small NSObject.
private final class LockRelay: NSObject {
    private static let mapping: [NSNotification.Name: PolicyEvent] = [
        NSNotification.Name("com.apple.screenIsLocked"): .away(.lock),
        NSNotification.Name("com.apple.screenIsUnlocked"): .returned(.lock),
    ]

    private let forward: (PolicyEvent) -> Void

    init(forward: @escaping (PolicyEvent) -> Void) {
        self.forward = forward
        super.init()
        let center = DistributedNotificationCenter.default()
        for name in Self.mapping.keys {
            center.addObserver(self, selector: #selector(receive(_:)), name: name, object: nil,
                               suspensionBehavior: .deliverImmediately)
        }
    }

    func invalidate() {
        DistributedNotificationCenter.default().removeObserver(self)
    }

    @objc private func receive(_ note: Notification) {
        guard let event = Self.mapping[note.name] else { return }
        // Distributed notifications are posted from the main run loop in
        // practice; the hop keeps the "onEvent runs on main" promise if not.
        if Thread.isMainThread {
            forward(event)
        } else {
            DispatchQueue.main.async { [forward] in forward(event) }
        }
    }
}
