import AutohideUICore
import SwiftUI

struct AppsPane: View {
    @EnvironmentObject private var model: MainWindowModel

    var body: some View {
        if !model.daemonReachable {
            DaemonDownView()
        } else {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    PaneHeader(
                        title: "Apps",
                        subtitle: "Window activity the daemon tracks, and when each whole-app hide lands."
                    )

                    if model.apps.isEmpty {
                        emptyCard
                    } else {
                        Card {
                            VStack(alignment: .leading, spacing: 0) {
                                ForEach(Array(model.apps.enumerated()), id: \.element.name) { index, app in
                                    if index > 0 {
                                        Divider().overlay(Theme.Palette.separator).padding(.vertical, 8)
                                    }
                                    AppRow(app: app)
                                }
                            }
                        }
                    }
                }
                .padding(24)
                .frame(maxWidth: 720, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    private var emptyCard: some View {
        Card {
            HStack(spacing: 10) {
                Image(systemName: "sparkles")
                    .foregroundStyle(.secondary)
                Text("Nothing tracked yet — switch between a few apps and they'll appear here.")
                    .font(.system(size: 12.5))
                    .foregroundStyle(.secondary)
            }
        }
    }
}

private struct AppRow: View {
    let app: AppInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Text(app.name)
                    .font(.system(size: 13, weight: .semibold))
                if app.hidden {
                    badge("Hidden", symbol: "eye.slash")
                }
                if app.disabled {
                    badge("Excluded", symbol: "slash.circle")
                }
                if app.unhidable != nil {
                    badge("Fullscreen", symbol: "arrow.up.left.and.arrow.down.right")
                }
                Spacer()
                Text(remainingLabel)
                    .font(.system(size: 12, weight: .medium))
                    .foregroundStyle(remainingColor)
            }

            if let windows = app.windows, !windows.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(windows, id: \.id) { window in
                        HStack {
                            Image(systemName: "macwindow")
                                .font(.system(size: 10))
                                .foregroundStyle(.tertiary)
                            Text(windowDisplayTitle(window.title, id: window.id))
                                .font(.system(size: 12))
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                                .truncationMode(.middle)
                            Spacer()
                        }
                    }
                }
                .padding(.leading, 2)
            }
        }
        .padding(.vertical, 2)
    }

    private var remainingLabel: String {
        if app.disabled { return "never hides" }
        if app.hidden { return "hidden" }
        if let reason = app.unhidable { return "unhidable: \(reason)" }
        if app.timeRemaining == "0s" { return "hiding soon" }
        return "hides in \(prettyGoDuration(app.timeRemaining))"
    }

    private var remainingColor: Color {
        if app.disabled || app.hidden || app.unhidable != nil { return .secondary }
        return Theme.Palette.running
    }

    private func badge(_ text: String, symbol: String) -> some View {
        HStack(spacing: 3) {
            Image(systemName: symbol).font(.system(size: 9))
            Text(text).font(.system(size: 10.5, weight: .medium))
        }
        .foregroundStyle(.secondary)
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(Theme.Palette.chip)
        .clipShape(Capsule())
    }
}
