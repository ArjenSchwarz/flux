import Foundation

/// Server-assigned alert rule. The id, createdAt, and updatedAt fields are
/// populated by the backend; the client treats them as opaque and never
/// mutates them locally.
public struct SoCAlertRule: Identifiable, Codable, Sendable, Equatable, Hashable {
    public let id: String
    public var thresholdPercent: Int
    public var windowStart: String
    public var windowEnd: String
    public var enabled: Bool
    public var label: String?
    public let createdAt: Date
    public var updatedAt: Date

    public init(
        id: String,
        thresholdPercent: Int,
        windowStart: String,
        windowEnd: String,
        enabled: Bool,
        label: String?,
        createdAt: Date,
        updatedAt: Date
    ) {
        self.id = id
        self.thresholdPercent = thresholdPercent
        self.windowStart = windowStart
        self.windowEnd = windowEnd
        self.enabled = enabled
        self.label = label
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }
}
