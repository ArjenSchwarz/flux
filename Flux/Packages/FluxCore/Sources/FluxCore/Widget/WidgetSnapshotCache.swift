import Foundation

public final class WidgetSnapshotCache: Sendable {
    private static let storageKey = "widgetSnapshotV1"

    private static let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    private static let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    private let defaults: UserDefaults?
    private let nowProvider: @Sendable () -> Date

    public init(
        suiteName: String = UserDefaults.fluxAppGroupSuiteName,
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

        guard let envelope = try? Self.decoder.decode(StatusSnapshotEnvelope.self, from: data) else {
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

        // Existing strictly newer → skip. Equal timestamps fall through and overwrite (Decision 17).
        if let existing = read(), existing.fetchedAt > envelope.fetchedAt {
            return false
        }

        guard let data = try? Self.encoder.encode(envelope) else {
            return false
        }

        defaults.set(data, forKey: Self.storageKey)
        return true
    }

    public func clear() {
        defaults?.removeObject(forKey: Self.storageKey)
    }
}
