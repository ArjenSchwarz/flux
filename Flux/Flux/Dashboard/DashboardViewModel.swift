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

    /// Active simulation preset id, or nil when not simulating. In-memory only
    /// (transient session state, Decision 5): nil on cold launch, survives
    /// auto-refresh and tab navigation, never persisted.
    private(set) var activeSimulationPresetID: String?

    /// The watts that produced the *currently displayed* simulated status.
    /// The banner sources its name/delta from this (not from the live presets
    /// list) so the banner and the figures always describe the same watts even
    /// across a cross-device edit ([2.7]).
    private(set) var activeSimulationDeltaWatts: Int?
    private(set) var activeSimulationName: String?

    private let apiClient: any FluxAPIClient
    private let simulationService: SimulationPresetsService
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
        simulationService: SimulationPresetsService = .shared,
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
        self.simulationService = simulationService
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

    /// Async version of the auto-refresh loop, intended for use with SwiftUI's
    /// `.task` modifier. Unlike `startAutoRefresh()` which detaches a `Task`
    /// from view lifecycle, this runs structured under the caller's task, so
    /// SwiftUI can cancel it deterministically on view disappear.
    ///
    /// Until the first successful load, the loop retries every second up to
    /// `runAutoRefreshFastPathLimit` attempts so that transient launch-time
    /// cancellations on macOS clear quickly. After that cap, fall back to the
    /// normal active/inactive interval so a persistent error (no network,
    /// misconfiguration) doesn't hammer the API every second indefinitely.
    func runAutoRefresh() async {
        var hasLoadedOnce = false
        var fastPathAttempts = 0
        while !Task.isCancelled {
            await refresh()
            if status != nil { hasLoadedOnce = true }
            let useFastPath = !hasLoadedOnce && fastPathAttempts < Self.runAutoRefreshFastPathLimit
            let interval: Duration = useFastPath ? .seconds(1) : currentInterval
            if useFastPath { fastPathAttempts += 1 }
            do {
                try await sleep(interval)
            } catch {
                return
            }
        }
    }

    static let runAutoRefreshFastPathLimit = 10

    func updateActivityTier(_ tier: ActivityTier) {
        let wasActive = (activityTier == .active)
        activityTier = tier
        if !wasActive && tier == .active {
            Task { await self.refreshIfIdle() }
        }
    }

    /// True while a simulation is active. Drives the banner and value tinting;
    /// independent of data availability (a stale/failed simulated fetch keeps
    /// the banner up while the affected values fall back to the error path,
    /// per [4.5]).
    var isSimulating: Bool { activeSimulationPresetID != nil }

    /// Activate (or switch to) a preset. Triggers an immediate refresh so the
    /// on-screen figures and tint change at once ([2.2] replace-on-switch,
    /// immediacy in the design).
    func activateSimulation(presetID: String) async {
        activeSimulationPresetID = presetID
        await refresh()
    }

    /// Turn simulation off. Clears the active id and immediately re-fetches the
    /// real status so the values and all simulated markings drop in the same
    /// cycle ([5.5]).
    func stopSimulation() async {
        activeSimulationPresetID = nil
        activeSimulationDeltaWatts = nil
        activeSimulationName = nil
        await refresh()
    }

    func refresh() async {
        guard !isLoading else { return }

        isLoading = true
        defer { isLoading = false }

        // Resolve the active preset's *current* watts from the presets list
        // each cycle ([2.7]). If the active id is absent (deleted locally or
        // removed via sync), simulation turns off ([2.4]).
        var simulateWatts: Int?
        var simulateName: String?
        if let activeID = activeSimulationPresetID {
            if let preset = simulationService.presets.first(where: { $0.id == activeID }) {
                simulateWatts = preset.watts
                simulateName = preset.label
            } else {
                activeSimulationPresetID = nil
                activeSimulationDeltaWatts = nil
                activeSimulationName = nil
            }
        }

        do {
            let simulating = simulateWatts != nil
            let response: StatusResponse
            if let watts = simulateWatts {
                response = try await apiClient.fetchStatus(simulateLoadWatts: watts)
            } else {
                response = try await apiClient.fetchStatus()
            }
            let fetchedAt = nowProvider()
            status = response
            lastSuccessfulFetch = fetchedAt
            error = nil

            if simulating {
                // Record the watts that produced the displayed status so the
                // banner and figures always describe the same watts ([2.7]).
                activeSimulationDeltaWatts = simulateWatts
                activeSimulationName = simulateName
                // A simulated status must never leak into the shared widget
                // cache — widgets always show real data (Decision 13). Skip the
                // cache write and the widget-reload trigger while simulating.
                return
            }

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
