#if os(macOS)
import Foundation
import Testing
@testable import FluxCore

@Suite
struct ControlSOCProviderTests {
    private static func uniqueSuite() -> String { "test.control.\(UUID().uuidString)" }

    private static func clearSuite(_ suite: String) {
        UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite)
    }

    private static func makeStatus(soc: Double) -> StatusResponse {
        StatusResponse(
            live: LiveData(
                ppv: 1800,
                pload: 412,
                pbat: 500,
                pgrid: 210,
                pgridSustained: false,
                soc: soc,
                timestamp: "2026-04-20T10:00:00Z"
            ),
            battery: nil,
            rolling15min: nil,
            offpeak: nil,
            todayEnergy: nil
        )
    }

    @Test
    func cacheHitWithinThresholdReturnsFreshSOCValue() async throws {
        let cacheSuite = Self.uniqueSuite()
        let logicSuite = Self.uniqueSuite()
        defer {
            Self.clearSuite(cacheSuite)
            Self.clearSuite(logicSuite)
        }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let cache = WidgetSnapshotCache(suiteName: cacheSuite)
        let envelope = StatusSnapshotEnvelope(
            fetchedAt: now.addingTimeInterval(-60),
            status: Self.makeStatus(soc: 47.0)
        )
        #expect(cache.writeIfNewer(envelope) == true)

        let logic = StatusTimelineLogic(
            apiClient: nil,
            cache: WidgetSnapshotCache(suiteName: logicSuite),
            tokenProvider: { nil },
            nowProvider: { now }
        )
        let provider = ControlSOCProvider(
            cache: cache,
            logic: logic,
            nowProvider: { now }
        )

        let value = try await provider.currentValue()

        #expect(value.percent == 47.0)
        #expect(value.stale == false)
    }

    @Test
    func cacheStaleFallsBackToLiveLogicSnapshot() async throws {
        let cacheSuite = Self.uniqueSuite()
        let logicSuite = Self.uniqueSuite()
        defer {
            Self.clearSuite(cacheSuite)
            Self.clearSuite(logicSuite)
        }
        let now = Date(timeIntervalSince1970: 2_000_000)
        let cache = WidgetSnapshotCache(suiteName: cacheSuite)
        let staleEnvelope = StatusSnapshotEnvelope(
            fetchedAt: now.addingTimeInterval(-1200),
            status: Self.makeStatus(soc: 11.0)
        )
        #expect(cache.writeIfNewer(staleEnvelope) == true)

        let client = StubFluxAPIClient(result: .success(Self.makeStatus(soc: 88.5)))
        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: WidgetSnapshotCache(suiteName: logicSuite),
            tokenProvider: { "token" },
            nowProvider: { now }
        )
        let provider = ControlSOCProvider(
            cache: cache,
            logic: logic,
            nowProvider: { now }
        )

        let value = try await provider.currentValue()

        #expect(value.percent == 88.5)
        #expect(value.stale == false)
    }

    @Test
    func cacheMissFallsBackToLiveLogicSnapshot() async throws {
        let cacheSuite = Self.uniqueSuite()
        let logicSuite = Self.uniqueSuite()
        defer {
            Self.clearSuite(cacheSuite)
            Self.clearSuite(logicSuite)
        }
        let now = Date(timeIntervalSince1970: 2_000_000)
        let cache = WidgetSnapshotCache(suiteName: cacheSuite)
        let client = StubFluxAPIClient(result: .success(Self.makeStatus(soc: 73.0)))
        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: WidgetSnapshotCache(suiteName: logicSuite),
            tokenProvider: { "token" },
            nowProvider: { now }
        )
        let provider = ControlSOCProvider(
            cache: cache,
            logic: logic,
            nowProvider: { now }
        )

        let value = try await provider.currentValue()

        #expect(value.percent == 73.0)
        #expect(value.stale == false)
    }

    @Test
    func staleCacheWithFailedFetchReturnsStaleFromCacheFallback() async throws {
        let suite = Self.uniqueSuite()
        defer { Self.clearSuite(suite) }
        let now = Date(timeIntervalSince1970: 2_000_000)
        let cache = WidgetSnapshotCache(suiteName: suite)
        let staleEnvelope = StatusSnapshotEnvelope(
            fetchedAt: now.addingTimeInterval(-1200),
            status: Self.makeStatus(soc: 25.0)
        )
        #expect(cache.writeIfNewer(staleEnvelope) == true)

        let client = StubFluxAPIClient(result: .failure(FluxAPIError.networkError("boom")))
        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: cache,
            tokenProvider: { "token" },
            nowProvider: { now }
        )
        let provider = ControlSOCProvider(
            cache: cache,
            logic: logic,
            nowProvider: { now }
        )

        let value = try await provider.currentValue()

        #expect(value.percent == 25.0)
        #expect(value.stale == true)
    }
}
#endif
