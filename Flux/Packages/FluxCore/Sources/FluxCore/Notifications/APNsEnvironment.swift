import Foundation

/// Reads the embedded `aps-environment` entitlement from the running app
/// and exposes it as a stable string. Two devices on the same backend can
/// be on different APNs hosts (sandbox vs production) — this is the input
/// that tells the backend which host to use for each device.
///
/// The entitlement is set by Xcode at sign time:
/// - Debug builds installed via Xcode → `aps-environment = development`.
/// - TestFlight, App Store, Ad Hoc → `aps-environment = production`.
///
/// The Security framework's `SecTaskCopyValueForEntitlement` is not exposed
/// to Swift through the public module on iOS/macOS, so we read the same
/// value out of the bundled provisioning profile instead. On the Simulator
/// no profile is embedded; the `#if DEBUG` fallback covers that path.
public enum APNsEnvironment: Sendable {
    public static let development = "development"
    public static let production = "production"

    /// Returns "development" or "production" for the running build.
    public static func current() -> String {
        if let env = readApsEnvironmentFromProvisioningProfile() {
            return env
        }
        #if DEBUG
        return development
        #else
        return production
        #endif
    }

    private static func readApsEnvironmentFromProvisioningProfile() -> String? {
        guard let profileURL = profileURL(),
              let data = try? Data(contentsOf: profileURL) else {
            return nil
        }
        // The .mobileprovision / .provisionprofile file is a CMS-signed
        // container wrapping an XML plist. Strip the signature framing by
        // locating the embedded plist payload directly.
        guard let scan = String(data: data, encoding: .ascii),
              let openRange = scan.range(of: "<?xml"),
              let closeRange = scan.range(of: "</plist>") else {
            return nil
        }
        let plistRange = openRange.lowerBound ..< closeRange.upperBound
        guard let plistData = String(scan[plistRange]).data(using: .ascii) else {
            return nil
        }
        guard let profile = try? PropertyListSerialization.propertyList(
            from: plistData, options: [], format: nil) as? [String: Any],
              let entitlements = profile["Entitlements"] as? [String: Any],
              let apsEnv = entitlements["aps-environment"] as? String,
              !apsEnv.isEmpty else {
            return nil
        }
        return apsEnv
    }

    private static func profileURL() -> URL? {
        #if os(iOS)
        return Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision")
        #elseif os(macOS)
        let url = Bundle.main.bundleURL
            .appendingPathComponent("Contents")
            .appendingPathComponent("embedded.provisionprofile")
        return FileManager.default.fileExists(atPath: url.path) ? url : nil
        #else
        return nil
        #endif
    }
}
