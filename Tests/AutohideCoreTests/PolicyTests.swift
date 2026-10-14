import XCTest
import AutohideCore

private let t0 = Date(timeIntervalSince1970: 1_000_000)

private func app(_ pid: PID, _ name: String, bundleID: String? = nil,
                 hidden: Bool = false, visible: Bool = true) -> AppSnapshot {
    AppSnapshot(pid: pid, identity: AppIdentity(name: name, bundleID: bundleID),
                isHidden: hidden, hasVisibleWindow: visible && !hidden)
}

/// A scripted desktop that drives PolicyState the way the Engine does (§3):
/// events are applied as they arrive, and each evaluation ticks, observes
/// (skipped while away), and decides. Time moves only when a test moves it.
private struct Desk {
    var state = PolicyState()
    var config = Config(exempt: [])
    var now = t0
    var apps: [AppSnapshot]
    var frontmost: PID?
    var fullscreen = false
    var locked = false

    init(_ apps: [AppSnapshot], frontmost: PID?) {
        self.apps = apps
        self.frontmost = frontmost
    }

    var observation: Observation {
        Observation(frontmost: frontmost, apps: apps, onFullscreenSpace: fullscreen, screenLocked: locked)
    }

    mutating func advance(_ seconds: TimeInterval) { now += seconds }
    mutating func at(_ seconds: TimeInterval) { now = t0 + seconds }
    mutating func send(_ event: PolicyEvent) { state.apply(event, at: now) }

    mutating func evaluate() -> Decision {
        state.tick(at: now)
        if !state.isAway { state.observe(observation, at: now) }
        return Policy.decide(state: state, observation: observation, config: config, now: now)
    }

    func status(_ pid: PID) -> AppStatus {
        Policy.status(of: apps.first { $0.pid == pid }!, state: state,
                      observation: observation, config: config, now: now)
    }

    func lastActive(_ pid: PID) -> Date? { state.timers[pid]?.lastActive }

    mutating func update(_ pid: PID, hidden: Bool? = nil, visible: Bool? = nil) {
        let index = apps.firstIndex { $0.pid == pid }!
        if let hidden { apps[index].isHidden = hidden }
        if let visible { apps[index].hasVisibleWindow = visible }
    }
}

/// The timer rules of design.md §4.3. Tests named `testBnn_…` are the
/// behaviours §6 requires, numbered as there.
final class PolicyTests: XCTestCase {
    private let terminal: PID = 101, notes: PID = 102, preview: PID = 103, chess: PID = 104

    private func desk(frontmost: PID? = 101) -> Desk {
        Desk([app(101, "Terminal"), app(102, "Notes"), app(103, "Preview")], frontmost: frontmost)
    }

    // MARK: - §6 behaviours

    func testB01_ProtectedAppIsNeverHiddenEvenOverdue() {
        var desk = desk()
        XCTAssertEqual(desk.evaluate().hide, [])
        XCTAssertEqual(desk.state.protected, terminal)
        desk.advance(3600)
        XCTAssertEqual(desk.evaluate().hide, [notes, preview], "Terminal is overdue too, but protected")
        XCTAssertEqual(desk.status(terminal), .protected)
    }

    func testB01_FrontmostIsNeverHiddenEvenBeforeItIsProtected() {
        var state = PolicyState()
        for pid in [terminal, notes, preview] { state.timers[pid] = AppTimer(lastActive: t0, wasVisible: true) }
        state.protected = terminal
        let observation = Observation(frontmost: notes, apps: [app(101, "Terminal"), app(102, "Notes"), app(103, "Preview")],
                                      onFullscreenSpace: false)
        let decision = Policy.decide(state: state, observation: observation, config: Config(exempt: []), now: t0 + 3600)
        XCTAssertEqual(decision.hide, [preview])
    }

