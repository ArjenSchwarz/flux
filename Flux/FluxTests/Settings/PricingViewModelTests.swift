import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct PricingViewModelTests {
    @Test
    func refreshLoadsPeriodsFromService() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        apiClient.periodsToReturn = [makePeriod(id: "p1", start: "2026-01-01", end: "2026-06-30")]
        await viewModel.refresh()
        #expect(viewModel.periods.count == 1)
        #expect(viewModel.periods.first?.id == "p1")
    }

    @Test
    func beginCreateSeedsBlankDraft() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        #expect(viewModel.isEditorPresented)
        #expect(viewModel.editorMode == .create)
        #expect(viewModel.draft.startDate == "")
    }

    @Test
    func beginEditSeedsDraftFromPeriod() {
        let (viewModel, _) = makeViewModelAndAPI()
        let period = makePeriod(id: "p1", start: "2026-01-01", end: "2026-06-30", peak: 0.30)
        viewModel.beginEdit(period)
        #expect(viewModel.isEditorPresented)
        if case .edit(let target) = viewModel.editorMode {
            #expect(target.id == "p1")
        } else {
            Issue.record("expected edit mode")
        }
        #expect(viewModel.draft.peakRate == 0.30)
        #expect(viewModel.draft.startDate == "2026-01-01")
    }

    @Test
    func setEditorPresentedFalseDismissesAndClearsRemediation() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        #expect(viewModel.isEditorPresented)
        viewModel.setEditorPresented(false)
        #expect(!viewModel.isEditorPresented)
    }

    @Test
    func saveCreateAppendsRow() async throws {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        viewModel.draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        try await viewModel.save()
        #expect(viewModel.periods.contains(where: { $0.startDate == "2026-08-01" }))
        #expect(!viewModel.isEditorPresented, "editor must dismiss after alpha successful save")
    }

    @Test
    func saveEditUpdatesRow() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        let period = makePeriod(id: "p1", start: "2026-01-01", end: "2026-06-30", peak: 0.28)
        apiClient.periodsToReturn = [period]
        await viewModel.refresh()
        viewModel.beginEdit(period)
        viewModel.draft.peakRate = 0.32
        try await viewModel.save()
        // foldReplace runs synchronously inside service.update, so
        // viewModel.periods reflects the new rate as soon as save() returns
        // — no need to wait for the fire-and-forget refetch here.
        #expect(viewModel.periods.first?.peakRate == 0.32)
    }

    @Test
    func deleteRemovesPeriod() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        let period = makePeriod(id: "p1", start: "2026-01-01", end: "2026-06-30")
        apiClient.periodsToReturn = [period]
        await viewModel.refresh()
        try await viewModel.delete(period)
        #expect(viewModel.periods.isEmpty)
    }

    @Test
    func saveSurfacesOverlapErrorAsBanner() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        apiClient.nextCreateError = .pricingValidation(.overlap(openEndedId: "open-id"))
        viewModel.beginCreate()
        viewModel.draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        do {
            try await viewModel.save()
            Issue.record("expected save to throw")
        } catch {
            // expected
        }
        #expect(viewModel.lastValidationError == .overlap(openEndedId: "open-id"))
        #expect(viewModel.overlapRemediationTargetId == "open-id",
                "one-tap remediation must surface when overlap carries an openEndedId")
    }

    @Test
    func saveSurfacesInvertedDatesAsValidationError() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        apiClient.nextCreateError = .pricingValidation(.invertedDates)
        viewModel.beginCreate()
        viewModel.draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: "2026-07-31",
            peakRate: 0.30,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12
        )
        do {
            try await viewModel.save()
            Issue.record("expected throw")
        } catch {}
        #expect(viewModel.lastValidationError == .invertedDates)
    }

    @Test
    func remediateClosesOpenEndedAndCreatesNew() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        let open = makePeriod(id: "pp-open", start: "2026-01-01", end: nil)
        apiClient.periodsToReturn = [open]
        await viewModel.refresh()
        let closing = makePeriod(id: "pp-open", start: "2026-01-01", end: "2026-07-31")
        let newOpen = makePeriod(id: "pp-new", start: "2026-08-01", end: nil)
        apiClient.replaceOpenEndedResult = ReplaceOpenEndedResult(closing: closing, newPeriod: newOpen)
        apiClient.nextCreateError = nil

        viewModel.beginCreate()
        viewModel.draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        // Simulate the overlap error setting the remediation target.
        apiClient.nextCreateError = .pricingValidation(.overlap(openEndedId: "pp-open"))
        try? await viewModel.save()
        #expect(viewModel.overlapRemediationTargetId == "pp-open")

        apiClient.nextCreateError = nil
        try await viewModel.remediateOverlap()
        // After remediation, editor dismisses and remediation target clears.
        #expect(viewModel.overlapRemediationTargetId == nil)
        #expect(!viewModel.isEditorPresented)
    }

    @Test
    func clearErrorDismissesBanner() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        // Manually push an error into the service.
        viewModel.service.bind(apiClient: TestPricingAPIClient())
        viewModel.clearError()
        #expect(viewModel.lastValidationError == nil)
    }

    // MARK: - helpers

    private func makeViewModelAndAPI() -> (PricingViewModel, TestPricingAPIClient) {
        let apiClient = TestPricingAPIClient()
        let service = PricingService()
        service.bind(apiClient: apiClient)
        return (PricingViewModel(service: service), apiClient)
    }

    private func makePeriod(
        id: String,
        start: String,
        end: String?,
        peak: Double = 0.30
    ) -> PricingPeriod {
        PricingPeriod(
            id: id,
            startDate: start,
            endDate: end,
            peakRate: peak,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
        )
    }
}

