import Foundation
import UserNotifications
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

/// Owns the SoC alert rules cache, the registration cache, and the
/// optimistic CRUD path. View models wrap this @Observable to drive the
/// settings UI.
@MainActor
@Observable
public final class SoCAlertsService {
    public static let shared = SoCAlertsService()

    public private(set) var rules: [SoCAlertRule] = []
    public private(set) var authStatus: UNAuthorizationStatus = .notDetermined
    public private(set) var lastError: Error?

    private let deviceIdentifier: DeviceIdentifier
    private let registrationCache: UserDefaults
    private var apiClient: (any FluxAPIClient)?

    /// State of the last successful registration; used to short-circuit
    /// duplicate POSTs while the device is on the same {token, tz}.
    private struct LastRegistration: Codable {
        let apnsToken: String?
        let tzIdentifier: String
        let tzUpdatedAt: Int64
    }
    private static let lastRegistrationKey = "flux.soc.lastRegistration"

    /// Pending registration retained when the backend POST fails so the next
    /// foreground hook can replay it.
    private var pendingRegistration: (token: Data?, tz: TimeZone)?

    public init(
        deviceIdentifier: DeviceIdentifier = .shared,
        registrationCache: UserDefaults = .standard
    ) {
        self.deviceIdentifier = deviceIdentifier
        self.registrationCache = registrationCache
    }

    /// Wires the API client. Call once from the app's startup path.
    public func bind(apiClient: any FluxAPIClient) {
        self.apiClient = apiClient
    }

    /// Drops the in-memory lastError; the editor sheet calls this on
    /// dismiss so the banner clears.
    public func clearError() {
        lastError = nil
    }

    // MARK: - Registration

    /// Request notification permission and (on grant) register for remote
    /// notifications. Safe to call on every Settings appearance.
    public func requestAuthorizationAndRegister() async throws {
        let status = await NotificationAuthService.currentStatus()
        authStatus = status
        if status == .notDetermined {
            let granted = try await NotificationAuthService.requestAlertsAuthorization()
            authStatus = granted ? .authorized : .denied
        }
        if authStatus == .authorized {
            await registerForRemoteNotificationsOnSystem()
            // Token arrives via the app-delegate callback → registerDeviceIfNeeded.
        } else {
            // Denied or undetermined-after-prompt: still POST a device row
            // (without a token) so the backend knows about the device and
            // the next foreground after granting can attach the token.
            try await registerDeviceIfNeeded(token: nil, tz: .current)
        }
    }

    /// Idempotent registration. POSTs only when the (token, tz) tuple
    /// differs from the last successfully-sent one cached in UserDefaults.
    public func registerDeviceIfNeeded(token: Data?, tz: TimeZone) async throws {
        guard let apiClient else { return }
        let tokenHex = token.map { hexString($0) }
        let cached = loadLastRegistration()
        if let cached, cached.apnsToken == tokenHex, cached.tzIdentifier == tz.identifier {
            // Nothing changed since the last successful POST.
            return
        }
        let registration = DeviceRegistration(
            deviceId: deviceIdentifier.currentOrGenerate(),
            platform: currentPlatform,
            apnsToken: tokenHex,
            tzIdentifier: tz.identifier,
            tzUpdatedAt: Int64(Date().timeIntervalSince1970)
        )
        do {
            _ = try await apiClient.registerDevice(registration)
            saveLastRegistration(LastRegistration(
                apnsToken: tokenHex,
                tzIdentifier: tz.identifier,
                tzUpdatedAt: registration.tzUpdatedAt
            ))
            pendingRegistration = nil
            lastError = nil
        } catch {
            pendingRegistration = (token, tz)
            lastError = error
            throw error
        }
    }

    /// Foreground hook — replays the pending registration and refreshes
    /// rules. The app delegate calls this on `applicationDidBecomeActive`.
    public func foregroundHook() async {
        if let pending = pendingRegistration {
            do {
                try await registerDeviceIfNeeded(token: pending.token, tz: pending.tz)
            } catch {
                // Stored already in lastError; nothing to do.
            }
        }
    }

    // MARK: - Rule CRUD

    public func refresh() async throws {
        guard let apiClient else { return }
        let deviceID = deviceIdentifier.currentOrGenerate()
        do {
            let remote = try await apiClient.fetchRules(deviceId: deviceID)
            rules = remote
            lastError = nil
        } catch {
            lastError = error
            throw error
        }
    }

    public func create(_ draft: SoCAlertRuleDraft) async throws -> SoCAlertRule {
        guard let apiClient else {
            throw FluxAPIError.notConfigured
        }
        let deviceID = deviceIdentifier.currentOrGenerate()
        do {
            let created = try await apiClient.createRule(deviceId: deviceID, rule: draft)
            rules.append(created)
            lastError = nil
            return created
        } catch {
            lastError = error
            throw error
        }
    }

    public func update(_ rule: SoCAlertRule) async throws -> SoCAlertRule {
        guard let apiClient else { throw FluxAPIError.notConfigured }
        let deviceID = deviceIdentifier.currentOrGenerate()
        do {
            let updated = try await apiClient.updateRule(deviceId: deviceID, rule: rule)
            if let idx = rules.firstIndex(where: { $0.id == updated.id }) {
                rules[idx] = updated
            }
            lastError = nil
            return updated
        } catch {
            lastError = error
            throw error
        }
    }

    public func delete(_ ruleId: String) async throws {
        guard let apiClient else { throw FluxAPIError.notConfigured }
        let deviceID = deviceIdentifier.currentOrGenerate()
        do {
            try await apiClient.deleteRule(deviceId: deviceID, ruleId: ruleId)
            rules.removeAll { $0.id == ruleId }
            lastError = nil
        } catch {
            lastError = error
            throw error
        }
    }

    // MARK: - Helpers

    private var currentPlatform: String {
        #if os(iOS)
        return "ios"
        #elseif os(macOS)
        return "macos"
        #else
        return "ios"
        #endif
    }

    private func hexString(_ data: Data) -> String {
        data.map { String(format: "%02x", $0) }.joined()
    }

    private func loadLastRegistration() -> LastRegistration? {
        guard let raw = registrationCache.data(forKey: Self.lastRegistrationKey) else { return nil }
        return try? JSONDecoder().decode(LastRegistration.self, from: raw)
    }

    private func saveLastRegistration(_ value: LastRegistration) {
        if let data = try? JSONEncoder().encode(value) {
            registrationCache.set(data, forKey: Self.lastRegistrationKey)
        }
    }

    /// Triggers the system's remote-notification registration. The delegate
    /// callback (`didRegisterForRemoteNotificationsWithDeviceToken`) wraps
    /// the result back through `registerDeviceIfNeeded`.
    private func registerForRemoteNotificationsOnSystem() async {
        #if canImport(UIKit)
        await MainActor.run { UIApplication.shared.registerForRemoteNotifications() }
        #elseif canImport(AppKit)
        await MainActor.run { NSApplication.shared.registerForRemoteNotifications() }
        #endif
    }
}
