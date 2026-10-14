// Client half of the IPC protocol (IPCProtocol.swift): connect to the daemon's
// unix socket, write one request line, read one response line, close. It must
// time out rather than hang the terminal when the daemon is down or wedged.

import Foundation
import AutohideCore

/// Why a request did not get an answer. `notRunning` is the normal "daemon is
/// down" case (no socket file, or a stale one nobody is listening on); every
/// other case means a daemon exists but misbehaved.
enum IPCClientError: Error, CustomStringConvertible {
    case notRunning(socket: String)
    case timedOut(seconds: TimeInterval)
    case socketPathTooLong(String)
    case system(call: String, errno: Int32)
    case badResponse(String)

    var description: String {
        switch self {
        case .notRunning:
            return "autohide is not running"
        case .timedOut(let seconds):
            return "the daemon did not answer within \(seconds)s (it may be wedged; try `autohide restart`)"
        case .socketPathTooLong(let path):
            return "socket path is longer than the 103-byte unix limit: \(path)"
        case .system(let call, let code):
            return "\(call) failed: \(String(cString: strerror(code)))"
        case .badResponse(let detail):
            return "unreadable reply from the daemon: \(detail)"
        }
    }
}

enum IPCClient {
    /// A reply larger than this is a protocol error, not something to buffer.
    private static let maxReplyBytes = 8 << 20

    /// One blocking round trip on a fresh connection. Every blocking call is
    /// bounded by the kernel socket timeouts, which are re-armed with the time
    /// left before each read, so the whole exchange finishes within `timeout`.
    static func send(_ request: IPC.Request, socket: URL, timeout: TimeInterval = 2) throws -> IPC.Response {
        let deadline = Date().addingTimeInterval(timeout)
        let fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw IPCClientError.system(call: "socket", errno: errno) }
        defer { close(fd) }

        // A daemon that closes early must surface as EPIPE, not kill the CLI with SIGPIPE.
        var on: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &on, socklen_t(MemoryLayout<Int32>.size))
        try setTimeout(fd, SO_SNDTIMEO, timeout)
        try setTimeout(fd, SO_RCVTIMEO, timeout)

        try connect(fd, to: socket.path)
        try writeAll(fd, try IPC.encodeLine(request), timeout: timeout)
        let reply = try readLine(fd, deadline: deadline, timeout: timeout)
        do {
            return try IPC.decodeLine(IPC.Response.self, from: reply)
        } catch {
            throw IPCClientError.badResponse(String(describing: error))
        }
    }

    /// Reads up to the first newline (excluded), or to EOF if the server closes
    /// without one.
    private static func readLine(_ fd: Int32, deadline: Date, timeout: TimeInterval) throws -> Data {
        var reply = Data()
        var chunk = [UInt8](repeating: 0, count: 64 * 1024)
        while true {
            let remaining = deadline.timeIntervalSinceNow
            guard remaining > 0 else { throw IPCClientError.timedOut(seconds: timeout) }
            try setTimeout(fd, SO_RCVTIMEO, remaining)

            let count = chunk.withUnsafeMutableBytes { read(fd, $0.baseAddress, $0.count) }
            if count > 0 {
                reply.append(contentsOf: chunk[0..<count])
                if let newline = reply.firstIndex(of: UInt8(ascii: "\n")) {
                    return reply[reply.startIndex..<newline]
                }
                guard reply.count <= maxReplyBytes else {
                    throw IPCClientError.badResponse("reply exceeds \(maxReplyBytes) bytes")
                }
            } else if count == 0 {
                // The server closed without a newline: take what it sent, if anything.
                guard !reply.isEmpty else {
                    throw IPCClientError.badResponse("the daemon closed the connection without replying")
                }
                return reply
            } else {
                let code = errno
                if code == EINTR { continue }
                if code == EAGAIN || code == EWOULDBLOCK { throw IPCClientError.timedOut(seconds: timeout) }
                throw IPCClientError.system(call: "read", errno: code)
            }
        }
    }

    /// Unix-socket connect never waits on macOS: a missing file is ENOENT, a
    /// stale socket file is ECONNREFUSED, and a full backlog is also
    /// ECONNREFUSED. The first two are the ordinary "not running" case.
    private static func connect(_ fd: Int32, to path: String) throws {
        var address = sockaddr_un()
        let pathBytes = Array(path.utf8)
        let capacity = MemoryLayout.size(ofValue: address.sun_path)
        guard pathBytes.count < capacity else { throw IPCClientError.socketPathTooLong(path) }

        address.sun_family = sa_family_t(AF_UNIX)
        address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        withUnsafeMutableBytes(of: &address.sun_path) { $0.copyBytes(from: pathBytes) }

        let result = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard result == 0 else {
            let code = errno
            if code == ENOENT || code == ECONNREFUSED { throw IPCClientError.notRunning(socket: path) }
            throw IPCClientError.system(call: "connect", errno: code)
        }
    }

    private static func writeAll(_ fd: Int32, _ data: Data, timeout: TimeInterval) throws {
        try data.withUnsafeBytes { (buffer: UnsafeRawBufferPointer) in
            var offset = 0
            while offset < buffer.count {
                let written = write(fd, buffer.baseAddress! + offset, buffer.count - offset)
                if written > 0 {
                    offset += written
                    continue
                }
                let code = errno
                if code == EINTR { continue }
                if code == EAGAIN || code == EWOULDBLOCK { throw IPCClientError.timedOut(seconds: timeout) }
                if code == EPIPE || code == ECONNRESET {
                    throw IPCClientError.badResponse("the daemon closed the connection before reading the request")
                }
                throw IPCClientError.system(call: "write", errno: code)
            }
        }
    }

    private static func setTimeout(_ fd: Int32, _ option: Int32, _ seconds: TimeInterval) throws {
        // A zero timeval means "block forever", so never let it reach zero.
        let clamped = max(seconds, 0.001)
        var value = timeval(tv_sec: Int(clamped), tv_usec: Int32((clamped - floor(clamped)) * 1_000_000))
        guard setsockopt(fd, SOL_SOCKET, option, &value, socklen_t(MemoryLayout<timeval>.size)) == 0 else {
            throw IPCClientError.system(call: "setsockopt", errno: errno)
        }
    }
}
