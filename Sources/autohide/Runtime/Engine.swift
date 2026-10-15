// Engine.swift — owns PolicyState, Config, timers; main-thread only.
// The hub of the daemon: events and probes go in, Policy decides, Hider acts.
// MenuBar and IPCServer reach it only through EngineControl, and only on main.

import AppKit
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

extension Notification.Name {
    /// Posted on main when the Engine's paused/active state flips (menu, IPC, or a
    /// timed pause expiring), so the menu-bar icon can follow without polling.
    static let engineStateDidChange = Notification.Name("com.autohide.engineStateDidChange")
}

/// Why a hide was requested. A timed hide must still be due when it is
/// issued; a hide-others hide needs only Policy.mayHideNow.
private enum HideTrigger: String {
    case timeout
    case hideOthers = "hide-others"
}

/// Runs the evaluation loop in design.md §3: reload config if changed, expire
/// pauses, observe, decide, hide, and re-arm one one-shot timer at nextCheck.
///
/// Wake-ups come from three places, all on the main run loop: WorkspaceEvents
/// (push), the one-shot deadline timer (the next hide or pause expiry), and a
/// 20 s heartbeat that reconciles anything the notifications missed. Between
/// them the process sleeps, which is what keeps idle CPU at ~0.
final class Engine: EngineControl {
    static let heartbeat: TimeInterval = 20
    /// Fire the deadline timer a hair late so `lastActive + timeout <= now` holds
    /// when it runs, instead of waking a few ms early and re-arming.
    static let deadlineLeeway: TimeInterval = 0.05
    /// Floor for the next wake-up. A policy that kept returning a past nextCheck
    /// would otherwise spin the main thread; this caps it at four evaluations a second.
    static let minimumRearm: TimeInterval = 0.25
    /// Follow-up probes after events whose effect reaches CGWindowList late: a
    /// launched app's first window (often a second or more after didLaunch), an
    /// unhidden app's windows, and the windows of a newly active Space. The
    /// appearance rule starts an app's timeout when its window is first SEEN, so
    /// without these it is first seen at the next heartbeat, and the app hides
    /// up to `heartbeat` seconds late (measured: 16–20 s instead of 10 s).
    static let settleDelays: [TimeInterval] = [0.25, 1, 2.5, 5, 10]

    private(set) var config: Config
    private let paths: ConfigPaths
    private let probe: WindowProbe
    private let hider: Hider

    private var state = PolicyState()
    private var events: WorkspaceEvents?
    private var deadlineTimer: Timer?
    private var heartbeatTimer: Timer?
    private var running = false
    private let startedAt = Date()
    /// The last probe result, kept for `status` (fullscreen Space) and the menu.
    private var lastObservation: Observation?
    /// PIDs with a hide queued or not yet completed. Hider confirms
    /// asynchronously (up to ~2 s), and without this a heartbeat or event
    /// evaluate in that window would issue a second hide for the same app.
    private var hidesInFlight: Set<PID> = []
    /// Hides decided but not yet issued, oldest deadline first. Each goes out
    /// on its own later main-run-loop turn; see scheduleHideTurn.
    private var queuedHides: [(pid: PID, trigger: HideTrigger)] = []
    private var hideTurnScheduled = false
    /// Pending settle probes (see `settleDelays`), sorted. They share the one
    /// deadline timer with hide deadlines; evaluate drops the ones that passed.
    private var settleChecks: [Date] = []
    /// Identity of config.json at the last load. A cheap stat per evaluate picks
    /// up hand edits and CLI writes without a file watcher.
    private var configStamp: FileStamp?
    private var publishedPaused = false

    init(paths: ConfigPaths, config: Config,
         probe: WindowProbe = WindowProbe(), hider: Hider = Hider()) {
        self.paths = paths
        self.config = config
        self.probe = probe
        self.hider = hider
        configStamp = FileStamp(paths.configFile)
    }

