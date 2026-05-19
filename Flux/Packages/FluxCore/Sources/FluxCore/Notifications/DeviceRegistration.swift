import Foundation

/// Wire shape for POST /devices. The backend stores the row keyed by
/// deviceId; the tzUpdatedAt monotonic counter prevents lost-update races
/// (AC 4.5).
public struct DeviceRegistration: Codable, Sendable, Equatable {
    public let deviceId: String
    public let platform: String           // "ios" | "macos"
    public let apnsToken: String?         // omitted when authorisation denied
    public let tzIdentifier: String
    public let tzUpdatedAt: Int64

    public init(
        deviceId: String,
        platform: String,
        apnsToken: String?,
        tzIdentifier: String,
        tzUpdatedAt: Int64
    ) {
        self.deviceId = deviceId
        self.platform = platform
        self.apnsToken = apnsToken
        self.tzIdentifier = tzIdentifier
        self.tzUpdatedAt = tzUpdatedAt
    }
}

/// Wire shape returned by POST /devices. The backend always echoes the
/// stored row, including any fields the client did not send (e.g., the
/// token preserved from a previous registration).
public struct DeviceItemResponse: Codable, Sendable, Equatable {
    public let deviceId: String
    public let platform: String
    public let apnsToken: String?
    public let tzIdentifier: String
    public let tzUpdatedAt: Int64?
    public let tokenStatus: String?
    public let lastRegisteredAt: String?

    public init(
        deviceId: String,
        platform: String,
        apnsToken: String? = nil,
        tzIdentifier: String,
        tzUpdatedAt: Int64? = nil,
        tokenStatus: String? = nil,
        lastRegisteredAt: String? = nil
    ) {
        self.deviceId = deviceId
        self.platform = platform
        self.apnsToken = apnsToken
        self.tzIdentifier = tzIdentifier
        self.tzUpdatedAt = tzUpdatedAt
        self.tokenStatus = tokenStatus
        self.lastRegisteredAt = lastRegisteredAt
    }
}

/// Wrapper for GET /devices/{id}/rules. The backend returns
/// {"rules":[...]} so the client can iterate without unwrapping.
public struct SoCAlertRulesResponse: Codable, Sendable, Equatable {
    public let rules: [SoCAlertRule]
    public init(rules: [SoCAlertRule]) { self.rules = rules }
}
