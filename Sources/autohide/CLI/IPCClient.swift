// Client half of the IPC protocol (IPCProtocol.swift): connect to the daemon's
// unix socket, write one request line, read one response line, close. It must
// time out rather than hang the terminal when the daemon is down or wedged.

import Foundation
import AutohideCore

enum IPCClient {
    static func send(_ request: IPC.Request, socket: URL, timeout: TimeInterval = 2) throws -> IPC.Response {
        preconditionFailure("unimplemented")
    }
}
