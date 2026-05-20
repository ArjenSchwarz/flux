import Foundation

/// A stable per-install identifier used by the SoC alerts backend to scope
/// rules, fire-state, and the APNs token (Decision 3 / Decision 8). Stored
/// in the app's container `UserDefaults` so iOS/macOS uninstall resets it,
/// which Decision 5 / AC 4.6 explicitly want.
///
/// NOT stored in the Keychain (survives uninstall on iOS) and NOT in the
/// app-group `UserDefaults` (the widget extension would keep it alive after
/// the host app is deleted).
public struct DeviceIdentifier: Sendable {
    public static let key = "flux.device.id"

    private let defaults: UserDefaults

    /// Construct against an explicit defaults suite. Tests pass an isolated
    /// `UserDefaults(suiteName:)`; production constructs `shared`.
    public init(userDefaults: UserDefaults) {
        self.defaults = userDefaults
    }

    /// Returns the existing identifier or generates and stores a new one.
    /// Subsequent reads return the persisted value. Idempotent.
    public func currentOrGenerate() -> String {
        if let existing = defaults.string(forKey: Self.key), !existing.isEmpty {
            return existing
        }
        let fresh = UUID().uuidString
        defaults.set(fresh, forKey: Self.key)
        return fresh
    }

    /// Production accessor. Uses `UserDefaults.standard` (the app's own
    /// container suite). Documented at the call site (the SoCAlertsService
    /// init wrapper) so future maintainers see why this is not `.fluxAppGroup`.
    public static let shared = DeviceIdentifier(userDefaults: .standard)
}
