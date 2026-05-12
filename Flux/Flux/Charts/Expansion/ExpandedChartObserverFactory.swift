import FluxCore
import Foundation

enum ExpandedChartObserverFactory {
    /// Builds the same `FluxAPIClient` configuration as `AppNavigationView`
    /// from the shared app-group settings + keychain token. Returns `nil`
    /// when the app is not yet configured (no URL or no token), in which
    /// case the enlarged presentation falls back to its missing-data
    /// placeholder.
    @MainActor
    static func makeAPIClient(keychainService: KeychainService) -> (any FluxAPIClient)? {
        guard let urlString = UserDefaults.fluxAppGroup.apiURL?
            .trimmingCharacters(in: .whitespacesAndNewlines),
              let url = URL(string: urlString),
              keychainService.loadToken()?.isEmpty == false
        else {
            return nil
        }
        return URLSessionAPIClient(baseURL: url, keychainService: keychainService)
    }
}
