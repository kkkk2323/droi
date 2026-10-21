// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "DroiKit",
    platforms: [.iOS(.v18), .macOS(.v15)],
    products: [
        .library(name: "DroiKit", targets: ["DroiKit"])
    ],
    dependencies: [
        // Switched to the GitHub URL at a tag before release.
        .package(path: "/Users/clive/dev/droid-sdk-swift")
    ],
    targets: [
        .target(
            name: "DroiKit",
            dependencies: [.product(name: "DroidProtocol", package: "droid-sdk-swift")]
        ),
        .testTarget(name: "DroiKitTests", dependencies: ["DroiKit"]),
    ]
)
