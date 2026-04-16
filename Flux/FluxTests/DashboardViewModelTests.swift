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
            .failure(FluxAPIError.serverError),
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