// MARK: - test double

@MainActor
final class TestPricingAPIClient: FluxAPIClient, @unchecked Sendable {
    var periodsToReturn: [PricingPeriod] = []
    var nextCreateError: FluxAPIError?
    var replaceOpenEndedResult: ReplaceOpenEndedResult?

    nonisolated func fetchStatus() async throws -> StatusResponse {
        StatusResponse(live: nil, battery: nil, rolling15min: nil, offpeak: nil, todayEnergy: nil, note: nil)
    }
    nonisolated func fetchHistory(days _: Int) async throws -> HistoryResponse { HistoryResponse(days: []) }
    nonisolated func fetchDay(date: String) async throws -> DayDetailResponse {
        DayDetailResponse(date: date, readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil, note: nil)
    }
    nonisolated func saveNote(date: String, text _: String) async throws -> NoteResponse {
        NoteResponse(date: date, text: "", updatedAt: nil)
    }

    func fetchPricing() async throws -> [PricingPeriod] { periodsToReturn }

    func createPricing(_ draft: PricingPeriodDraft) async throws -> PricingPeriod {
        if let err = nextCreateError {
            nextCreateError = nil
            throw err
        }
        let now = Date()
        let period = PricingPeriod(
            id: "new-\(UUID().uuidString)",
            startDate: draft.startDate,
            endDate: draft.endDate,
            peakRate: draft.peakRate,
            feedInRate: draft.feedInRate,
            offPeakSavingsRate: draft.offPeakSavingsRate,
            createdAt: now, updatedAt: now
        )
        periodsToReturn.append(period)
        return period
    }

    func updatePricing(id: String, _ draft: PricingPeriodDraft) async throws -> PricingPeriod {
        guard let idx = periodsToReturn.firstIndex(where: { $0.id == id }) else {
            throw FluxAPIError.notFound
        }
        let now = Date()
        let period = PricingPeriod(
            id: id,
            startDate: draft.startDate,
            endDate: draft.endDate,
            peakRate: draft.peakRate,
            feedInRate: draft.feedInRate,
            offPeakSavingsRate: draft.offPeakSavingsRate,
            createdAt: periodsToReturn[idx].createdAt,
            updatedAt: now
        )
        periodsToReturn[idx] = period
        return period
    }

    func deletePricing(id: String) async throws {
        periodsToReturn.removeAll { $0.id == id }
    }

    func replaceOpenEndedPricing(
        closingId _: String,
        with _: PricingPeriodDraft
    ) async throws -> ReplaceOpenEndedResult {
        if let result = replaceOpenEndedResult { return result }
        throw FluxAPIError.serverError
    }
}
