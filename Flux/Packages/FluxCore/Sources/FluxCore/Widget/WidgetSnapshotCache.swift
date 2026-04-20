import Foundation

public final class WidgetSnapshotCache: Sendable {
    private static let storageKey = "widgetSnapshotV1"

    private let defaults: UserDefaults?
    private let nowProvider: @Sendable () -> Date

    public init(
        suiteName: String = "group.me.nore.ig.flux",
        nowProvider: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.defaults = UserDefaults(suiteName: suiteName)
        self.nowProvider = nowProvider
    }

    public func read() -> StatusSnapshotEnvelope? {
        guard let defaults,
              let data = defaults.data(forKey: Self.storageKey) else {
            return nil
        }

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601

        guard let envelope = try? decoder.decode(StatusSnapshotEnvelope.self, from: data) else {
            return nil
        }

        guard envelope.schemaVersion == StatusSnapshotEnvelope.currentSchemaVersion else {
            return nil
        }

        return envelope
    }

    @discardableResult
    public func writeIfNewer(_ envelope: StatusSnapshotEnvelope) -> Bool {
        guard let defaults else { return false }

        if let existing = read(), existing.fetchedAt > envelope.fetchedAt {
            return false
        }

        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601

        guard let data = try? encoder.encode(envelope) else {
            return false
        }

        defaults.set(data, forKey: Self.storageKey)
        return true
    }

    public func clear() {
        defaults?.removeObject(forKey: Self.storageKey)
    }
}
