// SpaceProbe.swift — method chosen by spike.md (read-only SkyLight CGS calls expected).
// Window bounds cannot tell fullscreen from maximized on the owner's Mac (the menu
// bar auto-hides), so this must ask the window server for the Space type.

enum SpaceProbe {
    /// True when the active Space is a fullscreen / Split View Space. False on any failure.
    static func activeSpaceIsFullscreen() -> Bool {
        preconditionFailure("unimplemented")
    }
}
