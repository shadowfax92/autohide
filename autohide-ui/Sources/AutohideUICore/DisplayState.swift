import Foundation

// Pure presentation mapping for daemon state, kept UI-framework-free so it
// stays unit-testable.

public enum Severity: Equatable {
    case ok
    case warn
    case neutral
}

public struct TrackingDisplay: Equatable {
    public let label: String
    public let severity: Severity
}

/// Maps the daemon's window_tracking mode string (daemon/daemon.go
/// resolveWindowStatus) to a user-facing label + severity.
public func trackingDisplay(_ raw: String?) -> TrackingDisplay {
    switch raw {
    case "active":
        return TrackingDisplay(label: "Per-window", severity: .ok)
    case "app-only: accessibility not granted":
        return TrackingDisplay(label: "App-only — grant Accessibility", severity: .warn)
    case "off":
        return TrackingDisplay(label: "Off", severity: .neutral)
    case "starting":
        return TrackingDisplay(label: "Starting…", severity: .neutral)
    case let .some(value) where value.hasPrefix("legacy"):
        let detail = value.dropFirst("legacy:".count).trimmingCharacters(in: .whitespaces)
        return TrackingDisplay(label: "App-level — \(detail)", severity: .warn)
    default:
        return TrackingDisplay(label: "Unknown", severity: .neutral)
    }
}

public enum PermissionState: Equatable {
    case unknown
    case granted
    case denied

    public init(_ granted: Bool?) {
        switch granted {
        case .some(true): self = .granted
        case .some(false): self = .denied
        case .none: self = .unknown
        }
    }
}

/// Prettifies Go duration strings ("1h2m0s" → "1h 2m"): keeps the two
/// largest nonzero units, "now" for zero, and passes through anything it
/// can't parse so odd daemon output still renders.
public func prettyGoDuration(_ raw: String) -> String {
    var hours = 0, minutes = 0, seconds = 0
    var number = ""
    var parsedUnits = 0
    for char in raw {
        if char.isNumber {
            number.append(char)
        } else {
            guard let value = Int(number) else { return raw }
            switch char {
            case "h": hours = value
            case "m": minutes = value
            case "s": seconds = value
            default: return raw
            }
            number = ""
            parsedUnits += 1
        }
    }
    guard number.isEmpty, parsedUnits > 0 else { return raw }

    if hours > 0 {
        return minutes > 0 ? "\(hours)h \(minutes)m" : "\(hours)h"
    }
    if minutes > 0 {
        return seconds > 0 ? "\(minutes)m \(seconds)s" : "\(minutes)m"
    }
    return seconds > 0 ? "\(seconds)s" : "now"
}

/// Pause menu options; durationArg is the IPC pause arg (nil = indefinite).
public enum PausePreset: CaseIterable {
    case indefinite
    case thirtyMinutes
    case oneHour
    case twoHours

    public var durationArg: String? {
        switch self {
        case .indefinite: return nil
        case .thirtyMinutes: return "30m"
        case .oneHour: return "1h"
        case .twoHours: return "2h"
        }
    }

    public var menuLabel: String {
        switch self {
        case .indefinite: return "Until resumed"
        case .thirtyMinutes: return "For 30 minutes"
        case .oneHour: return "For 1 hour"
        case .twoHours: return "For 2 hours"
        }
    }
}

/// Window titles need Screen Recording; fall back to the stable window ID.
public func windowDisplayTitle(_ title: String?, id: UInt32) -> String {
    guard let title, !title.isEmpty else { return "Window \(id)" }
    return title
}

/// Renders the daemon's RFC3339 resume_at as a countdown; nil when the
/// timestamp doesn't parse (the caller falls back to a plain "Paused").
public func resumeCountdown(from rfc3339: String, now: Date) -> String? {
    guard let date = ISO8601DateFormatter().date(from: rfc3339) else { return nil }
    let remaining = Int(date.timeIntervalSince(now).rounded())
    guard remaining > 0 else { return "resuming…" }
    let hours = remaining / 3600
    let minutes = (remaining % 3600) / 60
    let seconds = remaining % 60
    if hours > 0 {
        return minutes > 0 ? "resumes in \(hours)h \(minutes)m" : "resumes in \(hours)h"
    }
    if minutes > 0 {
        return "resumes in \(minutes)m"
    }
    return "resumes in \(seconds)s"
}
