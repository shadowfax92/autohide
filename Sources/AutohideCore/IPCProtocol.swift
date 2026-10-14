// The wire format between `autohide <command>` (IPCClient, CLI process) and the
// daemon (IPCServer). One request per connection: the client writes one JSON
// line, the server writes one JSON line and closes. Both sides must go through
// encodeLine/decodeLine so they always agree on date encoding (ISO-8601).

import Foundation

public enum IPC {
    public enum Command: String, Codable { case status, list, pause, resume, hideOthers = "hide-others", reload, quit }

    public struct Request: Codable, Equatable {
        public var command: Command
        public var seconds: Double?            // pause only; nil = until resumed

        public init(command: Command, seconds: Double? = nil) {
            self.command = command
            self.seconds = seconds
        }
    }

    /// Daemon state for `autohide status`.
    public struct StatusPayload: Codable, Equatable {
        public var pid: Int32
        public var version: String
        public var startedAt: Date
        public var paused: Bool
        public var pausedUntil: Date?          // nil while paused = until resumed
        public var away: Bool
        public var fullscreenSpace: Bool
        public var timeout: Double
        public var exempt: [String]
        public var appTimeouts: [String: Double]
        public var onlyApps: [String]
        public var protectedApp: String?
        public var configPath: String
        public var logPath: String
        public var accessibility: Bool         // informational: AX fallback available

        public init(pid: Int32, version: String, startedAt: Date, paused: Bool, pausedUntil: Date?,
                    away: Bool, fullscreenSpace: Bool, timeout: Double, exempt: [String],
                    appTimeouts: [String: Double], onlyApps: [String], protectedApp: String?,
                    configPath: String, logPath: String, accessibility: Bool) {
            self.pid = pid
            self.version = version
            self.startedAt = startedAt
            self.paused = paused
            self.pausedUntil = pausedUntil
            self.away = away
            self.fullscreenSpace = fullscreenSpace
            self.timeout = timeout
            self.exempt = exempt
            self.appTimeouts = appTimeouts
            self.onlyApps = onlyApps
            self.protectedApp = protectedApp
            self.configPath = configPath
            self.logPath = logPath
            self.accessibility = accessibility
        }
    }

    /// One row of `autohide list`.
    public struct AppRow: Codable, Equatable {
        public var pid: Int32
        public var name: String
        public var bundleID: String?
        public var state: String               // AppStatus.label
        public var hidesIn: Double?            // seconds, pending only
        public var timeout: Double?            // nil when exempt

        public init(pid: Int32, name: String, bundleID: String?, state: String, hidesIn: Double?, timeout: Double?) {
            self.pid = pid
            self.name = name
            self.bundleID = bundleID
            self.state = state
            self.hidesIn = hidesIn
            self.timeout = timeout
        }
    }

    public struct Response: Codable, Equatable {
        public var ok: Bool
        public var error: String?
        public var status: StatusPayload?
        public var apps: [AppRow]?
        public var hidden: Int?                // hide-others count

        public init(ok: Bool, error: String? = nil, status: StatusPayload? = nil, apps: [AppRow]? = nil, hidden: Int? = nil) {
            self.ok = ok
            self.error = error
            self.status = status
            self.apps = apps
            self.hidden = hidden
        }
    }

    /// One line of JSON terminated by "\n". Compact output keeps it one line:
    /// JSON escapes any newline inside a string, so the only raw "\n" is the
    /// terminator the reader waits for.
    public static func encodeLine<T: Encodable>(_ value: T) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        encoder.dateEncodingStrategy = .custom { date, encoder in
            var container = encoder.singleValueContainer()
            try container.encode(date.formatted(fractionalISO8601))
        }
        var data = try encoder.encode(value)
        data.append(0x0A)
        return data
    }

    /// Decodes the first line of `data`; the terminator is optional, so a reader
    /// that hit EOF before "\n" still decodes.
    public static func decodeLine<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        let line = data.firstIndex(of: 0x0A).map { data[data.startIndex..<$0] } ?? data[...]
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let text = try container.decode(String.self)
            if let date = try? fractionalISO8601.parse(text) { return date }
            if let date = try? wholeISO8601.parse(text) { return date }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "not an ISO-8601 date: \(text)")
        }
        return try decoder.decode(T.self, from: Data(line))
    }

    // Millisecond ISO-8601 in UTC ("1970-01-12T13:46:40.000Z"). JSONEncoder's
    // built-in .iso8601 drops fractions, which would make a pause deadline or
    // startedAt differ after a round trip. Whole-second dates are still
    // accepted so a hand-written request decodes. FormatStyles are values, so
    // sharing them across the socket thread and main is safe.
    private static let fractionalISO8601 = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
    private static let wholeISO8601 = Date.ISO8601FormatStyle()
}
