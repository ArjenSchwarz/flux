// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "FluxCore",
    platforms: [.iOS(.v26)],
    products: [
        .library(name: "FluxCore", targets: ["FluxCore"])
    ],
    targets: [
        .target(name: "FluxCore"),
        .testTarget(name: "FluxCoreTests", dependencies: ["FluxCore"])
    ]
)
