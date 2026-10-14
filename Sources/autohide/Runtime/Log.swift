// Log.swift — ISO-8601 timestamped lines to paths.logFile; rotate to .1 at 1 MB.
// Before Log.configure, writes to stderr. Debug lines only when AUTOHIDE_DEBUG=1.
// Every line carries a full date: the old log printed only "12:24PM" (diagnosis R9).

import Foundation

enum Log {
    static func configure(file: URL) {
        preconditionFailure("unimplemented")
    }

    static func debug(_ message: @autoclosure () -> String) {
        guard ProcessInfo.processInfo.environment["AUTOHIDE_DEBUG"] == "1" else { return }
        write("DEBUG", message())
    }

    static func info(_ message: String) { write("INFO", message) }
    static func warn(_ message: String) { write("WARN", message) }
    static func error(_ message: String) { write("ERROR", message) }

    private static func write(_ level: String, _ message: String) {
        let stamp = ISO8601DateFormatter().string(from: Date())
        FileHandle.standardError.write(Data("\(stamp) \(level) \(message)\n".utf8))
    }
}
