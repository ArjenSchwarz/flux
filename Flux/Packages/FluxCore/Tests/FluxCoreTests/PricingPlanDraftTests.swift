import Foundation
import Testing
@testable import FluxCore

/// Client-side validation must mirror the server rules (AC 6.4), so every
/// case here has a counterpart in `internal/plan`'s Validate table.
@Suite
struct PricingPlanDraftTests {
    @Test
    func validDraftPassesValidation() {
        #expect(makeDraft().validate() == nil)
    }

    @Test
    func planWithoutWindowsIsValid() {
        var draft = makeDraft()
        draft.windows = []
        draft.savingsReferenceRate = nil
        #expect(draft.validate() == nil)
    }

    @Test
    func malformedDatesAreRejected() {
        var draft = makeDraft()
        draft.startDate = "not-a-date"
        #expect(draft.validate() == .invalidStartDate)

        draft = makeDraft()
        draft.endDate = "2026-02-30"
        #expect(draft.validate() == .invalidEndDate)
    }

    @Test
    func endDateMustBeStrictlyAfterStartDate() {
        var draft = makeDraft()
        draft.endDate = "2026-07-31"
        #expect(draft.validate() == .invertedDates)

        // Exclusive ends make endDate == startDate a plan that prices no days.
        draft = makeDraft()
        draft.endDate = draft.startDate
        #expect(draft.validate() == .invertedDates)
    }

    @Test
    func openEndedDraftSkipsTheEndDateRules() {
        var draft = makeDraft()
        draft.endDate = nil
        #expect(draft.validate() == nil)
    }

    @Test
    func bandBoundariesMustBeParseableAndOrdered() {
        var draft = makeDraft()
        draft.windows = [PlanWindow(start: "25:00", end: "26:00", free: false, rate: 0.2)]
        #expect(draft.validate() == .bandWindowInvalid)

        draft.windows = [PlanWindow(start: "10:00", end: "9:00", free: false, rate: 0.2)]
        #expect(draft.validate() == .bandWindowInvalid)

        draft.windows = [PlanWindow(start: "15:00", end: "15:00", free: false, rate: 0.2)]
        #expect(draft.validate() == .bandWindowInvalid)
    }

    @Test
    func endOfDayBoundaryIsAccepted() {
        var draft = makeDraft()
        draft.windows = [PlanWindow(start: "18:00", end: "24:00", free: false, rate: 0.2)]
        draft.savingsReferenceRate = nil
        #expect(draft.validate() == nil)
    }

    @Test
    func overlappingWindowsAreRejected() {
        var draft = makeDraft()
        draft.windows = [
            PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
            PlanWindow(start: "14:00", end: "18:00", free: false, rate: 0.2)
        ]
        #expect(draft.validate() == .bandOverlap)
    }

    @Test
    func abuttingWindowsDoNotCountAsOverlap() {
        var draft = makeDraft()
        draft.windows = [
            PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
            PlanWindow(start: "15:00", end: "18:00", free: false, rate: 0.2)
        ]
        #expect(draft.validate() == nil)
    }

    @Test
    func atMostOneFreeWindowIsAllowed() {
        var draft = makeDraft()
        draft.windows = [
            PlanWindow(start: "10:00", end: "12:00", free: true, rate: nil),
            PlanWindow(start: "13:00", end: "15:00", free: true, rate: nil)
        ]
        #expect(draft.validate() == .multipleFreeBands)
    }

    @Test
    func aFreeWindowSpanningTheWholeDayLeavesNoRatedBand() {
        var draft = makeDraft()
        draft.windows = [PlanWindow(start: "00:00", end: "24:00", free: true, rate: nil)]
        #expect(draft.validate() == .noRatedBand)
    }

