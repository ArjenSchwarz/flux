import Foundation
import Observation

@MainActor @Observable
final class SettingsViewModel {
    var apiURL = ""
    var apiToken = ""
    var loadAlertThreshold: Double = 3000

    private(set) var isValidating = false
    private(set) var validationError: String?
    private(set) var shouldDismiss = false

    private let keychainService: KeychainService
    private let userDefaults: UserDefaults
    private let apiClientFactory: @Sendable (URL, String) -> any FluxAPIClient

    init(
        keychainService: KeychainService = KeychainService(),
        userDefaults: UserDefaults = .standard,
        apiClientFactory: @escaping @Sendable (URL, String) -> any FluxAPIClient = { baseURL, token in
            URLSessionAPIClient(baseURL: baseURL, token: token)
        }
    ) {
        self.keychainService = keychainService
        self.userDefaults = userDefaults
        self.apiClientFactory = apiClientFactory
    }

    func save() async {
        guard !isValidating else { return }

        let capturedURLString = apiURL.trimmingCharacters(in: .whitespacesAndNewlines)
        let capturedToken = apiToken
        let capturedThreshold = loadAlertThreshold

        guard let baseURL = URL(string: capturedURLString), capturedToken.isEmpty == false else {
            validationError = "Enter a valid API URL and token."
            return
        }

        isValidating = true
        validationError = nil
        shouldDismiss = false
        defer { isValidating = false }

        do {
            let validationClient = apiClientFactory(baseURL, capturedToken)
            _ = try await validationClient.fetchStatus()

            try keychainService.saveToken(capturedToken)
            userDefaults.apiURL = capturedURLString
            userDefaults.loadAlertThreshold = capturedThreshold
            shouldDismiss = true
        } catch let apiError as FluxAPIError {
            validationError = message(for: apiError)
        } catch {
            validationError = error.localizedDescription
        }
    }

    func loadExisting() {
        apiURL = userDefaults.apiURL ?? ""
        apiToken = keychainService.loadToken() ?? ""
        loadAlertThreshold = userDefaults.loadAlertThreshold
    }

    private func message(for error: FluxAPIError) -> String {
        switch error {
        case .notConfigured:
            return "Settings are incomplete."
        case .unauthorized:
            return "Authentication failed. Check your API token."
        case let .badRequest(message):
            return message
        case .serverError:
            return "Server error while validating settings."
        case let .networkError(message):
            return message
        case let .decodingError(message):
            return message
        case let .unexpectedStatus(statusCode):
            return "Unexpected response (\(statusCode))."
        }
    }
}