    func start() {         // WorkspaceEvents.start, first evaluate, heartbeat timer
        guard !running else { return }
        running = true
        Log.info("engine started: \(describe(config))")

        // WorkspaceEvents delivers on main, so the event can mutate state directly.
        let events = WorkspaceEvents { [weak self] event in self?.receive(event) }
        events.start()
        self.events = events

        // No timers survive a restart (design §2): this first observe gives every
        // running app a fresh timeout and protects whatever is frontmost.
        evaluate(reason: "start")

        let heartbeat = Timer(timeInterval: Self.heartbeat, repeats: true) { [weak self] _ in
            self?.heartbeatTick()
        }
        // 10% tolerance lets macOS coalesce this wake-up with others.
        heartbeat.tolerance = Self.heartbeat / 10
        // .common so the loop keeps running while a menu is open (event-tracking mode).
        RunLoop.main.add(heartbeat, forMode: .common)
        heartbeatTimer = heartbeat
    }

    func stop() {
        guard running else { return }
        running = false
        events?.stop()
        events = nil
        heartbeatTimer?.invalidate()
        heartbeatTimer = nil
        deadlineTimer?.invalidate()
        deadlineTimer = nil
        settleChecks.removeAll()
        // Nothing may hide after stop: queued hides are dropped, and in-flight
        // attempts end as .withdrawn before any AX fallback could run.
        queuedHides.removeAll()
        hider.cancelAll()
        hidesInFlight.removeAll()
        Log.info("engine stopped")
    }

    /// design.md §3, steps 1–7. Cheap enough to run on every event: one stat, one
    /// CGWindowList probe, and a pure decision.
    func evaluate(reason: String) {
        guard running else { return }
        let now = Date()

        reloadConfigIfChanged()                                    // 1
        let hadTimedPause = state.pausedUntil.map { $0 != .distantFuture } ?? false
        state.tick(at: now)                                        // 2
        if hadTimedPause, state.pausedUntil == nil {
            Log.info("pause expired: every app gets a fresh \(DurationFormat.format(config.timeout)) timeout")
        }
        publishPausedIfChanged(at: now)
        if state.isAway {                                          // 3
            // Nothing can hide while away; `.returned`, an activation, or the
            // heartbeat's presence check ends it and re-evaluates.
            deadlineTimer?.invalidate()
            deadlineTimer = nil
            Log.debug("evaluate(\(reason)): away \(state.awayReasons.map(\.rawValue).sorted())")
            return
        }

        let observation = probe.observe()                          // 4
        if observation.onFullscreenSpace != (lastObservation?.onFullscreenSpace ?? false) {
            Log.info(observation.onFullscreenSpace
                     ? "on a fullscreen Space (\(describeSpace())): not hiding"
                     : "off the fullscreen Space (\(describeSpace())): hiding resumes")
        }
        lastObservation = observation
        state.observe(observation, at: now)
        if state.isAway {
            // PolicyState.observe recorded a locked screen that no notification
            // reported (missed, or the daemon started while locked).
            Log.info("away: lock (the probe saw the screen locked; no notification arrived)")
        }

        let decision = Policy.decide(state: state, observation: observation,     // 5
                                     config: config, now: now)
        Log.debug(describe(decision, observation, reason: reason, now: now))

        for pid in decision.hide {                                 // 6
            enqueueHide(pid, trigger: .timeout)
        }
        settleChecks.removeAll { $0 <= now }
        arm([decision.nextCheck, settleChecks.first].compactMap { $0 }.min(), now: now)   // 7
    }

    func handle(_ request: IPC.Request) -> IPC.Response {   // IPCServer handler body
        switch request.command {
        case .status:
            evaluate(reason: "ipc status")
            return IPC.Response(ok: true, status: statusPayload())
        case .list:
            return IPC.Response(ok: true, apps: appRows())
        case .pause:
            if let seconds = request.seconds, !(seconds.isFinite && seconds > 0) {
                return IPC.Response(ok: false, error: "pause duration must be a positive number of seconds")
            }
            pause(seconds: request.seconds)
            return IPC.Response(ok: true, status: statusPayload())
        case .resume:
            resume()
            return IPC.Response(ok: true, status: statusPayload())
        case .hideOthers:
            return IPC.Response(ok: true, hidden: hideOthersNow())
        case .reload:
            if let failure = loadConfig(reason: "reload requested") {
                return IPC.Response(ok: false, error: "config not reloaded: \(failure)")
            }
            evaluate(reason: "reload")
            return IPC.Response(ok: true, status: statusPayload())
        case .quit:
            // The engine stops now so nothing else hides. The process exit belongs
            // to the daemon (IPCServer.onResponseSent in Daemon.swift): it runs only
            // after this reply is written, so the client sees ok:true, not EOF.
            Log.info("quit requested over IPC")
            stop()
            return IPC.Response(ok: true)
        }
    }

