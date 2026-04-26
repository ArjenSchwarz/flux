import Foundation
import Testing
import WidgetKit
@testable import FluxCore

@Suite
struct StatusTimelineLogicTests {
    // MARK: - Fixtures & helpers

    private static func makeStatus(
        soc: Double = 62.4,
        pbat: Double = 500,
        cutoffPercent: Int = 10
    ) -> StatusResponse {
        StatusResponse(
            live: LiveData(
                ppv: 1800,
                pload: 412,
                pbat: pbat,
                pgrid: 210,
                pgridSustained: false,
                soc: soc,
                timestamp: "2026-04-20T10:00:00Z"
            ),
            battery: BatteryInfo(
                capacityKwh: 10,
                cutoffPercent: cutoffPercent,
                estimatedCutoffTime: "2026-04-20T17:12:00Z",
                low24h: nil
            ),
            rolling15min: RollingAvg(
                avgLoad: 400,
                avgPbat: 500,
                estimatedCutoffTime: "2026-04-20T17:12:00Z"
            ),
            offpeak: nil,
            todayEnergy: nil
        )
    }

    private static func uniqueSuite() -> String { "test.widget.\(UUID().uuidString)" }

    private static func cache(suiteName: String) -> WidgetSnapshotCache {
        WidgetSnapshotCache(suiteName: suiteName)
    }

    private static func makeEnvelope(
        fetchedAt: Date,
        soc: Double = 62.4,
        pbat: Double = 500
    ) -> StatusSnapshotEnvelope {
        StatusSnapshotEnvelope(
            fetchedAt: fetchedAt,
            status: makeStatus(soc: soc, pbat: pbat)
        )
    }

    // MARK: - placeholder

    @Test
    func placeholderReturnsPlausibleFixtureData() {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let logic = StatusTimelineLogic(
            apiClient: nil,
            cache: Self.cache(suiteName: suite),
            tokenProvider: { nil },
            nowProvider: { Date(timeIntervalSince1970: 0) }
        )

        let entry = logic.placeholder()

        #expect(entry.source == .placeholder)
        #expect(entry.envelope != nil)
        #expect(entry.envelope?.status.live?.soc != nil)
    }

    // MARK: - timeline with no cache / no token

