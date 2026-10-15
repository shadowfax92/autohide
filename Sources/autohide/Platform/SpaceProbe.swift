// SpaceProbe.swift — method chosen by spike.md (read-only SkyLight CGS calls).
// Window bounds cannot tell fullscreen from maximized on the owner's Mac (the menu
// bar auto-hides, and Monocle owns a display-sized window on the normal desktop),
// so this asks the window server for the active Space's type instead.

import Foundation

/// Answers "is the user on a Space where nothing may hide?" for WindowProbe.
///
/// While SkyLight answers, it fails CLOSED: only type 0 (a normal desktop)
/// reads as not-fullscreen, and any other value pauses hiding. spike.md Q4
/// proved type 0 on the normal desktop but could not observe a fullscreen
/// Space (yabai reads 4), so an unexpected value must not hide a Split View
/// partner.
///
/// If the private symbols are gone (a future macOS), it fails OPEN: false,
/// with one warning in the log. Returning true there would silently disable
/// every hide forever, which is worse than the rare fullscreen-Space miss.
/// There is deliberately no AXFullScreen fallback: the core path needs no
/// Accessibility (design.md §2).
enum SpaceProbe {
    /// CGSSpaceGetType of an ordinary desktop Space (spike.md Q4, three routes agree).
    static let normalDesktopType: Int32 = 0

    /// True when the active Space is a fullscreen / Split View Space, or any
    /// other non-desktop type. False when SkyLight's symbols cannot be resolved.
    /// ~0.4 µs; no permission needed.
    static func activeSpaceIsFullscreen() -> Bool {
        guard let skyLight = SkyLight.shared else { return false }
        return skyLight.activeSpaceType() != normalDesktopType
    }

    /// Raw CGSSpaceGetType of the active Space, or nil when the symbols are
    /// missing or there is no active Space. For logs and diagnostics only;
    /// decisions go through activeSpaceIsFullscreen().
    static func activeSpaceType() -> Int32? {
        SkyLight.shared?.activeSpaceType()
    }

    /// Private, read-only SkyLight entry points, resolved once with dlsym.
    /// dlsym rather than a linked import: if Apple removes a symbol, a hard
    /// import makes dyld kill the daemon at launch, while dlsym just yields nil.
    /// The CGS* names are tried first and the SLS* aliases second; both exist
    /// on macOS 26.6 (spike.md gotcha 7).
    private struct SkyLight {
        typealias MainConnection = @convention(c) () -> Int32
        typealias GetActiveSpace = @convention(c) (Int32) -> UInt64
        typealias SpaceGetType = @convention(c) (Int32, UInt64) -> Int32

        let mainConnection: MainConnection
        let activeSpace: GetActiveSpace
        let spaceType: SpaceGetType

        /// Resolved on first use; a static let initializer runs exactly once,
        /// so the failure warning is logged once per process.
        static let shared: SkyLight? = {
            guard let main = lookup(["CGSMainConnectionID", "SLSMainConnectionID"], as: MainConnection.self),
                  let active = lookup(["CGSGetActiveSpace", "SLSGetActiveSpace"], as: GetActiveSpace.self),
                  let type = lookup(["CGSSpaceGetType", "SLSSpaceGetType"], as: SpaceGetType.self)
            else {
                Log.warn("SpaceProbe: SkyLight Space symbols not found; fullscreen Spaces cannot be detected, hiding stays enabled on them")
                return nil
            }
            return SkyLight(mainConnection: main, activeSpace: active, spaceType: type)
        }()

        /// nil when there is no active Space id (0), which activeSpaceIsFullscreen
        /// reads as "not a desktop" and so pauses hiding.
        func activeSpaceType() -> Int32? {
            let connection = mainConnection()
            let space = activeSpace(connection)
            guard space != 0 else { return nil }
            return spaceType(connection, space)
        }

        private static func lookup<T>(_ names: [String], as: T.Type) -> T? {
            // RTLD_DEFAULT; AppKit has already loaded CoreGraphics and SkyLight.
            let rtldDefault = UnsafeMutableRawPointer(bitPattern: -2)
            for name in names {
                if let symbol = dlsym(rtldDefault, name) { return unsafeBitCast(symbol, to: T.self) }
            }
            return nil
        }
    }
}
