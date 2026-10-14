import XCTest
import AutohideCore

/// Config as a value: matching, effective timeouts, exemption edits, and the
/// config.json encoding. Disk I/O is covered in ConfigStoreTests.
final class ConfigTests: XCTestCase {
    private let whatsApp = AppIdentity(name: "\u{200E}WhatsApp", bundleID: "net.whatsapp.WhatsApp")
    private let slack = AppIdentity(name: "Slack", bundleID: "com.tinyspeck.slackmacgap")

    func testDefaults() {
        let config = Config.default
        XCTAssertEqual(config.timeout, 60)
        XCTAssertEqual(config.exempt, ["Finder"])
        XCTAssertEqual(config.appTimeouts, [:])
        XCTAssertEqual(config.onlyApps, [])
        XCTAssertEqual(Config.minimumTimeout, 5)
    }

    // MARK: - AppMatch

    func testNormalizeLowercasesTrimsAndStripsFormatCharacters() {
        XCTAssertEqual(AppMatch.normalize("  WhatsApp\u{200E} "), "whatsapp")
        XCTAssertEqual(AppMatch.normalize("\u{200E}WhatsApp"), "whatsapp")
        XCTAssertEqual(AppMatch.normalize("\u{200B}Wispr\u{FEFF} Flow"), "wispr flow")
        XCTAssertEqual(AppMatch.normalize("\u{200E} Finder\n"), "finder")
    }

    func testMatchesByNameOrBundleIDIgnoringCaseAndFormatCharacters() {
        XCTAssertTrue(AppMatch.matches("whatsapp", whatsApp))
        XCTAssertTrue(AppMatch.matches("WHATSAPP\u{200E}", whatsApp))
        XCTAssertTrue(AppMatch.matches("NET.whatsapp.whatsapp", whatsApp))
        XCTAssertFalse(AppMatch.matches("Whats", whatsApp))
        XCTAssertFalse(AppMatch.matches("slack", whatsApp))
    }

    func testEmptyEntryMatchesNothing() {
        XCTAssertFalse(AppMatch.matches("", AppIdentity(name: "")))
        XCTAssertFalse(AppMatch.matches("\u{200E} ", AppIdentity(name: "\u{200E}")))
    }

    // MARK: - Effective timeout

    func testEffectiveTimeoutIsTheDefaultForAnOrdinaryApp() {
        XCTAssertEqual(Config(timeout: 90).effectiveTimeout(for: slack), 90)
    }

    func testEffectiveTimeoutUsesAPerAppOverrideByNameOrBundleID() {
        XCTAssertEqual(Config(appTimeouts: ["slack": 300]).effectiveTimeout(for: slack), 300)
        XCTAssertEqual(Config(appTimeouts: ["com.tinyspeck.slackmacgap": 120]).effectiveTimeout(for: slack), 120)
    }

    func testEffectiveTimeoutIsNilForExemptApps() {
        let config = Config(exempt: ["WhatsApp"])
        XCTAssertTrue(config.isExempt(whatsApp))
        XCTAssertNil(config.effectiveTimeout(for: whatsApp))
        XCTAssertEqual(config.effectiveTimeout(for: slack), 60)
    }

    func testEffectiveTimeoutIsNilOutsideANonEmptyOnlyApps() {
        let config = Config(exempt: [], onlyApps: ["slack"])
        XCTAssertEqual(config.effectiveTimeout(for: slack), 60)
        XCTAssertNil(config.effectiveTimeout(for: whatsApp))
        XCTAssertFalse(config.isExempt(whatsApp), "outside only_apps is not the same as exempt")
    }

    func testEffectiveTimeoutIsClampedToTheMinimum() {
        XCTAssertEqual(Config(timeout: 1).effectiveTimeout(for: slack), Config.minimumTimeout)
        XCTAssertEqual(Config(appTimeouts: ["Slack": 2]).effectiveTimeout(for: slack), Config.minimumTimeout)
    }

    // MARK: - setExempt

    func testSetExemptAddsSortedAndDeduplicated() {
        var config = Config(exempt: ["Finder"])
        config.setExempt("Wispr Flow", true)
        config.setExempt("codex", true)
        config.setExempt("FINDER", true)
        XCTAssertEqual(config.exempt, ["codex", "Finder", "Wispr Flow"])
    }

    func testSetExemptStoresTheNameWithoutFormatCharacters() {
        var config = Config(exempt: [])
        config.setExempt("\u{200E}WhatsApp ", true)
        XCTAssertEqual(config.exempt, ["WhatsApp"])
        config.setExempt("", true)
        XCTAssertEqual(config.exempt, ["WhatsApp"], "a blank name is not an exemption")
    }

    func testSetExemptRemovesByNormalizedMatch() {
        var config = Config(exempt: ["Finder", "Slack", "slack "])
        config.setExempt("SLACK\u{200E}", false)
        XCTAssertEqual(config.exempt, ["Finder"])
        config.setExempt("Nope", false)
        XCTAssertEqual(config.exempt, ["Finder"])
    }

    // MARK: - config.json encoding

    func testMissingKeysFallBackToDefaults() throws {
        XCTAssertEqual(try decode("{}"), Config.default)
        let partial = try decode(#"{"timeout": "2m"}"#)
        XCTAssertEqual(partial.timeout, 120)
        XCTAssertEqual(partial.exempt, Config.default.exempt)
        XCTAssertEqual(partial.appTimeouts, [:])
        XCTAssertEqual(partial.onlyApps, [])
    }

    func testDurationsAreWrittenAsStrings() throws {
        let config = Config(timeout: 90, exempt: ["Finder"], appTimeouts: ["Slack": 300], onlyApps: [])
        let object = try JSONSerialization.jsonObject(with: JSONEncoder().encode(config)) as? [String: Any]
        XCTAssertEqual(object?["timeout"] as? String, "1m30s")
        XCTAssertEqual(object?["app_timeouts"] as? [String: String], ["Slack": "5m"])
        XCTAssertEqual(object?["exempt"] as? [String], ["Finder"])
        XCTAssertEqual(object?["only_apps"] as? [String], [])
    }

    func testUnknownKeysAreIgnored() throws {
        let config = try decode(#"{"timeout": "30s", "focus": {"keep_recent": 3}, "bogus": 1}"#)
        XCTAssertEqual(config.timeout, 30)
    }

    func testGoStyleDurationsDecode() throws {
        let config = try decode(#"{"timeout": "2m0s", "app_timeouts": {"Slack": "1h0m0s"}}"#)
        XCTAssertEqual(config.timeout, 120)
        XCTAssertEqual(config.appTimeouts, ["Slack": 3600])
    }

    func testAnInvalidDurationIsACorruptFile() {
        XCTAssertThrowsError(try decode(#"{"timeout": "soon"}"#))
        XCTAssertThrowsError(try decode(#"{"app_timeouts": {"Slack": "-5s"}}"#))
    }

    func testEncodeThenDecodeIsIdentity() throws {
        let config = Config(timeout: 45, exempt: ["Codex", "Wispr Flow"],
                            appTimeouts: ["Slack": 300, "com.apple.Preview": 5400], onlyApps: ["TextEdit"])
        XCTAssertEqual(try JSONDecoder().decode(Config.self, from: JSONEncoder().encode(config)), config)
    }

    private func decode(_ json: String) throws -> Config {
        try JSONDecoder().decode(Config.self, from: Data(json.utf8))
    }
}
