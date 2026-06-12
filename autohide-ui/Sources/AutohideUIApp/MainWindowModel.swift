import AutohideUICore
import Combine
import Foundation

enum NavSection: String, CaseIterable, Identifiable {
    case home
    case apps
    case settings

    var id: String { rawValue }

    var title: String {
        switch self {
        case .home: return "Home"
        case .apps: return "Apps"
        case .settings: return "Settings"
        }
    }

    var symbol: String {
        switch self {
        case .home: return "house"
        case .apps: return "macwindow.on.rectangle"
        case .settings: return "gearshape"
        }
    }
}

enum PermissionKind {
    case accessibility
    case screenRecording
}

/// Observable backing store for the SwiftUI window. `DaemonController` owns
/// the action callbacks and pushes fresh daemon state in from its poll loop.
@MainActor
final class MainWindowModel: ObservableObject {
    @Published var section: NavSection = .home
    @Published var status: DaemonStatus?
    @Published var apps: [AppInfo] = []
    /// Optimistic until the first poll answers, so the not-running banner
    /// doesn't flash during launch.
    @Published var daemonReachable = true
    @Published var grantInFlight = false
    /// Last failed action's message, rendered as a dismissible banner.
    @Published var lastError: String?

    var onPause: ((_ duration: String?) -> Void)?
    var onResume: (() -> Void)?
    var onSetFocusMode: ((Bool) -> Void)?
    var onSetTimeout: ((String) -> Void)?
    var onGrantAccessibility: (() -> Void)?
    var onOpenSystemSettings: ((PermissionKind) -> Void)?

    var accessibility: PermissionState { PermissionState(status?.axTrusted) }
    var screenRecording: PermissionState { PermissionState(status?.screenRecording) }
}
