import Foundation

public struct StatusSnapshotEnvelope: Codable, Sendable {
    public static let currentSchemaVersion: Int = 1

    public let schemaVersion: Int
    public let fetchedAt: Date
    public let status: StatusResponse

    public init(
        schemaVersion: Int = currentSchemaVersion,
        fetchedAt: Date,
        status: StatusResponse
    ) {
        self.schemaVersion = schemaVersion
        self.fetchedAt = fetchedAt
        self.status = status
    }
}
