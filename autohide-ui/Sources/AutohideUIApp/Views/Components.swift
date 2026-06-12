import AutohideUICore
import SwiftUI

// Reusable styled controls mirrored from sable-macos's component vocabulary.

/// Title + subtitle header shown at the top of every pane.
struct PaneHeader: View {
    let title: String
    let subtitle: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
                .font(.system(size: 22, weight: .semibold))
            Text(subtitle)
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// White rounded card used to group settings and content.
struct Card<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        content
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.Palette.card)
            .clipShape(RoundedRectangle(cornerRadius: Theme.Metric.cardCorner, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: Theme.Metric.cardCorner, style: .continuous)
                    .stroke(Theme.Palette.cardStroke, lineWidth: 1)
            )
    }
}

/// A labelled settings row: title (+ optional help) on the left, control on the right.
struct FieldRow<Control: View>: View {
    let title: String
    var help: String? = nil
    @ViewBuilder var control: Control

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: 13, weight: .medium))
                if let help {
                    Text(help)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 12)
            control
        }
        .frame(maxWidth: .infinity)
    }
}

struct SectionLabel: View {
    let text: String

    var body: some View {
        Text(text.uppercased())
            .font(.system(size: 11, weight: .semibold))
            .kerning(0.6)
            .foregroundStyle(.secondary)
    }
}

struct StatusChip: View {
    let text: String
    let symbol: String
    let color: Color
    var busy: Bool = false

    var body: some View {
        HStack(spacing: 5) {
            if busy {
                ProgressView().controlSize(.small).scaleEffect(0.6).frame(width: 12, height: 12)
            } else {
                Image(systemName: symbol).font(.system(size: 11, weight: .semibold))
            }
            Text(text).font(.system(size: 12, weight: .medium))
        }
        .foregroundStyle(color)
        .padding(.horizontal, 9)
        .padding(.vertical, 4)
        .background(color.opacity(0.12))
        .clipShape(Capsule())
    }
}

extension Severity {
    var color: Color {
        switch self {
        case .ok: return Theme.Palette.ok
        case .warn: return Theme.Palette.warn
        case .neutral: return .secondary
        }
    }
}

extension PermissionState {
    var chipText: String {
        switch self {
        case .granted: return "Granted"
        case .denied: return "Needed"
        case .unknown: return "Checking…"
        }
    }

    var chipSymbol: String {
        switch self {
        case .granted: return "checkmark.circle.fill"
        case .denied: return "exclamationmark.circle.fill"
        case .unknown: return "circle.dotted"
        }
    }

    var chipColor: Color {
        switch self {
        case .granted: return Theme.Palette.ok
        case .denied: return Theme.Palette.warn
        case .unknown: return .secondary
        }
    }
}

struct PermissionRow: View {
    let title: String
    let detail: String
    let symbol: String
    let state: PermissionState
    let buttonTitle: String
    var busy: Bool = false
    let action: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: symbol)
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(.secondary)
                .frame(width: 30, height: 30)
                .background(Theme.Palette.chip)
                .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))

            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 13, weight: .semibold))
                Text(detail).font(.system(size: 11)).foregroundStyle(.secondary)
            }

            Spacer(minLength: 10)

            StatusChip(
                text: state.chipText,
                symbol: state.chipSymbol,
                color: state.chipColor,
                busy: busy
            )

            Button(buttonTitle, action: action)
                .controlSize(.small)
                .disabled(busy)
        }
    }
}
