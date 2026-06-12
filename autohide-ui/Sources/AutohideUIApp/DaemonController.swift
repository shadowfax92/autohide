import AppKit
import AutohideUICore
import Combine
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
    private var cancellables = Set<AnyCancellable>()
    /// Skips ticks while one poll is still on the wire so a wedged daemon
    /// (5s deadlines) can't backlog the serial queue and starve actions.
    private var refreshInFlight = false
    /// One missed poll is launch timing (bundle launch starts the UI before
    /// the daemon binds); two in a row is a daemon that's actually down.
    private var consecutiveFailures = 0

    init(model: MainWindowModel, client: IPCClient = IPCClient()) {
        self.model = model
        self.client = client
        wireActions()
        // Refresh the moment the Apps pane opens so it never shows a stale
        // empty state for a poll interval. $section emits before the model
        // property updates, so pass the new value through.
        model.$section
            .removeDuplicates()
            .sink { [weak self] section in
                if section == .apps { self?.refresh(for: section) }
            }
            .store(in: &cancellables)
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
        refresh(for: model.section)
    }

    private func refresh(for section: NavSection) {
        guard !refreshInFlight else { return }
        refreshInFlight = true
        let wantApps = section == .apps
        queue.async { [client] in
            let status = try? client.status()
            let apps = (status != nil && wantApps) ? try? client.list(windows: true) : nil
            Task { @MainActor [weak self] in
                guard let self else { return }
                self.refreshInFlight = false
                if let status {
                    self.consecutiveFailures = 0
                    self.model.daemonReachable = true
                    self.model.status = status
                } else {
                    self.consecutiveFailures += 1
                    if self.consecutiveFailures >= 2 {
                        self.model.daemonReachable = false
                    }
                }
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
            let failure = Self.failureMessage { try work(client) }
            Task { @MainActor [weak self] in
                guard let self else { return }
                if let failure { self.model.lastError = failure }
                self.refresh()
            }
        }
    }

    /// Daemon errors are actionable ("invalid duration: …") and belong in
    /// the banner; unreachability is already rendered by DaemonDownView, so
    /// it stays silent here.
    private nonisolated static func failureMessage(_ work: () throws -> Void) -> String? {
        do {
            try work()
            return nil
        } catch IPCError.daemonError(let message) {
            return message
        } catch IPCError.daemonUnreachable {
            return nil
        } catch {
            return "Unexpected daemon response."
        }
    }

    /// Fires the system grant dialog via the daemon (the prompt must come
    /// from the daemon's process tree to register the right TCC identity).
    /// System Settings opens unless the grant already took effect — even
    /// when the prompt fails (helper missing), toggling autohide there is
    /// the manual remedy.
    private func grantAccessibility() {
        model.grantInFlight = true
        queue.async { [client] in
            var trusted = false
            let failure = Self.failureMessage { trusted = try client.promptAccessibility() }
            Task { @MainActor [weak self] in
                guard let self else { return }
                self.model.grantInFlight = false
                if let failure { self.model.lastError = failure }
                if !trusted { self.openSystemSettings(.accessibility) }
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
