import SwiftUI

struct OverlayView: View {
    let info: FocusInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(info.task)
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(.white)
                    .lineLimit(1)

                Spacer()

                Text(info.paused ? "paused" : info.timeString)
                    .font(.system(size: 13, weight: .bold, design: .monospaced))
                    .foregroundStyle(timeColor)
            }

            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    RoundedRectangle(cornerRadius: 3)
                        .fill(.white.opacity(0.15))
                        .frame(height: 5)

                    RoundedRectangle(cornerRadius: 3)
                        .fill(barGradient)
                        .frame(width: geo.size.width * info.progress, height: 5)
                }
            }
            .frame(height: 5)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .frame(width: 240)
        .background(.ultraThinMaterial)
        .background(Color.black.opacity(0.3))
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .strokeBorder(.white.opacity(0.1), lineWidth: 0.5)
        )
    }

    private var timeColor: Color {
        if info.paused { return .yellow }
        if info.remainingSeconds <= 60 { return .red }
        if info.remainingSeconds <= 300 { return .orange }
        return .white
    }

    private var barGradient: LinearGradient {
        if info.remainingSeconds <= 60 {
            return LinearGradient(colors: [.red, .orange], startPoint: .leading, endPoint: .trailing)
        }
        return LinearGradient(colors: [.cyan, .blue], startPoint: .leading, endPoint: .trailing)
    }
}

#Preview {
    OverlayView(info: .placeholder)
        .padding()
        .background(.black)
}
