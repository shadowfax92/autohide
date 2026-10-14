import XCTest
import AutohideCore

/// DurationFormat is the one parser for durations in config.json, on the
/// command line, and in the legacy Go TOML, so it must accept Go's "2m0s".
final class DurationTests: XCTestCase {
    func testParsesUnitsAndCombinations() {
        let cases: [(String, TimeInterval)] = [
            ("45s", 45), ("1m", 60), ("1m30s", 90), ("2h", 7200), ("1h30m", 5400),
            ("1.5m", 90), (" 60s ", 60), ("10S", 10),
        ]
        for (text, seconds) in cases {
            XCTAssertEqual(DurationFormat.parse(text), seconds, text)
        }
    }

    func testParsesGoStyle() {
        XCTAssertEqual(DurationFormat.parse("2m0s"), 120)
        XCTAssertEqual(DurationFormat.parse("1h0m0s"), 3600)
        XCTAssertEqual(DurationFormat.parse("1m0s"), 60)
        XCTAssertEqual(DurationFormat.parse("1h30m0s"), 5400)
    }

    func testParsesBareSeconds() {
        XCTAssertEqual(DurationFormat.parse("90"), 90)
        XCTAssertEqual(DurationFormat.parse("5"), 5)
    }

    func testRejectsGarbageNegativeAndZero() {
        let rejected = ["", " ", "abc", "-5s", "0s", "0", "0m0s", "5x", "s", "1m30",
                        "1.2.3s", "1 m", "+5s", "nan", "inf", ".s", "1e3s"]
        for text in rejected {
            XCTAssertNil(DurationFormat.parse(text), "\"\(text)\" should be rejected")
        }
    }

    func testFormatsCanonically() {
        XCTAssertEqual(DurationFormat.format(45), "45s")
        XCTAssertEqual(DurationFormat.format(60), "1m")
        XCTAssertEqual(DurationFormat.format(90), "1m30s")
        XCTAssertEqual(DurationFormat.format(3600), "1h")
        XCTAssertEqual(DurationFormat.format(5400), "1h30m")
        XCTAssertEqual(DurationFormat.format(3661), "1h1m1s")
        XCTAssertEqual(DurationFormat.format(7200), "2h")
        XCTAssertEqual(DurationFormat.format(1.5), "1.5s")
    }

    func testFormatsNonPositiveAsZero() {
        XCTAssertEqual(DurationFormat.format(0), "0s")
        XCTAssertEqual(DurationFormat.format(-5), "0s")
    }

    func testFormatThenParseIsIdentity() {
        for seconds: TimeInterval in [1, 5, 45, 59, 60, 61, 90, 120, 300, 3599, 3600, 3601, 5400, 86400, 2.5] {
            XCTAssertEqual(DurationFormat.parse(DurationFormat.format(seconds)), seconds, "\(seconds)")
        }
    }

    func testParseThenFormatCanonicalizes() {
        XCTAssertEqual(DurationFormat.parse("2m0s").map(DurationFormat.format), "2m")
        XCTAssertEqual(DurationFormat.parse("1h0m0s").map(DurationFormat.format), "1h")
        XCTAssertEqual(DurationFormat.parse("90").map(DurationFormat.format), "1m30s")
        XCTAssertEqual(DurationFormat.parse("45s").map(DurationFormat.format), "45s")
    }
}
