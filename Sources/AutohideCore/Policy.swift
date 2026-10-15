// The only place timer semantics live. PolicyState is the timer ledger, fed by
// events and observations; Policy.decide is the pure rule that turns it into
// "hide these PIDs, check again at T". No AppKit, no clock, no I/O: `now` is
// always passed in, so every rule is unit-testable. Do not add compensating
// layers here (diagnosis R7); change the rule instead.
//
// The whole rule: an on-screen, non-protected, non-exempt app hides at
// lastActive + timeout, where lastActive is the latest of: the user left it,
// it launched, it was unhidden, it came on screen, or the user came back
// (wake, unlock, end of pause). Nothing hides while away, paused, or on a
// fullscreen Space.

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

/// Cheap facts about the login session, read by the Engine's heartbeat while
/// away (WindowProbe.presence) to end an away whose return notification was
/// missed. See PolicyState.reconcileAway.
public struct Presence: Equatable {
    public var onConsole: Bool             // this session owns the screen (not switched out)
    public var screenLocked: Bool
    public var displayAwake: Bool          // main display
    public var lastInput: Date?            // last keyboard/mouse/trackpad event; nil = unknown

    public init(onConsole: Bool, screenLocked: Bool, displayAwake: Bool, lastInput: Date?) {
        self.onConsole = onConsole
        self.screenLocked = screenLocked
        self.displayAwake = displayAwake
        self.lastInput = lastInput
    }
}

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
    /// Bumped by every activation and unhide of this app. The Engine records it
    /// when it issues a hide, and markRefused drops a refusal whose attempt
    /// predates the current value (see markRefused).
    public var activationGeneration: Int

    public init(lastActive: Date, wasVisible: Bool = false, refusedUntil: Date? = nil,
                activationGeneration: Int = 0) {
        self.lastActive = lastActive
        self.wasVisible = wasVisible
        self.refusedUntil = refusedUntil
        self.activationGeneration = activationGeneration
    }
}

/// Why the Engine asked for a hide. A timed hide must still be `.due` when it
/// is issued; a hide-others hide needs only Policy.mayHideNow.
public enum HideTrigger: String, Equatable {
    case timeout
    case hideOthers = "hide-others"
}

/// All mutable timer state. Owned by the Engine and mutated only on main.
/// It is never persisted: a restart gives every app a fresh timeout.
public struct PolicyState: Equatable {
    public static let refusalBackoff: TimeInterval = 300
    /// Input this recent, and newer than awaySince, proves a return (reconcileAway).
    public static let presenceWindow: TimeInterval = 30
    public var timers: [PID: AppTimer]
    public var protected: PID?             // last activated regular app
    public var awayReasons: Set<AwayReason>
    /// When awayReasons last became non-empty; nil while not away. Kept by
    /// apply and observe, the only mutators that take a clock.
    public private(set) var awaySince: Date?
    public var pausedUntil: Date?          // Date.distantFuture = until resumed

    public init() {
        timers = [:]
        protected = nil
        awayReasons = []
        pausedUntil = nil
    }

    public var isAway: Bool { !awayReasons.isEmpty }

    /// A timed pause whose moment has passed no longer counts, even before
    /// `tick` clears it.
    public func isPaused(at now: Date) -> Bool {
        guard let pausedUntil else { return false }
        return pausedUntil > now
    }

