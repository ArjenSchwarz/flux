import Foundation

extension UserDefaults {
    public static let fluxAppGroupSuiteName = "group.me.nore.ig.flux"

    public static let fluxAppGroup: UserDefaults = UserDefaults(suiteName: fluxAppGroupSuiteName) ?? .standard

    public static let apiURLKey = "apiURL"
    public static let themeIdentifierKey = "themeIdentifier"
    public static let appFontFamilyKey = "appFontFamily"

    private enum Keys {
        static let apiURL = UserDefaults.apiURLKey
        static let loadAlertThreshold = "loadAlertThreshold"
        static let widgetUsesSymbols = "widgetUsesSymbols"
        static let appFontFamily = UserDefaults.appFontFamilyKey
        static let themeIdentifier = UserDefaults.themeIdentifierKey
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

    /// PostScript family name applied to all text in the app. Empty string
    /// means "use the system font" (San Francisco).
    public var appFontFamily: String {
        get { string(forKey: Keys.appFontFamily) ?? "" }
        set { set(newValue, forKey: Keys.appFontFamily) }
    }

    /// Identifier of the chosen appearance ("system", "light", "dark").
    /// Empty string means "use the default".
    public var themeIdentifier: String {
        get { string(forKey: Keys.themeIdentifier) ?? "" }
        set { set(newValue, forKey: Keys.themeIdentifier) }
    }
}
