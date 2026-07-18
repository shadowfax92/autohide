import XCTest

@testable import AutohideUICore

final class ProtocolTests: XCTestCase {
    func testStatusDecodesAllFields() throws {
        let json = """
            {"running": true, "paused": true, "focus_mode": false, "uptime": "1h2m0s",
             "tracked_count": 7, "resume_at": "2026-06-12T14:00:00-07:00",
             "window_tracking": "active", "ax_trusted": true, "screen_recording": false,
             "default_timeout": "1m", "timeout_presets": ["30s", "1m", "2m", "5m"]}
            """
        let status = try IPCClient.decoder.decode(DaemonStatus.self, from: Data(json.utf8))
        XCTAssertTrue(status.running)
        XCTAssertTrue(status.paused)
        XCTAssertFalse(status.focusMode)
        XCTAssertEqual(status.uptime, "1h2m0s")
        XCTAssertEqual(status.trackedCount, 7)
        XCTAssertEqual(status.resumeAt, "2026-06-12T14:00:00-07:00")
        XCTAssertEqual(status.windowTracking, "active")
        XCTAssertEqual(status.axTrusted, true)
        XCTAssertEqual(status.screenRecording, false)
        XCTAssertEqual(status.defaultTimeout, "1m")
        XCTAssertEqual(status.timeoutPresets, ["30s", "1m", "2m", "5m"])
    }

    func testStatusPermissionsAbsentDecodeAsNil() throws {
        let json = """
            {"running": true, "paused": false, "focus_mode": false, "uptime": "5s", "tracked_count": 0}
            """
        let status = try IPCClient.decoder.decode(DaemonStatus.self, from: Data(json.utf8))
        XCTAssertNil(status.axTrusted)
        XCTAssertNil(status.screenRecording)
        XCTAssertNil(status.resumeAt)
        XCTAssertNil(status.windowTracking)
    }

    func testListDecodesAppsWithAndWithoutWindows() throws {
        let json = """
            {"apps": [
              {"name": "Slack", "last_active": "2026-06-12T12:00:00-07:00", "timeout": "1m0s",
               "hidden": false, "unhidable": "fullscreen", "time_remaining": "0s", "disabled": false, "window_count": 2,
               "windows": [
                 {"id": 9, "title": "general", "last_active": "2026-06-12T12:00:00-07:00"},
                 {"id": 10, "last_active": "2026-06-12T12:00:00-07:00"}
               ]},
              {"name": "Finder", "last_active": "2026-06-12T12:00:00-07:00", "timeout": "0s",
               "hidden": false, "time_remaining": "0s", "disabled": true, "window_count": 0}
            ]}
            """
        let list = try IPCClient.decoder.decode(AppList.self, from: Data(json.utf8))
        XCTAssertEqual(list.apps.count, 2)
        XCTAssertEqual(list.apps[0].windows?.count, 2)
        XCTAssertEqual(list.apps[0].windows?[0].title, "general")
        XCTAssertNil(list.apps[0].windows?[1].title)
        XCTAssertEqual(list.apps[0].windows?[1].id, 10)
        XCTAssertEqual(list.apps[0].unhidable, "fullscreen")
        XCTAssertTrue(list.apps[1].disabled)
        XCTAssertNil(list.apps[1].unhidable)
        XCTAssertNil(list.apps[1].windows)
    }
}
