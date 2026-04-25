import Foundation

extension UserDefaults {
    public static let fluxAppGroupSuiteName = "group.me.nore.ig.flux"

    public static let fluxAppGroup: UserDefaults = UserDefaults(suiteName: fluxAppGroupSuiteName) ?? .standard

    private enum Keys {
        static let apiURL = "apiURL"
        static let loadAlertThreshold = "loadAlertThreshold"
        static let widgetUsesSymbols = "widgetUsesSymbols"
    }

    public static let loadAlertThresholdDefault: Double = 3000

    public var apiURL: String? {
        get {
            if let value = string(forKey: Keys.apiURL), !value.isEmpty {
                return value
            }
            if self !== UserDefaults.standard,
               let legacy = UserDefaults.standard.string(forKey: Keys.apiURL),
               !legacy.isEmpty {
                return legacy
            }
            return nil
        }
        set { set(newValue, forKey: Keys.apiURL) }
    }

    public var loadAlertThreshold: Double {
        get {
            let stored = double(forKey: Keys.loadAlertThreshold)
            if stored > 0 { return stored }

            if self !== UserDefaults.standard {
                let legacy = UserDefaults.standard.double(forKey: Keys.loadAlertThreshold)
                if legacy > 0 { return legacy }
            }
            return Self.loadAlertThresholdDefault
        }
        set { set(newValue, forKey: Keys.loadAlertThreshold) }
    }

    public var widgetUsesSymbols: Bool {
        get { bool(forKey: Keys.widgetUsesSymbols) }
        set { set(newValue, forKey: Keys.widgetUsesSymbols) }
    }
}
