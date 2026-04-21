import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct DashboardViewModelTests {
    @Test
    func refreshUpdatesStatusOnSuccessAndPreservesOnFailure() async {
        let apiClient = MockDashboardAPIClient()
        let firstStatus = makeStatusResponse(soc: 72)
        await apiClient.setStatusResults([
            .success(firstStatus),
            .failure(FluxAPIError.serverError)
        ])
        let viewModel = DashboardViewModel(apiClient: apiClient)

        await viewModel.refresh()
        #expect(viewModel.status?.live?.soc == 72)
        #expect(viewModel.error == nil)

        await viewModel.refresh()
        #expect(viewModel.status?.live?.soc == 72)
        #expect(viewModel.error == .serverError)
    }

    @Test
    func refreshSkipsWhenAlreadyLoading() async {
        let apiClient = MockDashboardAPIClient()
        await apiClient.setStatusResults([.success(makeStatusResponse(soc: 70))])
        await apiClient.setFetchDelay(.milliseconds(80))
        let viewModel = DashboardViewModel(apiClient: apiClient)

        let firstRefresh = Task { await viewModel.refresh() }
        await waitForCallCount(apiClient, expectedCount: 1)

        await viewModel.refresh()
        #expect(await apiClient.fetchStatusCallCount == 1)
        await firstRefresh.value
    }

    @Test
    func startAutoRefreshIsIdempotent() async throws {
        let apiClient = MockDashboardAPIClient()
        await apiClient.setStatusResults(Array(repeating: .success(makeStatusResponse(soc: 71)), count: 16))
        let viewModel = DashboardViewModel(apiClient: apiClient, refreshInterval: .milliseconds(15))

        viewModel.startAutoRefresh()
        viewModel.startAutoRefresh()
        try await Task.sleep(for: .milliseconds(70))
        viewModel.stopAutoRefresh()

        let callCount = await apiClient.fetchStatusCallCount
        #expect(callCount >= 2)
        #expect(callCount <= 7)
    }

    @Test
    func stopAutoRefreshCancelsRefreshTask() async throws {
        let apiClient = MockDashboardAPIClient()
        await apiClient.setStatusResults(Array(repeating: .success(makeStatusResponse(soc: 75)), count: 16))
        let viewModel = DashboardViewModel(apiClient: apiClient, refreshInterval: .milliseconds(10))

        viewModel.startAutoRefresh()
        try await Task.sleep(for: .milliseconds(30))
        viewModel.stopAutoRefresh()
        let callsAfterStop = await apiClient.fetchStatusCallCount

        try await Task.sleep(for: .milliseconds(35))
        #expect(await apiClient.fetchStatusCallCount == callsAfterStop)
    }

    private func makeStatusResponse(soc: Double) -> StatusResponse {
        StatusResponse(
            live: LiveData(
                ppv: 1000,
                pload: 700,
                pbat: -250,
                pgrid: -50,
                pgridSustained: false,
                soc: soc,
                timestamp: "2026-04-15T00:00:00Z"
            ),
            battery: nil,
            rolling15min: nil,
            offpeak: nil,
            todayEnergy: nil
        )
    }

    private func waitForCallCount(_ apiClient: MockDashboardAPIClient, expectedCount: Int) async {
        for _ in 0 ..< 50 where await apiClient.fetchStatusCallCount < expectedCount {
            await Task.yield()
        }
    }
}

@MainActor @Suite(.serialized)
struct DashboardViewModelWidgetCacheTests {
    @Test
    func successfulRefreshWritesEnvelopeToCache() async {
        let context = makeContext()
        defer { context.cleanUp() }
        await context.apiClient.setStatusResults([.success(makeStatusResponse(soc: 62))])

        await context.viewModel.refresh()

        let envelope = context.cache.read()
        #expect(envelope?.status.live?.soc == 62)
        #expect(envelope?.fetchedAt == context.now)
    }

    @Test
    func failedRefreshDoesNotWriteToCache() async {
        let context = makeContext()
        defer { context.cleanUp() }
        await context.apiClient.setStatusResults([.failure(FluxAPIError.serverError)])

        await context.viewModel.refresh()

        #expect(context.cache.read() == nil)
        #expect(context.reloadCounter.count == 0)
    }

    @Test
    func successfulRefreshTriggersReloadExactlyOnce() async {
        let context = makeContext()
        defer { context.cleanUp() }
        await context.apiClient.setStatusResults([.success(makeStatusResponse(soc: 55))])

        await context.viewModel.refresh()

        #expect(context.reloadCounter.count == 1)
    }

