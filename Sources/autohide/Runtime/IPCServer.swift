// IPCServer.swift — unix socket server on a background thread; each request → handler on main (sync).
// The socket thread never touches Engine state itself. Main never waits on the
// socket thread, so the DispatchQueue.main.sync hop cannot deadlock.

import Foundation
import AutohideCore

/// The daemon end of `autohide status|list|pause|…`. One request per connection
/// (IPCProtocol.swift): read one JSON line, run the handler on main, write one
/// JSON line, close. Connections are served one at a time on a private serial
/// queue; a client that stalls is cut off by a 2 s socket timeout, so it can
/// delay the next request but never wedge the daemon.
final class IPCServer {
    enum StartError: Error, CustomStringConvertible {
        case pathTooLong(String)
        case notASocket(String)
        case system(String, Int32)

        var description: String {
            switch self {
            case .pathTooLong(let path): return "socket path is too long for a unix socket (max 103 bytes): \(path)"
            case .notASocket(let path): return "refusing to replace a non-socket file at \(path)"
            case .system(let call, let code): return "\(call) failed: \(String(cString: strerror(code)))"
            }
        }
    }

    static let ioTimeout: TimeInterval = 2
    static let maxRequestBytes = 64 * 1024

    private let socket: URL
    private let handler: (IPC.Request) -> IPC.Response
    private let queue = DispatchQueue(label: "com.autohide.ipc")
    private var source: DispatchSourceRead?

    /// Runs on the IPC queue after a response is fully written and its connection
    /// closed. The daemon uses it to exit on `quit` only once the client has its
    /// reply. Set before `start()`; it is read from the IPC queue afterwards.
    var onResponseSent: ((IPC.Request) -> Void)?

    init(socket: URL, handler: @escaping (IPC.Request) -> IPC.Response) {
        self.socket = socket
        self.handler = handler
    }

    func start() throws {   // unlinks a stale socket file, binds, listens, chmod 0600
        let path = socket.path
        var addr = sockaddr_un()
        let pathBytes = Array(path.utf8)
        guard pathBytes.count < MemoryLayout.size(ofValue: addr.sun_path) else {
            throw StartError.pathTooLong(path)
        }
        try removeStaleSocket(at: path)

        let fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw StartError.system("socket", errno) }
        var started = false
        defer { if !started { close(fd) } }
        _ = fcntl(fd, F_SETFD, FD_CLOEXEC)

        addr.sun_family = sa_family_t(AF_UNIX)
        addr.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        withUnsafeMutableBytes(of: &addr.sun_path) { $0.copyBytes(from: pathBytes) }
        let bound = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                bind(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard bound == 0 else { throw StartError.system("bind \(path)", errno) }
        // Tighten permissions before listen(): until then a connect() is refused,
        // so there is no window in which another user could reach the socket.
        guard chmod(path, 0o600) == 0 else { throw StartError.system("chmod \(path)", errno) }
        // A full AF_UNIX backlog refuses the connect outright (the CLI would read
        // that as "not running"), so take the largest queue the kernel allows.
        guard listen(fd, SOMAXCONN) == 0 else { throw StartError.system("listen", errno) }
        // Non-blocking so a spurious readiness event cannot park the queue in accept().
        _ = fcntl(fd, F_SETFL, fcntl(fd, F_GETFL) | O_NONBLOCK)

        let source = DispatchSource.makeReadSource(fileDescriptor: fd, queue: queue)
        source.setEventHandler { [weak self] in self?.acceptPending(on: fd) }
        // The cancel handler runs after any in-flight event handler returns, so
        // the descriptor is never closed under a connection being served.
        source.setCancelHandler { close(fd) }
        source.resume()
        self.source = source
        started = true
        Log.info("ipc listening on \(path)")
    }

    /// Stops accepting and removes the socket file. Never waits for the IPC queue:
    /// it may be blocked in DispatchQueue.main.sync on the very thread calling this.
    func stop() {
        guard let source else { return }
        source.cancel()
        self.source = nil
        unlink(socket.path)
    }

    // MARK: - IPC queue

