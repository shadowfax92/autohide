import AutohideUICore
import SwiftUI

struct HomePane: View {
    @EnvironmentObject private var model: MainWindowModel

    var body: some View {
        if !model.daemonReachable {
            DaemonDownView()
        } else {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    PaneHeader(
                        title: "Welcome to autohide",
                        subtitle: "Apps you stop using quietly disappear; summon them back any time."
                    )

                    statusCard
                    actionsCard
                    howItWorksCard
                }
                .padding(24)
                .frame(maxWidth: 720, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    // MARK: - Status

    private var stateChip: StatusChip {
        guard let status = model.status else {
            return StatusChip(text: "Connecting…", symbol: "circle.dotted", color: .secondary, busy: true)
        }
        if status.paused {
            let countdown = status.resumeAt.flatMap { resumeCountdown(from: $0, now: Date()) }
            return StatusChip(
                text: countdown.map { "Paused — \($0)" } ?? "Paused",
                symbol: "pause.circle.fill",
                color: Theme.Palette.warn
            )
        }
        if status.focusMode {
            return StatusChip(text: "Focus Mode", symbol: "scope", color: Theme.Palette.running)
        }
        return StatusChip(text: "Active", symbol: "checkmark.circle.fill", color: Theme.Palette.ok)
    }

    private var statusCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    SectionLabel(text: "Status")
                    Spacer()
                    stateChip
                }
                Divider().overlay(Theme.Palette.separator)
                detailRow(label: "Tracked apps", value: "\(model.status?.trackedCount ?? 0)")
                detailRow(label: "Uptime", value: prettyGoDuration(model.status?.uptime ?? "0s"))
                HStack {
                    Text("Window tracking")
                        .font(.system(size: 13))
                        .foregroundStyle(.secondary)
                    Spacer()
                    let tracking = trackingDisplay(model.status?.windowTracking)
                    Text(tracking.label)
                        .font(.system(size: 13, weight: .medium))
                        .foregroundStyle(tracking.severity.color)
                }
                Divider().overlay(Theme.Palette.separator)
                HStack(spacing: 10) {
                    permissionChip(state: model.accessibility, label: "Accessibility")
                    permissionChip(state: model.screenRecording, label: "Screen Recording")
                    Spacer()
                }
            }
        }
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
            Spacer()
            Text(value)
                .font(.system(size: 13, weight: .medium))
        }
    }

    private func permissionChip(state: PermissionState, label: String) -> some View {
        Button {
            model.section = .settings
        } label: {
            StatusChip(text: label, symbol: state.chipSymbol, color: state.chipColor)
        }
        .buttonStyle(.plain)
    }

    // MARK: - Quick actions

    private var actionsCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                SectionLabel(text: "Quick actions")
                FieldRow(
                    title: model.status?.paused == true ? "Resume hiding" : "Pause hiding",
                    help: "Stop the timers while presenting or screen sharing."
                ) {
                    if model.status?.paused == true {
                        Button("Resume") { model.onResume?() }
                            .controlSize(.small)
                    } else {
                        Menu {
                            ForEach(Array(PausePreset.allCases.enumerated()), id: \.offset) { _, preset in
                                Button(preset.menuLabel) { model.onPause?(preset.durationArg) }
                            }
                        } label: {
                            Text("Pause")
                        }
                        .controlSize(.small)
                        .frame(width: 90)
                    }
                }
                Divider().overlay(Theme.Palette.separator)
                FieldRow(
                    title: "Focus Mode",
                    help: "Immediately hide everything except the app you're using."
                ) {
                    Toggle("", isOn: focusBinding)
                        .toggleStyle(.switch)
                        .labelsHidden()
                        .controlSize(.small)
                }
            }
        }
    }

    private var focusBinding: Binding<Bool> {
        Binding(
            get: { model.status?.focusMode ?? false },
            set: { model.onSetFocusMode?($0) }
        )
    }

    // MARK: - How it works

    private var howItWorksCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 10) {
                SectionLabel(text: "How it works")
                howRow(number: "1", text: "Use your apps normally — autohide watches which windows are active.")
                howRow(number: "2", text: "Apps you stop using are hidden after the timeout; stale windows minimize to the Dock.")
                howRow(number: "3", text: "Switch back (or un-minimize) and the timer starts fresh.")
            }
        }
    }

    private func howRow(number: String, text: String) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Text(number)
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(.white)
                .frame(width: 18, height: 18)
                .background(Circle().fill(Color.accentColor))
            Text(text)
                .font(.system(size: 13))
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