    @Test
    func reloadIsNotTriggeredWhenWriteIfNewerReturnsFalse() async {
        let context = makeContext()
        defer { context.cleanUp() }

        let futureEnvelope = StatusSnapshotEnvelope(
            fetchedAt: context.now.addingTimeInterval(60),
            status: makeStatusResponse(soc: 90)
        )
        _ = context.cache.writeIfNewer(futureEnvelope)

        await context.apiClient.setStatusResults([.success(makeStatusResponse(soc: 33))])

        await context.viewModel.refresh()

        #expect(context.reloadCounter.count == 0)
        #expect(context.cache.read()?.status.live?.soc == 90)
    }

    @Test
    func secondRefreshWithinDebounceDoesNotTriggerReload() async {
        let timeline = TestClock(start: Date(timeIntervalSince1970: 1_000))
        let context = makeContext(clock: timeline)
        defer { context.cleanUp() }
        await context.apiClient.setStatusResults([
            .success(makeStatusResponse(soc: 60)),
            .success(makeStatusResponse(soc: 61))
        ])

        await context.viewModel.refresh()
        timeline.advance(by: 60) // 1 minute later, still within debounce
        await context.viewModel.refresh()

        #expect(context.reloadCounter.count == 1)
    }

    @Test
    func secondRefreshAfterDebounceTriggersReload() async {
        let timeline = TestClock(start: Date(timeIntervalSince1970: 2_000))
        let context = makeContext(clock: timeline)
        defer { context.cleanUp() }
        await context.apiClient.setStatusResults([
            .success(makeStatusResponse(soc: 60)),
            .success(makeStatusResponse(soc: 61))
        ])

        await context.viewModel.refresh()
        timeline.advance(by: 5 * 60) // exactly at debounce boundary
        await context.viewModel.refresh()

        #expect(context.reloadCounter.count == 2)
    }

    private struct Context {
        let viewModel: DashboardViewModel
        let apiClient: MockDashboardAPIClient
        let cache: WidgetSnapshotCache
        let reloadCounter: ReloadCounter
        let now: Date
        let cleanUp: () -> Void
    }

    private func makeContext(clock: TestClock? = nil) -> Context {
        let apiClient = MockDashboardAPIClient()
        let suiteName = "DashboardViewModelWidgetCacheTests.\(UUID().uuidString)"
        let cache = WidgetSnapshotCache(suiteName: suiteName)
        let reloadCounter = ReloadCounter()

        let fixedNow = Date(timeIntervalSince1970: 3_000)
        let nowProvider: @Sendable () -> Date = clock.map { c in { c.now } } ?? { fixedNow }
        let currentNow: Date = clock?.now ?? fixedNow

        let viewModel = DashboardViewModel(
            apiClient: apiClient,
            widgetCache: cache,
            widgetReloadTrigger: { reloadCounter.increment() },
            nowProvider: nowProvider
        )

        return Context(
            viewModel: viewModel,
            apiClient: apiClient,
            cache: cache,
            reloadCounter: reloadCounter,
            now: currentNow,
            cleanUp: {
                cache.clear()
                UserDefaults(suiteName: suiteName)?.removePersistentDomain(forName: suiteName)
            }
        )
    }
}

private func makeStatusResponse(soc: Double) -> StatusResponse {
    StatusResponse(
        live: LiveData(
            ppv: 1000,
            pload: 700,
            pbat: -250,
            pgrid: -50,
            pgridSustained: false,
            soc: soc,
            timestamp: "2026-04-15T00:00:00Z"
        ),
        battery: nil,
        rolling15min: nil,
        offpeak: nil,
        todayEnergy: nil
    )
}

private final class ReloadCounter: @unchecked Sendable {
    private let lock = NSLock()
    private var _count = 0

    var count: Int {
        lock.lock(); defer { lock.unlock() }
        return _count
    }

    func increment() {
        lock.lock(); defer { lock.unlock() }
        _count += 1
    }
}

private final class TestClock: @unchecked Sendable {
    private let lock = NSLock()
    private var current: Date

    init(start: Date) {
        self.current = start
    }

    var now: Date {
        lock.lock(); defer { lock.unlock() }
        return current
    }

    func advance(by seconds: TimeInterval) {
        lock.lock(); defer { lock.unlock() }
        current = current.addingTimeInterval(seconds)
    }
}

private actor MockDashboardAPIClient: FluxAPIClient {
    var statusResults: [Result<StatusResponse, Error>] = []
    var fetchStatusCallCount = 0
    var fetchDelay: Duration?

    func setStatusResults(_ statusResults: [Result<StatusResponse, Error>]) {
        self.statusResults = statusResults
    }

    func setFetchDelay(_ delay: Duration?) {
        fetchDelay = delay
    }

    func fetchStatus() async throws -> StatusResponse {
        fetchStatusCallCount += 1
        if let fetchDelay {
            try await Task.sleep(for: fetchDelay)
        }

        guard statusResults.isEmpty == false else {
            throw FluxAPIError.notConfigured
        }
        let result = statusResults.removeFirst()
        return try result.get()
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchDay(date _: String) async throws -> DayDetailResponse {
        throw FluxAPIError.notConfigured
    }
}
