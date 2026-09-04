// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "natterwire",
    platforms: [.macOS(.v14)],
    products: [
        .library(name: "NatterwireCore", targets: ["NatterwireCore"]),
        .executable(name: "natterwire", targets: ["Natterwire"]),
    ],
    targets: [
        .target(
            name: "NatterwireCore",
            linkerSettings: [.linkedLibrary("sqlite3")]
        ),
        .executableTarget(name: "Natterwire", dependencies: ["NatterwireCore"]),
        .testTarget(name: "NatterwireTests", dependencies: ["NatterwireCore"]),
    ]
)
