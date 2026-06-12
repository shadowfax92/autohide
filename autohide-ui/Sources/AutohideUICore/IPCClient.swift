import Foundation

public enum IPCError: Error, Equatable {
    case daemonUnreachable
    case daemonError(String)
    case badResponse(String)
}

/// Synchronous client for the daemon's unix-socket JSON-lines protocol —
/// one request line, one response line per connection, mirroring the Go
/// client (autohide/ipc/client.go). Call off the main thread.
public final class IPCClient: Sendable {
    public static var defaultSocketPath: String {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/autohide/autohide.sock").path
    }

    public static let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return decoder
    }()

    private let socketPath: String

    public init(socketPath: String = IPCClient.defaultSocketPath) {
        self.socketPath = socketPath
    }

    // MARK: - Commands

    public func status() throws -> DaemonStatus {
        try send("status", decoding: DaemonStatus.self)
    }

    public func list(windows: Bool) throws -> AppList {
        try send("list", args: windows ? ["windows": "true"] : nil, decoding: AppList.self)
    }

    /// Pass a Go duration string ("30m") for a timed pause, nil for indefinite.
    public func pause(duration: String? = nil) throws {
        try send("pause", args: duration.map { ["duration": $0] })
    }

    public func resume() throws {
        try send("resume")
    }

    public func setFocusMode(_ on: Bool) throws {
        try send(on ? "focus_on" : "focus_off")
    }

    public func setTimeout(_ duration: String) throws {
        try send("set_timeout", args: ["duration": duration])
    }

    public func promptAccessibility() throws -> Bool {
        try send("ax_prompt", decoding: AXPromptResult.self).axTrusted
    }

    // MARK: - Transport

    private struct Request: Encodable {
        let command: String
        let args: [String: String]?
    }

    private struct Envelope<T: Decodable>: Decodable {
        let ok: Bool
        let data: T?
        let error: String?
    }

    private struct NoData: Decodable {}

    func send<T: Decodable>(_ command: String, args: [String: String]? = nil, decoding type: T.Type) throws -> T {
        let envelope: Envelope<T> = try roundTrip(command: command, args: args)
        guard let data = envelope.data else {
            throw IPCError.badResponse("missing data for \(command)")
        }
        return data
    }

    func send(_ command: String, args: [String: String]? = nil) throws {
        let _: Envelope<NoData> = try roundTrip(command: command, args: args)
    }

    private func roundTrip<T: Decodable>(command: String, args: [String: String]?) throws -> Envelope<T> {
        let payload = try JSONEncoder().encode(Request(command: command, args: args))
        let raw = try exchange(payload)
        guard let envelope = try? Self.decoder.decode(Envelope<T>.self, from: raw) else {
            throw IPCError.badResponse(String(data: raw, encoding: .utf8) ?? "<binary>")
        }
        guard envelope.ok else {
            throw IPCError.daemonError(envelope.error ?? "unknown daemon error")
        }
        return envelope
    }

    /// Writes one request line and reads one response line over a fresh
    /// AF_UNIX connection. Connect failures (daemon down, stale socket) map
    /// to .daemonUnreachable; 5s I/O deadlines match the Go client.
    private func exchange(_ payload: Data) throws -> Data {
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw IPCError.daemonUnreachable }
        defer { close(fd) }

        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let pathBytes = Array(socketPath.utf8)
        let capacity = MemoryLayout.size(ofValue: addr.sun_path) - 1
        guard pathBytes.count <= capacity else { throw IPCError.daemonUnreachable }
        withUnsafeMutableBytes(of: &addr.sun_path) { raw in
            raw.copyBytes(from: pathBytes)
        }

        var timeout = timeval(tv_sec: 5, tv_usec: 0)
        let timeoutLen = socklen_t(MemoryLayout<timeval>.size)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, timeoutLen)
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, timeoutLen)

        let connected = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard connected == 0 else { throw IPCError.daemonUnreachable }

        var out = payload
        out.append(0x0A)
        try out.withUnsafeBytes { (buffer: UnsafeRawBufferPointer) in
            var offset = 0
            while offset < buffer.count {
                let written = write(fd, buffer.baseAddress!.advanced(by: offset), buffer.count - offset)
                guard written > 0 else { throw IPCError.daemonUnreachable }
                offset += written
            }
        }

        var response = Data()
        var chunk = [UInt8](repeating: 0, count: 4096)
        while !response.contains(0x0A) {
            let count = read(fd, &chunk, chunk.count)
            if count > 0 {
                response.append(contentsOf: chunk[0..<count])
            } else if count == 0 {
                break
            } else {
                throw IPCError.daemonUnreachable
            }
        }
        guard let newline = response.firstIndex(of: 0x0A) else {
            throw IPCError.badResponse("no response line")
        }
        return response.prefix(upTo: newline)
    }
}