    private func removeStaleSocket(at path: String) throws {
        var info = stat()
        guard lstat(path, &info) == 0 else {
            if errno == ENOENT { return }
            throw StartError.system("lstat \(path)", errno)
        }
        // The daemon holds the per-directory lock, so any socket here is a leftover
        // from a previous run. Anything that is not a socket is not ours to delete.
        guard (info.st_mode & S_IFMT) == S_IFSOCK else { throw StartError.notASocket(path) }
        guard unlink(path) == 0 else { throw StartError.system("unlink \(path)", errno) }
    }

    private func acceptPending(on listener: Int32) {
        while true {
            let client = accept(listener, nil, nil)
            if client < 0 {
                if errno == EINTR { continue }
                return   // EAGAIN: backlog drained
            }
            serve(client)
        }
    }

    private func serve(_ client: Int32) {
        configureClient(client)
        var request: IPC.Request?
        let response: IPC.Response

        switch readLine(from: client) {
        case .failure(let failure):
            Log.warn("ipc: \(failure.reason)")
            response = IPC.Response(ok: false, error: failure.reason)
        case .success(let line):
            do {
                let decoded = try IPC.decodeLine(IPC.Request.self, from: line)
                request = decoded
                // Thread hop: Engine state is main-thread only. `sync` makes this
                // connection's reply the handler's result. It cannot deadlock
                // because main never waits on this queue (stop() only cancels).
                response = DispatchQueue.main.sync { handler(decoded) }
            } catch {
                Log.warn("ipc: malformed request (\(line.count) bytes): \(error)")
                response = IPC.Response(ok: false, error: "malformed request: \(error.localizedDescription)")
            }
        }

        let data = (try? IPC.encodeLine(response))
            ?? Data("{\"ok\":false,\"error\":\"internal: response encoding failed\"}\n".utf8)
        writeAll(data, to: client)
        close(client)
        if let request { onResponseSent?(request) }
    }

    private func configureClient(_ fd: Int32) {
        // accept() inherits O_NONBLOCK from the listener on Darwin; the reads below
        // rely on the socket timeouts instead, so turn it back off.
        _ = fcntl(fd, F_SETFL, fcntl(fd, F_GETFL) & ~O_NONBLOCK)
        _ = fcntl(fd, F_SETFD, FD_CLOEXEC)
        // A client that hangs up before reading its reply must cost an EPIPE, not
        // a SIGPIPE that kills the daemon.
        var on: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &on, socklen_t(MemoryLayout<Int32>.size))
        var timeout = timeval(tv_sec: Int(Self.ioTimeout), tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
    }

    private struct ReadFailure: Error { let reason: String }

    /// Bytes up to (not including) the first newline. A client that closes its
    /// write side without a newline still gets its bytes decoded.
    private func readLine(from fd: Int32) -> Result<Data, ReadFailure> {
        var buffer = Data()
        var chunk = [UInt8](repeating: 0, count: 4096)
        while buffer.count <= Self.maxRequestBytes {
            let n = chunk.withUnsafeMutableBytes { recv(fd, $0.baseAddress, $0.count, 0) }
            if n > 0 {
                buffer.append(contentsOf: chunk[0..<n])
                if let newline = buffer.firstIndex(of: UInt8(ascii: "\n")) {
                    return .success(Data(buffer[..<newline]))
                }
            } else if n == 0 {
                return buffer.isEmpty ? .failure(ReadFailure(reason: "empty request")) : .success(buffer)
            } else if errno == EINTR {
                continue
            } else if errno == EAGAIN || errno == EWOULDBLOCK {
                return .failure(ReadFailure(reason: "timed out waiting for a request line"))
            } else {
                return .failure(ReadFailure(reason: "read failed: \(String(cString: strerror(errno)))"))
            }
        }
        return .failure(ReadFailure(reason: "request exceeds \(Self.maxRequestBytes) bytes"))
    }

    private func writeAll(_ data: Data, to fd: Int32) {
        data.withUnsafeBytes { raw in
            guard var cursor = raw.baseAddress else { return }
            var remaining = raw.count
            while remaining > 0 {
                let n = send(fd, cursor, remaining, 0)
                if n < 0 {
                    if errno == EINTR { continue }
                    return   // the client went away or timed out; nothing else to do
                }
                cursor += n
                remaining -= n
            }
        }
    }
}
