import AutohideUICore
import SwiftUI

/// App shell: sable-style left nav rail and a routed content area.
struct MainView: View {
    @EnvironmentObject private var model: MainWindowModel

    var body: some View {
        HStack(spacing: 0) {
            Sidebar()
            Divider().overlay(Theme.Palette.separator)
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Theme.Palette.windowBackground)
        }
        .frame(minWidth: 800, minHeight: 540)
        .background(Theme.Palette.windowBackground)
    }

    @ViewBuilder
    private var content: some View {
        switch model.section {
        case .home: HomePane()
        case .apps: AppsPane()
        case .settings: SettingsPane()
        }
    }
}

/// Full-pane placeholder shown while the daemon socket is unreachable.
struct DaemonDownView: View {
    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: "moon.zzz")
                .font(.system(size: 28, weight: .medium))
                .foregroundStyle(.secondary)
            Text("Daemon not running")
                .font(.system(size: 15, weight: .semibold))
            Text("Start it with `autohide start` — this window reconnects automatically.")
                .font(.system(size: 12))
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: - Sidebar

private struct Sidebar: View {
    @EnvironmentObject private var model: MainWindowModel

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            brand
                .padding(.top, 30)
                .padding(.bottom, 16)
                .padding(.horizontal, 12)

            ForEach(NavSection.allCases) { section in
                NavRow(
                    section: section,
                    isSelected: model.section == section,
                    action: { model.section = section }
                )
            }

            Spacer()

            if model.accessibility == .denied {
                permissionsNote
            }
        }
        .padding(.horizontal, 10)
        .padding(.bottom, 12)
        .frame(width: Theme.Metric.sidebarWidth)
        .frame(maxHeight: .infinity, alignment: .top)
        .background(Theme.Palette.sidebarBackground)
    }

    private var brand: some View {
        HStack(spacing: 9) {
            Image(systemName: "eye.slash")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: 28, height: 28)
                .background(
                    LinearGradient(
                        colors: [Color.accentColor, Color.accentColor.opacity(0.7)],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
                .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
            Text("autohide")
                .font(.system(size: 16, weight: .semibold))
        }
    }

    private var permissionsNote: some View {
        Button {
            model.section = .settings
        } label: {
            HStack(spacing: 7) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.system(size: 11))
                Text("Accessibility needed")
                    .font(.system(size: 11.5, weight: .medium))
                Spacer(minLength: 0)
            }
            .foregroundStyle(Theme.Palette.warn)
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .background(Theme.Palette.warn.opacity(0.12))
            .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
        }
        .buttonStyle(.plain)
        .padding(.horizontal, 2)
    }
}

private struct NavRow: View {
    let section: NavSection
    let isSelected: Bool
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: section.symbol)
                    .font(.system(size: 13, weight: .medium))
                    .frame(width: 18)
                Text(section.title)
                    .font(.system(size: 13.5, weight: .medium))
                Spacer(minLength: 0)
            }
            .foregroundStyle(isSelected ? Color.accentColor : Color.primary)
            .padding(.horizontal, 10)
            .frame(height: 32)
            .background(
                RoundedRectangle(cornerRadius: Theme.Metric.rowCorner, style: .continuous)
                    .fill(isSelected ? Theme.Palette.rowSelected : (hovering ? Theme.Palette.chip : .clear))
            )
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}
