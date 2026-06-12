import SwiftUI

// Placeholder panes; replaced by the real Home/Apps/Settings views.

struct HomePane: View {
    @EnvironmentObject private var model: MainWindowModel

    var body: some View {
        if model.daemonReachable {
            PaneHeader(title: "Home", subtitle: "Daemon status and quick actions.")
                .padding(24)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        } else {
            DaemonDownView()
        }
    }
}

struct AppsPane: View {
    var body: some View {
        PaneHeader(title: "Apps", subtitle: "Tracked apps and windows.")
            .padding(24)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

struct SettingsPane: View {
    var body: some View {
        PaneHeader(title: "Settings", subtitle: "Permissions and timing.")
            .padding(24)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}
