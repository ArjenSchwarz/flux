import Foundation
import Testing
@testable import FluxCore

@Suite
struct WidgetSnapshotCacheTests {
    private func makeStatus() -> StatusResponse {
        StatusResponse(
            live: LiveData(
                ppv: 1000,
                pload: 500,
                pbat: 200,
                pgrid: -50,
                pgridSustained: false,
                soc: 75.5,
                timestamp: "2026-04-15T10:00:00Z"
            ),
            battery: nil,
            rolling15min: nil,
            offpeak: nil,
            todayEnergy: nil
        )
    }

    private func makeSuiteName() -> String {
        "test.widget.\(UUID().uuidString)"
    }

    private func clearSuite(_ suiteName: String) {
        guard let defaults = UserDefaults(suiteName: suiteName) else { return }
        defaults.removePersistentDomain(forName: suiteName)
    }

    @Test
    func writeIfNewerSucceedsOnEmptyCache() {
        let suite = makeSuiteName()
        defer { clearSuite(suite) }
        let cache = WidgetSnapshotCache(suiteName: suite)
        let envelope = StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 1_000_000),
            status: makeStatus()
        )

        let wrote = cache.writeIfNewer(envelope)

        #expect(wrote == true)
        let read = cache.read()
        #expect(read?.fetchedAt == envelope.fetchedAt)
        #expect(read?.status.live?.soc == 75.5)
    }

    @Test
    func writeIfNewerReturnsFalseWhenExistingIsNewer() {
        let suite = makeSuiteName()
        defer { clearSuite(suite) }
        let cache = WidgetSnapshotCache(suiteName: suite)
        let newer = StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 2_000_000),
            status: makeStatus()
        )
        let older = StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 1_000_000),
            status: makeStatus()
        )

        #expect(cache.writeIfNewer(newer) == true)
        #expect(cache.writeIfNewer(older) == false)
        #expect(cache.read()?.fetchedAt == newer.fetchedAt)
    }

    @Test
    func writeIfNewerReturnsTrueWhenTimestampsAreEqual() {
        let suite = makeSuiteName()
        defer { clearSuite(suite) }
        let cache = WidgetSnapshotCache(suiteName: suite)
        let fetchedAt = Date(timeIntervalSince1970: 1_500_000)
        let first = StatusSnapshotEnvelope(fetchedAt: fetchedAt, status: makeStatus())
        let second = StatusSnapshotEnvelope(fetchedAt: fetchedAt, status: makeStatus())

        #expect(cache.writeIfNewer(first) == true)
        #expect(cache.writeIfNewer(second) == true)
    }

    @Test
    func writeIfNewerSucceedsWhenExistingIsOlder() {
        let suite = makeSuiteName()
        defer { clearSuite(suite) }
        let cache = WidgetSnapshotCache(suiteName: suite)
        let older = StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 1_000_000),
            status: makeStatus()
        )
        let newer = StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 2_000_000),
            status: makeStatus()
        )

        #expect(cache.writeIfNewer(older) == true)
        #expect(cache.writeIfNewer(newer) == true)
        #expect(cache.read()?.fetchedAt == newer.fetchedAt)
    }

    @Test
    func readReturnsNilForUnknownSchemaVersion() throws {
        let suite = makeSuiteName()
        defer { clearSuite(suite) }
        let defaults = try #require(UserDefaults(suiteName: suite))

        let futureJSON = """
        {
          "schemaVersion": 99,
          "fetchedAt": "2026-01-01T00:00:00Z",
          "status": {}
        }
        """
        defaults.set(Data(futureJSON.utf8), forKey: "widgetSnapshotV1")

        let cache = WidgetSnapshotCache(suiteName: suite)
        #expect(cache.read() == nil)
    }

    @Test
    func readReturnsNilForGarbageBytes() throws {
        let suite = makeSuiteName()
        defer { clearSuite(suite) }
        let defaults = try #require(UserDefaults(suiteName: suite))

        defaults.set(Data([0x00, 0x01, 0x02, 0xFF]), forKey: "widgetSnapshotV1")

        let cache = WidgetSnapshotCache(suiteName: suite)
        #expect(cache.read() == nil)
    }

    @Test
    func clearRemovesStoredEnvelope() {
        let suite = makeSuiteName()
        defer { clearSuite(suite) }
        let cache = WidgetSnapshotCache(suiteName: suite)
        let envelope = StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 1_000_000),
            status: makeStatus()
        )
        #expect(cache.writeIfNewer(envelope) == true)
        #expect(cache.read() != nil)

        cache.clear()

        #expect(cache.read() == nil)
    }
}
