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
        #expect(PricingEditor.localValidationMessage(for: .invertedDates).contains("End date"))
        #expect(PricingEditor.localValidationMessage(for: .ratePrecision).contains("four decimal"))
        #expect(PricingEditor.localValidationMessage(for: .rateOutOfRange).contains("$0.00"))
        #expect(PricingEditor.localValidationMessage(for: .invalidStartDate).contains("YYYY-MM-DD"))
    }

    @Test
    func dateRoundTripFormatsAsISO() throws {
        let date = PricingEditor.parseDate("2026-04-15")
        let unwrapped = try #require(date)
        let formatted = PricingEditor.formatDate(unwrapped)
        #expect(formatted == "2026-04-15")
    }

    @Test
    func parseRejectsEmptyString() throws {
        #expect(PricingEditor.parseDate("") == nil)
    }

    // MARK: - Editor mode via ViewModel

    @Test
    func draftValidatesOnTypicalCreateInputs() throws {
        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.2873,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12
        )
        #expect(draft.validate() == nil)
    }

    @Test
    func draftRejectsRatePrecisionGreaterThan4DP() throws {
        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.123456,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12
        )
        #expect(draft.validate() == .ratePrecision)
    }

    @Test
    func draftRejectsRateOutOfRange() throws {
        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 11.0,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12
        )
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
        viewModel.draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        try? await viewModel.save()
        #expect(viewModel.overlapRemediationTargetId == "pp-open")
        #expect(viewModel.lastValidationError == .overlap(openEndedId: "pp-open"))
    }
}
