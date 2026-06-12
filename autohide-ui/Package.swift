// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "autohide-ui",
    platforms: [
        .macOS(.v13),
    ],
    products: [
        .library(name: "AutohideUICore", targets: ["AutohideUICore"]),
    ],
    targets: [
        .target(name: "AutohideUICore"),
        .testTarget(name: "AutohideUICoreTests", dependencies: ["AutohideUICore"]),
    ]
)
