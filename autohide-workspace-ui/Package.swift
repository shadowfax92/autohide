// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "autohide-workspace-ui",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(
            name: "autohide-workspace-ui",
            path: "Sources"
        )
    ]
)
