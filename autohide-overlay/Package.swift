// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "autohide-overlay",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(
            name: "autohide-overlay",
            path: "Sources"
        )
    ]
)