    /// activated(p): timers[old protected].lastActive = now; protected = p;
    ///               timers[p].lastActive = now, refusedUntil = nil, activationGeneration += 1;
    ///               awayReasons.removeAll() (user is clearly back) — with refresh if it was away.
    /// launched(p):  timers[p] = AppTimer(lastActive: now)
    /// terminated(p): remove timer; if protected == p, protected = nil
    /// hidden(p):    timers[p].wasVisible = false
    /// unhidden(p):  timers[p].lastActive = now, refusedUntil = nil, activationGeneration += 1
    /// spaceChanged: no direct change (the next observe applies the appearance rule)
    /// away(r):      insert r
    /// returned(r):  remove r; if now empty → refreshAll(now)
    ///
    /// Events only touch PIDs that already have a timer, except `launched`,
    /// which starts one. Any other new PID gets its timer from the next observe.
    public mutating func apply(_ event: PolicyEvent, at now: Date) {
        defer { stampAway(now) }
        switch event {
        case .activated(let pid):
            // An activation proves the user is back even if the wake or unlock
            // notification was missed, so it ends "away" like a return does.
            if isAway {
                awayReasons.removeAll()
                refreshAll(now)
            }
            // The app the user just left starts its countdown now.
            if let previous = protected { timers[previous]?.lastActive = now }
            protected = pid
            timers[pid]?.lastActive = now
            timers[pid]?.refusedUntil = nil
            timers[pid]?.activationGeneration += 1
        case .launched(let pid):
            timers[pid] = AppTimer(lastActive: now)
        case .terminated(let pid):
            timers[pid] = nil
            if protected == pid { protected = nil }
        case .hidden(let pid):
            timers[pid]?.wasVisible = false
        case .unhidden(let pid):
            timers[pid]?.lastActive = now
            timers[pid]?.refusedUntil = nil
            timers[pid]?.activationGeneration += 1
        case .spaceChanged:
            break
        case .away(let reason):
            awayReasons.insert(reason)
        case .returned(let reason):
            // Refreshes even when `reason` was never recorded: a missed willSleep
            // must not turn the wake into a burst of overdue hides.
            awayReasons.remove(reason)
            if awayReasons.isEmpty { refreshAll(now) }
        }
    }

    /// Reconcile with reality: add unseen PIDs (lastActive = now), drop PIDs not
    /// in obs.apps, appearance rule (wasVisible false → hasVisibleWindow true ⇒
    /// lastActive = max(lastActive, now)), set wasVisible, and treat a changed
    /// regular obs.frontmost as an activation. A locked obs records .lock; an
    /// unlocked one removes it.
    ///
    /// This is what makes a missed notification harmless: the heartbeat's
    /// observe rebuilds the PID set, visibility, and the protected app from
    /// what is actually on screen. A protected PID that is gone is cleared too.
    public mutating func observe(_ obs: Observation, at now: Date) {
        defer { stampAway(now) }
        if obs.screenLocked {
            // A lock the notification missed, or a start while locked. Recorded,
            // not just obeyed, so the unlock is a return with fresh timeouts
            // instead of a burst of hides for everything that aged meanwhile.
            awayReasons.insert(.lock)
        } else if awayReasons.contains(.lock) {
            // An unlock seen by the probe is a return (fresh timeouts), exactly as
            // if the screenIsUnlocked notification had arrived.
            apply(.returned(.lock), at: now)
        }

        let live = Set(obs.apps.map(\.pid))
        timers = timers.filter { live.contains($0.key) }
        if let current = protected, !live.contains(current) { protected = nil }

        for app in obs.apps {
            var timer = timers[app.pid] ?? AppTimer(lastActive: now)
            // Appearance rule: coming on screen (new window, unhide, switching to
            // this Space) gives a fresh timeout from that moment.
            if app.hasVisibleWindow && !timer.wasVisible {
                timer.lastActive = max(timer.lastActive, now)
            }
            timer.wasVisible = app.hasVisibleWindow
            timers[app.pid] = timer
        }

        // Only a regular app (one in obs.apps) can take over protection. Spotlight
        // or a menu-bar popover in front leaves the app you were in protected.
        // Nobody activates apps behind the lock screen, and treating whatever is
        // frontmost there as an activation would clear the lock just recorded.
        if !obs.screenLocked, let front = obs.frontmost, front != protected, live.contains(front) {
            apply(.activated(front), at: now)
        }
    }

    /// The heartbeat's answer to a missed return notification (didWake,
    /// screensDidWake, screenIsUnlocked, session switched back): without it,
    /// §3 step 3 never probes while away and hiding stops until the next app
    /// switch. The user is provably back when the session is on the console and
    /// unlocked, and either the lock was the only reason (unlocking is the
    /// return), or the display is awake and there was input within
    /// `presenceWindow` and after the away began. Input from before it (the
    /// lid-close right after willSleep) proves nothing. Every recorded reason
    /// then gets `.returned`, which refreshes every timer. Returns the reasons
    /// it cleared, sorted; empty when it changed nothing.
    @discardableResult
    public mutating func reconcileAway(_ presence: Presence, at now: Date) -> [AwayReason] {
        guard isAway, presence.onConsole, !presence.screenLocked else { return [] }
        let inputSinceAway = presence.lastInput.map {
            now.timeIntervalSince($0) < PolicyState.presenceWindow && $0 > (awaySince ?? .distantFuture)
        } ?? false
        guard awayReasons == [.lock] || (presence.displayAwake && inputSinceAway) else { return [] }
        let cleared = awayReasons.sorted { $0.rawValue < $1.rawValue }
        for reason in cleared { apply(.returned(reason), at: now) }
        return cleared
    }

