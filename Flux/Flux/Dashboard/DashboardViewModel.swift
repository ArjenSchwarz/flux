import FluxCore
import Foundation
import Observation
import WidgetKit

@MainActor @Observable
final class DashboardViewModel {
    enum ActivityTier: Sendable, Equatable {
        case active
        case inactive
    }

    private(set) var status: StatusResponse?
    private(set) var lastSuccessfulFetch: Date?
    private(set) var error: FluxAPIError?
    private(set) var isLoading = false
    private(set) var activityTier: ActivityTier = .active

    private let apiClient: any FluxAPIClient
    private let nowProvider: @Sendable () -> Date
    private let sleep: @Sendable (Duration) async throws -> Void
    private let widgetCache: WidgetSnapshotCache
    private let widgetReloadTrigger: @Sendable () -> Void
    private let widgetReloadDebounce: TimeInterval
    private let controlReloadTrigger: @Sendable () -> Void
    private let controlReloadDebounce: TimeInterval
    private var refreshTask: Task<Void, Never>?
    private var lastWidgetReload: Date?
    private var lastControlReload: Date?

    init(
        apiClient: any FluxAPIClient,
        widgetCache: WidgetSnapshotCache = WidgetSnapshotCache(),
        widgetReloadTrigger: @escaping @Sendable () -> Void = {
            WidgetCenter.shared.reloadTimelines(ofKind: WidgetKinds.battery)
            WidgetCenter.shared.reloadTimelines(ofKind: WidgetKinds.accessory)
        },
        widgetReloadDebounce: TimeInterval = 5 * 60,
        controlReloadTrigger: @escaping @Sendable () -> Void = {
            #if os(macOS)
            ControlCenter.shared.reloadControls(ofKind: WidgetKinds.controlBattery)
            #endif
        },
        controlReloadDebounce: TimeInterval = 60,
        nowProvider: @escaping @Sendable () -> Date = { .now },
        sleep: @escaping @Sendable (Duration) async throws -> Void = { duration in
            try await Task.sleep(for: duration)
        }
    ) {
        self.apiClient = apiClient
        self.widgetCache = widgetCache
        self.widgetReloadTrigger = widgetReloadTrigger
        self.widgetReloadDebounce = widgetReloadDebounce
        self.controlReloadTrigger = controlReloadTrigger
        self.controlReloadDebounce = controlReloadDebounce
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
                    try await self.sleep(self.currentInterval)
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

    func updateActivityTier(_ tier: ActivityTier) {
        let wasActive = (activityTier == .active)
        activityTier = tier
        if !wasActive && tier == .active {
            Task { await self.refreshIfIdle() }
        }
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

            #if os(macOS)
            if shouldTriggerControlReload(at: fetchedAt) {
                controlReloadTrigger()
                lastControlReload = fetchedAt
            }
            #endif
        } catch is CancellationError {
            // View lifecycle can cancel in-flight requests; not a real error.
        } catch let urlError as URLError where urlError.code == .cancelled {
            // URLSession reports cancellation as URLError.cancelled.
        } catch {
            self.error = FluxAPIError.from(error)
        }
    }

    private var currentInterval: Duration {
        switch activityTier {
        case .active: .seconds(10)
        case .inactive: .seconds(60)
        }
    }

    private func refreshIfIdle() async {
        guard !isLoading else { return }
        await refresh()
    }

    private func shouldTriggerWidgetReload(at fetchedAt: Date) -> Bool {
        guard let last = lastWidgetReload else { return true }
        return fetchedAt.timeIntervalSince(last) >= widgetReloadDebounce
    }

    private func shouldTriggerControlReload(at fetchedAt: Date) -> Bool {
        guard let last = lastControlReload else { return true }
        return fetchedAt.timeIntervalSince(last) >= controlReloadDebounce
    }
}
