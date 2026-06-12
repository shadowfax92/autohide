import SwiftUI

/// Light-only design tokens mirrored from sable-macos's Theme.swift so the
/// window reads as the same family of app.
enum Theme {
    enum Palette {
        static let windowBackground = Color(red: 0.98, green: 0.98, blue: 0.99)
        static let sidebarBackground = Color(red: 0.965, green: 0.965, blue: 0.975)
        static let card = Color.white
        static let cardStroke = Color.black.opacity(0.08)
        static let separator = Color.black.opacity(0.08)
        static let rowSelected = Color.accentColor.opacity(0.14)
        static let chip = Color.black.opacity(0.05)

        static let ok = Color(red: 0.18, green: 0.64, blue: 0.34)
        static let warn = Color(red: 0.85, green: 0.53, blue: 0.06)
        static let error = Color(red: 0.83, green: 0.24, blue: 0.20)
        static let running = Color(red: 0.0, green: 0.48, blue: 1.0)
    }

    enum Metric {
        static let rowCorner: CGFloat = 8
        static let cardCorner: CGFloat = 12
        static let sidebarWidth: CGFloat = 220
    }
}