    // MARK: EngineControl

    var protectedApp: AppIdentity? {
        guard let pid = state.protected,
              let app = NSRunningApplication(processIdentifier: pid) else { return nil }
        return WindowProbe.identity(of: app)
    }

    func statusPayload() -> IPC.StatusPayload {
        let now = Date()
        let paused = state.isPaused(at: now)
        let until = state.pausedUntil
        return IPC.StatusPayload(
            pid: getpid(),
            version: AutohideVersion.current,
            startedAt: startedAt,
            paused: paused,
            pausedUntil: paused && until != .distantFuture ? until : nil,
            away: state.isAway,
            fullscreenSpace: lastObservation?.onFullscreenSpace ?? false,
            timeout: config.timeout,
            exempt: config.exempt,
            appTimeouts: config.appTimeouts,
            onlyApps: config.onlyApps,
            protectedApp: protectedApp?.name,
            configPath: paths.configFile.path,
            logPath: paths.logFile.path,
            accessibility: Hider.accessibilityTrusted)
    }

    func appRows() -> [IPC.AppRow] {
        // Refresh first so rows reflect this instant. While away evaluate does not
        // probe, so take a read-only look instead of feeding the ledger.
        evaluate(reason: "ipc list")
        let observation = state.isAway ? probe.observe() : (lastObservation ?? probe.observe())
        let now = Date()
        return observation.apps
            .map { app -> IPC.AppRow in
                let status = Policy.status(of: app, state: state, observation: observation,
                                           config: config, now: now)
                var hidesIn: Double?
                if case .pending(let hideAt) = status { hidesIn = max(0, hideAt.timeIntervalSince(now)) }
                return IPC.AppRow(pid: app.pid, name: app.identity.name, bundleID: app.identity.bundleID,
                                  state: status.label, hidesIn: hidesIn,
                                  timeout: config.effectiveTimeout(for: app.identity))
            }
            .sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
    }

    func pause(seconds: TimeInterval?) {
        let now = Date()
        state.pause(until: seconds.map { now.addingTimeInterval($0) }, at: now)
        // In-flight attempts must not reach their AX fallback while paused;
        // queued ones are withdrawn by stillEligible when their turn comes.
        hider.cancelAll()
        Log.info(seconds.map { "paused for \(DurationFormat.format($0))" } ?? "paused until resumed")
        evaluate(reason: "pause")
        publishPausedIfChanged(at: now)
    }

    func resume() {
        let now = Date()
        state.resume(at: now)
        Log.info("resumed: every app gets a fresh \(DurationFormat.format(config.timeout)) timeout")
        evaluate(reason: "resume")
        publishPausedIfChanged(at: now)
    }

    /// Hides every visible app that config allows to hide, ignoring timers but
    /// not suspensions: Policy.mayHideNow is the timed path's own rule, so this
    /// does nothing while away, locked, paused, or on a fullscreen Space, and
    /// never touches the frontmost, protected, or exempt app. Returns how many
    /// hides were queued; each is re-checked when issued, and the outcomes are
    /// logged like timed hides.
    func hideOthersNow() -> Int {
        guard running else { return 0 }
        // Reloads config, expires a timed pause, and refreshes the ledger first.
        evaluate(reason: "hide-others")
        let now = Date()
        guard !state.isAway, let observation = lastObservation else {
            Log.info("hide-others ignored: away")
            return 0
        }
        let targets = observation.apps.filter {
            Policy.mayHideNow($0, state: state, observation: observation, config: config, now: now)
        }
        var queued = 0
        for app in targets where enqueueHide(app.pid, trigger: .hideOthers) {
            queued += 1
        }
        Log.info("hide-others: queued \(queued) hides (\(observation.apps.count) apps seen)")
        return queued
    }