    @Test
    func ratedWindowsTilingTheDayAreValidDespiteZeroWidthRemainder() {
        var draft = makeDraft()
        draft.windows = [
            PlanWindow(start: "00:00", end: "10:00", free: false, rate: 0.28),
            PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
            PlanWindow(start: "15:00", end: "24:00", free: false, rate: 0.30)
        ]
        #expect(draft.validate() == nil)
    }

    @Test
    func aFreeWindowRequiresASavingsReferenceRate() {
        var draft = makeDraft()
        draft.savingsReferenceRate = nil
        #expect(draft.validate() == .savingsRateMissing)
    }

    @Test
    func ratesMustBeInRange() {
        var draft = makeDraft()
        draft.defaultRate = 10.5
        #expect(draft.validate() == .rateOutOfRange)

        draft = makeDraft()
        draft.feedInRate = -0.1
        #expect(draft.validate() == .rateOutOfRange)

        draft = makeDraft()
        draft.savingsReferenceRate = 11
        #expect(draft.validate() == .rateOutOfRange)

        draft = makeDraft()
        draft.windows = [PlanWindow(start: "01:00", end: "06:00", free: false, rate: 20)]
        draft.savingsReferenceRate = nil
        #expect(draft.validate() == .rateOutOfRange)
    }

    @Test
    func ratesMustFitFourDecimalPlaces() {
        var draft = makeDraft()
        draft.defaultRate = 0.12345
        #expect(draft.validate() == .ratePrecision)

        draft = makeDraft()
        draft.windows = [PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.123456)]
        draft.savingsReferenceRate = nil
        #expect(draft.validate() == .ratePrecision)
    }

    @Test
    func aFreeWindowsRateIsIgnoredByTheRateRules() {
        var draft = makeDraft()
        // A free window carries no rate by contract, so a stale value on it
        // must not fail validation.
        draft.windows = [PlanWindow(start: "10:00", end: "15:00", free: true, rate: 99.12345)]
        #expect(draft.validate() == nil)
    }

    @Test
    func roundingNormalisesToFourDecimalPlaces() {
        #expect(PricingPlanDraft.roundedToFourDP(0.123456) == 0.1235)
        #expect(PricingPlanDraft.roundedToFourDP(0.35) == 0.35)
    }

    @Test
    func draftFromPlanRoundTripsEveryWritableField() {
        let plan = PricingPlan(
            id: "pp-1",
            startDate: "2026-08-01",
            endDate: "2026-12-01",
            defaultRate: 0.35,
            windows: [
                PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
                PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28)
            ],
            feedInRate: 0.05,
            savingsReferenceRate: 0.35,
            createdAt: Date(timeIntervalSince1970: 0),
            updatedAt: Date(timeIntervalSince1970: 0)
        )
        let draft = PricingPlanDraft(plan: plan)
        #expect(draft.startDate == plan.startDate)
        #expect(draft.endDate == plan.endDate)
        #expect(draft.defaultRate == plan.defaultRate)
        #expect(draft.windows == plan.windows)
        #expect(draft.feedInRate == plan.feedInRate)
        #expect(draft.savingsReferenceRate == plan.savingsReferenceRate)
    }

    @Test
    func draftEncodesTheBandWireShape() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        let data = try encoder.encode(makeDraft())
        let json = String(decoding: data, as: UTF8.self)
        #expect(json.contains("\"defaultRate\":0.35"))
        #expect(json.contains("\"windows\""))
        #expect(json.contains("\"savingsReferenceRate\":0.35"))
        // The legacy three-rate fields must not appear — the server rejects
        // that shape with `legacy_shape` (AC 7.3).
        #expect(!json.contains("peakRate"))
        #expect(!json.contains("offPeakSavingsRate"))
    }

    private func makeDraft() -> PricingPlanDraft {
        PricingPlanDraft(
            startDate: "2026-08-01",
            endDate: "2026-12-01",
            defaultRate: 0.35,
            windows: [PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil)],
            feedInRate: 0.05,
            savingsReferenceRate: 0.35
        )
    }
}
