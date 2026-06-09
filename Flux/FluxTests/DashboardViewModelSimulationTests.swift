import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor
@Suite(.serialized)
struct DashboardViewModelSimulationTests {
    // MARK: - Activation resolves watts and sends one simulated request

    @Test
    func activatingPresetSendsSimulatedRequestWithItsWatts() async {
        let ctx = makeContext()
        ctx.api.simulatedStatus = makeStatusResponse(soc: 50)
        await ctx.seedPresets([("p1", "Charge car", 1700)])

        await ctx.viewModel.activateSimulation(presetID: "p1")

        #expect(ctx.viewModel.isSimulating)
        #expect(ctx.api.lastSimulateWatts == 1700)
        #expect(ctx.api.simulateCallCount == 1)
        #expect(ctx.viewModel.status?.live?.soc == 50)
    }

    @Test
    func unsimulatedRefreshUsesPlainFetch() async {
        let ctx = makeContext()
        ctx.api.plainStatus = makeStatusResponse(soc: 80)
        await ctx.viewModel.refresh()
        #expect(ctx.viewModel.isSimulating == false)
        #expect(ctx.api.simulateCallCount == 0)
        #expect(ctx.api.plainCallCount == 1)
        #expect(ctx.viewModel.status?.live?.soc == 80)
    }

    // MARK: - Single active preset (replace on switch)

    @Test
    func activatingSecondPresetReplacesFirst() async {
        let ctx = makeContext()
        ctx.api.simulatedStatus = makeStatusResponse(soc: 40)
        await ctx.seedPresets([("p1", "Charge car", 1700), ("p2", "Heat pump", 3200)])

        await ctx.viewModel.activateSimulation(presetID: "p1")
        await ctx.viewModel.activateSimulation(presetID: "p2")

        #expect(ctx.viewModel.activeSimulationPresetID == "p2")
        #expect(ctx.api.lastSimulateWatts == 3200)
    }

    // MARK: - Watts re-resolved each refresh (sync edit)

    @Test
    func editedWattsFlowToNextSimulatedFetch() async {
        let ctx = makeContext()
        ctx.api.simulatedStatus = makeStatusResponse(soc: 40)
        await ctx.seedPresets([("p1", "Charge car", 1700)])

        await ctx.viewModel.activateSimulation(presetID: "p1")
        #expect(ctx.api.lastSimulateWatts == 1700)

        // Simulate a cross-device edit: the preset's watts change in the list.
        await ctx.updatePresetWatts("p1", 5000)
        await ctx.viewModel.refresh()
        #expect(ctx.api.lastSimulateWatts == 5000)
    }

    // MARK: - Deleted/absent active preset clears simulation

    @Test
    func deletedActivePresetClearsSimulation() async {
        let ctx = makeContext()
        ctx.api.simulatedStatus = makeStatusResponse(soc: 40)
        ctx.api.plainStatus = makeStatusResponse(soc: 90)
        await ctx.seedPresets([("p1", "Charge car", 1700)])

        await ctx.viewModel.activateSimulation(presetID: "p1")
        #expect(ctx.viewModel.isSimulating)

        // The preset disappears from the list (deleted here or via sync).
        await ctx.removeAllPresets()
        await ctx.viewModel.refresh()

        #expect(ctx.viewModel.isSimulating == false)
        #expect(ctx.viewModel.activeSimulationPresetID == nil)
        // Falls back to the plain (real) status.
        #expect(ctx.viewModel.status?.live?.soc == 90)
    }

    // MARK: - Stop

    @Test
    func stopSimulationFetchesRealStatusImmediately() async {
        let ctx = makeContext()
        ctx.api.simulatedStatus = makeStatusResponse(soc: 40)
        ctx.api.plainStatus = makeStatusResponse(soc: 88)
        await ctx.seedPresets([("p1", "Charge car", 1700)])

        await ctx.viewModel.activateSimulation(presetID: "p1")
        let plainCallsBeforeStop = ctx.api.plainCallCount
        await ctx.viewModel.stopSimulation()

        #expect(ctx.viewModel.isSimulating == false)
        #expect(ctx.api.plainCallCount == plainCallsBeforeStop + 1, "stop must fetch real status immediately")
        #expect(ctx.viewModel.status?.live?.soc == 88)
    }

    // MARK: - Widget cache skipped while simulating

    @Test
    func widgetCacheNotWrittenWhileSimulating() async {
        let ctx = makeContext()
        ctx.api.simulatedStatus = makeStatusResponse(soc: 33)
        await ctx.seedPresets([("p1", "Charge car", 1700)])

        await ctx.viewModel.activateSimulation(presetID: "p1")

        #expect(ctx.cache.read() == nil, "a simulated status must never be cached for widgets")
        #expect(ctx.reloadCounter.count == 0)
        ctx.cleanUp()
    }

