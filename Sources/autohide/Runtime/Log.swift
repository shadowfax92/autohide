// Log.swift — ISO-8601 timestamped lines to paths.logFile; rotate to .1 at 1 MB.
// Before Log.configure, writes to stderr. Debug lines only when AUTOHIDE_DEBUG=1.
// Every line carries a full date: the old log printed only "12:24PM" (diagnosis R9).

import Foundation

/// The daemon's only log sink. Called from main and from the IPC queue, so every
/// write is serialized by one lock. Writes go straight to the file descriptor
/// (no buffering), so nothing is lost when the process exits mid-run.
enum Log {
    static let maxBytes: UInt64 = 1 << 20

    private static let lock = NSLock()
    private static var fileURL: URL?
    private static var handle: FileHandle?
    private static var bytesWritten: UInt64 = 0
    // Read once: the environment cannot change under a running daemon.
    private static let debugEnabled = ProcessInfo.processInfo.environment["AUTOHIDE_DEBUG"] == "1"
    // A foreground run in a terminal also echoes to stderr. Under launchd stderr is
    // launchd.log, which must not get a second copy of every line.
    private static let echoToStderr = isatty(STDERR_FILENO) == 1
    // Local time with its UTC offset and milliseconds, e.g. 2026-10-14T16:45:12.345-07:00.
    private static let stamp: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        formatter.timeZone = .current
        return formatter
    }()

    /// Appends to `file` from now on, rotating first if it is already over the limit.
    /// On failure the log stays on stderr, which launchd captures in launchd.log.
    static func configure(file: URL) {
        lock.lock()
        defer { lock.unlock() }
        fileURL = file
        try? FileManager.default.createDirectory(
            at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        openLocked()
        if bytesWritten >= maxBytes { rotateLocked() }
    }

    static func debug(_ message: @autoclosure () -> String) {
        guard debugEnabled else { return }
        write("DEBUG", message())
    }

    static func info(_ message: String) { write("INFO", message) }
    static func warn(_ message: String) { write("WARN", message) }
    static func error(_ message: String) { write("ERROR", message) }

    private static func write(_ level: String, _ message: String) {
        lock.lock()
        defer { lock.unlock() }
        // One event per line, so a multi-line error description cannot split it.
        let flat = message.replacingOccurrences(of: "\n", with: " ")
        let line = Data("\(stamp.string(from: Date())) \(level) \(flat)\n".utf8)

        if handle != nil, bytesWritten + UInt64(line.count) > maxBytes {
            rotateLocked()
        }
        guard let target = handle else {
            FileHandle.standardError.write(line)
            return
        }
        do {
            // The throwing API: FileHandle.write(_: Data) raises an uncatchable
            // Objective-C exception on a full disk and would take the daemon down.
            try target.write(contentsOf: line)
            bytesWritten += UInt64(line.count)
            if echoToStderr { FileHandle.standardError.write(line) }
        } catch {
            FileHandle.standardError.write(line)
        }
    }

    private static func openLocked() {
        guard let url = fileURL else { return }
        // O_APPEND keeps every write at the true end of the file even if another
        // process (a second daemon refused by the lock) appended a line meanwhile.
        let fd = open(url.path, O_WRONLY | O_APPEND | O_CREAT | O_CLOEXEC, 0o644)
        guard fd >= 0 else {
            handle = nil
            FileHandle.standardError.write(Data("autohide: cannot open log \(url.path): errno \(errno)\n".utf8))
            return
        }
        var info = stat()
        bytesWritten = fstat(fd, &info) == 0 ? UInt64(info.st_size) : 0
        handle = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
    }

    /// autohide.log → autohide.log.1 (replacing the previous .1), then a fresh file.
    private static func rotateLocked() {
        guard let url = fileURL else { return }
        try? handle?.close()
        handle = nil
        let rotated = URL(fileURLWithPath: url.path + ".1")
        try? FileManager.default.removeItem(at: rotated)
        try? FileManager.default.moveItem(at: url, to: rotated)
        openLocked()
    }
}
