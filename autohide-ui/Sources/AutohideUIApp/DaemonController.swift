import AppKit
import AutohideUICore
import Foundation

/// Bridges the window model to the daemon: polls status every 2s (plus the
/// app list while the Apps pane is visible), and runs user actions on a
/// serial background queue so the synchronous socket client never blocks the
/// main thread. Every action ends with a refresh so the UI converges fast.
@MainActor
final class DaemonController {
    private let client: IPCClient
    private let model: MainWindowModel
    private let queue = DispatchQueue(label: "autohide-ui.ipc")
    private var timer: Timer?

    init(model: MainWindowModel, client: IPCClient = IPCClient()) {
        self.model = model
        self.client = client
        wireActions()
    }

    func start() {
        refresh()
        let timer = Timer(timeInterval: 2, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refresh() }
        }
        // .common keeps the poll alive while menus/sliders track.
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    func refresh() {
        let wantApps = model.section == .apps
        queue.async { [client] in
            let status = try? client.status()
            let apps = (status != nil && wantApps) ? try? client.list(windows: true) : nil
            Task { @MainActor [weak self] in
                guard let self else { return }
                self.model.daemonReachable = status != nil
                if let status { self.model.status = status }
                if let apps { self.model.apps = apps.apps }
            }
        }
    }

    private func wireActions() {
        model.onPause = { [weak self] duration in
            self?.perform { client in try client.pause(duration: duration) }
        }
        model.onResume = { [weak self] in
            self?.perform { client in try client.resume() }
        }
        model.onSetFocusMode = { [weak self] on in
            self?.perform { client in try client.setFocusMode(on) }
        }
        model.onSetTimeout = { [weak self] duration in
            self?.perform { client in try client.setTimeout(duration) }
        }
        model.onGrantAccessibility = { [weak self] in self?.grantAccessibility() }
        model.onOpenSystemSettings = { [weak self] kind in self?.openSystemSettings(kind) }
    }

    private func perform(_ work: @escaping (IPCClient) throws -> Void) {
        queue.async { [client] in
            try? work(client)
            Task { @MainActor [weak self] in self?.refresh() }
        }
    }

    /// Fires the system grant dialog via the daemon (the prompt must come
    /// from the daemon's process tree to register the right TCC identity),
    /// then deep-links System Settings so the user lands on the toggle.
    private func grantAccessibility() {
        model.grantInFlight = true
        queue.async { [client] in
            _ = try? client.promptAccessibility()
            Task { @MainActor [weak self] in
                guard let self else { return }
                self.model.grantInFlight = false
                self.openSystemSettings(.accessibility)
                self.refresh()
            }
        }
    }

    private func openSystemSettings(_ kind: PermissionKind) {
        let raw: String
        switch kind {
        case .accessibility:
            raw = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
        case .screenRecording:
            raw = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
        }
        if let url = URL(string: raw) {
            NSWorkspace.shared.open(url)
        }
    }
}
