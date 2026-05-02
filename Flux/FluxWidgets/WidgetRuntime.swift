import FluxCore
import Foundation

enum WidgetRuntime {
    static let session: URLSession = {
        let config = URLSessionConfiguration.default
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.urlCache = nil
        config.timeoutIntervalForRequest = 5
        config.timeoutIntervalForResource = 5
        config.waitsForConnectivity = false
        return URLSession(configuration: config)
    }()

    static func makeLogic() -> StatusTimelineLogic {
        let cache = WidgetSnapshotCache()
        let keychain = KeychainService()
        let client = makeAPIClient(keychain: keychain)
        return StatusTimelineLogic(
            apiClient: client,
            cache: cache,
            tokenProvider: { keychain.loadToken() }
        )
    }

    static func makeAPIClient(keychain: KeychainService) -> (any FluxAPIClient)? {
        guard let raw = UserDefaults.fluxAppGroup.apiURL?
                .trimmingCharacters(in: .whitespacesAndNewlines),
              !raw.isEmpty,
              let url = URL(string: raw) else {
            return nil
        }
        return URLSessionAPIClient(
            baseURL: url,
            keychainService: keychain,
            session: session
        )
    }
}
