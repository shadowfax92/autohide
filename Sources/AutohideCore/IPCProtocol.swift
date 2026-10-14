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

    /// One line of JSON terminated by "\n".
    public static func encodeLine<T: Encodable>(_ value: T) throws -> Data {
        preconditionFailure("unimplemented")
    }

    public static func decodeLine<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        preconditionFailure("unimplemented")
    }
}
