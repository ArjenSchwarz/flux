import Foundation
import Testing
@testable import FluxCore

@MainActor
@Suite(.serialized)
struct SimulationPresetsServiceTests {
    @Test
    func refreshLoadsPresetsFromBackend() async throws {
        let api = SimulationTestAPIClient()
        let now = Date()
        api.presets = [
            SimulationPreset(id: "p1", label: "Charge car", watts: 1700, createdAt: now, updatedAt: now)
        ]
        let svc = makeService(api: api)
        try await svc.refresh()
        #expect(svc.presets.count == 1)
        #expect(svc.presets.first?.id == "p1")
        #expect(svc.lastError == nil)
    }

    @Test
    func refreshFailureSetsLastErrorAndLeavesListUnchanged() async {
        let api = SimulationTestAPIClient()
        api.refreshError = FluxAPIError.serverError
        let svc = makeService(api: api)
        try? await svc.refresh()
        #expect(svc.presets.isEmpty)
        #expect(svc.lastError != nil)
    }

    @Test
    func createAppliesAfterServerConfirms() async throws {
        let api = SimulationTestAPIClient()
        let svc = makeService(api: api)
        let created = try await svc.create(SimulationPresetDraft(label: "Charge car", watts: 1700))
        #expect(svc.presets.contains { $0.id == created.id })
        #expect(svc.lastError == nil)
    }

    @Test
    func createFailureSetsLastErrorAndLeavesListUnchanged() async {
        let api = SimulationTestAPIClient()
        api.createError = FluxAPIError.ruleCapReached
        let svc = makeService(api: api)
        do {
            _ = try await svc.create(SimulationPresetDraft(label: "x", watts: 100))
            Issue.record("expected create to throw")
        } catch {
            #expect(svc.presets.isEmpty)
            #expect(svc.lastError != nil)
        }
    }

    @Test
    func updateReplacesAfterServerConfirms() async throws {
        let api = SimulationTestAPIClient()
        let svc = makeService(api: api)
        let created = try await svc.create(SimulationPresetDraft(label: "Charge car", watts: 1700))
        var edited = created
        edited.watts = 7000
        let updated = try await svc.update(edited)
        #expect(updated.watts == 7000)
        #expect(svc.presets.first { $0.id == created.id }?.watts == 7000)
    }

    @Test
    func updateFailureSetsLastErrorAndLeavesListUnchanged() async throws {
        let api = SimulationTestAPIClient()
        let svc = makeService(api: api)
        let created = try await svc.create(SimulationPresetDraft(label: "Charge car", watts: 1700))
        api.updateError = FluxAPIError.serverError
        var edited = created
        edited.watts = 9999
        do {
            _ = try await svc.update(edited)
            Issue.record("expected update to throw")
        } catch {
            #expect(svc.presets.first { $0.id == created.id }?.watts == 1700)
            #expect(svc.lastError != nil)
        }
    }

    @Test
    func deleteRemovesAfterServerConfirms() async throws {
        let api = SimulationTestAPIClient()
        let svc = makeService(api: api)
        let created = try await svc.create(SimulationPresetDraft(label: "Charge car", watts: 1700))
        try await svc.delete(created.id)
        #expect(svc.presets.isEmpty)
        #expect(svc.lastError == nil)
    }

    @Test
    func deleteFailureSetsLastErrorAndLeavesListUnchanged() async throws {
        let api = SimulationTestAPIClient()
        let svc = makeService(api: api)
        let created = try await svc.create(SimulationPresetDraft(label: "Charge car", watts: 1700))
        api.deleteError = FluxAPIError.serverError
        do {
            try await svc.delete(created.id)
            Issue.record("expected delete to throw")
        } catch {
            #expect(svc.presets.contains { $0.id == created.id })
            #expect(svc.lastError != nil)
        }
    }

    @Test
    func clearErrorResetsLastError() async {
        let api = SimulationTestAPIClient()
        api.refreshError = FluxAPIError.serverError
        let svc = makeService(api: api)
        try? await svc.refresh()
        #expect(svc.lastError != nil)
        svc.clearError()
        #expect(svc.lastError == nil)
    }

    // MARK: - helpers

    private func makeService(api: SimulationTestAPIClient) -> SimulationPresetsService {
        let service = SimulationPresetsService()
        service.bind(apiClient: api)
        return service
    }
}

// MARK: - Test double

@MainActor
final class SimulationTestAPIClient: FluxAPIClient, @unchecked Sendable {
    var presets: [SimulationPreset] = []
    var refreshError: Error?
    var createError: Error?
    var updateError: Error?
    var deleteError: Error?
    private var nextSeq = 1

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
        if let createError { throw createError }
        let now = Date()
        let created = SimulationPreset(
            id: "test-\(nextSeq)",
            label: draft.label,
            watts: draft.watts,
            createdAt: now,
            updatedAt: now
        )
        nextSeq += 1
        presets.append(created)
        return created
    }

    func updatePreset(_ preset: SimulationPreset) async throws -> SimulationPreset {
        if let updateError { throw updateError }
        guard let idx = presets.firstIndex(where: { $0.id == preset.id }) else {
            throw FluxAPIError.notFound
        }
        var updated = preset
        updated.updatedAt = Date()
        presets[idx] = updated
        return updated
    }

    func deletePreset(id: String) async throws {
        if let deleteError { throw deleteError }
        presets.removeAll { $0.id == id }
    }
}