    func reloadConfig() {
        loadConfig(reason: "reload requested")
        evaluate(reason: "reload")
    }

    func setTimeout(_ seconds: TimeInterval) {
        guard seconds.isFinite, seconds > 0 else { return }
        var updated = config
        updated.timeout = seconds
        persist(updated, change: "timeout set to \(DurationFormat.format(seconds))")
    }

    func setExempt(_ app: String, _ exempt: Bool) {
        var updated = config
        updated.setExempt(app, exempt)
        persist(updated, change: exempt ? "exempted \"\(app)\"" : "no longer exempt: \"\(app)\"")
    }

    // MARK: - Events and timers

    private func receive(_ event: PolicyEvent) {
        let now = Date()
        switch event {
        case .away(let reason): Log.info("away: \(reason.rawValue)")
        case .returned(let reason): Log.info("returned: \(reason.rawValue)")
        case .spaceChanged:
            // Rare, so always logged. It is also the live evidence for what
            // SpaceProbe reads on a fullscreen Space, which spike.md Q4 never saw.
            Log.info("space changed: \(describeSpace())")
        default: Log.debug("event \(event)")
        }
        let wasAway = state.isAway
        state.apply(event, at: now)
        switch event {
        case .launched, .unhidden, .spaceChanged, .returned:
            scheduleSettleChecks(after: now)
        default:
            break
        }
        // An activation is proof the user is back (design §7.2), even if the
        // matching wake/unlock notification never arrived.
        if wasAway, !state.isAway, case .activated = event {
            Log.info("returned: activation cleared away")
        }
        evaluate(reason: "\(event)")
    }

    /// While away, evaluate never probes, so a missed return notification would
    /// stop hiding until the next app switch. The heartbeat asks the session
    /// instead; PolicyState.reconcileAway decides whether that proves a return.
    private func heartbeatTick() {
        if running, state.isAway {
            let presence = WindowProbe.presence()
            let cleared = state.reconcileAway(presence, at: Date())
            if !cleared.isEmpty {
                let input = presence.lastInput.map { String(format: "%.0fs ago", -$0.timeIntervalSinceNow) } ?? "unknown"
                Log.info("returned: \(cleared.map(\.rawValue).joined(separator: ", ")) (heartbeat: on console, unlocked, "
                    + "display awake=\(presence.displayAwake), last input \(input); the return notification was missed)")
            }
        }
        evaluate(reason: "heartbeat")
    }

    private func scheduleSettleChecks(after now: Date) {
        settleChecks.append(contentsOf: Self.settleDelays.map { now.addingTimeInterval($0) })
        settleChecks.sort()
    }

    /// One one-shot timer, replaced on every evaluate, so there is never more than
    /// one pending deadline wake-up no matter how many apps are counting down.
    private func arm(_ nextCheck: Date?, now: Date) {
        deadlineTimer?.invalidate()
        deadlineTimer = nil
        guard let nextCheck else { return }
        let fireAt = max(nextCheck.addingTimeInterval(Self.deadlineLeeway),
                         now.addingTimeInterval(Self.minimumRearm))
        let timer = Timer(fire: fireAt, interval: 0, repeats: false) { [weak self] _ in
            self?.evaluate(reason: "deadline")
        }
        timer.tolerance = 0.1
        RunLoop.main.add(timer, forMode: .common)
        deadlineTimer = timer
    }

    // MARK: - Hiding

    /// Queues one hide unless that app already has one queued or in flight.
    /// Returns whether it was queued.
    @discardableResult
    private func enqueueHide(_ pid: PID, trigger: HideTrigger) -> Bool {
        guard !hidesInFlight.contains(pid) else {
            Log.debug("hide pid=\(pid) skipped: already in flight")
            return false
        }
        hidesInFlight.insert(pid)
        queuedHides.append((pid, trigger))
        scheduleHideTurn()
        return true
    }

