import Foundation

extension UserDefaults {
    static let fluxAppGroup: UserDefaults = {
        guard let defaults = UserDefaults(suiteName: "group.me.nore.ig.flux") else {
            fatalError("App Group 'group.me.nore.ig.flux' is not configured.")
        }
        return defaults
    }()

    private enum Keys {
        static let apiURL = "apiURL"
        static let loadAlertThreshold = "loadAlertThreshold"
    }

    var apiURL: String? {
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

    var loadAlertThreshold: Double {
        get {
            let stored = double(forKey: Keys.loadAlertThreshold)
            if stored > 0 { return stored }

            if self !== UserDefaults.standard {
                let legacy = UserDefaults.standard.double(forKey: Keys.loadAlertThreshold)
                if legacy > 0 { return legacy }
            }
            return 3000
        }
        set { set(newValue, forKey: Keys.loadAlertThreshold) }
    }
}
