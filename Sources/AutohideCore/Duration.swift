// Duration strings as they appear in config.json and on the command line.
// Pure. It must also accept what the old Go daemon wrote ("2m0s"), because
// the legacy config.toml is migrated through it.

import Foundation

public enum DurationFormat {
    private static let unitSeconds: [Character: TimeInterval] = ["h": 3600, "m": 60, "s": 1]

    /// "45s", "1m", "1m30s", "2h", "1h0m0s" (Go style), "90" (bare seconds).
    /// nil for garbage, negative, or zero.
    ///
    /// Grammar: a bare number, or one or more `<number><h|m|s>` terms that are
    /// summed. Numbers are plain decimals ("1.5m"); signs, exponents, inner
    /// spaces, and a trailing number without a unit ("1m30") are rejected.
    public static func parse(_ text: String) -> TimeInterval? {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if let bare = number(Substring(text)) {
            return bare > 0 ? bare : nil
        }

        var total: TimeInterval = 0
        var digits = Substring()
        var sawTerm = false
        for character in text {
            if let unit = unitSeconds[character] {
                guard let value = number(digits) else { return nil }
                total += value * unit
                digits = Substring()
                sawTerm = true
            } else if character.isASCII && (character.isNumber || character == ".") {
                digits.append(character)
            } else {
                return nil
            }
        }
        guard sawTerm, digits.isEmpty, total > 0, total.isFinite else { return nil }
        return total
    }

    /// 45 → "45s", 60 → "1m", 90 → "1m30s", 3600 → "1h", 5400 → "1h30m".
    /// Zero components are omitted; sub-second precision is kept to the
    /// millisecond ("1.5s"). Non-positive or non-finite input formats as "0s".
    public static func format(_ seconds: TimeInterval) -> String {
        guard seconds.isFinite, seconds > 0 else { return "0s" }
        // Integer milliseconds, so 59.9996 cannot print as "60.000s".
        let millis = Int64((seconds * 1000).rounded())
        let hours = millis / 3_600_000
        let minutes = millis % 3_600_000 / 60_000
        let secondMillis = millis % 60_000

        var text = ""
        if hours > 0 { text += "\(hours)h" }
        if minutes > 0 { text += "\(minutes)m" }
        if secondMillis > 0 || text.isEmpty {
            text += "\(secondMillis / 1000)"
            if secondMillis % 1000 != 0 {
                var fraction = String(format: "%03lld", secondMillis % 1000)
                while fraction.hasSuffix("0") { fraction.removeLast() }
                text += ".\(fraction)"
            }
            text += "s"
        }
        return text
    }

    /// A plain non-negative decimal ("90", "1.5"). Double(_:) alone would also
    /// accept "nan", "inf", "1e3", and "+5", none of which are durations.
    private static func number(_ text: Substring) -> TimeInterval? {
        guard !text.isEmpty,
              text.allSatisfy({ $0.isASCII && ($0.isNumber || $0 == ".") }),
              text.filter({ $0 == "." }).count <= 1,
              text.contains(where: \.isNumber)
        else { return nil }
        return TimeInterval(text)
    }
}