    @Test
    func widgetCacheWrittenWhenNotSimulating() async {
        let ctx = makeContext()
        ctx.api.plainStatus = makeStatusResponse(soc: 64)
        await ctx.viewModel.refresh()
        #expect(ctx.cache.read()?.status.live?.soc == 64)
        ctx.cleanUp()
    }

    // MARK: - Banner presentation values

    @Test
    func bannerExposesActivePresetNameAndDelta() async {
        let ctx = makeContext()
        ctx.api.simulatedStatus = makeStatusResponse(soc: 40)
        await ctx.seedPresets([("p1", "Charge car", 1700)])

        await ctx.viewModel.activateSimulation(presetID: "p1")
        #expect(ctx.viewModel.activeSimulationName == "Charge car")
        // Delta is the preset's added load (always positive), sourced from the
        // watts that produced the displayed status.
        #expect(ctx.viewModel.activeSimulationDeltaWatts == 1700)
    }

    // MARK: - helpers

    private struct Context {
        let viewModel: DashboardViewModel
        let api: SimDashboardAPIClient
        let service: SimulationPresetsService
        let cache: WidgetSnapshotCache
        let reloadCounter: SimReloadCounter
        let suiteName: String
        let seedPresets: ([(String, String, Int)]) async -> Void
        let updatePresetWatts: (String, Int) async -> Void
        let removeAllPresets: () async -> Void
        let cleanUp: () -> Void
    }

    private func makeContext() -> Context {
        let api = SimDashboardAPIClient()
        let service = SimulationPresetsService()
        service.bind(apiClient: api)
        let suiteName = "DashboardViewModelSimulationTests.\(UUID().uuidString)"
        let cache = WidgetSnapshotCache(suiteName: suiteName)
        let reloadCounter = SimReloadCounter()
        let fixedNow = Date(timeIntervalSince1970: 3_000)

        let viewModel = DashboardViewModel(
            apiClient: api,
            simulationService: service,
            widgetCache: cache,
            widgetReloadTrigger: { reloadCounter.increment() },
            nowProvider: { fixedNow }
        )

        return Context(
            viewModel: viewModel,
            api: api,
            service: service,
            cache: cache,
            reloadCounter: reloadCounter,
            suiteName: suiteName,
            seedPresets: { specs in
                api.storedPresets = specs.map { id, label, watts in
                    SimulationPreset(
                        id: id, label: label, watts: watts,
                        createdAt: Date(timeIntervalSince1970: 1), updatedAt: Date(timeIntervalSince1970: 1)
                    )
                }
                try? await service.refresh()
            },
            updatePresetWatts: { id, watts in
                api.storedPresets = api.storedPresets.map {
                    guard $0.id == id else { return $0 }
                    var copy = $0
                    copy.watts = watts
                    return copy
                }
                try? await service.refresh()
            },
            removeAllPresets: {
                api.storedPresets = []
                try? await service.refresh()
            },
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
            ppv: 1000, pload: 700, pbat: 250, pgrid: 0,
            pgridSustained: false, soc: soc, timestamp: "2026-06-09T10:00:00Z"
        ),
        battery: nil,
        rolling15min: nil,
        offpeak: nil,
        todayEnergy: nil
    )
}

private final class SimReloadCounter: @unchecked Sendable {
    private let lock = NSLock()
    private var _count = 0
    var count: Int { lock.lock(); defer { lock.unlock() }; return _count }
    func increment() { lock.lock(); defer { lock.unlock() }; _count += 1 }
}

@MainActor
private final class SimDashboardAPIClient: FluxAPIClient, @unchecked Sendable {
    var storedPresets: [SimulationPreset] = []
    var plainStatus = StatusResponse(live: nil, battery: nil, rolling15min: nil, offpeak: nil, todayEnergy: nil)
    var simulatedStatus = StatusResponse(live: nil, battery: nil, rolling15min: nil, offpeak: nil, todayEnergy: nil)
    var plainCallCount = 0
    var simulateCallCount = 0
    var lastSimulateWatts: Int?

    nonisolated func fetchHistory(days _: Int) async throws -> HistoryResponse { HistoryResponse(days: []) }
    nonisolated func fetchDay(date: String) async throws -> DayDetailResponse {
        DayDetailResponse(date: date, readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil, note: nil)
    }
    nonisolated func saveNote(date: String, text _: String) async throws -> NoteResponse {
        NoteResponse(date: date, text: "", updatedAt: nil)
    }

    func fetchStatus() async throws -> StatusResponse {
        plainCallCount += 1
        return plainStatus
    }

    func fetchStatus(simulateLoadWatts: Int) async throws -> StatusResponse {
        simulateCallCount += 1
        lastSimulateWatts = simulateLoadWatts
        return simulatedStatus
    }

    func fetchPresets() async throws -> [SimulationPreset] { storedPresets }
}
