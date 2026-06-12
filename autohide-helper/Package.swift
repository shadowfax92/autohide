// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "autohide-helper",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(
            name: "autohide-helper",
            path: "Sources"
        )
    ]
)