    @Test
    func timelineEmptyCacheNoTokenReturnsPlaceholder() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let fetchSpy = FetchSpy()
        let client = StubFluxAPIClient(result: .success(Self.makeStatus()), spy: fetchSpy)

        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: Self.cache(suiteName: suite),
            tokenProvider: { nil },
            nowProvider: { now }
        )

        let timeline = await logic.timeline()

        #expect(timeline.entries.count == 1)
        let entry = timeline.entries[0]
        #expect(entry.envelope == nil)
        #expect(entry.source == .placeholder)
        let expectedReload = now.addingTimeInterval(StatusTimelineLogic.refreshInterval)
        let policyDescription = String(describing: timeline.policy)
        #expect(policyDescription.contains(String(describing: expectedReload)))
        #expect(await fetchSpy.count == 0)
    }

    // MARK: - timeline with token + empty cache + success

    @Test
    func timelineTokenPresentEmptyCacheSuccessfulFetchProducesStackedEntries() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let client = StubFluxAPIClient(result: .success(Self.makeStatus()))

        let cache = Self.cache(suiteName: suite)
        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: cache,
            tokenProvider: { "token" },
            nowProvider: { now }
        )

        let timeline = await logic.timeline()

        #expect(timeline.entries.count == 3)
        #expect(timeline.entries[0].date == now)
        #expect(timeline.entries[0].source == .live)
        #expect(timeline.entries[1].staleness == .stale)
        #expect(timeline.entries[2].staleness == .offline)

        let written = cache.read()
        #expect(written != nil)
        #expect(written?.fetchedAt == now)
    }

    // MARK: - timeline with token + empty cache + fetch throws

    @Test
    func timelineTokenPresentEmptyCacheFetchThrowsReturnsPlaceholder() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let client = StubFluxAPIClient(result: .failure(FluxAPIError.networkError("boom")))

        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: Self.cache(suiteName: suite),
            tokenProvider: { "token" },
            nowProvider: { now }
        )

        let timeline = await logic.timeline()

        #expect(timeline.entries.count == 1)
        #expect(timeline.entries[0].envelope == nil)
        #expect(timeline.entries[0].source == .placeholder)
    }

    // MARK: - timeline with cache present

    @Test
    func timelineCachePresentFetchThrowsReturnsCacheEntry() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let cachedAt = now.addingTimeInterval(-60 * 60) // 60 minutes ago → stale
        let cache = Self.cache(suiteName: suite)
        cache.writeIfNewer(Self.makeEnvelope(fetchedAt: cachedAt))

        let client = StubFluxAPIClient(result: .failure(FluxAPIError.networkError("boom")))

        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: cache,
            tokenProvider: { "token" },
            nowProvider: { now }
        )

        let timeline = await logic.timeline()

        #expect(timeline.entries.first?.source == .cache)
        #expect(timeline.entries.first?.staleness == .stale)
        #expect(timeline.entries.first?.envelope?.fetchedAt == cachedAt)
    }

    @Test
    func timelineCachePresentFetchSucceedsNewerOverwrites() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let cache = Self.cache(suiteName: suite)
        cache.writeIfNewer(Self.makeEnvelope(fetchedAt: now.addingTimeInterval(-3600), soc: 20))

        let client = StubFluxAPIClient(result: .success(Self.makeStatus(soc: 80)))
        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: cache,
            tokenProvider: { "token" },
            nowProvider: { now }
        )

        _ = await logic.timeline()

        let written = cache.read()
        #expect(written?.fetchedAt == now)
        #expect(written?.status.live?.soc == 80)
    }

    @Test
    func timelineCachePresentFetchSucceedsEqualFetchedAtStillWrites() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let cache = Self.cache(suiteName: suite)
        cache.writeIfNewer(Self.makeEnvelope(fetchedAt: now, soc: 20))

        let client = StubFluxAPIClient(result: .success(Self.makeStatus(soc: 80)))
        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: cache,
            tokenProvider: { "token" },
            nowProvider: { now }
        )

        _ = await logic.timeline()

        #expect(cache.read()?.status.live?.soc == 80)
    }

    // MARK: - keychain locked

    @Test
    func timelineKeychainInteractionNotAllowedUsesCacheOnly() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let cache = Self.cache(suiteName: suite)
        cache.writeIfNewer(Self.makeEnvelope(fetchedAt: now.addingTimeInterval(-120)))

        let fetchSpy = FetchSpy()
        let client = StubFluxAPIClient(result: .success(Self.makeStatus()), spy: fetchSpy)

        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: cache,
            tokenProvider: { nil },
            nowProvider: { now }
        )

        let timeline = await logic.timeline()

        #expect(timeline.entries.first?.source == .cache)
        #expect(await fetchSpy.count == 0)
    }

    // MARK: - timeout fallback

    @Test
    func timelineFetchTimesOutReturnsCacheEntry() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let cache = Self.cache(suiteName: suite)
        let cachedAt = now.addingTimeInterval(-300)
        cache.writeIfNewer(Self.makeEnvelope(fetchedAt: cachedAt))

        let client = DelayedFluxAPIClient(delay: .seconds(10), result: Self.makeStatus())
        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: cache,
            tokenProvider: { "token" },
            nowProvider: { now },
            fetchTimeout: .milliseconds(50)
        )

        let timeline = await logic.timeline()

        #expect(timeline.entries.first?.source == .cache)
        #expect(timeline.entries.first?.envelope?.fetchedAt == cachedAt)
    }

    // MARK: - session config helper

    @Test
    func validateWidgetSessionTimeoutsAcceptsFiveSecondConfig() {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 5
        config.timeoutIntervalForResource = 5

        #expect(StatusTimelineLogic.validateWidgetSessionTimeouts(config))
    }

    @Test
    func validateWidgetSessionTimeoutsRejectsLargerConfig() {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 60

        #expect(!StatusTimelineLogic.validateWidgetSessionTimeouts(config))
    }

    // MARK: - relevance + migrator

    @Test
    func timelineEntriesCarryNonDefaultRelevance() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let now = Date(timeIntervalSince1970: 1_000_000)
        let client = StubFluxAPIClient(result: .success(Self.makeStatus()))
        let logic = StatusTimelineLogic(
            apiClient: client,
            cache: Self.cache(suiteName: suite),
            tokenProvider: { "token" },
            nowProvider: { now }
        )

        let timeline = await logic.timeline()

        for entry in timeline.entries {
            #expect(entry.relevance != nil)
        }
    }

    @Test
    func timelineCallsMigratorAtTheTop() async {
        let suite = Self.uniqueSuite()
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let spy = MigratorSpy()
        let logic = StatusTimelineLogic(
            apiClient: nil,
            cache: Self.cache(suiteName: suite),
            tokenProvider: { nil },
            nowProvider: { Date(timeIntervalSince1970: 0) },
            migrator: { await spy.record() }
        )

        _ = await logic.timeline()

        #expect(await spy.count == 1)
    }
}

// MARK: - Test doubles

actor FetchSpy {
    private(set) var count: Int = 0
    func increment() { count += 1 }
}

actor MigratorSpy {
    private(set) var count: Int = 0
    func record() { count += 1 }
}

final class StubFluxAPIClient: FluxAPIClient, @unchecked Sendable {
    private let result: Result<StatusResponse, Error>
    private let spy: FetchSpy?

    init(result: Result<StatusResponse, Error>, spy: FetchSpy? = nil) {
        self.result = result
        self.spy = spy
    }

    func fetchStatus() async throws -> StatusResponse {
        if let spy { await spy.increment() }
        return try result.get()
    }

    func fetchHistory(days: Int) async throws -> HistoryResponse {
        HistoryResponse(days: [])
    }

    func fetchDay(date: String) async throws -> DayDetailResponse {
        DayDetailResponse(date: date, readings: [], summary: nil, peakPeriods: nil, eveningNight: nil)
    }
}

final class DelayedFluxAPIClient: FluxAPIClient, @unchecked Sendable {
    private let delay: Duration
    private let result: StatusResponse

    init(delay: Duration, result: StatusResponse) {
        self.delay = delay
        self.result = result
    }

    func fetchStatus() async throws -> StatusResponse {
        try await Task.sleep(for: delay)
        return result
    }

    func fetchHistory(days: Int) async throws -> HistoryResponse {
        HistoryResponse(days: [])
    }

    func fetchDay(date: String) async throws -> DayDetailResponse {
        DayDetailResponse(date: date, readings: [], summary: nil, peakPeriods: nil, eveningNight: nil)
    }
}
