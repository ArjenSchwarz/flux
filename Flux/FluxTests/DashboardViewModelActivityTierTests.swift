import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct DashboardViewModelActivityTierTests {
    @Test
    func updateActivityTierToInactiveSwitchesNextSleepTo60s() async {
        let apiClient = MockActivityTierAPIClient()
        await apiClient.setStatusResults([.success(makeStatusResponse(soc: 70))])
        let recorder = SleepRecorder()

        let viewModel = DashboardViewModel(
            apiClient: apiClient,
            sleep: { duration in
                await recorder.record(duration)
                throw CancellationError()
            }
        )

        viewModel.updateActivityTier(.inactive)
        viewModel.startAutoRefresh()

        for _ in 0 ..< 200 where await recorder.durations.isEmpty {
            await Task.yield()
        }
        viewModel.stopAutoRefresh()

        let recorded = await recorder.durations
        #expect(recorded.first == .seconds(60))
    }

    @Test
    func updateActivityTierFromInactiveToActiveTriggersImmediateRefresh() async {
        let apiClient = MockActivityTierAPIClient()
        await apiClient.setStatusResults(Array(repeating: .success(makeStatusResponse(soc: 70)), count: 5))

        let viewModel = DashboardViewModel(apiClient: apiClient)

        viewModel.updateActivityTier(.inactive)
        let initialCalls = await apiClient.fetchStatusCallCount

        viewModel.updateActivityTier(.active)

        for _ in 0 ..< 200 where (await apiClient.fetchStatusCallCount) <= initialCalls {
            await Task.yield()
        }

        #expect(await apiClient.fetchStatusCallCount == initialCalls + 1)
    }

    @Test
    func tierTransitionDoesNotEnqueueParallelRefreshWhenInFlight() async throws {
        let apiClient = MockActivityTierAPIClient()
        await apiClient.setStatusResults(Array(repeating: .success(makeStatusResponse(soc: 70)), count: 5))
        await apiClient.setFetchDelay(.milliseconds(100))

        let viewModel = DashboardViewModel(apiClient: apiClient)

        viewModel.updateActivityTier(.inactive)

        let refreshTask = Task { await viewModel.refresh() }

        for _ in 0 ..< 100 where (await apiClient.fetchStatusCallCount) < 1 {
            await Task.yield()
        }

        viewModel.updateActivityTier(.active)

        try await Task.sleep(for: .milliseconds(50))
        #expect(await apiClient.fetchStatusCallCount == 1)

        await refreshTask.value
    }

    @Test
    func updateActivityTierToSameStateDoesNotTriggerRefresh() async throws {
        let apiClient = MockActivityTierAPIClient()
        await apiClient.setStatusResults(Array(repeating: .success(makeStatusResponse(soc: 70)), count: 5))

        let viewModel = DashboardViewModel(apiClient: apiClient)

        viewModel.updateActivityTier(.active)
        try await Task.sleep(for: .milliseconds(20))
        let initialCalls = await apiClient.fetchStatusCallCount

        viewModel.updateActivityTier(.active)
        try await Task.sleep(for: .milliseconds(50))

        #expect(await apiClient.fetchStatusCallCount == initialCalls)
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
}

private actor SleepRecorder {
    private(set) var durations: [Duration] = []

    func record(_ duration: Duration) {
        durations.append(duration)
    }
}

private actor MockActivityTierAPIClient: FluxAPIClient {
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

    func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}
