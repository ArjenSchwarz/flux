import Foundation

extension UserDefaults {
    private enum Keys {
        static let apiURL = "apiURL"
        static let loadAlertThreshold = "loadAlertThreshold"
    }

    var apiURL: String? {
        get { string(forKey: Keys.apiURL) }
        set { set(newValue, forKey: Keys.apiURL) }
    }

    var loadAlertThreshold: Double {
        get {
            let storedValue = double(forKey: Keys.loadAlertThreshold)
            return storedValue == 0 ? 3000 : storedValue
        }
        set { set(newValue, forKey: Keys.loadAlertThreshold) }
    }
}
