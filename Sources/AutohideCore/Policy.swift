// The only place timer semantics live. PolicyState is the timer ledger, fed by
// events and observations; Policy.decide is the pure rule that turns it into
// "hide these PIDs, check again at T". No AppKit, no clock, no I/O: `now` is
// always passed in, so every rule is unit-testable. Do not add compensating
// layers here (diagnosis R7); change the rule instead.

import Foundation

/// One regular app as WindowProbe saw it at a single instant.
public struct AppSnapshot: Equatable {
    public var pid: PID
    public var identity: AppIdentity
    public var isHidden: Bool
    public var hasVisibleWindow: Bool      // ≥1 on-screen layer-0 window, current Space

    public init(pid: PID, identity: AppIdentity, isHidden: Bool, hasVisibleWindow: Bool) {
        self.pid = pid
        self.identity = identity
        self.isHidden = isHidden
        self.hasVisibleWindow = hasVisibleWindow
    }
}

/// A full picture of the desktop from WindowProbe. PolicyState.observe
/// reconciles against it, which self-heals any notification that was missed.
public struct Observation: Equatable {
    public var frontmost: PID?             // NSWorkspace frontmostApplication, any activation policy
    public var apps: [AppSnapshot]         // regular apps only, excluding autohide itself
    public var onFullscreenSpace: Bool
    public var screenLocked: Bool          // CGSessionCopyCurrentDictionary reconcile (§3 step 4)

    public init(frontmost: PID?, apps: [AppSnapshot], onFullscreenSpace: Bool, screenLocked: Bool = false) {
        self.frontmost = frontmost
        self.apps = apps
        self.onFullscreenSpace = onFullscreenSpace
        self.screenLocked = screenLocked
    }
}

public enum AwayReason: String, Equatable, Hashable { case sleep, displaySleep, lock, session }

/// What WorkspaceEvents tells the Engine, already translated from NSWorkspace
/// and distributed notifications.
public enum PolicyEvent: Equatable {
    case activated(PID)        // a REGULAR app became active
    case launched(PID)
    case terminated(PID)
    case hidden(PID)
    case unhidden(PID)
    case spaceChanged
    case away(AwayReason)
    case returned(AwayReason)
}

/// Per-PID timer. An app hides at lastActive + its effective timeout.
public struct AppTimer: Equatable {
    public var lastActive: Date
    public var wasVisible: Bool
    public var refusedUntil: Date?

    public init(lastActive: Date, wasVisible: Bool = false, refusedUntil: Date? = nil) {
        self.lastActive = lastActive
        self.wasVisible = wasVisible
        self.refusedUntil = refusedUntil
    }
}

/// All mutable timer state. Owned by the Engine and mutated only on main.
/// It is never persisted: a restart gives every app a fresh timeout.
public struct PolicyState: Equatable {
    public static let refusalBackoff: TimeInterval = 300
    public var timers: [PID: AppTimer]
    public var protected: PID?             // last activated regular app
    public var awayReasons: Set<AwayReason>
    public var pausedUntil: Date?          // Date.distantFuture = until resumed

    public init() {
        timers = [:]
        protected = nil
        awayReasons = []
        pausedUntil = nil
    }

    public var isAway: Bool { !awayReasons.isEmpty }

    public func isPaused(at now: Date) -> Bool {
        preconditionFailure("unimplemented")
    }

    /// activated(p): timers[old protected].lastActive = now; protected = p;
    ///               timers[p].lastActive = now, refusedUntil = nil;
    ///               awayReasons.removeAll() (user is clearly back) — with refresh if it was away.
    /// launched(p):  timers[p] = AppTimer(lastActive: now)
    /// terminated(p): remove timer; if protected == p, protected = nil
    /// hidden(p):    timers[p].wasVisible = false
    /// unhidden(p):  timers[p].lastActive = now, refusedUntil = nil
    /// spaceChanged: no direct change (the next observe applies the appearance rule)
    /// away(r):      insert r
    /// returned(r):  remove r; if now empty → refreshAll(now)
    public mutating func apply(_ event: PolicyEvent, at now: Date) {
        preconditionFailure("unimplemented")
    }

    /// Reconcile with reality: add unseen PIDs (lastActive = now), drop PIDs not
    /// in obs.apps, appearance rule (wasVisible false → hasVisibleWindow true ⇒
    /// lastActive = max(lastActive, now)), set wasVisible, and treat a changed
    /// regular obs.frontmost as an activation. If !obs.screenLocked, remove .lock.
    public mutating func observe(_ obs: Observation, at now: Date) {
        preconditionFailure("unimplemented")
    }

    public mutating func markRefused(_ pid: PID, at now: Date) {     // refusedUntil = now + refusalBackoff
        preconditionFailure("unimplemented")
    }

    public mutating func pause(until: Date?, at now: Date) {         // nil → distantFuture
        preconditionFailure("unimplemented")
    }

    public mutating func resume(at now: Date) {                      // pausedUntil = nil; refreshAll(now)
        preconditionFailure("unimplemented")
    }

    /// Expire a timed pause whose time has passed (→ resume(at:)).
    public mutating func tick(at now: Date) {
        preconditionFailure("unimplemented")
    }

    /// Every timer's lastActive = max(lastActive, now).
    public mutating func refreshAll(_ now: Date) {
        preconditionFailure("unimplemented")
    }
}

/// Why an app is or is not about to hide. Shown by `autohide list` and the menu.
public enum AppStatus: Equatable {
    case protected, exempt, hidden, offSpace, paused, away, fullscreenSpace
    case refused(until: Date)
    case pending(hideAt: Date)
    case due

    public var label: String {
        switch self {
        case .protected: return "frontmost"
        case .exempt: return "exempt"
        case .hidden: return "hidden"
        case .offSpace: return "off-space"
        case .paused: return "paused"
        case .away: return "away"
        case .fullscreenSpace: return "fullscreen-space"
        case .refused: return "refused"
        case .pending: return "pending"
        case .due: return "due"
        }
    }
}

/// The output of one evaluation: what to hide now, and when to look again.
public struct Decision: Equatable {
    public var hide: [PID]          // ordered oldest deadline first
    public var nextCheck: Date?     // earliest future deadline / refusal expiry / pause expiry

    public init(hide: [PID] = [], nextCheck: Date? = nil) {
        self.hide = hide
        self.nextCheck = nextCheck
    }
}

public enum Policy {
    /// Pure. Never returns obs.frontmost or state.protected. Returns no hides
    /// when away, paused, or onFullscreenSpace (nextCheck = pause expiry if timed).
    /// A candidate is: not protected, not hidden, hasVisibleWindow,
    /// effectiveTimeout != nil, timer exists, not refused. It is due when
    /// lastActive + timeout <= now.
    public static func decide(state: PolicyState, observation: Observation,
                              config: Config, now: Date) -> Decision {
        preconditionFailure("unimplemented")
    }

    public static func status(of app: AppSnapshot, state: PolicyState,
                              observation: Observation, config: Config, now: Date) -> AppStatus {
        preconditionFailure("unimplemented")
    }
}
