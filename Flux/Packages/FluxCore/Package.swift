// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "FluxCore",
    platforms: [.iOS(.v26)],
    products: [
        .library(name: "FluxCore", targets: ["FluxCore"])
    ],
    targets: [
        .target(
            name: "FluxCore",
            swiftSettings: [.swiftLanguageMode(.v5)]
        ),
        .testTarget(
            name: "FluxCoreTests",
            dependencies: ["FluxCore"],
            swiftSettings: [.swiftLanguageMode(.v5)]
        )
    ]
)
