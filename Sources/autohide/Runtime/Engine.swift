// Engine.swift — owns PolicyState, Config, timers; main-thread only.
// The hub of the daemon: events and probes go in, Policy decides, Hider acts.
// MenuBar and IPCServer reach it only through EngineControl, and only on main.

import Foundation
import AutohideCore

/// What the menu bar and the IPC handler may ask of the Engine. Every call
/// happens on main; IPCServer hops there with DispatchQueue.main.sync first.
protocol EngineControl: AnyObject {
    var config: Config { get }
    var protectedApp: AppIdentity? { get }
    func statusPayload() -> IPC.StatusPayload
    func appRows() -> [IPC.AppRow]
    func pause(seconds: TimeInterval?)     // nil = until resumed
    func resume()
    func hideOthersNow() -> Int            // hide every visible, non-protected, non-exempt app now
    func reloadConfig()
    func setTimeout(_ seconds: TimeInterval)          // persists to config.json
    func setExempt(_ app: String, _ exempt: Bool)     // persists to config.json
}

/// Runs the evaluation loop in design.md §3: reload config if changed, expire
/// pauses, observe, decide, hide, and re-arm one one-shot timer at nextCheck.
final class Engine: EngineControl {
    static let heartbeat: TimeInterval = 20

    private(set) var config: Config
    private let paths: ConfigPaths
    private let probe: WindowProbe
    private let hider: Hider

    init(paths: ConfigPaths, config: Config,
         probe: WindowProbe = WindowProbe(), hider: Hider = Hider()) {
        self.paths = paths
        self.config = config
        self.probe = probe
        self.hider = hider
    }

    func start() {         // WorkspaceEvents.start, first evaluate, heartbeat timer
        preconditionFailure("unimplemented")
    }

    func stop() {
        preconditionFailure("unimplemented")
    }

    func evaluate(reason: String) {
        preconditionFailure("unimplemented")
    }

    func handle(_ request: IPC.Request) -> IPC.Response {   // IPCServer handler body
        preconditionFailure("unimplemented")
    }

    // MARK: EngineControl

    var protectedApp: AppIdentity? {
        preconditionFailure("unimplemented")
    }

    func statusPayload() -> IPC.StatusPayload {
        preconditionFailure("unimplemented")
    }

    func appRows() -> [IPC.AppRow] {
        preconditionFailure("unimplemented")
    }

    func pause(seconds: TimeInterval?) {
        preconditionFailure("unimplemented")
    }

    func resume() {
        preconditionFailure("unimplemented")
    }

    func hideOthersNow() -> Int {
        preconditionFailure("unimplemented")
    }

    func reloadConfig() {
        preconditionFailure("unimplemented")
    }

    func setTimeout(_ seconds: TimeInterval) {
        preconditionFailure("unimplemented")
    }

    func setExempt(_ app: String, _ exempt: Bool) {
        preconditionFailure("unimplemented")
    }
}
