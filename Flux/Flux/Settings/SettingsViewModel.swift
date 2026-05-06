import FluxCore
import Foundation
import Observation
import WidgetKit

@MainActor @Observable
final class SettingsViewModel {
    var apiURL = ""
    var apiToken = ""
    var loadAlertThreshold: Double = 3000
    var widgetUsesSymbols = false
    /// Empty string means "use the system font"; any other value is a
    /// PostScript family name returned by `AppFont.installedFamilies()`.
    var appFontFamily: String = ""
    var theme: ThemeChoice = .default

    /// Populated by `loadFontFamilies()` so opening Settings on macOS doesn't
    /// block the main thread on the initial `NSFontManager.availableFontFamilies`
    /// call (~100–300 ms cold). Empty until the background task completes.
    private(set) var installedFontFamilies: [String] = []

    private(set) var isValidating = false
    private(set) var validationError: String?
    private(set) var shouldDismiss = false

    private let keychainService: KeychainService
    private let userDefaults: UserDefaults
    private let apiClientFactory: @Sendable (URL, String) -> any FluxAPIClient
    private let writeURL: @MainActor (String) -> Void
    private let notificationCenter: NotificationCenter

    init(
        keychainService: KeychainService = KeychainService(),
        userDefaults: UserDefaults = .fluxAppGroup,
        apiClientFactory: @escaping @Sendable (URL, String) -> any FluxAPIClient = { baseURL, token in
            URLSessionAPIClient(baseURL: baseURL, token: token)
        },
        writeURL: @escaping @MainActor (String) -> Void = { url in
            iCloudURLMirror.shared.write(url)
        },
        notificationCenter: NotificationCenter = .default
    ) {
        self.keychainService = keychainService
        self.userDefaults = userDefaults
        self.apiClientFactory = apiClientFactory
        self.writeURL = writeURL
        self.notificationCenter = notificationCenter
    }

    func save() async {
        guard !isValidating else { return }

        let capturedURLString = apiURL.trimmingCharacters(in: .whitespacesAndNewlines)
        let capturedToken = apiToken
        let capturedThreshold = loadAlertThreshold
        let capturedUsesSymbols = widgetUsesSymbols

        guard let baseURL = URL(string: capturedURLString), capturedToken.isEmpty == false else {
            validationError = "Enter a valid API URL and token."
            return
        }

        guard baseURL.scheme == "https" else {
            validationError = "API URL must use HTTPS."
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
            writeURL(capturedURLString)
            userDefaults.loadAlertThreshold = capturedThreshold
            userDefaults.widgetUsesSymbols = capturedUsesSymbols
            userDefaults.appFontFamily = appFontFamily
            userDefaults.themeIdentifier = theme.rawValue
            WidgetCenter.shared.reloadAllTimelines()
            notificationCenter.post(name: .fluxCredentialsChanged, object: nil)
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
        widgetUsesSymbols = userDefaults.widgetUsesSymbols
        appFontFamily = userDefaults.appFontFamily
        theme = ThemeChoice(rawValue: userDefaults.themeIdentifier) ?? .default
    }

    /// Loads the installed font family list off the main actor so opening
    /// Settings stays responsive on macOS. The list is memoised inside
    /// `AppFont`, so the second invocation returns instantly. Marked
    /// `@MainActor` to make the assignment after the `await` explicit —
    /// callers must invoke from the main actor.
    @MainActor func loadFontFamilies() async {
        guard installedFontFamilies.isEmpty else { return }
        let families = await Task.detached(priority: .userInitiated) {
            AppFont.installedFamilies()
        }.value
        installedFontFamilies = families
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
