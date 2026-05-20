import Foundation
#if canImport(Security)
import Security
#endif

/// Reads the embedded `aps-environment` entitlement from the running app
/// and exposes it as a stable string. Two devices on the same backend can
/// be on different APNs hosts (sandbox vs production) — this is the input
/// that tells the backend which host to use for each device.
///
/// The entitlement is set by Xcode at sign time:
/// - Debug builds installed via Xcode → `aps-environment = development`.
/// - TestFlight, App Store, Ad Hoc → `aps-environment = production`.
///
/// `SecTaskCopyValueForEntitlement` is the documented runtime API for an
/// app to read its own entitlements. It is sandbox-safe and does not
/// require any private API.
public enum APNsEnvironment: Sendable {
    public static let development = "development"
    public static let production = "production"

    /// Returns "development" or "production" based on the embedded
    /// `aps-environment` entitlement. Falls back to "development" when the
    /// Security framework can't resolve the entitlement (unit tests, host
    /// processes without an entitlement file). The fallback is the
    /// development host because that matches Xcode-installed builds, which
    /// is the only environment that runs without going through code-sign.
    public static func current() -> String {
        #if canImport(Security)
        guard let task = SecTaskCreateFromSelf(nil) else {
            return development
        }
        var error: Unmanaged<CFError>?
        let value = SecTaskCopyValueForEntitlement(
            task,
            "aps-environment" as CFString,
            &error
        )
        if let string = value as? String, !string.isEmpty {
            return string
        }
        #endif
        return development
    }
}
