// Duration strings as they appear in config.json and on the command line.
// Pure. It must also accept what the old Go daemon wrote ("2m0s"), because
// the legacy config.toml is migrated through it.

import Foundation

/// The one parser and printer for durations: config.json, the CLI, and the
/// legacy TOML all go through it. parse admits only whole milliseconds in
/// `resolution...maximum`, and format prints exactly those back, so
/// `parse(format(x)) == x` for every x that parse returns. Config relies on
/// that to never write a file it cannot load again.
public enum DurationFormat {
    private static let unitSeconds: [Character: TimeInterval] = ["h": 3600, "m": 60, "s": 1]

    /// The longest accepted duration: 7 days. Far beyond any useful hide
    /// timeout or pause, and small enough that every derived Date, dispatch
    /// deadline, and millisecond count stays exact.
    public static let maximum: TimeInterval = 7 * 24 * 3600
    /// The shortest accepted duration, and the precision every parsed value is
    /// rounded to. format prints milliseconds, so anything finer would not
    /// survive a save → load.
    public static let resolution: TimeInterval = 0.001

    /// "45s", "1m", "1m30s", "2h", "1h0m0s" (Go style), "90" (bare seconds).
    /// nil for garbage, and for anything non-finite, under 1 ms, or over 7 days.
    /// The result is rounded to whole milliseconds.
    ///
    /// Grammar: a bare number, or one or more `<number><h|m|s>` terms that are
    /// summed. Numbers are plain decimals ("1.5m"); signs, exponents, inner
    /// spaces, and a trailing number without a unit ("1m30") are rejected.
    public static func parse(_ text: String) -> TimeInterval? {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if let bare = number(Substring(text)) {
            return bounded(bare)
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
        guard sawTerm, digits.isEmpty else { return nil }
        return bounded(total)
    }

    /// 45 → "45s", 60 → "1m", 90 → "1m30s", 3600 → "1h", 5400 → "1h30m".
    /// Zero components are omitted; sub-second precision is kept to the
    /// millisecond ("1.5s"). Non-positive or non-finite input formats as "0s".
    /// Never traps: it also runs in log lines for values that did not come
    /// through parse (a raw IPC pause, a hand-built Config).
    public static func format(_ seconds: TimeInterval) -> String {
        guard seconds.isFinite, seconds > 0 else { return "0s" }
        // Integer milliseconds, so 59.9996 cannot print as "60.000s". Int64(_:)
        // traps past Int64.max, so larger values clamp to it. Double(Int64.max)
        // rounds up to 2^63, which is why the test is `<`.
        let scaled = (seconds * 1000).rounded()
        let millis = scaled < Double(Int64.max) ? Int64(scaled) : Int64.max
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

    /// Range check, then whole milliseconds. The bounds apply to the value as
    /// written, so "0.0009s" is refused rather than rounded up to 1 ms. The
    /// rounding makes the result canonical: format prints it exactly, and
    /// parsing that text lands on the same Double.
    private static func bounded(_ seconds: TimeInterval) -> TimeInterval? {
        guard seconds.isFinite, seconds >= resolution, seconds <= maximum else { return nil }
        return (seconds * 1000).rounded() / 1000
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
