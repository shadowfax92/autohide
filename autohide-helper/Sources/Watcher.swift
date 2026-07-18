import AppKit
import Foundation

private struct WatchEvent: Encodable {
    let ts: Int64
    let type: String
    let pid: Int32?
    let name: String?
}

private final class WorkspaceWatcher {
    private let workspaceCenter = NSWorkspace.shared.notificationCenter
    private let distributedCenter = DistributedNotificationCenter.default()
    private var observers: [NSObjectProtocol] = []
    private let outputLock = NSLock()

    /// Streams workspace and session events until the parent closes stdin.
    func run() -> Never {
        observeWorkspace(NSWorkspace.didActivateApplicationNotification, type: "activate", includeApp: true)
        observeWorkspace(NSWorkspace.didDeactivateApplicationNotification, type: "deactivate", includeApp: true)
        observeWorkspace(NSWorkspace.activeSpaceDidChangeNotification, type: "space")
        observeWorkspace(NSWorkspace.willSleepNotification, type: "sleep")
        observeWorkspace(NSWorkspace.didWakeNotification, type: "wake")
        observeDistributed("com.apple.screenIsLocked", type: "lock")
        observeDistributed("com.apple.screenIsUnlocked", type: "unlock")

        DispatchQueue.global(qos: .utility).async {
            FileHandle.standardInput.readDataToEndOfFile()
            exit(0)
        }
        RunLoop.main.run()
        exit(0)
    }

    private func observeWorkspace(
        _ name: NSNotification.Name,
        type: String,
        includeApp: Bool = false
    ) {
        observers.append(workspaceCenter.addObserver(forName: name, object: nil, queue: nil) { [self] note in
            if includeApp {
                guard let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication else {
                    return
                }
                emit(type: type, app: app)
            } else {
                emit(type: type)
            }
        })
    }

    private func observeDistributed(_ name: String, type: String) {
        observers.append(distributedCenter.addObserver(
            forName: Notification.Name(name),
            object: nil,
            queue: nil
        ) { [self] _ in
            emit(type: type)
        })
    }

    private func emit(type: String, app: NSRunningApplication? = nil) {
        let event = WatchEvent(
            ts: Int64(Date().timeIntervalSince1970 * 1_000),
            type: type,
            pid: app?.processIdentifier,
            name: app.map { $0.localizedName ?? "" }
        )
        let line = encodeJSON(event) + "\n"
        outputLock.lock()
        FileHandle.standardOutput.write(Data(line.utf8))
        outputLock.unlock()
    }
}

/// Runs the daemon-owned workspace event stream.
func runWatcher() -> Never {
    WorkspaceWatcher().run()
}
