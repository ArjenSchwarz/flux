import Foundation

extension UserDefaults {
    public static let fluxAppGroupSuiteName = "group.me.nore.ig.flux"

    public static let fluxAppGroup: UserDefaults = UserDefaults(suiteName: fluxAppGroupSuiteName) ?? .standard

    public static let apiURLKey = "apiURL"
    public static let themeIdentifierKey = "themeIdentifier"

    private enum Keys {
        static let apiURL = UserDefaults.apiURLKey
        static let loadAlertThreshold = "loadAlertThreshold"
        static let widgetUsesSymbols = "widgetUsesSymbols"
        static let heroFontIdentifier = "heroFontIdentifier"
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

    /// Identifier of the font used for the Dashboard hero numeral. Body and
    /// values stay on San Francisco regardless. Persisted as a raw string so
    /// the app group can mirror the value into widgets if needed later.
    public var heroFontIdentifier: String {
        get { string(forKey: Keys.heroFontIdentifier) ?? "" }
        set { set(newValue, forKey: Keys.heroFontIdentifier) }
    }

    /// Identifier of the chosen appearance ("system", "light", "dark").
    /// Empty string means "use the default".
    public var themeIdentifier: String {
        get { string(forKey: Keys.themeIdentifier) ?? "" }
        set { set(newValue, forKey: Keys.themeIdentifier) }
    }
}
