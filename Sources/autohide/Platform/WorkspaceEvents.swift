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
final class WorkspaceEvents {
    private let onEvent: (PolicyEvent) -> Void

    init(onEvent: @escaping (PolicyEvent) -> Void) {
        self.onEvent = onEvent
    }

    func start() {
        preconditionFailure("unimplemented")
    }

    func stop() {
        preconditionFailure("unimplemented")
    }
}
