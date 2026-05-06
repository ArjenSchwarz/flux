import Foundation

public enum SettingsSuiteMigrator {
    public static let currentVersion: Int = 2

    private static let versionKey = "settingsMigrationVersion"
    private static let apiURLKey = "apiURL"
    private static let thresholdKey = "loadAlertThreshold"
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
            if let apiURL = standard.string(forKey: apiURLKey),
               suite.string(forKey: apiURLKey) == nil {
                suite.set(apiURL, forKey: apiURLKey)
                changed = true
            }

            let standardThreshold = standard.double(forKey: thresholdKey)
            if standardThreshold > 0,
               suite.object(forKey: thresholdKey) == nil {
                suite.set(standardThreshold, forKey: thresholdKey)
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
