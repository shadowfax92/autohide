import XCTest
import AutohideCore

/// ConfigStore against a real temporary directory: saving, loading, the
/// first-run decision in loadOrCreate, and the one-time TOML migration.
final class ConfigStoreTests: XCTestCase {
    private var directory: URL!

    override func setUpWithError() throws {
        directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("autohide-core-tests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: directory)
    }

    // MARK: - ConfigPaths

    func testPathsLiveUnderOneDirectory() {
        let paths = ConfigPaths(directory: URL(fileURLWithPath: "/tmp/ah", isDirectory: true))
        XCTAssertEqual(paths.configFile.path, "/tmp/ah/config.json")
        XCTAssertEqual(paths.legacyTOML.path, "/tmp/ah/config.toml")
        XCTAssertEqual(paths.socket.path, "/tmp/ah/autohide.sock")
        XCTAssertEqual(paths.logFile.path, "/tmp/ah/autohide.log")
        XCTAssertEqual(paths.lockFile.path, "/tmp/ah/autohide.lock")
    }

    func testStandardPathsHonorTheEnvironmentOverride() {
        let previous = ProcessInfo.processInfo.environment["AUTOHIDE_CONFIG_DIR"]
        defer {
            if let previous { setenv("AUTOHIDE_CONFIG_DIR", previous, 1) } else { unsetenv("AUTOHIDE_CONFIG_DIR") }
        }
        setenv("AUTOHIDE_CONFIG_DIR", directory.path, 1)
        XCTAssertEqual(ConfigPaths.standard().configFile.resolvingSymlinksInPath(),
                       directory.appendingPathComponent("config.json").resolvingSymlinksInPath())
        unsetenv("AUTOHIDE_CONFIG_DIR")
        XCTAssertTrue(ConfigPaths.standard().directory.path.hasSuffix("/.config/autohide"))
    }

    // MARK: - save / load

    func testSaveThenLoadIsIdentity() throws {
        let url = directory.appendingPathComponent("config.json")
        let config = Config(timeout: 90, exempt: ["Codex", "Finder", "\u{00C9}cran"],
                            appTimeouts: ["Slack": 300, "com.apple.Preview": 3661], onlyApps: ["TextEdit", "Chess"])
        try ConfigStore.save(config, to: url)
        XCTAssertEqual(try ConfigStore.load(url), config)
    }

    func testSaveWritesPrettySortedJSONWithATrailingNewline() throws {
        let url = directory.appendingPathComponent("config.json")
        try ConfigStore.save(Config(timeout: 90, appTimeouts: ["Slack": 300]), to: url)
        let text = try String(contentsOf: url, encoding: .utf8)
        XCTAssertTrue(text.hasSuffix("}\n"), text)
        XCTAssertGreaterThan(text.split(separator: "\n").count, 4, "pretty-printed")
        XCTAssertTrue(text.contains("\"1m30s\""), text)
        XCTAssertTrue(text.contains("\"5m\""), text)
        let keys = ["\"app_timeouts\"", "\"exempt\"", "\"only_apps\"", "\"timeout\""]
        let offsets = keys.map { text.range(of: $0)!.lowerBound }
        XCTAssertEqual(offsets, offsets.sorted(), "keys are sorted")
    }

    func testSaveReplacesTheFileByRenameAndLeavesNoTempFiles() throws {
        let url = directory.appendingPathComponent("config.json")
        try ConfigStore.save(Config(timeout: 30), to: url)
        let firstInode = try inode(url)
        try ConfigStore.save(Config(timeout: 45), to: url)
        XCTAssertNotEqual(try inode(url), firstInode, "a rename installs a new file; an in-place write keeps the inode")
        XCTAssertEqual(try ConfigStore.load(url).timeout, 45)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: directory.path), ["config.json"])
    }

    func testSaveKeepsASymlinkedConfigASymlink() throws {
        let target = directory.appendingPathComponent("dotfiles-config.json")
        let link = directory.appendingPathComponent("config.json")
        try ConfigStore.save(Config(timeout: 30), to: target)
        try FileManager.default.createSymbolicLink(at: link, withDestinationURL: target)
        try ConfigStore.save(Config(timeout: 45), to: link)
        XCTAssertNoThrow(try FileManager.default.destinationOfSymbolicLink(atPath: link.path))
        XCTAssertEqual(try ConfigStore.load(target).timeout, 45)
    }

    func testSaveCreatesTheParentDirectory() throws {
        let url = directory.appendingPathComponent("nested/dir/config.json")
        try ConfigStore.save(Config.default, to: url)
        XCTAssertEqual(try ConfigStore.load(url), Config.default)
    }

    func testLoadThrowsOnACorruptFile() throws {
        let url = directory.appendingPathComponent("config.json")
        try Data("{ not json".utf8).write(to: url)
        XCTAssertThrowsError(try ConfigStore.load(url))
    }

    // MARK: - loadOrCreate

    func testLoadOrCreateWritesDefaultsWhenNothingExists() throws {
        let paths = ConfigPaths(directory: directory.appendingPathComponent("fresh", isDirectory: true))
        XCTAssertEqual(try ConfigStore.loadOrCreate(paths), Config.default)
        XCTAssertEqual(try ConfigStore.load(paths.configFile), Config.default)
    }

    func testLoadOrCreateMigratesALegacyTOMLOnceAndLeavesItInPlace() throws {
        let paths = ConfigPaths(directory: directory)
        try Data(Self.ownersLegacyTOML.utf8).write(to: paths.legacyTOML)
        let migrated = try ConfigStore.loadOrCreate(paths)
        XCTAssertEqual(migrated.exempt, ["Codex", "Finder", "Wispr Flow"])
        XCTAssertEqual(try ConfigStore.load(paths.configFile), migrated)
        XCTAssertEqual(try String(contentsOf: paths.legacyTOML, encoding: .utf8), Self.ownersLegacyTOML,
                       "the old installed daemon still reads config.toml")
    }

    func testLoadOrCreatePrefersConfigJSONOverTheTOML() throws {
        let paths = ConfigPaths(directory: directory)
        let existing = Config(timeout: 30, exempt: ["Slack"])
        try ConfigStore.save(existing, to: paths.configFile)
        try Data(Self.ownersLegacyTOML.utf8).write(to: paths.legacyTOML)
        XCTAssertEqual(try ConfigStore.loadOrCreate(paths), existing)
        XCTAssertEqual(try ConfigStore.load(paths.configFile), existing)
    }

    func testLoadOrCreateThrowsOnACorruptConfigJSON() throws {
        let paths = ConfigPaths(directory: directory)
        try Data(#"{"timeout": "soon"}"#.utf8).write(to: paths.configFile)
        XCTAssertThrowsError(try ConfigStore.loadOrCreate(paths))
    }

    // MARK: - Legacy TOML migration

    /// The owner's real ~/.config/autohide/config.toml, verbatim.
    static let ownersLegacyTOML = """
    [general]
      default_timeout = "2m0s"
      check_interval = "5s"
      system_exclude = ["Finder"]
      window_tracking = true
      hide_other_spaces = true

    [apps]
      [apps.Codex]
        disabled = true
      [apps.Finder]
        disabled = true
      [apps."Wispr Flow"]
        disabled = true

    [menubar]
      timeout_presets = ["30s", "1m0s", "2m0s"]

    [focus]
      keep_recent = 3
      grace = "10s"

    """

    func testB14_MigratesTheOwnersRealLegacyTOML() {
        let config = ConfigStore.migrateLegacyTOML(Self.ownersLegacyTOML)
        XCTAssertEqual(config.exempt, ["Codex", "Finder", "Wispr Flow"])
        XCTAssertEqual(config.timeout, 60, "the old 2m default_timeout does not carry over")
        XCTAssertEqual(config.appTimeouts, [:])
        XCTAssertEqual(config.onlyApps, [])
    }

    func testMigratesAPerAppTimeout() {
        let config = ConfigStore.migrateLegacyTOML("""
        [general]
          system_exclude = ["Finder"]
        [apps]
          [apps.Slack]
            timeout = "5m0s"
          [apps."Google Chrome"]
            timeout = "1h0m0s"
            disabled = false
        """)
        XCTAssertEqual(config.appTimeouts, ["Slack": 300, "Google Chrome": 3600])
        XCTAssertEqual(config.exempt, ["Finder"])
    }

    func testMigrationKeepsTheDefaultExemptionsWhenSystemExcludeIsMissing() {
        let config = ConfigStore.migrateLegacyTOML("[apps.Codex]\ndisabled = true\n")
        XCTAssertEqual(config.exempt, ["Codex", "Finder"])
        XCTAssertEqual(ConfigStore.migrateLegacyTOML("[general]\nsystem_exclude = []\n").exempt, [])
    }

    func testMigrationToleratesCommentsQuotesMultilineArraysAndGarbage() {
        let config = ConfigStore.migrateLegacyTOML("""
        # autohide config
        this line is not toml
        [general]
        default_timeout = "10m"   # ignored
        system_exclude = [
          "Finder",   # file manager
          'Dock Helper',
        ]
        [apps.'Literal Name']
        disabled = true # trailing comment
        [apps."com.example.dotted"]
        timeout = "90"
        [apps.Broken]
        timeout = "soon"
        """)
        XCTAssertEqual(config.timeout, 60)
        XCTAssertEqual(config.exempt, ["Dock Helper", "Finder", "Literal Name"])
        XCTAssertEqual(config.appTimeouts, ["com.example.dotted": 90])
    }

    func testMigratingEmptyTextGivesDefaults() {
        XCTAssertEqual(ConfigStore.migrateLegacyTOML(""), Config.default)
    }

    private func inode(_ url: URL) throws -> UInt64 {
        let attributes = try FileManager.default.attributesOfItem(atPath: url.path)
        return (attributes[.systemFileNumber] as? NSNumber)?.uint64Value ?? 0
    }
}