    /// Keeps awaySince in step with awayReasons; apply and observe call it last.
    private mutating func stampAway(_ now: Date) {
        if awayReasons.isEmpty {
            awaySince = nil
        } else if awaySince == nil {
            awaySince = now
        }
    }

    /// refusedUntil = now + refusalBackoff, unless the app was activated or
    /// unhidden after the refused hide was issued. `generation` is the app's
    /// activationGeneration captured when the Engine issued the hide.
    ///
    /// Hider reports a refusal asynchronously (it confirms for up to 1 s), so
    /// it can land after the user switched to that very app, and even after
    /// they left it again. A refusal backs off "until the app's next
    /// activation", and that activation has already happened; recording it
    /// would keep the app on screen for five minutes after the user leaves it.
    /// Whether the app is protected when the refusal lands cannot tell, since
    /// the user may have come and gone; a changed generation can. Returns
    /// whether the backoff was recorded.
    @discardableResult
    public mutating func markRefused(_ pid: PID, generation: Int?, at now: Date) -> Bool {
        guard let timer = timers[pid], timer.activationGeneration == generation else { return false }
        timers[pid]?.refusedUntil = now + PolicyState.refusalBackoff
        return true
    }

    /// The last word on a queued hide, asked on the main-run-loop turn that
    /// issues it, against a probe taken on that turn (Engine.stillEligible).
    /// Other main-thread work can run in the turns between deciding and
    /// issuing, so focus, protection, suspensions, config, and the app's own
    /// visibility are judged again here by the same rules as `decide` and
    /// `mayHideNow`.
    ///
    /// The probe is not otherwise fed into the ledger: the next evaluate's
    /// observe does that. The one exception is a locked screen, recorded as
    /// `.lock` before withdrawing. Forgetting it would let an unlock that no
    /// evaluation saw skip the return, so apps that aged behind the lock
    /// screen would hide without a fresh timeout.
    public mutating func recheckHide(_ pid: PID, trigger: HideTrigger, observation: Observation,
                                     config: Config, now: Date) -> Bool {
        if observation.screenLocked {
            apply(.away(.lock), at: now)
            return false
        }
        guard !isAway, let app = observation.apps.first(where: { $0.pid == pid }) else { return false }
        switch trigger {
        case .timeout:
            return Policy.status(of: app, state: self, observation: observation, config: config, now: now) == .due
        case .hideOthers:
            return Policy.mayHideNow(app, state: self, observation: observation, config: config, now: now)
        }
    }

    /// A config change that makes an app hideable (its exemption removed, or
    /// newly inside `only_apps`) starts that app's timeout now, as coming on
    /// screen does. Its timer kept running while it could not hide, so without
    /// this it would hide the instant the exemption is lifted. Apps that were
    /// already hideable keep their timers: a shorter timeout applies at once.
    public mutating func configChanged(from old: Config, to new: Config, apps: [AppSnapshot], at now: Date) {
        for app in apps where old.effectiveTimeout(for: app.identity) == nil
            && new.effectiveTimeout(for: app.identity) != nil {
            guard let lastActive = timers[app.pid]?.lastActive else { continue }
            timers[app.pid]?.lastActive = max(lastActive, now)
        }
    }

    public mutating func pause(until: Date?, at now: Date) {         // nil → distantFuture
        pausedUntil = until ?? .distantFuture
    }

    /// Ending a pause is a return: every timer gets a fresh timeout. A resume
    /// while not paused changes nothing.
    public mutating func resume(at now: Date) {                      // pausedUntil = nil; refreshAll(now)
        guard pausedUntil != nil else { return }
        pausedUntil = nil
        refreshAll(now)
    }

