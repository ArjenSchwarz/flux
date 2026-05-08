import Foundation

extension UserDefaults {
    public static let fluxAppGroupSuiteName = "group.me.nore.ig.flux"

    public static let fluxAppGroup: UserDefaults = UserDefaults(suiteName: fluxAppGroupSuiteName) ?? .standard

    public static let apiURLKey = "apiURL"
    public static let themeIdentifierKey = "themeIdentifier"
    public static let appFontFamilyKey = "appFontFamily"
    public static let loadAlertThresholdKey = "loadAlertThreshold"
    public static let widgetUsesSymbolsKey = "widgetUsesSymbols"
    public static let lastSeenWhatsNewVersionKey = "lastSeenWhatsNewVersion"

    public static let loadAlertThresholdDefault: Double = 3000

    public var apiURL: String? {
        get {
            if let value = string(forKey: Self.apiURLKey), !value.isEmpty {
                return value
            }
            if self !== UserDefaults.standard,
               let legacy = UserDefaults.standard.string(forKey: Self.apiURLKey),
               !legacy.isEmpty {
                return legacy
            }
            return nil
        }
        set { set(newValue, forKey: Self.apiURLKey) }
    }

    public var loadAlertThreshold: Double {
        get {
            let stored = double(forKey: Self.loadAlertThresholdKey)
            if stored > 0 { return stored }

            if self !== UserDefaults.standard {
                let legacy = UserDefaults.standard.double(forKey: Self.loadAlertThresholdKey)
                if legacy > 0 { return legacy }
            }
            return Self.loadAlertThresholdDefault
        }
        set { set(newValue, forKey: Self.loadAlertThresholdKey) }
    }

    public var widgetUsesSymbols: Bool {
        get { bool(forKey: Self.widgetUsesSymbolsKey) }
        set { set(newValue, forKey: Self.widgetUsesSymbolsKey) }
    }

    /// PostScript family name applied to all text in the app. Empty string
    /// means "use the system font" (San Francisco).
    public var appFontFamily: String {
        get { string(forKey: Self.appFontFamilyKey) ?? "" }
        set { set(newValue, forKey: Self.appFontFamilyKey) }
    }

    /// Identifier of the chosen appearance ("system", "light", "dark").
    /// Empty string means "use the default".
    public var themeIdentifier: String {
        get { string(forKey: Self.themeIdentifierKey) ?? "" }
        set { set(newValue, forKey: Self.themeIdentifierKey) }
    }

    public var lastSeenWhatsNewVersion: String? {
        get { string(forKey: Self.lastSeenWhatsNewVersionKey) }
        set { set(newValue, forKey: Self.lastSeenWhatsNewVersionKey) }
    }
}

extension UserDefaults {
    /// True if any pre-existing Flux preference key has ever been written on
    /// this device. Used to distinguish a fresh install from a pre-feature
    /// upgrade in the What's New auto-presentation flow.
    public var hasAnyFluxPreferenceWritten: Bool {
        let known: [String] = [
            Self.apiURLKey,
            Self.themeIdentifierKey,
            Self.appFontFamilyKey,
            Self.loadAlertThresholdKey,
            Self.widgetUsesSymbolsKey
        ]
        return known.contains { object(forKey: $0) != nil }
    }
}