    /// Hides go out one per main-run-loop turn, never from the evaluation that
    /// decided them. NSWorkspace and NSRunningApplication focus state is a
    /// snapshot refreshed between turns (spike gotcha 2): hides issued in the
    /// deciding turn, or a batch issued in one turn, would all act on focus as
    /// it was when that turn began. A block enqueued from inside a running
    /// main-queue block runs on a later run-loop iteration (measured), so
    /// chaining one block per hide gives each a snapshot taken after the
    /// previous hide was sent.
    private func scheduleHideTurn() {
        guard !hideTurnScheduled, !queuedHides.isEmpty else { return }
        hideTurnScheduled = true
        DispatchQueue.main.async { [weak self] in self?.issueNextHide() }
    }

    private func issueNextHide() {
        hideTurnScheduled = false
        guard !queuedHides.isEmpty else { return }
        let (pid, trigger) = queuedHides.removeFirst()
        defer { scheduleHideTurn() }
        guard running else {
            hidesInFlight.remove(pid)
            return
        }
        let now = Date()
        let identity = lastObservation?.apps.first { $0.pid == pid }?.identity ?? AppIdentity(name: "pid \(pid)")
        let idle = state.timers[pid].map { now.timeIntervalSince($0.lastActive) }
        let timeout = config.effectiveTimeout(for: identity)
        Log.debug("hide pid=\(pid) issued by=\(trigger.rawValue) (\(queuedHides.count) still queued)")

        // Hider asks stillEligible on this turn right before hide(), and again
        // right before any AX fallback, each in the same turn as the act. That is
        // as close as macOS allows: there is no atomic "hide unless active", so a
        // click on the app that WindowServer is delivering while hide() is being
        // sent can still lose to it. The completion arrives on main, possibly
        // ~2 s later, and only records the outcome; the next evaluate re-decides.
        hider.hide(pid, stillEligible: { [weak self] in
            self?.stillEligible(pid, trigger) ?? false
        }, completion: { [weak self] result in
            self?.hideFinished(pid, identity: identity, result: result,
                               idle: idle, timeout: timeout, trigger: trigger)
        })
    }

    /// Hider's last-moment check. It takes a fresh probe rather than reusing the
    /// deciding evaluation's: focus, protection, pause, away or lock, the
    /// fullscreen Space, config, and the app's own visibility can all change in
    /// the turns between deciding and acting. The probe is not fed into the
    /// ledger (no state.observe); the next evaluate reconciles as usual.
    private func stillEligible(_ pid: PID, _ trigger: HideTrigger) -> Bool {
        guard running, !state.isAway else { return false }
        reloadConfigIfChanged()
        let observation = probe.observe()
        guard let app = observation.apps.first(where: { $0.pid == pid }) else { return false }
        let now = Date()
        switch trigger {
        case .timeout:
            return Policy.status(of: app, state: state, observation: observation, config: config, now: now) == .due
        case .hideOthers:
            return Policy.mayHideNow(app, state: state, observation: observation, config: config, now: now)
        }
    }

    private func hideFinished(_ pid: PID, identity: AppIdentity, result: HideResult,
                              idle: TimeInterval?, timeout: TimeInterval?, trigger: HideTrigger) {
        hidesInFlight.remove(pid)
        let app = "app=\"\(identity.name)\" pid=\(pid)"
        switch result {
        case .hidden:
            let idleText = idle.map { String(format: "%.1fs", $0) } ?? "-"
            let timeoutText = timeout.map(DurationFormat.format) ?? "-"
            Log.info("hid \(app) idle=\(idleText) timeout=\(timeoutText) by=\(trigger.rawValue)")
        case .alreadyHidden:
            Log.debug("hide \(app): already hidden")
        case .gone:
            Log.debug("hide \(app): process gone")
        case .becameFrontmost:
            // The user switched to it while the hide was in flight. Its
            // activation reset the timer; there is nothing to back off from.
            Log.debug("hide \(app): skipped, it became frontmost")
        case .withdrawn:
            Log.debug("hide \(app): withdrawn, no longer eligible by=\(trigger.rawValue)")
        case .refused(let reason):
            // Back off instead of retrying every evaluate (diagnosis R8). One line
            // per refusal; the next attempt is at least refusalBackoff away.
            // markRefused ignores the app if the user has switched to it since.
            state.markRefused(pid, at: Date())
            Log.warn("hide refused \(app) reason=\"\(reason)\" retry_in=\(DurationFormat.format(PolicyState.refusalBackoff))")
        }
    }

