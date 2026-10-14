// swift-tools-version: 5.9
// One executable (`autohide`: LaunchAgent daemon, menu-bar item, and CLI) on top
// of AutohideCore, a pure library with no AppKit that holds config, IPC types,
// and every timer rule. The tests cover AutohideCore only.
import PackageDescription
let package = Package(
    name: "autohide",
    platforms: [.macOS(.v13)],
    products: [.executable(name: "autohide", targets: ["autohide"])],
    targets: [
        .target(name: "AutohideCore", path: "Sources/AutohideCore"),
        .executableTarget(name: "autohide", dependencies: ["AutohideCore"], path: "Sources/autohide"),
        .testTarget(name: "AutohideCoreTests", dependencies: ["AutohideCore"], path: "Tests/AutohideCoreTests"),
    ]
)
