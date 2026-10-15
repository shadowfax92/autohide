import XCTest
import AutohideCore

/// The CLI and the daemon only agree if both go through encodeLine/decodeLine,
/// so these tests pin the wire shape: one JSON line, ISO-8601 dates.
final class IPCProtocolTests: XCTestCase {
    private let t0 = Date(timeIntervalSince1970: 1_000_000)

    func testEveryRequestCommandRoundTrips() throws {
        let commands: [IPC.Command] = [.status, .list, .pause, .resume, .hideOthers, .reload, .quit]
        for command in commands {
            let request = IPC.Request(command: command)
            XCTAssertEqual(try roundTrip(request), request, command.rawValue)
        }
        let timedPause = IPC.Request(command: .pause, seconds: 1800)
        XCTAssertEqual(try roundTrip(timedPause), timedPause)
    }

    func testHideOthersUsesItsKebabCaseName() throws {
        let line = try text(IPC.encodeLine(IPC.Request(command: .hideOthers)))
        XCTAssertTrue(line.contains("\"hide-others\""), line)
    }

    func testStatusResponseRoundTripsWithDates() throws {
        let response = IPC.Response(ok: true, status: status(pausedUntil: t0.addingTimeInterval(1800.25)))
        XCTAssertEqual(try roundTrip(response), response)
        let untilResumed = IPC.Response(ok: true, status: status(pausedUntil: nil))
        XCTAssertEqual(try roundTrip(untilResumed), untilResumed)
    }

    func testListResponseRoundTrips() throws {
        let response = IPC.Response(ok: true, apps: [
            IPC.AppRow(pid: 101, name: "Terminal", bundleID: "com.apple.Terminal", state: "frontmost", hidesIn: nil, timeout: 60),
            IPC.AppRow(pid: 102, name: "\u{200E}WhatsApp", bundleID: nil, state: "exempt", hidesIn: nil, timeout: nil),
            IPC.AppRow(pid: 103, name: "Notes", bundleID: "com.apple.Notes", state: "pending", hidesIn: 12.5, timeout: 60),
        ])
        XCTAssertEqual(try roundTrip(response), response)
    }

    func testErrorAndHideOthersResponsesRoundTrip() throws {
        let failure = IPC.Response(ok: false, error: "unknown command \"bogus\"\nsecond line")
        XCTAssertEqual(try roundTrip(failure), failure)
        let hidden = IPC.Response(ok: true, hidden: 3)
        XCTAssertEqual(try roundTrip(hidden), hidden)
    }

    func testEncodeLineIsExactlyOneLineEndingInNewline() throws {
        let response = IPC.Response(ok: false, error: "multi\nline\nerror", status: status(pausedUntil: t0))
        let line = try text(IPC.encodeLine(response))
        XCTAssertTrue(line.hasSuffix("\n"))
        XCTAssertEqual(line.filter { $0 == "\n" }.count, 1, line)
    }

    func testDatesAreISO8601() throws {
        let line = try text(IPC.encodeLine(IPC.Response(ok: true, status: status(pausedUntil: nil))))
        XCTAssertTrue(line.contains("1970-01-12T13:46:40"), line)
    }

    func testDecodeAcceptsISO8601WithoutFractionalSecondsAndWithoutANewline() throws {
        var object = try JSONSerialization.jsonObject(
            with: IPC.encodeLine(IPC.Response(ok: true, status: status(pausedUntil: nil)))) as! [String: Any]
        var statusObject = object["status"] as! [String: Any]
        statusObject["startedAt"] = "1970-01-12T13:46:40Z"
        object["status"] = statusObject
        let data = try JSONSerialization.data(withJSONObject: object)
        XCTAssertEqual(try IPC.decodeLine(IPC.Response.self, from: data).status?.startedAt, t0)
    }

    func testStatusFromADaemonThatStillSendsAccessibilityDecodes() throws {
        // After an upgrade the new CLI talks to the old daemon until a restart.
        var object = try JSONSerialization.jsonObject(
            with: IPC.encodeLine(IPC.Response(ok: true, status: status(pausedUntil: nil)))) as! [String: Any]
        var statusObject = object["status"] as! [String: Any]
        XCTAssertNil(statusObject["accessibility"])
        statusObject["accessibility"] = true
        object["status"] = statusObject
        let data = try JSONSerialization.data(withJSONObject: object)
        XCTAssertEqual(try IPC.decodeLine(IPC.Response.self, from: data).status, status(pausedUntil: nil))
    }

    func testDecodeRejectsGarbage() {
        XCTAssertThrowsError(try IPC.decodeLine(IPC.Request.self, from: Data("not json\n".utf8)))
        XCTAssertThrowsError(try IPC.decodeLine(IPC.Request.self, from: Data(#"{"command":"explode"}"#.utf8)))
        XCTAssertThrowsError(try IPC.decodeLine(IPC.Request.self, from: Data()))
    }

    private func status(pausedUntil: Date?) -> IPC.StatusPayload {
        IPC.StatusPayload(
            pid: 4242, version: AutohideVersion.current, startedAt: t0, paused: true, pausedUntil: pausedUntil,
            away: false, fullscreenSpace: false, timeout: 60, exempt: ["Finder"], appTimeouts: ["Slack": 300],
            onlyApps: [], protectedApp: "Terminal", configPath: "/tmp/ah/config.json",
            logPath: "/tmp/ah/autohide.log")
    }

    private func roundTrip<T: Codable>(_ value: T) throws -> T {
        try IPC.decodeLine(T.self, from: IPC.encodeLine(value))
    }

    private func text(_ data: Data) -> String {
        String(decoding: data, as: UTF8.self)
    }
}
