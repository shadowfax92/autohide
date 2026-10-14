// Where autohide keeps its files, and how config.json is read, written, and
// migrated once from the old Go config.toml. The daemon (on main) and the CLI
// process both write the file, so saves must be atomic: a reader must never
// see a half-written config.

import Foundation

/// Every file one autohide instance uses, all under one directory. A test
/// instance gets its own directory (`--config-dir`), so its socket, log, and
/// lock never collide with the real install.
public struct ConfigPaths: Equatable {
    public var directory: URL        // default ~/.config/autohide
    public var configFile: URL       // <dir>/config.json
    public var legacyTOML: URL       // <dir>/config.toml
    public var socket: URL           // <dir>/autohide.sock  (unix path limit 104 bytes)
    public var logFile: URL          // <dir>/autohide.log
    public var lockFile: URL         // <dir>/autohide.lock

    public init(directory: URL) {
        self.directory = directory
        configFile = directory.appendingPathComponent("config.json")
        legacyTOML = directory.appendingPathComponent("config.toml")
        socket = directory.appendingPathComponent("autohide.sock")
        logFile = directory.appendingPathComponent("autohide.log")
        lockFile = directory.appendingPathComponent("autohide.lock")
    }

    /// $AUTOHIDE_CONFIG_DIR if set, else ~/.config/autohide.
    public static func standard() -> ConfigPaths {
        if let override = ProcessInfo.processInfo.environment["AUTOHIDE_CONFIG_DIR"], !override.isEmpty {
            let path = (override as NSString).expandingTildeInPath
            return ConfigPaths(directory: URL(fileURLWithPath: path, isDirectory: true).standardizedFileURL)
        }
        let home = FileManager.default.homeDirectoryForCurrentUser
        return ConfigPaths(directory: home.appendingPathComponent(".config/autohide", isDirectory: true))
    }
}

public enum ConfigStore {
    /// Creates the directory if needed. config.json exists → load it.
    /// Else config.toml exists → migrate, save, and return the result. Else save
    /// and return defaults. Throws only on an unreadable/corrupt config.json.
    public static func loadOrCreate(_ paths: ConfigPaths) throws -> Config {
        preconditionFailure("unimplemented")
    }

    public static func load(_ url: URL) throws -> Config {
        preconditionFailure("unimplemented")
    }

    /// Atomic (write temp + rename), pretty-printed, sorted keys, trailing newline.
    public static func save(_ config: Config, to url: URL) throws {
        preconditionFailure("unimplemented")
    }

    /// Best-effort read of the old Go TOML: `[apps.X]` / `[apps."X Y"]` tables
    /// with `disabled = true` → exempt, `timeout = "5m0s"` → appTimeouts, and
    /// `system_exclude = [...]` → exempt. `default_timeout` is ignored (60 s
    /// default wins). Unknown lines are ignored.
    public static func migrateLegacyTOML(_ text: String) -> Config {
        preconditionFailure("unimplemented")
    }
}
