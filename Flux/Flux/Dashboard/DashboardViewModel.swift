import FluxCore
import Foundation
import Observation
import WidgetKit

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
    private let widgetCache: WidgetSnapshotCache
    private let widgetReloadTrigger: @Sendable () -> Void
    private let widgetReloadDebounce: TimeInterval
    private var refreshTask: Task<Void, Never>?
    private var lastWidgetReload: Date?

    init(
        apiClient: any FluxAPIClient,
        refreshInterval: Duration = .seconds(10),
        widgetCache: WidgetSnapshotCache = WidgetSnapshotCache(),
        widgetReloadTrigger: @escaping @Sendable () -> Void = {
            WidgetCenter.shared.reloadTimelines(ofKind: "me.nore.ig.flux.widget.battery")
            WidgetCenter.shared.reloadTimelines(ofKind: "me.nore.ig.flux.widget.accessory")
        },
        widgetReloadDebounce: TimeInterval = 5 * 60,
        nowProvider: @escaping @Sendable () -> Date = { .now },
        sleep: @escaping @Sendable (Duration) async throws -> Void = { duration in
            try await Task.sleep(for: duration)
        }
    ) {
        self.apiClient = apiClient
        self.refreshInterval = refreshInterval
        self.widgetCache = widgetCache
        self.widgetReloadTrigger = widgetReloadTrigger
        self.widgetReloadDebounce = widgetReloadDebounce
        self.nowProvider = nowProvider
        self.sleep = sleep
    }

    func startAutoRefresh() {
        guard refreshTask == nil else { return }

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
            let fetchedAt = nowProvider()
            status = response
            lastSuccessfulFetch = fetchedAt
            error = nil

            let envelope = StatusSnapshotEnvelope(fetchedAt: fetchedAt, status: response)
            let wrote = widgetCache.writeIfNewer(envelope)

            if wrote, shouldTriggerWidgetReload(at: fetchedAt) {
                widgetReloadTrigger()
                lastWidgetReload = fetchedAt
            }
        } catch is CancellationError {
            // View lifecycle can cancel in-flight requests; not a real error.
        } catch let urlError as URLError where urlError.code == .cancelled {
            // URLSession reports cancellation as URLError.cancelled.
        } catch {
            self.error = FluxAPIError.from(error)
        }
    }

    private func shouldTriggerWidgetReload(at fetchedAt: Date) -> Bool {
        guard let last = lastWidgetReload else { return true }
        return fetchedAt.timeIntervalSince(last) >= widgetReloadDebounce
    }
}
