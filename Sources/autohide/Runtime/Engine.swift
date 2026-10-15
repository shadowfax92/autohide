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
    /// PIDs whose Hider call has not completed yet. Hider confirms asynchronously
    /// (up to ~1.5 s), and without this a heartbeat or event evaluate in that
    /// window would issue a second hide for the same app.
    private var hidesInFlight: Set<PID> = []
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
            // Nothing can hide while away; `.returned` or an activation re-evaluates.
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

        let decision = Policy.decide(state: state, observation: observation,     // 5
                                     config: config, now: now)
        Log.debug(describe(decision, observation, reason: reason, now: now))

        for pid in decision.hide {                                 // 6
            requestHide(pid, observation: observation, now: now, trigger: "timeout")
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

    /// Hides every visible app that config allows to hide, ignoring timers. The
    /// frontmost and protected apps, exempt apps, and apps outside `only_apps`
    /// are never touched. Pause still wins. Returns how many hides were issued;
    /// their outcomes arrive asynchronously and are logged like timed hides.
    func hideOthersNow() -> Int {
        guard running else { return 0 }
        // Reloads config, expires a timed pause, and refreshes the ledger first.
        evaluate(reason: "hide-others")
        let now = Date()
        guard !state.isPaused(at: now) else {
            Log.info("hide-others ignored: paused")
            return 0
        }
        let observation = state.isAway ? probe.observe() : (lastObservation ?? probe.observe())

        let targets = observation.apps.filter { app in
            !app.isHidden && app.hasVisibleWindow
                && app.pid != observation.frontmost && app.pid != state.protected
                && config.effectiveTimeout(for: app.identity) != nil
        }
        var issued = 0
        for app in targets where requestHide(app.pid, observation: observation, now: now, trigger: "hide-others") {
            issued += 1
        }
        Log.info("hide-others: issued \(issued) hides (\(observation.apps.count) apps seen)")
        return issued
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

    /// §3 step 3 skips the probe while away, which would leave a missed
    /// screenIsUnlocked stuck until the next app switch. So when the lock is the
    /// only away reason, the heartbeat reads the probe's session lock state, and an
    /// unlocked screen counts as the return (fresh timeouts). While still locked
    /// the ledger is left alone. Sleep and display sleep still end only by event
    /// or activation: their probe answers mean nothing while asleep.
    private func heartbeatTick() {
        if running, state.awayReasons == [.lock], !probe.observe().screenLocked {
            Log.info("returned: lock (heartbeat saw the screen unlocked; the notification was missed)")
            state.apply(.returned(.lock), at: Date())
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

    /// Issues one hide unless it is already in flight or the app just became
    /// frontmost. Returns whether a hide was issued.
    @discardableResult
    private func requestHide(_ pid: PID, observation: Observation, now: Date, trigger: String) -> Bool {
        guard !hidesInFlight.contains(pid) else {
            Log.debug("hide pid=\(pid) skipped: already in flight")
            return false
        }
        // Last-moment guard (design §7.3): never hide what NSWorkspace calls
        // frontmost, whatever the observation or Policy said. NSWorkspace state is a
        // per-run-loop-turn snapshot (spike gotcha 2), so this cannot see a switch
        // that happens during this turn; activation resetting lastActive covers that.
        if NSWorkspace.shared.frontmostApplication?.processIdentifier == pid {
            Log.debug("hide pid=\(pid) skipped: became frontmost")
            return false
        }
        let identity = observation.apps.first { $0.pid == pid }?.identity
            ?? AppIdentity(name: "pid \(pid)")
        let idle = state.timers[pid].map { now.timeIntervalSince($0.lastActive) }
        let timeout = config.effectiveTimeout(for: identity)

        hidesInFlight.insert(pid)
        // Hider completes on main, possibly ~1.5 s later (it waits for didHide or
        // an isHidden poll). Everything about the app may have changed by then, so
        // the completion only records the outcome; the next evaluate re-decides.
        hider.hide(pid) { [weak self] result in
            self?.hideFinished(pid, identity: identity, result: result,
                               idle: idle, timeout: timeout, trigger: trigger)
        }
        return true
    }

    private func hideFinished(_ pid: PID, identity: AppIdentity, result: HideResult,
                              idle: TimeInterval?, timeout: TimeInterval?, trigger: String) {
        hidesInFlight.remove(pid)
        let app = "app=\"\(identity.name)\" pid=\(pid)"
        switch result {
        case .hidden:
            let idleText = idle.map { String(format: "%.1fs", $0) } ?? "-"
            let timeoutText = timeout.map(DurationFormat.format) ?? "-"
            Log.info("hid \(app) idle=\(idleText) timeout=\(timeoutText) by=\(trigger)")
        case .alreadyHidden:
            Log.debug("hide \(app): already hidden")
        case .gone:
            Log.debug("hide \(app): process gone")
        case .becameFrontmost:
            // The user switched to it while the hide was in flight. Its
            // activation reset the timer; there is nothing to back off from.
            Log.debug("hide \(app): skipped, it became frontmost")
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