    func testB02_VisibleAppIsDueExactlyAtLastActivePlusTimeout() {
        var desk = desk()
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: t0 + 60))
        desk.at(59.999)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: t0 + 60))
        XCTAssertEqual(desk.status(notes), .pending(hideAt: t0 + 60))
        desk.at(60)
        XCTAssertEqual(desk.status(notes), .due)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [notes, preview], nextCheck: nil))
    }

    func testB02_NextCheckIsTheEarliestDeadline() {
        var desk = desk()
        desk.update(preview, visible: false)
        _ = desk.evaluate()
        desk.at(30)
        desk.update(preview, visible: true)
        XCTAssertEqual(desk.evaluate().nextCheck, t0 + 60, "Notes (t0+60) is earlier than Preview (t0+90)")
        desk.at(60)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [notes], nextCheck: t0 + 90))
    }

    func testB03_ActivatingAnotherAppStartsTheOldAppsTimerAtThatMoment() {
        var desk = desk()
        _ = desk.evaluate()
        desk.at(100)
        desk.frontmost = notes
        desk.send(.activated(notes))
        XCTAssertEqual(desk.lastActive(terminal), t0 + 100)
        XCTAssertEqual(desk.lastActive(notes), t0 + 100)
        XCTAssertEqual(desk.state.protected, notes)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [preview], nextCheck: t0 + 160))
        desk.send(.hidden(preview))
        desk.update(preview, hidden: true, visible: false)
        desk.at(160)
        XCTAssertEqual(desk.evaluate().hide, [terminal])
    }

    func testB04_ExemptAppsAreNeverHiddenMatchingIgnoresCaseAndFormatCharacters() {
        var desk = Desk([app(101, "Terminal"), app(102, "\u{200E}WhatsApp"), app(103, "Preview", bundleID: "com.apple.Preview")],
                        frontmost: terminal)
        desk.config = Config(exempt: ["whatsapp", "COM.APPLE.PREVIEW"])
        _ = desk.evaluate()
        desk.advance(3600)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: nil))
        XCTAssertEqual(desk.status(102), .exempt)
        XCTAssertEqual(desk.status(103), .exempt)
    }

    func testB04_AppsOutsideANonEmptyOnlyAppsAreNeverHidden() {
        var desk = desk()
        desk.config = Config(exempt: [], onlyApps: ["\u{200E}PREVIEW"])
        _ = desk.evaluate()
        desk.advance(3600)
        XCTAssertEqual(desk.evaluate().hide, [preview])
        XCTAssertEqual(desk.status(notes), .exempt)
    }

    func testB05_HiddenAppsAndAppsWithoutAVisibleWindowAreSkipped() {
        var desk = desk()
        desk.update(notes, hidden: true)
        desk.update(preview, visible: false)
        _ = desk.evaluate()
        desk.advance(3600)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: nil))
        XCTAssertEqual(desk.status(notes), .hidden)
        XCTAssertEqual(desk.status(preview), .offSpace)
    }

    func testB05_AppearingOnScreenGivesAFreshTimeout() {
        var desk = desk()
        desk.update(preview, visible: false)
        _ = desk.evaluate()
        desk.at(500)
        desk.update(preview, visible: true)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [notes], nextCheck: t0 + 560))
        XCTAssertEqual(desk.lastActive(preview), t0 + 500)
    }

    func testB05_StayingOnScreenDoesNotRefreshTheTimer() {
        var desk = desk()
        _ = desk.evaluate()
        desk.at(30)
        _ = desk.evaluate()
        XCTAssertEqual(desk.lastActive(notes), t0)
    }

    func testB05_HiddenThenShownAgainIsAnAppearance() {
        var desk = desk()
        _ = desk.evaluate()
        desk.at(60)
        XCTAssertEqual(desk.evaluate().hide, [notes, preview])
        desk.send(.hidden(notes))
        desk.update(notes, hidden: true, visible: false)
        XCTAssertEqual(desk.state.timers[notes]?.wasVisible, false)
        desk.at(200)
        desk.send(.unhidden(notes))
        desk.update(notes, hidden: false, visible: true)
        XCTAssertEqual(desk.lastActive(notes), t0 + 200)
        desk.at(201)
        _ = desk.evaluate()
        XCTAssertEqual(desk.lastActive(notes), t0 + 201, "the appearance rule takes the later time")
    }

    func testB06_AwaySuspendsHidingAndClearingTheLastReasonRefreshesEveryTimer() {
        var desk = desk()
        _ = desk.evaluate()
        desk.at(10)
        desk.send(.away(.lock))
        desk.send(.away(.sleep))
        desk.at(1000)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: nil))
        XCTAssertEqual(desk.status(notes), .away)
        desk.send(.returned(.lock))
        XCTAssertTrue(desk.state.isAway)
        XCTAssertEqual(desk.lastActive(notes), t0, "still asleep: no refresh yet")
        desk.at(1100)
        desk.send(.returned(.sleep))
        XCTAssertFalse(desk.state.isAway)
        XCTAssertEqual(desk.lastActive(notes), t0 + 1100)
        XCTAssertEqual(desk.lastActive(preview), t0 + 1100)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: t0 + 1160), "no burst of hides on return")
    }

    func testB06_AnActivationClearsAwayAndRefreshes() {
        var desk = desk()
        _ = desk.evaluate()
        desk.send(.away(.displaySleep))
        desk.at(400)
        desk.frontmost = notes
        desk.send(.activated(notes))
        XCTAssertFalse(desk.state.isAway)
        XCTAssertEqual(desk.lastActive(preview), t0 + 400)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: t0 + 460))
    }

    func testB06_AnUnlockedObservationClearsAStuckLock() {
        var desk = desk()
        _ = desk.evaluate()
        desk.send(.away(.lock))
        desk.at(300)
        desk.locked = true
        desk.state.observe(desk.observation, at: desk.now)
        XCTAssertEqual(desk.state.awayReasons, [.lock])
        desk.locked = false
        desk.state.observe(desk.observation, at: desk.now)
        XCTAssertFalse(desk.state.isAway)
        XCTAssertEqual(desk.lastActive(notes), t0 + 300, "an unlock is a return")
    }

    func testB06_ALockedObservationSuspendsHidingEvenWithoutALockEvent() {
        var desk = desk()
        _ = desk.evaluate()
        desk.at(3600)
        desk.locked = true
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: nil))
        XCTAssertEqual(desk.status(notes), .away)
    }

    func testB07_PauseSuspendsHidingUntilPausedUntil() {
        var desk = desk()
        _ = desk.evaluate()
        desk.state.pause(until: t0 + 100, at: desk.now)
        desk.at(90)
        XCTAssertTrue(desk.state.isPaused(at: desk.now))
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: t0 + 100))
        XCTAssertEqual(desk.status(notes), .paused)
        desk.at(99)
        desk.state.tick(at: desk.now)
        XCTAssertNotNil(desk.state.pausedUntil)
    }

    func testB07_TickPastPausedUntilResumesAndRefreshes() {
        var desk = desk()
        _ = desk.evaluate()
        desk.state.pause(until: t0 + 100, at: desk.now)
        desk.at(100)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: t0 + 160))
        XCTAssertNil(desk.state.pausedUntil)
        XCTAssertEqual(desk.lastActive(notes), t0 + 100)
    }

    func testB07_PauseUntilResumedHasNoWakeUpAndResumeRefreshes() {
        var desk = desk()
        _ = desk.evaluate()
        desk.state.pause(until: nil, at: desk.now)
        XCTAssertEqual(desk.state.pausedUntil, .distantFuture)
        desk.at(86_400)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: nil))
        desk.state.resume(at: desk.now)
        XCTAssertFalse(desk.state.isPaused(at: desk.now))
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: t0 + 86_460))
    }

    func testB08_ARefusedAppIsSkippedUntilRefusedUntil() {
        var desk = desk()
        _ = desk.evaluate()
        desk.at(60)
        XCTAssertEqual(desk.evaluate().hide, [notes, preview])
        desk.state.markRefused(notes, at: desk.now)
        XCTAssertEqual(desk.state.timers[notes]?.refusedUntil, t0 + 60 + PolicyState.refusalBackoff)
        desk.at(61)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [preview], nextCheck: t0 + 360))
        XCTAssertEqual(desk.status(notes), .refused(until: t0 + 360))
        desk.at(360)
        XCTAssertEqual(desk.evaluate().hide, [notes, preview])
    }

    func testB08_ActivationClearsARefusal() {
        var desk = desk()
        _ = desk.evaluate()
        desk.state.markRefused(notes, at: t0)
        desk.at(10)
        desk.frontmost = notes
        desk.send(.activated(notes))
        XCTAssertNil(desk.state.timers[notes]?.refusedUntil)
        desk.state.markRefused(preview, at: desk.now)
        desk.send(.unhidden(preview))
        XCTAssertNil(desk.state.timers[preview]?.refusedUntil, "an unhide clears it too")
    }

    func testB09_AFullscreenSpaceSuspendsHiding() {
        var desk = desk()
        _ = desk.evaluate()
        desk.at(3600)
        desk.fullscreen = true
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: nil))
        XCTAssertEqual(desk.status(notes), .fullscreenSpace)
        desk.fullscreen = false
        XCTAssertEqual(desk.evaluate().hide, [notes, preview])
    }

    func testB10_ATerminatedPIDIsDroppedAndARelaunchGetsAFreshTimer() {
        var desk = desk()
        _ = desk.evaluate()
        desk.at(50)
        desk.send(.terminated(notes))
        desk.apps.removeAll { $0.pid == notes }
        XCTAssertNil(desk.state.timers[notes])
        let relaunched: PID = 202
        desk.send(.launched(relaunched))
        desk.apps.append(app(relaunched, "Notes"))
        XCTAssertEqual(desk.lastActive(relaunched), t0 + 50)
        _ = desk.evaluate()
        desk.at(60)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [preview], nextCheck: t0 + 110))
    }

    func testB10_TerminatingTheProtectedAppUnprotectsIt() {
        var desk = desk()
        _ = desk.evaluate()
        desk.send(.terminated(terminal))
        XCTAssertNil(desk.state.protected)
        XCTAssertNil(desk.state.timers[terminal])
    }

    func testB10_ObserveDropsPIDsThatAreGone() {
        var desk = desk()
        _ = desk.evaluate()
        desk.apps.removeAll { $0.pid == notes || $0.pid == terminal }
        desk.frontmost = nil
        _ = desk.evaluate()
        XCTAssertEqual(Set(desk.state.timers.keys), [preview])
        XCTAssertNil(desk.state.protected, "a protected app that vanished is not kept")
    }

    func testB11_ObserveWithANewRegularFrontmostBehavesLikeActivated() {
        var observed = desk()
        _ = observed.evaluate()
        var notified = observed
        observed.at(30)
        notified.at(30)
        observed.frontmost = notes
        notified.frontmost = notes
        notified.send(.activated(notes))
        observed.state.observe(observed.observation, at: observed.now)
        notified.state.observe(notified.observation, at: notified.now)
        XCTAssertEqual(observed.state, notified.state)
        XCTAssertEqual(observed.state.protected, notes)
        XCTAssertEqual(observed.lastActive(terminal), t0 + 30)
    }

    func testB12_AnAccessoryFrontmostDoesNotUnprotectTheLastRegularApp() {
        var desk = desk()
        _ = desk.evaluate()
        let spotlight: PID = 999
        desk.at(100)
        desk.frontmost = spotlight
        XCTAssertEqual(desk.evaluate().hide, [notes, preview])
        XCTAssertEqual(desk.state.protected, terminal)
        XCTAssertEqual(desk.lastActive(terminal), t0, "no activation happened")
        desk.at(3600)
        XCTAssertFalse(desk.evaluate().hide.contains(terminal))
    }

    func testB13_APerAppTimeoutOverrideIsUsed() {
        var desk = desk()
        desk.config = Config(exempt: [], appTimeouts: ["notes": 300])
        _ = desk.evaluate()
        desk.at(60)
        XCTAssertEqual(desk.evaluate(), Decision(hide: [preview], nextCheck: t0 + 300))
        XCTAssertEqual(desk.status(notes), .pending(hideAt: t0 + 300))
    }

    func testB13_ATimeoutBelowFiveSecondsIsClampedToFive() {
        var desk = desk()
        desk.config = Config(timeout: 1, exempt: [], appTimeouts: ["Preview": 2])
        XCTAssertEqual(desk.evaluate(), Decision(hide: [], nextCheck: t0 + 5))
        desk.at(4.999)
        XCTAssertEqual(desk.evaluate().hide, [])
        desk.at(5)
        XCTAssertEqual(desk.evaluate().hide, [notes, preview])
    }

    // B14 (legacy TOML migration) lives in ConfigStoreTests.testB14_MigratesTheOwnersRealLegacyTOML.

    // MARK: - Ordering and events

    func testHidesAreOrderedOldestDeadlineFirst() {
        var desk = Desk([app(101, "Terminal"), app(104, "Chess"), app(103, "Preview"), app(102, "Notes")],
                        frontmost: terminal)
        _ = desk.evaluate()
        desk.state.timers[chess]?.lastActive = t0 + 20
        desk.state.timers[preview]?.lastActive = t0 + 10
        desk.at(100)
        XCTAssertEqual(desk.evaluate().hide, [notes, preview, chess])
    }

    func testLaunchedStartsAFreshTimer() {
        var state = PolicyState()
        state.apply(.launched(chess), at: t0)
        XCTAssertEqual(state.timers[chess], AppTimer(lastActive: t0))
    }

    func testSpaceChangedChangesNothingByItself() {
        var desk = desk()
        _ = desk.evaluate()
        let before = desk.state
        desk.at(20)
        desk.send(.spaceChanged)
        XCTAssertEqual(desk.state, before)
    }

    func testAReturnWithoutASeenAwayStillRefreshes() {
        var desk = desk()
        _ = desk.evaluate()
        desk.at(500)
        desk.send(.returned(.sleep))
        XCTAssertEqual(desk.lastActive(notes), t0 + 500, "a missed willSleep must not cause a burst on wake")
    }

    func testRefreshAllNeverMovesATimerBackwards() {
        var state = PolicyState()
        state.timers[notes] = AppTimer(lastActive: t0 + 100)
        state.timers[preview] = AppTimer(lastActive: t0)
        state.refreshAll(t0 + 50)
        XCTAssertEqual(state.timers[notes]?.lastActive, t0 + 100)
        XCTAssertEqual(state.timers[preview]?.lastActive, t0 + 50)
    }

    func testResumeWhenNotPausedChangesNothing() {
        var desk = desk()
        _ = desk.evaluate()
        let before = desk.state
        desk.state.resume(at: t0 + 30)
        XCTAssertEqual(desk.state, before)
    }

    // MARK: - status

    func testStatusLabels() {
        let labels: [(AppStatus, String)] = [
            (.protected, "frontmost"), (.exempt, "exempt"), (.hidden, "hidden"), (.offSpace, "off-space"),
            (.paused, "paused"), (.away, "away"), (.fullscreenSpace, "fullscreen-space"),
            (.refused(until: t0), "refused"), (.pending(hideAt: t0), "pending"), (.due, "due"),
        ]
        for (status, label) in labels {
            XCTAssertEqual(status.label, label)
        }
    }

    func testStatusReportsEachState() {
        var desk = Desk([app(101, "Terminal"), app(102, "Notes"), app(103, "Finder"),
                         app(104, "Chess", hidden: true), app(105, "Preview", visible: false), app(106, "TextEdit")],
                        frontmost: terminal)
        desk.config = Config(exempt: ["Finder"])
        _ = desk.evaluate()
        desk.state.markRefused(106, at: t0)
        desk.at(30)
        XCTAssertEqual(desk.status(101), .protected)
        XCTAssertEqual(desk.status(101).label, "frontmost")
        XCTAssertEqual(desk.status(102), .pending(hideAt: t0 + 60))
        XCTAssertEqual(desk.status(103), .exempt)
        XCTAssertEqual(desk.status(104), .hidden)
        XCTAssertEqual(desk.status(105), .offSpace)
        XCTAssertEqual(desk.status(106), .refused(until: t0 + 300))
        desk.at(60)
        XCTAssertEqual(desk.status(102), .due)
        desk.fullscreen = true
        XCTAssertEqual(desk.status(102), .fullscreenSpace)
        desk.state.pause(until: nil, at: desk.now)
        XCTAssertEqual(desk.status(102), .paused)
        desk.send(.away(.session))
        XCTAssertEqual(desk.status(102), .away)
        XCTAssertEqual(desk.status(101), .protected, "per-app facts outrank global suspensions")
        XCTAssertEqual(desk.status(103), .exempt)
    }
}
