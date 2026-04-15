import Foundation
import Observation

@MainActor @Observable
final class DashboardViewModel {
    private(set) var status: StatusResponse?
    private(set) var lastSuccessfulFetch: Date?
    private(set) var error: FluxAPIError?
    private(set) var isLoading = false

    private let apiClient: any FluxAPIClient
    private let nowProvider: @Sendable () -> Date
    private let refreshInterval: Duration
    private let sleep: @Sendable (Duration) async throws -> Void
    private var refreshTask: Task<Void, Never>?

    init(
        apiClient: any FluxAPIClient,
        refreshInterval: Duration = .seconds(10),
        nowProvider: @escaping @Sendable () -> Date = { .now },
        sleep: @escaping @Sendable (Duration) async throws -> Void = { duration in
            try await Task.sleep(for: duration)
        }
    ) {
        self.apiClient = apiClient
        self.refreshInterval = refreshInterval
        self.nowProvider = nowProvider
        self.sleep = sleep
    }

    func startAutoRefresh() {
        refreshTask?.cancel()

        refreshTask = Task { [weak self] in
            guard let self else { return }

            while !Task.isCancelled {
                await self.refresh()

                do {
                    try await self.sleep(self.refreshInterval)
                } catch {
                    return
                }
            }
        }
    }

    func stopAutoRefresh() {
        refreshTask?.cancel()
        refreshTask = nil
    }

    func refresh() async {
        guard !isLoading else { return }

        isLoading = true
        defer { isLoading = false }

        do {
            let response = try await apiClient.fetchStatus()
            status = response
            lastSuccessfulFetch = nowProvider()
            error = nil
        } catch let apiError as FluxAPIError {
            error = apiError
        } catch {
            self.error = .networkError(error.localizedDescription)
        }
    }
}
