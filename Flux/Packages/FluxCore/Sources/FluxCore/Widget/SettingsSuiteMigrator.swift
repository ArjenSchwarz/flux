import Foundation

public enum SettingsSuiteMigrator {
    public static let currentVersion: Int = 1

    private static let versionKey = "settingsMigrationVersion"
    private static let apiURLKey = "apiURL"
    private static let thresholdKey = "loadAlertThreshold"

    @discardableResult
    public static func run(
        standard: UserDefaults = .standard,
        suite: UserDefaults = UserDefaults(suiteName: "group.me.nore.ig.flux")!
    ) -> Bool {
        if suite.integer(forKey: versionKey) >= currentVersion {
            return false
        }

        var copied = false

        if let apiURL = standard.string(forKey: apiURLKey),
           suite.string(forKey: apiURLKey) == nil {
            suite.set(apiURL, forKey: apiURLKey)
            copied = true
        }

        let standardThreshold = standard.double(forKey: thresholdKey)
        if standardThreshold > 0,
           suite.object(forKey: thresholdKey) == nil {
            suite.set(standardThreshold, forKey: thresholdKey)
            copied = true
        }

        suite.set(currentVersion, forKey: versionKey)
        return copied
    }
}
