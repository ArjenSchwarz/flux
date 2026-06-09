import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor
@Suite(.serialized)
struct SimulationPresetsViewModelTests {
    @Test
    func draftStartsInvalid() {
        let vm = makeViewModel()
        vm.beginCreate()
        #expect(vm.canSave == false, "fresh draft (empty label, 0 W) must not be savable")
    }

    @Test
    func canSaveReflectsValidDraft() {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.label = "Charge car"
        vm.draft.watts = 1700
        #expect(vm.canSave)
    }

    @Test
    func saveDisabledWhenWattsOutOfRange() {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.label = "Charge car"
        vm.draft.watts = 0
        #expect(vm.canSave == false)
        vm.draft.watts = 20001
        #expect(vm.canSave == false)
    }

    @Test
    func saveDisabledWhenLabelEmpty() {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.label = ""
        vm.draft.watts = 1700
        #expect(vm.canSave == false)
    }

    @Test
    func addAffordanceDisabledAtCap() async throws {
        let (service, _) = makeServiceAndAPI()
        let vm = SimulationPresetsViewModel(service: service)
        for index in 0 ..< 20 {
            vm.beginCreate()
            vm.draft.label = "Preset \(index)"
            vm.draft.watts = 1000
            _ = try await vm.save()
        }
        #expect(vm.presets.count == 20)
        #expect(vm.addAffordanceEnabled == false, "the 21st add affordance must be disabled (cap 20)")
        // canSave must also be false in create mode at the cap.
        vm.beginCreate()
        vm.draft.label = "Preset 21"
        vm.draft.watts = 1000
        #expect(vm.canSave == false)
    }

    @Test
    func saveCreatesNewPresetWhenEditorOpenedForCreate() async throws {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.label = "Charge car"
        vm.draft.watts = 1700
        try await vm.save()
        #expect(vm.presets.count == 1)
        #expect(vm.presets.first?.watts == 1700)
    }

    @Test
    func saveUpdatesExistingPresetWhenEditorOpenedForEdit() async throws {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.label = "Charge car"
        vm.draft.watts = 1700
        try await vm.save()
        let existing = try #require(vm.presets.first)
        vm.beginEdit(existing)
        vm.draft.watts = 7000
        try await vm.save()
        #expect(vm.presets.first?.watts == 7000)
        #expect(vm.presets.count == 1)
    }

    @Test
    func deleteRemovesPresetLocally() async throws {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.label = "Charge car"
        vm.draft.watts = 1700
        try await vm.save()
        let preset = try #require(vm.presets.first)
        try await vm.delete(preset)
        #expect(vm.presets.isEmpty)
    }

    @Test
    func showsErrorBannerWhenServiceReportsLastError() async {
        let (service, _) = makeServiceAndAPI(failOnRefresh: FluxAPIError.serverError)
        try? await service.refresh()
        let vm = SimulationPresetsViewModel(service: service)
        #expect(vm.showsErrorBanner == true)
    }

    @Test
    func clearErrorDismissesBanner() async {
        let (service, _) = makeServiceAndAPI(failOnRefresh: FluxAPIError.serverError)
        try? await service.refresh()
        let vm = SimulationPresetsViewModel(service: service)
        #expect(vm.showsErrorBanner)
        vm.clearError()
        #expect(vm.showsErrorBanner == false)
    }

    // MARK: - helpers

    private func makeViewModel() -> SimulationPresetsViewModel {
        let (service, _) = makeServiceAndAPI()
        return SimulationPresetsViewModel(service: service)
    }

    private func makeServiceAndAPI(
        failOnRefresh error: Error? = nil
    ) -> (SimulationPresetsService, PresetsViewModelTestAPIClient) {
        let service = SimulationPresetsService()
        let api = PresetsViewModelTestAPIClient(refreshError: error)
        service.bind(apiClient: api)
        return (service, api)
    }
}

@MainActor
final class PresetsViewModelTestAPIClient: FluxAPIClient, @unchecked Sendable {
    private var presets: [SimulationPreset] = []
    private var nextID = 1
    private let refreshError: Error?
    private static let cap = 20

    init(refreshError: Error? = nil) { self.refreshError = refreshError }

    nonisolated func fetchStatus() async throws -> StatusResponse {
        StatusResponse(live: nil, battery: nil, rolling15min: nil, offpeak: nil, todayEnergy: nil)
    }
    nonisolated func fetchHistory(days _: Int) async throws -> HistoryResponse { HistoryResponse(days: []) }
    nonisolated func fetchDay(date: String) async throws -> DayDetailResponse {
        DayDetailResponse(date: date, readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil, note: nil)
    }
    nonisolated func saveNote(date: String, text _: String) async throws -> NoteResponse {
        NoteResponse(date: date, text: "", updatedAt: nil)
    }

    func fetchPresets() async throws -> [SimulationPreset] {
        if let refreshError { throw refreshError }
        return presets
    }

    func createPreset(_ draft: SimulationPresetDraft) async throws -> SimulationPreset {
        if presets.count >= Self.cap { throw FluxAPIError.ruleCapReached }
        let now = Date(timeIntervalSince1970: TimeInterval(nextID))
        let created = SimulationPreset(
            id: "test-\(nextID)",
            label: draft.label,
            watts: draft.watts,
            createdAt: now,
            updatedAt: now
        )
        nextID += 1
        presets.append(created)
        return created
    }

    func updatePreset(_ preset: SimulationPreset) async throws -> SimulationPreset {
        guard let idx = presets.firstIndex(where: { $0.id == preset.id }) else {
            throw FluxAPIError.notFound
        }
        var updated = preset
        updated.updatedAt = Date()
        presets[idx] = updated
        return updated
    }

    func deletePreset(id: String) async throws {
        presets.removeAll { $0.id == id }
    }
}
