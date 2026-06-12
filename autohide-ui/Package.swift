// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "autohide-ui",
    platforms: [
        .macOS(.v13),
    ],
    products: [
        .executable(name: "autohide-ui", targets: ["AutohideUIApp"]),
        .library(name: "AutohideUICore", targets: ["AutohideUICore"]),
    ],
    targets: [
        .executableTarget(name: "AutohideUIApp", dependencies: ["AutohideUICore"]),
        .target(name: "AutohideUICore"),
        .testTarget(name: "AutohideUICoreTests", dependencies: ["AutohideUICore"]),
    ]
)
