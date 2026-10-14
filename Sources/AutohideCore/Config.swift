// The user's settings as a plain value, plus the rules for matching a config
// entry ("Finder", "com.tinyspeck.slackmacgap") to a running app. Pure: it never
// touches disk (ConfigStore does that). The Engine owns the live copy on main.

import Foundation

public typealias PID = Int32

/// How config matches an app: by display name or bundle ID, never by PID.
/// Timers are keyed by PID instead, so a relaunched app gets a fresh timer.
public struct AppIdentity: Hashable, Codable {
    public var name: String
    public var bundleID: String?

    public init(name: String, bundleID: String? = nil) {
        self.name = name
        self.bundleID = bundleID
    }
}

/// Name normalization for config entries. Apps report names with stray format
/// characters (the old daemon saw both "WhatsApp" and "\u{200E}WhatsApp"), so
/// every comparison goes through `normalize`.
public enum AppMatch {
    /// Lowercased, whitespace-trimmed, Unicode category Cf removed (U+200E, U+200B, U+FEFF…).
    public static func normalize(_ text: String) -> String {
        preconditionFailure("unimplemented")
    }

    /// An entry matches when it normalizes equal to the app's name or bundle ID.
    public static func matches(_ entry: String, _ app: AppIdentity) -> Bool {
        preconditionFailure("unimplemented")
    }
}

/// Everything the user can configure. Policy reads it on every decision, the
/// CLI and menu bar edit it, and ConfigStore persists it as config.json.
public struct Config: Equatable {
    public static let defaultTimeout: TimeInterval = 60
    public static let minimumTimeout: TimeInterval = 5
    public var timeout: TimeInterval                 // JSON "timeout": "60s"
    public var exempt: [String]                      // JSON "exempt": ["Finder"]
    public var appTimeouts: [String: TimeInterval]   // JSON "app_timeouts": {"Slack": "5m"}
    public var onlyApps: [String]                    // JSON "only_apps": [] (empty = all apps)

    public init(timeout: TimeInterval = Config.defaultTimeout,
                exempt: [String] = ["Finder"],
                appTimeouts: [String: TimeInterval] = [:],
                onlyApps: [String] = []) {
        self.timeout = timeout
        self.exempt = exempt
        self.appTimeouts = appTimeouts
        self.onlyApps = onlyApps
    }

    public static var `default`: Config { Config() }

    /// nil = never hide (exempt, or outside a non-empty onlyApps).
    /// Otherwise the per-app override or the default, clamped to minimumTimeout.
    public func effectiveTimeout(for app: AppIdentity) -> TimeInterval? {
        preconditionFailure("unimplemented")
    }

    public func isExempt(_ app: AppIdentity) -> Bool {
        preconditionFailure("unimplemented")
    }

    /// Adds/removes an exemption by normalized match; keeps the list deduplicated and sorted.
    public mutating func setExempt(_ app: String, _ exempt: Bool) {
        preconditionFailure("unimplemented")
    }
}

// The on-disk shape of config.json. Durations are human strings ("60s", "5m")
// so the file stays hand-editable, keys are snake_case, and a missing key falls
// back to its default so an old or partial file still loads. Unknown keys are
// ignored. A duration that does not parse is a corrupt file, not a default.
extension Config: Codable {
    private enum CodingKeys: String, CodingKey {
        case timeout
        case exempt
        case appTimeouts = "app_timeouts"
        case onlyApps = "only_apps"
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let defaults = Config.default

        func duration(_ text: String, _ key: CodingKeys) throws -> TimeInterval {
            guard let seconds = DurationFormat.parse(text) else {
                throw DecodingError.dataCorruptedError(
                    forKey: key, in: container, debugDescription: "invalid duration \"\(text)\"")
            }
            return seconds
        }

        let timeoutText = try container.decodeIfPresent(String.self, forKey: .timeout)
        let appTimeoutTexts = try container.decodeIfPresent([String: String].self, forKey: .appTimeouts) ?? [:]
        var appTimeouts: [String: TimeInterval] = [:]
        for (app, text) in appTimeoutTexts {
            appTimeouts[app] = try duration(text, .appTimeouts)
        }

        self.init(
            timeout: try timeoutText.map { try duration($0, .timeout) } ?? defaults.timeout,
            exempt: try container.decodeIfPresent([String].self, forKey: .exempt) ?? defaults.exempt,
            appTimeouts: appTimeouts,
            onlyApps: try container.decodeIfPresent([String].self, forKey: .onlyApps) ?? defaults.onlyApps)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(DurationFormat.format(timeout), forKey: .timeout)
        try container.encode(exempt, forKey: .exempt)
        try container.encode(appTimeouts.mapValues(DurationFormat.format), forKey: .appTimeouts)
        try container.encode(onlyApps, forKey: .onlyApps)
    }
}