    // MARK: - Config

    private func reloadConfigIfChanged() {
        guard FileStamp(paths.configFile) != configStamp else { return }
        loadConfig(reason: "config file changed")
    }

    /// Loads config.json. On failure the previous config stays in force; the
    /// stamp still advances so one bad edit is reported once, not every evaluate.
    @discardableResult
    private func loadConfig(reason: String) -> String? {
        let stamp = FileStamp(paths.configFile)
        do {
            let loaded = try ConfigStore.load(paths.configFile)
            configStamp = stamp
            if loaded != config {
                state.configChanged(from: config, to: loaded, apps: lastObservation?.apps ?? [], at: Date())
                config = loaded
                Log.info("config reloaded (\(reason)): \(describe(loaded))")
            }
            return nil
        } catch {
            configStamp = stamp
            Log.warn("config reload failed (\(reason)), keeping the previous config: \(error)")
            return "\(error)"
        }
    }

    /// Menu and IPC edits take effect immediately, and are written to config.json
    /// so the CLI, a hand edit, and the next start all see the same settings. A
    /// failed save still applies in memory (the user asked for it) and is logged.
    private func persist(_ updated: Config, change: String) {
        state.configChanged(from: config, to: updated, apps: lastObservation?.apps ?? [], at: Date())
        config = updated
        do {
            try ConfigStore.save(updated, to: paths.configFile)
            configStamp = FileStamp(paths.configFile)
            Log.info("config: \(change)")
        } catch {
            Log.error("config: \(change), but saving \(paths.configFile.path) failed: \(error)")
        }
        evaluate(reason: "config changed")
    }

    private func describeSpace() -> String {
        "space type=\(SpaceProbe.activeSpaceType().map(String.init) ?? "unknown")"
    }

    private func describe(_ config: Config) -> String {
        var parts = ["timeout=\(DurationFormat.format(config.timeout))", "exempt=\(config.exempt)"]
        if !config.appTimeouts.isEmpty {
            parts.append("app_timeouts=\(config.appTimeouts.mapValues(DurationFormat.format))")
        }
        if !config.onlyApps.isEmpty { parts.append("only_apps=\(config.onlyApps)") }
        return parts.joined(separator: " ")
    }

    private func describe(_ decision: Decision, _ observation: Observation, reason: String, now: Date) -> String {
        let frontmost = observation.frontmost.map { String($0) } ?? "-"
        let protected = state.protected.map { String($0) } ?? "-"
        let next = decision.nextCheck.map { String(format: "%.1fs", $0.timeIntervalSince(now)) } ?? "-"
        return "evaluate(\(reason)): apps=\(observation.apps.count) frontmost=\(frontmost) "
            + "protected=\(protected) hide=\(decision.hide) next=\(next)"
    }

    private func publishPausedIfChanged(at now: Date) {
        let paused = state.isPaused(at: now)
        guard paused != publishedPaused else { return }
        publishedPaused = paused
        NotificationCenter.default.post(name: .engineStateDidChange, object: self)
    }
}

/// Enough of stat(2) to tell that config.json was replaced or rewritten. The
/// CLI saves atomically (new inode via rename), and an in-place editor changes
/// mtime/size, so either kind of write changes the stamp. nil = no file.
private struct FileStamp: Equatable {
    var inode: UInt64
    var size: Int64
    var seconds: Int
    var nanoseconds: Int

    init?(_ url: URL) {
        var info = stat()
        guard stat(url.path, &info) == 0 else { return nil }
        inode = info.st_ino
        size = info.st_size
        seconds = info.st_mtimespec.tv_sec
        nanoseconds = info.st_mtimespec.tv_nsec
    }
}
