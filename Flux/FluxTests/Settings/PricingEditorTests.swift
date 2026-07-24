import FluxCore
import Foundation
import Testing
@testable import Flux

/// Lightweight view-state tests for PricingEditor. Asserts on the editor's
/// static helpers and view-model interactions, in lieu of the snapshot
/// framework (not wired up in this project).
@MainActor @Suite
struct PricingEditorTests {
    @Test
    func localValidationMessagesNameTheOffendingField() throws {
        #expect(PricingEditor.localValidationMessage(for: .invertedDates).contains("end date"))
        #expect(PricingEditor.localValidationMessage(for: .ratePrecision).contains("four decimal"))
        #expect(PricingEditor.localValidationMessage(for: .rateOutOfRange).contains("$0.00"))
        #expect(PricingEditor.localValidationMessage(for: .invalidStartDate).contains("YYYY-MM-DD"))
    }

    @Test
    func everyLocalValidationErrorHasAMessage() {
        let errors: [PricingPlanDraft.ValidationError] = [
            .invalidStartDate, .invalidEndDate, .invertedDates,
            .bandWindowInvalid, .bandOverlap, .multipleFreeBands,
            .noRatedBand, .savingsRateMissing, .rateOutOfRange, .ratePrecision
        ]
        for error in errors {
            #expect(!PricingEditor.localValidationMessage(for: error).isEmpty, "\(error)")
        }
    }

    @Test
    func bandValidationMessagesDescribeTheBandRule() {
        #expect(PricingEditor.localValidationMessage(for: .bandOverlap).lowercased().contains("overlap"))
        #expect(PricingEditor.localValidationMessage(for: .multipleFreeBands).lowercased().contains("free"))
        #expect(PricingEditor.localValidationMessage(for: .savingsRateMissing).lowercased().contains("savings"))
    }

    @Test
    func dateRoundTripFormatsAsISO() throws {
        let date = PricingEditor.parseDate("2026-04-15")
        let unwrapped = try #require(date)
        #expect(PricingEditor.formatDate(unwrapped) == "2026-04-15")
    }

    @Test
    func parseRejectsEmptyString() throws {
        #expect(PricingEditor.parseDate("") == nil)
    }

    // MARK: - Band time pickers

    @Test
    func bandTimeRoundTripsThroughTheTimePicker() throws {
        let date = try #require(PricingEditor.parseBandTime("10:30"))
        #expect(PricingEditor.formatBandTime(date) == "10:30")
    }

    @Test
    func endOfDayRoundTripsAsTwentyFourHundred() throws {
        // 24:00 has no clock representation, so the picker holds it as 23:59
        // and the formatter maps that one minute back to end-of-day. Without
        // this a plan whose last window ends at midnight could not be edited.
        let date = try #require(PricingEditor.parseBandTime("24:00"))
        #expect(PricingEditor.formatBandTime(date) == "24:00")
    }

    @Test
    func parseBandTimeRejectsMalformedInput() {
        #expect(PricingEditor.parseBandTime("nonsense") == nil)
        #expect(PricingEditor.parseBandTime("") == nil)
    }

    // MARK: - Draft validation through the editor

    @Test
    func draftValidatesOnTypicalCreateInputs() throws {
        let draft = PricingPlanDraft(
            startDate: "2026-08-01",
            endDate: nil,
            defaultRate: 0.2873,
            windows: [PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil)],
            feedInRate: 0.05,
            savingsReferenceRate: 0.12
        )
        #expect(draft.validate() == nil)
    }

    @Test
    func draftRejectsRatePrecisionGreaterThan4DP() throws {
        var draft = makeDraft()
        draft.defaultRate = 0.123456
        #expect(draft.validate() == .ratePrecision)
    }

    @Test
    func draftRejectsRateOutOfRange() throws {
        var draft = makeDraft()
        draft.defaultRate = 11.0
        #expect(draft.validate() == .rateOutOfRange)
    }

    @Test
    func remediationButtonAppearsAfterOverlapWithOpenEndedId() async throws {
        let apiClient = TestPricingAPIClient()
        apiClient.nextCreateError = .pricingValidation(.overlap(openEndedId: "pp-open"))
        let service = PricingService()
        service.bind(apiClient: apiClient)
        let viewModel = PricingViewModel(service: service)
        viewModel.beginCreate()
        viewModel.draft = makeDraft()
        try? await viewModel.save()
        #expect(viewModel.overlapRemediationTargetId == "pp-open")
        #expect(viewModel.lastValidationError == .overlap(openEndedId: "pp-open"))
    }

    private func makeDraft() -> PricingPlanDraft {
        PricingPlanDraft(
            startDate: "2026-08-01",
            endDate: nil,
            defaultRate: 0.30,
            windows: [PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil)],
            feedInRate: 0.06,
            savingsReferenceRate: 0.12
        )
    }
}