    /// Expire a timed pause whose time has passed (→ resume(at:)).
    public mutating func tick(at now: Date) {
        if let until = pausedUntil, until <= now { resume(at: now) }
    }

    /// Every timer's lastActive = max(lastActive, now).
    public mutating func refreshAll(_ now: Date) {
        timers = timers.mapValues { timer in
            var timer = timer
            timer.lastActive = max(timer.lastActive, now)
            return timer
        }
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
    ///
    /// To answer "why did X hide?": X was not frontmost or protected, was on
    /// screen, and `status` said `.due`. Every rule lives in `status`; this
    /// function only collects its answers. A locked screen in the observation
    /// counts as away even before the lock notification arrives.
    public static func decide(state: PolicyState, observation: Observation,
                              config: Config, now: Date) -> Decision {
        if let suspension = suspension(state: state, observation: observation, now: now) {
            // Away and fullscreen end with an event (wake, unlock, Space change)
            // that re-evaluates. A timed pause has no event, so it needs a wake-up.
            guard suspension == .paused, let until = state.pausedUntil, until != .distantFuture else {
                return Decision()
            }
            return Decision(nextCheck: until)
        }

        var due: [(hideAt: Date, pid: PID)] = []
        var nextCheck: Date?
        for app in observation.apps {
            switch status(of: app, state: state, observation: observation, config: config, now: now) {
            case .due:
                if let hideAt = hideAt(app, state: state, config: config) { due.append((hideAt, app.pid)) }
            case .pending(hideAt: let wake), .refused(until: let wake):
                nextCheck = min(nextCheck ?? wake, wake)
            default:
                continue
            }
        }
        due.sort { ($0.hideAt, $0.pid) < ($1.hideAt, $1.pid) }
        return Decision(hide: due.map(\.pid), nextCheck: nextCheck)
    }

    /// Whether a manual "hide others now" may hide this app: anything `decide`
    /// would hide sooner or later, ignoring its timer and any refusal backoff.
    /// Built on `status`, so the manual and timed paths share every rule:
    /// nothing while away, locked, paused, or on a fullscreen Space, and never
    /// the frontmost, protected, exempt, hidden, or off-Space app.
    public static func mayHideNow(_ app: AppSnapshot, state: PolicyState,
                                  observation: Observation, config: Config, now: Date) -> Bool {
        switch status(of: app, state: state, observation: observation, config: config, now: now) {
        case .pending, .refused, .due: return true
        default: return false
        }
    }

    /// Checked in order; the first rule that applies wins. Facts about the app
    /// itself (protected, exempt, hidden, off-space) outrank the desktop-wide
    /// suspensions, so `autohide list` still says "hidden" while paused.
    public static func status(of app: AppSnapshot, state: PolicyState,
                              observation: Observation, config: Config, now: Date) -> AppStatus {
        if app.pid == observation.frontmost || app.pid == state.protected { return .protected }
        guard let timeout = config.effectiveTimeout(for: app.identity) else { return .exempt }
        if app.isHidden { return .hidden }
        if !app.hasVisibleWindow { return .offSpace }
        if let suspension = suspension(state: state, observation: observation, now: now) { return suspension }
        // Not tracked yet: the next observe starts its timer now.
        guard let hideAt = hideAt(app, state: state, config: config) else { return .pending(hideAt: now + timeout) }
        if let until = state.timers[app.pid]?.refusedUntil, until > now { return .refused(until: until) }
        return hideAt <= now ? .due : .pending(hideAt: hideAt)
    }

    /// Why nothing on this desktop hides right now, if anything. The order only
    /// matters for what `status` reports when several apply.
    private static func suspension(state: PolicyState, observation: Observation, now: Date) -> AppStatus? {
        if state.isAway || observation.screenLocked { return .away }
        if state.isPaused(at: now) { return .paused }
        if observation.onFullscreenSpace { return .fullscreenSpace }
        return nil
    }

    /// lastActive + the app's effective timeout. nil when it never hides or has no timer yet.
    private static func hideAt(_ app: AppSnapshot, state: PolicyState, config: Config) -> Date? {
        guard let timer = state.timers[app.pid], let timeout = config.effectiveTimeout(for: app.identity) else {
            return nil
        }
        return timer.lastActive + timeout
    }
}
