import FluxCore
import Foundation
import Testing
@testable import Flux

// swiftlint:disable file_length type_body_length
@MainActor @Suite(.serialized)
struct PricingViewModelTests {
    @Test
    func refreshLoadsPlansFromService() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        apiClient.plansToReturn = [makePlan(id: "p1", start: "2026-01-01", end: "2026-07-01")]
        await viewModel.refresh()
        #expect(viewModel.plans.count == 1)
        #expect(viewModel.plans.first?.id == "p1")
    }

    @Test
    func beginCreateSeedsADraftWithTheCurrentFreeWindow() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        #expect(viewModel.isEditorPresented)
        #expect(viewModel.editorMode == .create)
        #expect(viewModel.draft.startDate == "")
        #expect(viewModel.draft.windows.isEmpty)
    }

    @Test
    func beginEditSeedsDraftFromPlanIncludingWindows() {
        let (viewModel, _) = makeViewModelAndAPI()
        let plan = makeTouPlan(id: "p1", start: "2026-08-01", end: nil)
        viewModel.beginEdit(plan)
        #expect(viewModel.isEditorPresented)
        if case .edit(let target) = viewModel.editorMode {
            #expect(target.id == "p1")
        } else {
            Issue.record("expected edit mode")
        }
        #expect(viewModel.draft.defaultRate == 0.35)
        #expect(viewModel.draft.startDate == "2026-08-01")
        #expect(viewModel.draft.windows.count == 2)
        #expect(viewModel.draft.windows[0].free)
        #expect(viewModel.draft.windows[1].rate == 0.28)
    }

    @Test
    func setEditorPresentedFalseDismissesAndClearsRemediation() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        #expect(viewModel.isEditorPresented)
        viewModel.setEditorPresented(false)
        #expect(!viewModel.isEditorPresented)
    }

    // MARK: - Window editing

    @Test
    func addWindowAppendsARatedWindowAtTheDefaultRate() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        viewModel.draft.defaultRate = 0.35
        viewModel.addWindow()
        #expect(viewModel.draft.windows.count == 1)
        #expect(!viewModel.draft.windows[0].free)
        #expect(viewModel.draft.windows[0].rate == 0.35)
    }

    @Test
    func removeWindowDropsTheRowAtTheGivenIndex() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginEdit(makeTouPlan(id: "p1", start: "2026-08-01", end: nil))
        viewModel.removeWindow(at: 0)
        #expect(viewModel.draft.windows.count == 1)
        #expect(viewModel.draft.windows[0].start == "01:00")
    }

    @Test
    func markingAWindowFreeDropsItsRate() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        viewModel.draft.defaultRate = 0.35
        viewModel.addWindow()
        viewModel.setWindowFree(true, at: 0)
        #expect(viewModel.draft.windows[0].free)
        #expect(viewModel.draft.windows[0].rate == nil)
    }

    @Test
    func markingAWindowRatedSeedsItFromTheDefaultRate() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginEdit(makeTouPlan(id: "p1", start: "2026-08-01", end: nil))
        viewModel.setWindowFree(false, at: 0)
        #expect(!viewModel.draft.windows[0].free)
        #expect(viewModel.draft.windows[0].rate == 0.35)
    }

    @Test
    func canSaveMirrorsDraftValidation() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        #expect(!viewModel.canSave, "a blank draft has no valid start date")

        viewModel.draft = makeDraft()
        #expect(viewModel.canSave)

        // A free window with no savings reference rate is rejected locally,
        // mirroring the server (AC 6.4).
        viewModel.draft.savingsReferenceRate = nil
        #expect(!viewModel.canSave)
    }

    // MARK: - Save

    @Test
    func saveCreateAppendsPlan() async throws {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        viewModel.draft = makeDraft()
        try await viewModel.save()
        #expect(viewModel.plans.contains(where: { $0.startDate == "2026-08-01" }))
        #expect(!viewModel.isEditorPresented, "editor must dismiss after a successful save")
    }

    @Test
    func saveNormalisesEveryRateToFourDecimalPlaces() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        viewModel.beginCreate()
        var draft = makeDraft()
        draft.defaultRate = 0.354321
        draft.feedInRate = 0.056789
        draft.savingsReferenceRate = 0.351111
        draft.windows = [
            PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
            PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.284567)
        ]
        viewModel.draft = draft
        try await viewModel.save()

        let sent = try #require(apiClient.lastCreatedDraft)
        #expect(sent.defaultRate == 0.3543)
        #expect(sent.feedInRate == 0.0568)
        #expect(sent.savingsReferenceRate == 0.3511)
        #expect(sent.windows[1].rate == 0.2846)
        // A free window carries no rate by contract.
        #expect(sent.windows[0].rate == nil)
    }

    @Test
    func saveEditUpdatesPlan() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        let plan = makePlan(id: "p1", start: "2026-01-01", end: "2026-07-01", defaultRate: 0.28)
        apiClient.plansToReturn = [plan]
        await viewModel.refresh()
        viewModel.beginEdit(plan)
        viewModel.draft.defaultRate = 0.32
        try await viewModel.save()
        // foldReplace runs synchronously inside service.update, so
        // viewModel.plans reflects the new rate as soon as save() returns.
        #expect(viewModel.plans.first?.defaultRate == 0.32)
    }

    @Test
    func deleteRemovesPlan() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        let plan = makePlan(id: "p1", start: "2026-01-01", end: "2026-07-01")
        apiClient.plansToReturn = [plan]
        await viewModel.refresh()
        try await viewModel.delete(plan)
        #expect(viewModel.plans.isEmpty)
    }

    // MARK: - Validation errors

    @Test
    func saveSurfacesOverlapErrorAsBanner() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        apiClient.nextCreateError = .pricingValidation(.overlap(openEndedId: "open-id"))
        viewModel.beginCreate()
        viewModel.draft = makeDraft()
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
    func anOverlapWithANonOpenEndedPlanOffersNoRemediation() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        apiClient.nextCreateError = .pricingValidation(.overlap(openEndedId: nil))
        viewModel.beginCreate()
        viewModel.draft = makeDraft()
        try? await viewModel.save()
        #expect(viewModel.lastValidationError == .overlap(openEndedId: nil))
        #expect(viewModel.overlapRemediationTargetId == nil)
    }

    @Test
    func saveSurfacesBandValidationErrors() async throws {
        let cases: [PricingValidationReason] = [
            .invertedDates, .bandWindowInvalid, .bandOverlap,
            .multipleFreeBands, .savingsRateMissing, .noRatedBand, .legacyShape
        ]
        for reason in cases {
            let (viewModel, apiClient) = makeViewModelAndAPI()
            apiClient.nextCreateError = .pricingValidation(reason)
            viewModel.beginCreate()
            viewModel.draft = makeDraft()
            try? await viewModel.save()
            #expect(viewModel.lastValidationError == reason, "\(reason)")
        }
    }

    // MARK: - Succession (AC 6.3 / 6.5)

    @Test
    func remediationEndsTheCurrentPlanOnTheSuccessorsStartDate() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        let open = makePlan(id: "pp-open", start: "2026-01-01", end: nil)
        apiClient.plansToReturn = [open]
        await viewModel.refresh()
        // The closing row's exclusive end date IS the successor's start date —
        // no ±1 arithmetic anywhere (AC 2.2).
        let closing = makePlan(id: "pp-open", start: "2026-01-01", end: "2026-08-01")
        let newOpen = makeTouPlan(id: "pp-new", start: "2026-08-01", end: nil)
        apiClient.replaceOpenEndedResult = ReplaceOpenEndedResult(closing: closing, newPlan: newOpen)

        viewModel.beginCreate()
        viewModel.draft = makeDraft()
        apiClient.nextCreateError = .pricingValidation(.overlap(openEndedId: "pp-open"))
        try? await viewModel.save()
        #expect(viewModel.overlapRemediationTargetId == "pp-open")

        apiClient.nextCreateError = nil
        try await viewModel.remediateOverlap()

        let sentClosingId = try #require(apiClient.lastReplaceClosingId)
        #expect(sentClosingId == "pp-open")
        let sentDraft = try #require(apiClient.lastReplaceDraft)
        #expect(sentDraft.startDate == "2026-08-01")
        #expect(viewModel.overlapRemediationTargetId == nil)
        #expect(!viewModel.isEditorPresented)
        #expect(viewModel.plans.first(where: { $0.id == "pp-open" })?.endDate == "2026-08-01")
    }

    @Test
    func remediationCopyUsesSwitchDayPhrasing() {
        // AC 6.5: the predecessor now ends ON the switch date, not the day
        // before it, so the affordance must not say "the day before".
        let copy = PricingViewModel.remediationFooter(startDate: "2026-08-01")
        #expect(copy.contains("2026-08-01"))
        #expect(!copy.lowercased().contains("day before"))
    }

    @Test
    func remediationSurfacesLegacyShapeRejection() async throws {
        let (viewModel, apiClient) = makeViewModelAndAPI()
        let open = makePlan(id: "pp-open", start: "2026-01-01", end: nil)
        apiClient.plansToReturn = [open]
        await viewModel.refresh()

        viewModel.beginCreate()
        viewModel.draft = makeDraft()
        apiClient.nextCreateError = .pricingValidation(.overlap(openEndedId: "pp-open"))
        try? await viewModel.save()

        apiClient.nextReplaceError = .pricingValidation(.legacyShape)
        try? await viewModel.remediateOverlap()
        #expect(viewModel.lastValidationError == .legacyShape)
        #expect(viewModel.isEditorPresented, "a failed remediation keeps the editor open")
    }

    @Test
    func clearErrorDismissesBanner() {
        let (viewModel, _) = makeViewModelAndAPI()
        viewModel.beginCreate()
        viewModel.service.bind(apiClient: TestPricingAPIClient())
        viewModel.clearError()
        #expect(viewModel.lastValidationError == nil)
    }

    // MARK: - Helpers

    private func makeViewModelAndAPI() -> (PricingViewModel, TestPricingAPIClient) {
        let apiClient = TestPricingAPIClient()
        let service = PricingService()
        service.bind(apiClient: apiClient)
        return (PricingViewModel(service: service), apiClient)
    }

    private func makeDraft() -> PricingPlanDraft {
        PricingPlanDraft(
            startDate: "2026-08-01",
            endDate: nil,
            defaultRate: 0.35,
            windows: [
                PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
                PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28)
            ],
            feedInRate: 0.06,
            savingsReferenceRate: 0.35
        )
    }

    private func makePlan(
        id: String,
        start: String,
        end: String?,
        defaultRate: Double = 0.30
    ) -> PricingPlan {
        PricingPlan(
            id: id,
            startDate: start,
            endDate: end,
            defaultRate: defaultRate,
            windows: [PlanWindow(start: "11:00", end: "14:00", free: true, rate: nil)],
            feedInRate: 0.05,
            savingsReferenceRate: 0.12,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
        )
    }

    private func makeTouPlan(id: String, start: String, end: String?) -> PricingPlan {
        PricingPlan(
            id: id,
            startDate: start,
            endDate: end,
            defaultRate: 0.35,
            windows: [
                PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
                PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28)
            ],
            feedInRate: 0.05,
            savingsReferenceRate: 0.35,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
        )
    }
}

