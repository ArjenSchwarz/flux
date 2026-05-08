import Foundation

public enum SettingsSuiteMigrator {
    public static let currentVersion: Int = 2

    private static let versionKey = "settingsMigrationVersion"
    /// Removed in version 2 — replaced by `appFontFamily`. The old enum
    /// rawValues (e.g. "geist") were not valid PostScript family names, so
    /// they couldn't be mapped to the new key — just clear them.
    private static let legacyHeroFontKey = "heroFontIdentifier"

    @discardableResult
    public static func run(
        standard: UserDefaults = .standard,
        suite: UserDefaults = .fluxAppGroup
    ) -> Bool {
        let storedVersion = suite.integer(forKey: versionKey)
        if storedVersion >= currentVersion {
            return false
        }

        var changed = false

        if storedVersion < 1 {
            if let apiURL = standard.string(forKey: UserDefaults.apiURLKey),
               suite.string(forKey: UserDefaults.apiURLKey) == nil {
                suite.set(apiURL, forKey: UserDefaults.apiURLKey)
                changed = true
            }

            let standardThreshold = standard.double(forKey: UserDefaults.loadAlertThresholdKey)
            if standardThreshold > 0,
               suite.object(forKey: UserDefaults.loadAlertThresholdKey) == nil {
                suite.set(standardThreshold, forKey: UserDefaults.loadAlertThresholdKey)
                changed = true
            }
        }

        if storedVersion < 2 {
            if suite.object(forKey: legacyHeroFontKey) != nil {
                suite.removeObject(forKey: legacyHeroFontKey)
                changed = true
            }
            if standard.object(forKey: legacyHeroFontKey) != nil {
                standard.removeObject(forKey: legacyHeroFontKey)
                changed = true
            }
        }

        suite.set(currentVersion, forKey: versionKey)
        return changed
    }
}
