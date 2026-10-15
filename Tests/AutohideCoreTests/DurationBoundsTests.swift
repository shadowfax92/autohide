import XCTest
import AutohideCore

/// The edges of the duration range (review finding #7): what parse refuses,
/// that format never traps, and that every duration Config accepts survives
/// a save → load through config.json.
final class DurationBoundsTests: XCTestCase {
    private let sevenDays: TimeInterval = 604_800

    // MARK: - parse

    func testRejectsTheInputsFromTheReview() {
        let fourHundredDigits = "1" + String(repeating: "0", count: 399)
        let rejected = [
            "9223372036854776s",          // trapped Int64 conversion in format
            "0.0001s",                    // sub-millisecond: saved as "0s", which no longer loads
            fourHundredDigits,            // bare number that Double reads as +inf
            fourHundredDigits + "s",
            "inf", "nan", "infs", "1e3", "1e3s",
            "168h0m1s", "604801", "604801s", "10080m1s",   // 7 days + 1 s
        ]
        for text in rejected {
            XCTAssertNil(DurationFormat.parse(text), "\"\(text.prefix(40))\" should be rejected")
        }
    }

    func testAcceptsExactlySevenDaysAndOneMillisecond() {
        XCTAssertEqual(DurationFormat.parse("604800"), sevenDays)
        XCTAssertEqual(DurationFormat.parse("168h"), sevenDays)
        XCTAssertEqual(DurationFormat.parse("167h59m59.999s"), sevenDays - 0.001)
        XCTAssertEqual(DurationFormat.parse("0.001s"), 0.001)
        XCTAssertEqual(DurationFormat.parse("0.001"), 0.001)
        XCTAssertEqual(DurationFormat.maximum, sevenDays)
        XCTAssertEqual(DurationFormat.resolution, 0.001)
    }

    func testRejectsJustOutsideTheBounds() {
        XCTAssertNil(DurationFormat.parse("604800.001s"))
        XCTAssertNil(DurationFormat.parse("168h0.001s"))
        XCTAssertNil(DurationFormat.parse("0.0009s"))
        XCTAssertNil(DurationFormat.parse("0.0005"))
    }

    func testRoundsToWholeMilliseconds() {
        XCTAssertEqual(DurationFormat.parse("1.0004s"), 1)
        XCTAssertEqual(DurationFormat.parse("1.0006s"), 1.001)
        XCTAssertEqual(DurationFormat.parse("0.0015s"), 0.002)
        XCTAssertEqual(DurationFormat.parse("1h0.0004s"), 3600)
        XCTAssertEqual(DurationFormat.parse("0." + String(repeating: "3", count: 400)), 0.333)
    }

    // MARK: - format

    func testFormatNeverTraps() {
        // Int64.max milliseconds is where the conversion clamps.
        let clamped = "2562047788015h12m55.807s"
        XCTAssertEqual(DurationFormat.format(9_223_372_036_854_776), clamped)
        XCTAssertEqual(DurationFormat.format(1e300), clamped)
        XCTAssertEqual(DurationFormat.format(.greatestFiniteMagnitude), clamped)
        XCTAssertEqual(DurationFormat.format(.infinity), "0s")
        XCTAssertEqual(DurationFormat.format(-.infinity), "0s")
        XCTAssertEqual(DurationFormat.format(.nan), "0s")
        XCTAssertEqual(DurationFormat.format(.leastNonzeroMagnitude), "0s")
    }

    func testFormatThenParseIsIdentityAtTheBounds() {
        for seconds: TimeInterval in [0.001, 0.002, 0.999, 1.001, 59.999, 3599.999, sevenDays - 0.001, sevenDays] {
            XCTAssertEqual(DurationFormat.parse(DurationFormat.format(seconds)), seconds, "\(seconds)")
        }
    }

    // MARK: - Config

    func testConfigRejectsOutOfRangeDurationsAtLoad() {
        let fourHundredDigits = "1" + String(repeating: "0", count: 399)
        for text in ["0.0001s", "9223372036854776s", fourHundredDigits, "168h0m1s"] {
            XCTAssertThrowsError(try decode(#"{"timeout": "\#(text)"}"#), "timeout \(text.prefix(40))")
            XCTAssertThrowsError(try decode(#"{"app_timeouts": {"Slack": "\#(text)"}}"#), "app timeout \(text.prefix(40))")
        }
    }

    func testConfigRefusesToEncodeADurationThatWouldNotLoadBack() {
        for seconds: TimeInterval in [0.0001, 9_223_372_036_854_776, .infinity, .nan, 0, -5, sevenDays + 1] {
            XCTAssertThrowsError(try JSONEncoder().encode(Config(timeout: seconds)), "timeout \(seconds)")
            XCTAssertThrowsError(try JSONEncoder().encode(Config(appTimeouts: ["Slack": seconds])), "app timeout \(seconds)")
        }
    }

    func testSaveThenLoadRoundTripsAtTheBounds() throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("autohide-duration-bounds-\(UUID().uuidString)", isDirectory: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appendingPathComponent("config.json")

        for (timeout, app) in [(0.001, sevenDays), (sevenDays, 0.001), (sevenDays - 0.001, 1.001)] {
            let config = Config(timeout: timeout, exempt: ["Finder"], appTimeouts: ["Slack": app], onlyApps: [])
            try ConfigStore.save(config, to: file)
            XCTAssertEqual(try ConfigStore.load(file), config, "timeout \(timeout), Slack \(app)")
        }
    }

    /// The review's corruption path: a sub-millisecond timeout loaded, then any
    /// unrelated edit saved it back as "0s" and the next load failed. Now the
    /// out-of-range value is refused, and a refused save leaves the file as it was.
    func testARefusedSaveLeavesTheLastGoodFile() throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("autohide-duration-bounds-\(UUID().uuidString)", isDirectory: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appendingPathComponent("config.json")

        var config = try decode(#"{"timeout": "0.001s"}"#)
        config.setExempt("Slack", true)
        try ConfigStore.save(config, to: file)
        XCTAssertEqual(try ConfigStore.load(file), config)

        var broken = config
        broken.timeout = 0.0001
        XCTAssertThrowsError(try ConfigStore.save(broken, to: file))
        XCTAssertEqual(try ConfigStore.load(file), config)
    }

    private func decode(_ json: String) throws -> Config {
        try JSONDecoder().decode(Config.self, from: Data(json.utf8))
    }
}
