// Duration strings as they appear in config.json and on the command line.
// Pure. It must also accept what the old Go daemon wrote ("2m0s"), because
// the legacy config.toml is migrated through it.

import Foundation

public enum DurationFormat {
    /// "45s", "1m", "1m30s", "2h", "1h0m0s" (Go style), "90" (bare seconds).
    /// nil for garbage, negative, or zero.
    public static func parse(_ text: String) -> TimeInterval? {
        preconditionFailure("unimplemented")
    }

    /// 45 → "45s", 60 → "1m", 90 → "1m30s", 3600 → "1h", 5400 → "1h30m".
    public static func format(_ seconds: TimeInterval) -> String {
        preconditionFailure("unimplemented")
    }
}
