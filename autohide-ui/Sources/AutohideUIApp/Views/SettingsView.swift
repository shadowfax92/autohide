import AppKit
import AutohideUICore
import SwiftUI

struct SettingsPane: View {
    @EnvironmentObject private var model: MainWindowModel

    private var configPath: String {
        (NSHomeDirectory() as NSString).appendingPathComponent(".config/autohide/config.toml")
    }

    var body: some View {
        if !model.daemonReachable {
            DaemonDownView()
        } else {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    PaneHeader(title: "Settings", subtitle: "Permissions and timing.")

                    permissionsCard
                    generalCard
                    footer
                }
                .padding(24)
                .frame(maxWidth: 720, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    // MARK: - Permissions

    private var permissionsCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                SectionLabel(text: "Permissions")
                PermissionRow(
                    title: "Accessibility",
                    detail: "Required — lets autohide read the focused window and minimize stale ones.",
                    symbol: "accessibility",
                    state: model.accessibility,
                    buttonTitle: model.accessibility == .granted ? "Settings" : "Grant",
                    busy: model.grantInFlight,
                    action: {
                        if model.accessibility == .granted {
                            model.onOpenSystemSettings?(.accessibility)
                        } else {
                            model.onGrantAccessibility?()
                        }
                    }
                )
                if model.accessibility == .denied {
                    grantHint
                }
                Divider().overlay(Theme.Palette.separator)
                PermissionRow(
                    title: "Screen Recording",
                    detail: "Optional — shows window titles in the Apps list.",
                    symbol: "camera.viewfinder",
                    state: model.screenRecording,
                    buttonTitle: "Settings",
                    action: { model.onOpenSystemSettings?(.screenRecording) }
                )
            }
        }
    }

    private var grantHint: some View {
        HStack(alignment: .top, spacing: 7) {
            Image(systemName: "info.circle")
                .font(.system(size: 11))
            // Single literal: concatenation infers String, which skips
            // markdown parsing and renders the ** verbatim.
            Text("Toggle **autohide** on in System Settings → Privacy & Security → Accessibility. If it's already on after a reinstall, switch it off and on again.")
                .font(.system(size: 11.5))
        }
        .foregroundStyle(.secondary)
        .padding(10)
        .background(Theme.Palette.chip)
        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
    }

    // MARK: - General

    private var generalCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                SectionLabel(text: "General")
                FieldRow(
                    title: "Hide after",
                    help: "How long an app or window can sit unused before it's hidden."
                ) {
                    Picker("", selection: timeoutBinding) {
                        ForEach(timeoutOptions, id: \.self) { preset in
                            Text(preset).tag(preset)
                        }
                    }
                    .pickerStyle(.menu)
                    .labelsHidden()
                    .frame(width: 110)
                }
            }
        }
    }

    /// Presets from the daemon, plus the current value when it's custom so
    /// the picker never renders blank.
    private var timeoutOptions: [String] {
        var options = model.status?.timeoutPresets ?? ["30s", "1m", "2m", "5m"]
        if let current = model.status?.defaultTimeout, !options.contains(current) {
            options.append(current)
        }
        return options
    }

    private var timeoutBinding: Binding<String> {
        Binding(
            get: { model.status?.defaultTimeout ?? "1m" },
            set: { model.onSetTimeout?($0) }
        )
    }

    // MARK: - Footer

    private var footer: some View {
        HStack(spacing: 8) {
            Image(systemName: "doc.text").foregroundStyle(.secondary)
            Text(configPath)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 8)
            Button("Reveal") {
                NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: configPath)])
            }
            .buttonStyle(.borderless)
            .controlSize(.small)
        }
        .padding(.horizontal, 4)
    }
}
