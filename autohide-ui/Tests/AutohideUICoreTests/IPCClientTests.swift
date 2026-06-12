import XCTest

@testable import AutohideUICore

/// Minimal line-oriented unix-socket server standing in for the daemon: each
/// connection reads one request line, records it, and answers with the canned
/// response. Mirrors the Go server's one-request-per-connection protocol.
final class FakeDaemon {
    let socketPath: String
    private let response: String
    private let listenFD: Int32
    private let queue = DispatchQueue(label: "fake-daemon")
    private let lock = NSLock()
    private var received: [String] = []
    private var stopped = false

    init(response: String) throws {
        // /tmp keeps the path under the 104-byte sun_path limit.
        self.socketPath = "/tmp/ah-ui-test-\(UUID().uuidString.prefix(8)).sock"
        self.response = response

        listenFD = socket(AF_UNIX, SOCK_STREAM, 0)
        guard listenFD >= 0 else { throw POSIXError(.EIO) }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(socketPath.utf8)
        withUnsafeMutableBytes(of: &addr.sun_path) { raw in
            raw.copyBytes(from: bytes)
        }
        let bindResult = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                bind(listenFD, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard bindResult == 0, listen(listenFD, 4) == 0 else {
            close(listenFD)
            throw POSIXError(.EIO)
        }
        queue.async { [self] in acceptLoop() }
    }

    private func acceptLoop() {
        while true {
            let conn = accept(listenFD, nil, nil)
            guard conn >= 0 else { return }
            var data = Data()
            var chunk = [UInt8](repeating: 0, count: 1024)
            while !data.contains(0x0A) {
                let n = read(conn, &chunk, chunk.count)
                guard n > 0 else { break }
                data.append(contentsOf: chunk[0..<n])
            }
            if let line = String(data: data, encoding: .utf8) {
                lock.lock()
                received.append(line.trimmingCharacters(in: .whitespacesAndNewlines))
                lock.unlock()
            }
            let out = Array((response + "\n").utf8)
            _ = out.withUnsafeBytes { write(conn, $0.baseAddress, $0.count) }
            close(conn)
        }
    }

    var receivedLines: [String] {
        lock.lock()
        defer { lock.unlock() }
        return received
    }

    func stop() {
        if !stopped {
            stopped = true
            close(listenFD)
            unlink(socketPath)
        }
    }

    deinit { stop() }
}

final class IPCClientTests: XCTestCase {
    func testStatusRoundTrip() throws {
        let daemon = try FakeDaemon(
            response: """
                {"ok": true, "data": {"running": true, "paused": false, "focus_mode": true, \
                "uptime": "5s", "tracked_count": 3, "window_tracking": "active", \
                "ax_trusted": true, "screen_recording": false, "default_timeout": "1m", \
                "timeout_presets": ["30s", "1m"]}}
                """)
        defer { daemon.stop() }

        let status = try IPCClient(socketPath: daemon.socketPath).status()
        XCTAssertTrue(status.running)
        XCTAssertTrue(status.focusMode)
        XCTAssertEqual(status.trackedCount, 3)
        XCTAssertEqual(status.axTrusted, true)
        XCTAssertEqual(status.screenRecording, false)

        let sent = try XCTUnwrap(daemon.receivedLines.first)
        XCTAssertTrue(sent.contains("\"command\":\"status\""), "sent: \(sent)")
    }

    func testArgsSerializeOnPause() throws {
        let daemon = try FakeDaemon(response: #"{"ok": true, "data": {"paused": true}}"#)
        defer { daemon.stop() }

        try IPCClient(socketPath: daemon.socketPath).pause(duration: "30m")

        let sent = try XCTUnwrap(daemon.receivedLines.first)
        XCTAssertTrue(sent.contains("\"command\":\"pause\""), "sent: \(sent)")
        XCTAssertTrue(sent.contains("\"duration\":\"30m\""), "sent: \(sent)")
    }

    func testUnreachableSocketThrowsTyped() {
        let client = IPCClient(socketPath: "/tmp/ah-ui-test-definitely-absent.sock")
        XCTAssertThrowsError(try client.status()) { error in
            XCTAssertEqual(error as? IPCError, .daemonUnreachable)
        }
    }

    func testDaemonErrorSurfaces() throws {
        let daemon = try FakeDaemon(response: #"{"ok": false, "error": "invalid duration: nope"}"#)
        defer { daemon.stop() }

        let client = IPCClient(socketPath: daemon.socketPath)
        XCTAssertThrowsError(try client.setTimeout("nope")) { error in
            XCTAssertEqual(error as? IPCError, .daemonError("invalid duration: nope"))
        }
    }

    func testGarbageResponseThrowsBadResponse() throws {
        let daemon = try FakeDaemon(response: "not json at all")
        defer { daemon.stop() }

        XCTAssertThrowsError(try IPCClient(socketPath: daemon.socketPath).status()) { error in
            guard case .badResponse = error as? IPCError else {
                return XCTFail("expected badResponse, got \(error)")
            }
        }
    }

    func testPromptAccessibilityDecodesResult() throws {
        let daemon = try FakeDaemon(response: #"{"ok": true, "data": {"ax_trusted": true}}"#)
        defer { daemon.stop() }

        let trusted = try IPCClient(socketPath: daemon.socketPath).promptAccessibility()
        XCTAssertTrue(trusted)
        let sent = try XCTUnwrap(daemon.receivedLines.first)
        XCTAssertTrue(sent.contains("\"command\":\"ax_prompt\""), "sent: \(sent)")
    }

    func testListRequestsWindows() throws {
        let daemon = try FakeDaemon(response: #"{"ok": true, "data": {"apps": []}}"#)
        defer { daemon.stop() }

        let list = try IPCClient(socketPath: daemon.socketPath).list(windows: true)
        XCTAssertTrue(list.apps.isEmpty)
        let sent = try XCTUnwrap(daemon.receivedLines.first)
        XCTAssertTrue(sent.contains("\"windows\":\"true\""), "sent: \(sent)")
    }
}
