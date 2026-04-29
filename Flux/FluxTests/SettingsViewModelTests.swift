import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct SettingsViewModelTests {
    @Test
    func saveValidatesUsingEnteredTokenAndStoresOnSuccess() async throws {
        let keychain = makeKeychainService()
        defer { try? keychain.deleteToken() }
        try keychain.saveToken("stored-token")

        let userDefaults = makeUserDefaults()
        defer { userDefaults.removePersistentDomain(forName: suiteName) }

        let apiClient = MockSettingsAPIClient()
        await apiClient.setStatusResult(.success(Self.validStatusResponse))
        let capture = CaptureBox()

        let viewModel = SettingsViewModel(
            keychainService: keychain,
            userDefaults: userDefaults,
            apiClientFactory: { _, token in
                capture.token = token
                return apiClient
            }
        )

        viewModel.apiURL = "https://example.com"
        viewModel.apiToken = "entered-token"
        viewModel.loadAlertThreshold = 3200
        await viewModel.save()

        #expect(capture.token == "entered-token")
        #expect(keychain.loadToken() == "entered-token")
        #expect(userDefaults.apiURL == "https://example.com")
        #expect(userDefaults.loadAlertThreshold == 3200)
        #expect(viewModel.shouldDismiss)
    }

    @Test
    func saveSetsValidationErrorAndDoesNotModifyKeychainOnFailure() async throws {
        let keychain = makeKeychainService()
        defer { try? keychain.deleteToken() }
        try keychain.saveToken("original-token")

        let userDefaults = makeUserDefaults()
        defer { userDefaults.removePersistentDomain(forName: suiteName) }
        userDefaults.apiURL = "https://existing.example.com"

        let apiClient = MockSettingsAPIClient()
        await apiClient.setStatusResult(.failure(FluxAPIError.unauthorized))

        let viewModel = SettingsViewModel(
            keychainService: keychain,
            userDefaults: userDefaults,
            apiClientFactory: { _, _ in apiClient }
        )

        viewModel.apiURL = "https://example.com"
        viewModel.apiToken = "bad-token"
        await viewModel.save()

        #expect(viewModel.validationError == "Authentication failed. Check your API token.")
        #expect(keychain.loadToken() == "original-token")
        #expect(userDefaults.apiURL == "https://existing.example.com")
        #expect(viewModel.shouldDismiss == false)
    }

    @Test
    func saveCapturesValuesAtStartAndDismissesOnSuccess() async throws {
        let keychain = makeKeychainService()
        defer { try? keychain.deleteToken() }

        let userDefaults = makeUserDefaults()
        defer { userDefaults.removePersistentDomain(forName: suiteName) }

        let apiClient = MockSettingsAPIClient()
        await apiClient.setStatusResult(.success(Self.validStatusResponse))
        await apiClient.setFetchDelay(.milliseconds(80))

        let capture = CaptureBox()
        let viewModel = SettingsViewModel(
            keychainService: keychain,
            userDefaults: userDefaults,
            apiClientFactory: { url, token in
                capture.url = url
                capture.token = token
                return apiClient
            }
        )

        viewModel.apiURL = "https://initial.example.com"
        viewModel.apiToken = "initial-token"
        viewModel.loadAlertThreshold = 3100

        let saveTask = Task { await viewModel.save() }
        try await Task.sleep(for: .milliseconds(10))

        viewModel.apiURL = "https://mutated.example.com"
        viewModel.apiToken = "mutated-token"
        viewModel.loadAlertThreshold = 9999
        await saveTask.value

        #expect(capture.url?.absoluteString == "https://initial.example.com")
        #expect(capture.token == "initial-token")
        #expect(keychain.loadToken() == "initial-token")
        #expect(userDefaults.apiURL == "https://initial.example.com")
        #expect(userDefaults.loadAlertThreshold == 3100)
        #expect(viewModel.shouldDismiss)
    }

    @Test
    func loadExistingPopulatesValuesFromStorage() throws {
        let keychain = makeKeychainService()
        defer { try? keychain.deleteToken() }
        try keychain.saveToken("existing-token")

        let userDefaults = makeUserDefaults()
        defer { userDefaults.removePersistentDomain(forName: suiteName) }
        userDefaults.apiURL = "https://flux.example.com"
        userDefaults.loadAlertThreshold = 4200

        let viewModel = SettingsViewModel(
            keychainService: keychain,
            userDefaults: userDefaults
        )
        viewModel.loadExisting()

        #expect(viewModel.apiURL == "https://flux.example.com")
        #expect(viewModel.apiToken == "existing-token")
        #expect(viewModel.loadAlertThreshold == 4200)
    }

    private static let validStatusResponse = StatusResponse(
        live: nil,
        battery: nil,
        rolling15min: nil,
        offpeak: nil,
        todayEnergy: nil
    )

    private let suiteName = "SettingsViewModelTests.\(UUID().uuidString)"

    private func makeKeychainService() -> KeychainService {
        KeychainService(
            service: "me.nore.ig.flux.tests.\(UUID().uuidString)",
            account: "api-token",
            accessGroup: nil
        )
    }

    private func makeUserDefaults() -> UserDefaults {
        let defaults = UserDefaults(suiteName: suiteName)!
        defaults.removePersistentDomain(forName: suiteName)
        return defaults
    }

}

private actor MockSettingsAPIClient: FluxAPIClient {
    var statusResult: Result<StatusResponse, Error> = .failure(FluxAPIError.notConfigured)
    var fetchStatusCallCount = 0
    var fetchDelay: Duration?

    func setStatusResult(_ result: Result<StatusResponse, Error>) {
        statusResult = result
    }

    func setFetchDelay(_ delay: Duration?) {
        fetchDelay = delay
    }

    func fetchStatus() async throws -> StatusResponse {
        fetchStatusCallCount += 1
        if let fetchDelay {
            try await Task.sleep(for: fetchDelay)
        }

        return try statusResult.get()
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchDay(date _: String) async throws -> DayDetailResponse {
        throw FluxAPIError.notConfigured
    }

    func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}

private final class CaptureBox: @unchecked Sendable {
    var token = ""
    var url: URL?
}
