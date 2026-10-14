// IPCServer.swift — unix socket server on a background thread; each request → handler on main (sync).
// The socket thread never touches Engine state itself. Main never waits on the
// socket thread, so the DispatchQueue.main.sync hop cannot deadlock.

import Foundation
import AutohideCore

final class IPCServer {
    private let socket: URL
    private let handler: (IPC.Request) -> IPC.Response

    init(socket: URL, handler: @escaping (IPC.Request) -> IPC.Response) {
        self.socket = socket
        self.handler = handler
    }

    func start() throws {   // unlinks a stale socket file, binds, listens, chmod 0600
        preconditionFailure("unimplemented")
    }

    func stop() {
        preconditionFailure("unimplemented")
    }
}
