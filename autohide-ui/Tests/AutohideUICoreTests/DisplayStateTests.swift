import XCTest

@testable import AutohideUICore

final class DisplayStateTests: XCTestCase {
    func testPrettyGoDuration() {
        let cases: [(String, String)] = [
            ("1m0s", "1m"),
            ("5s", "5s"),
            ("1h2m0s", "1h 2m"),
            ("2m30s", "2m 30s"),
            ("1h0m0s", "1h"),
            ("0s", "now"),
        ]
        for (input, want) in cases {
            XCTAssertEqual(prettyGoDuration(input), want, "input \(input)")
        }
    }

    func testPrettyGoDurationPassesThroughUnparseable() {
        XCTAssertEqual(prettyGoDuration("soon"), "soon")
        XCTAssertEqual(prettyGoDuration(""), "")
    }

    func testResumeCountdownFuture() {
        let now = Date(timeIntervalSince1970: 1_750_000_000)
        let resumeAt = ISO8601DateFormatter().string(from: now.addingTimeInterval(42 * 60))
        XCTAssertEqual(resumeCountdown(from: resumeAt, now: now), "resumes in 42m")
    }

    func testResumeCountdownPastClamps() {
        let now = Date(timeIntervalSince1970: 1_750_000_000)
        let resumeAt = ISO8601DateFormatter().string(from: now.addingTimeInterval(-30))
        XCTAssertEqual(resumeCountdown(from: resumeAt, now: now), "resuming…")
    }

    func testResumeCountdownGarbageIsNil() {
        XCTAssertNil(resumeCountdown(from: "not-a-date", now: Date(timeIntervalSince1970: 0)))
    }

    func testTrackingDisplayMapping() {
        XCTAssertEqual(trackingDisplay("active").severity, .ok)
        XCTAssertEqual(trackingDisplay("active").label, "Per-window")

        let appOnly = trackingDisplay("app-only: accessibility not granted")
        XCTAssertEqual(appOnly.severity, .warn)
        XCTAssertTrue(appOnly.label.contains("Accessibility"), "label: \(appOnly.label)")

        XCTAssertEqual(trackingDisplay("legacy: helper not found").severity, .warn)
        XCTAssertEqual(trackingDisplay("legacy: helper failing").severity, .warn)
        XCTAssertEqual(trackingDisplay("off").severity, .neutral)
        XCTAssertEqual(trackingDisplay("starting").severity, .neutral)
        XCTAssertEqual(trackingDisplay(nil).severity, .neutral)
        XCTAssertEqual(trackingDisplay(nil).label, "Unknown")
    }

    func testPermissionStateFromOptionalBool() {
        XCTAssertEqual(PermissionState(true), .granted)
        XCTAssertEqual(PermissionState(false), .denied)
        XCTAssertEqual(PermissionState(nil), .unknown)
    }

    func testPausePresetDurationArgs() {
        XCTAssertNil(PausePreset.indefinite.durationArg)
        XCTAssertEqual(PausePreset.thirtyMinutes.durationArg, "30m")
        XCTAssertEqual(PausePreset.oneHour.durationArg, "1h")
        XCTAssertEqual(PausePreset.twoHours.durationArg, "2h")
        XCTAssertEqual(PausePreset.allCases.count, 4)
    }

    func testWindowDisplayTitleFallsBackToID() {
        XCTAssertEqual(windowDisplayTitle("Docs — Chrome", id: 42), "Docs — Chrome")
        XCTAssertEqual(windowDisplayTitle("", id: 42), "Window 42")
        XCTAssertEqual(windowDisplayTitle(nil, id: 7), "Window 7")
    }
}