// MARK: - test double

@MainActor
final class TestPricingAPIClient: FluxAPIClient, @unchecked Sendable {
    var plansToReturn: [PricingPlan] = []
    var nextCreateError: FluxAPIError?
    var nextReplaceError: FluxAPIError?
    var replaceOpenEndedResult: ReplaceOpenEndedResult?
    private(set) var lastCreatedDraft: PricingPlanDraft?
    private(set) var lastReplaceClosingId: String?
    private(set) var lastReplaceDraft: PricingPlanDraft?

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

    func fetchPricing() async throws -> [PricingPlan] { plansToReturn }

    func createPricing(_ draft: PricingPlanDraft) async throws -> PricingPlan {
        lastCreatedDraft = draft
        if let err = nextCreateError {
            nextCreateError = nil
            throw err
        }
        let now = Date()
        let plan = PricingPlan(
            id: "new-\(UUID().uuidString)",
            startDate: draft.startDate,
            endDate: draft.endDate,
            defaultRate: draft.defaultRate,
            windows: draft.windows,
            feedInRate: draft.feedInRate,
            savingsReferenceRate: draft.savingsReferenceRate,
            createdAt: now, updatedAt: now
        )
        plansToReturn.append(plan)
        return plan
    }

    func updatePricing(id: String, _ draft: PricingPlanDraft) async throws -> PricingPlan {
        guard let idx = plansToReturn.firstIndex(where: { $0.id == id }) else {
            throw FluxAPIError.notFound
        }
        let now = Date()
        let plan = PricingPlan(
            id: id,
            startDate: draft.startDate,
            endDate: draft.endDate,
            defaultRate: draft.defaultRate,
            windows: draft.windows,
            feedInRate: draft.feedInRate,
            savingsReferenceRate: draft.savingsReferenceRate,
            createdAt: plansToReturn[idx].createdAt,
            updatedAt: now
        )
        plansToReturn[idx] = plan
        return plan
    }

    func deletePricing(id: String) async throws {
        plansToReturn.removeAll { $0.id == id }
    }

    func replaceOpenEndedPricing(
        closingId: String,
        with draft: PricingPlanDraft
    ) async throws -> ReplaceOpenEndedResult {
        lastReplaceClosingId = closingId
        lastReplaceDraft = draft
        if let err = nextReplaceError {
            nextReplaceError = nil
            throw err
        }
        guard let result = replaceOpenEndedResult else { throw FluxAPIError.serverError }
        return result
    }
}
// swiftlint:enable file_length type_body_length
