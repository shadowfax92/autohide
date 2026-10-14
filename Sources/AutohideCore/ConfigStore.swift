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
    ///
    /// Creating the directory and the first save are best-effort: a daemon that
    /// cannot write its config still runs with the returned value. config.toml
    /// is only read, never changed, because the old installed daemon reads it.
    public static func loadOrCreate(_ paths: ConfigPaths) throws -> Config {
        try? FileManager.default.createDirectory(at: paths.directory, withIntermediateDirectories: true)
        if FileManager.default.fileExists(atPath: paths.configFile.path) {
            return try load(paths.configFile)
        }

        let config: Config
        if let legacy = try? String(contentsOf: paths.legacyTOML, encoding: .utf8) {
            config = migrateLegacyTOML(legacy)
        } else {
            config = .default
        }
        try? save(config, to: paths.configFile)
        return config
    }

    public static func load(_ url: URL) throws -> Config {
        try JSONDecoder().decode(Config.self, from: Data(contentsOf: url))
    }

    /// Atomic (write temp + rename), pretty-printed, sorted keys, trailing newline.
    ///
    /// The daemon and the CLI both save, and the daemon reloads on an mtime
    /// change, so a reader must never see a half-written file: Data's .atomic
    /// option writes a temp file in the same directory and renames it over the
    /// target. A symlinked config (dotfiles) is resolved first so the rename
    /// replaces the real file and the link survives.
    public static func save(_ config: Config, to url: URL) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        var data = try encoder.encode(config)
        data.append(0x0A)

        let target = url.resolvingSymlinksInPath()
        try FileManager.default.createDirectory(at: target.deletingLastPathComponent(),
                                                withIntermediateDirectories: true)
        try data.write(to: target, options: .atomic)
    }

    /// Best-effort read of the old Go TOML: `[apps.X]` / `[apps."X Y"]` tables
    /// with `disabled = true` → exempt, `timeout = "5m0s"` → appTimeouts, and
    /// `system_exclude = [...]` → exempt. `default_timeout` is ignored (60 s
    /// default wins). Unknown lines are ignored.
    ///
    /// Like config.json, a missing `system_exclude` falls back to the default
    /// exemptions (Finder), matching what the Go daemon did with a missing key.
    /// This is a line reader for the shapes the Go encoder and a hand edit
    /// produce, not a TOML parser: inline tables and dotted keys are skipped.
    public static func migrateLegacyTOML(_ text: String) -> Config {
        var systemExclude: [String]?
        var disabledApps: [String] = []
        var appTimeouts: [String: TimeInterval] = [:]
        var table: [String] = []

        var lines = text.components(separatedBy: .newlines)[...]
        while var line = lines.popFirst().map(TOMLLine.stripComment) {
            line = line.trimmingCharacters(in: .whitespaces)
            if line.hasPrefix("[") && !line.hasPrefix("[[") && line.hasSuffix("]") {
                table = TOMLLine.splitKey(String(line.dropFirst().dropLast()))
                continue
            }
            guard let equals = TOMLLine.indexOutsideQuotes(of: "=", in: line) else { continue }
            let key = TOMLLine.splitKey(String(line[..<equals]))
            var value = line[line.index(after: equals)...].trimmingCharacters(in: .whitespaces)
            // A multi-line array: keep reading until its closing bracket.
            if value.hasPrefix("[") {
                while TOMLLine.indexOutsideQuotes(of: "]", in: value) == nil, let next = lines.popFirst() {
                    value += " " + TOMLLine.stripComment(next).trimmingCharacters(in: .whitespaces)
                }
            }

            switch (table, key) {
            case (["general"], ["system_exclude"]):
                systemExclude = TOMLLine.stringArray(value)
            case (let path, ["disabled"]) where path.count == 2 && path[0] == "apps":
                if value == "true" { disabledApps.append(path[1]) }
            case (let path, ["timeout"]) where path.count == 2 && path[0] == "apps":
                if let seconds = TOMLLine.string(value).flatMap(DurationFormat.parse) {
                    appTimeouts[path[1]] = seconds
                }
            default:
                continue
            }
        }

        var config = Config(exempt: [], appTimeouts: appTimeouts)
        for app in (systemExclude ?? Config.default.exempt) + disabledApps {
            config.setExempt(app, true)
        }
        return config
    }
}

/// The few TOML lexical rules migrateLegacyTOML needs: quoted strings
/// (basic "…" with \" escapes, literal '…'), `#` comments outside quotes, and
/// dotted keys such as `apps."Wispr Flow"`.
private enum TOMLLine {
    static func stripComment(_ line: String) -> String {
        guard let hash = indexOutsideQuotes(of: "#", in: line) else { return line }
        return String(line[..<hash])
    }

    /// The first `target` that is not inside a quoted string.
    static func indexOutsideQuotes(of target: Character, in text: String) -> String.Index? {
        var quote: Character?
        var escaped = false
        for index in text.indices {
            let character = text[index]
            if let open = quote {
                if escaped { escaped = false }
                else if character == "\\" && open == "\"" { escaped = true }
                else if character == open { quote = nil }
            } else if character == "\"" || character == "'" {
                quote = character
            } else if character == target {
                return index
            }
        }
        return nil
    }

    /// `apps."Wispr Flow"` → ["apps", "Wispr Flow"].
    static func splitKey(_ text: String) -> [String] {
        var parts: [String] = []
        var rest = Substring(text)
        while true {
            let piece = String(rest)
            guard let dot = indexOutsideQuotes(of: ".", in: piece) else {
                parts.append(unquote(piece))
                return parts
            }
            parts.append(unquote(String(piece[..<dot])))
            rest = piece[piece.index(after: dot)...]
        }
    }

    /// `"5m0s"` → 5m0s; nil when the value is not a quoted string.
    static func string(_ value: String) -> String? {
        let value = value.trimmingCharacters(in: .whitespaces)
        guard value.count >= 2, let first = value.first, first == value.last, first == "\"" || first == "'"
        else { return nil }
        return unquote(value)
    }

    /// `["Finder", 'Dock Helper',]` → ["Finder", "Dock Helper"]. Non-string items are dropped.
    static func stringArray(_ value: String) -> [String] {
        var body = value.trimmingCharacters(in: .whitespaces)
        guard body.hasPrefix("["), let close = indexOutsideQuotes(of: "]", in: body) else { return [] }
        body = String(body[body.index(after: body.startIndex)..<close])

        var items: [String] = []
        while !body.isEmpty {
            let end = indexOutsideQuotes(of: ",", in: body) ?? body.endIndex
            if let item = string(String(body[..<end])) { items.append(item) }
            body = end == body.endIndex ? "" : String(body[body.index(after: end)...])
        }
        return items
    }

    private static func unquote(_ text: String) -> String {
        let text = text.trimmingCharacters(in: .whitespaces)
        guard text.count >= 2, let first = text.first, first == text.last, first == "\"" || first == "'" else {
            return text
        }
        let inner = String(text.dropFirst().dropLast())
        guard first == "\"" else { return inner }
        return inner.replacingOccurrences(of: "\\\"", with: "\"").replacingOccurrences(of: "\\\\", with: "\\")
    }
}
