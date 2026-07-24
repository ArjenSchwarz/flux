import Foundation
import Testing
@testable import FluxCore

@Suite
struct PricingPlanTests {
    @Test
    func decodeFromBackendShape() throws {
        let jsonString = """
        {
          "id": "pp-1",
          "startDate": "2026-08-01",
          "endDate": "2026-12-01",
          "defaultRate": 0.35,
          "windows": [
            { "start": "10:00", "end": "15:00", "free": true },
            { "start": "01:00", "end": "06:00", "free": false, "rate": 0.28 }
          ],
          "feedInRate": 0.05,
          "savingsReferenceRate": 0.35,
          "createdAt": "2026-07-19T08:00:00Z",
          "updatedAt": "2026-07-19T08:00:00Z"
        }
        """
        let plan = try jsonDecoder().decode(PricingPlan.self, from: Data(jsonString.utf8))
        #expect(plan.id == "pp-1")
        #expect(plan.startDate == "2026-08-01")
        #expect(plan.endDate == "2026-12-01")
        #expect(plan.defaultRate == 0.35)
        #expect(plan.feedInRate == 0.05)
        #expect(plan.savingsReferenceRate == 0.35)
        #expect(plan.windows.count == 2)
        #expect(plan.windows[0] == PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil))
        #expect(plan.windows[1] == PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28))
    }

    @Test
    func decodeOpenEndedPlanWithoutEndDateOrSavingsRate() throws {
        let jsonString = """
        {
          "id": "pp-open",
          "startDate": "2026-07-01",
          "defaultRate": 0.30,
          "windows": [],
          "feedInRate": 0.06,
          "createdAt": "2026-07-01T00:00:00Z",
          "updatedAt": "2026-07-01T00:00:00Z"
        }
        """
        let plan = try jsonDecoder().decode(PricingPlan.self, from: Data(jsonString.utf8))
        #expect(plan.endDate == nil)
        #expect(plan.savingsReferenceRate == nil)
        #expect(plan.windows.isEmpty)
    }

    @Test
    func encodeRoundTripPreservesFields() throws {
        let plan = PricingPlan(
            id: "pp-1",
            startDate: "2026-08-01",
            endDate: nil,
            defaultRate: 0.3512,
            windows: [PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil)],
            feedInRate: 0.05,
            savingsReferenceRate: 0.35,
            createdAt: Date(timeIntervalSince1970: 1_715_000_000),
            updatedAt: Date(timeIntervalSince1970: 1_715_100_000)
        )
        let encoded = try jsonEncoder().encode(plan)
        let decoded = try jsonDecoder().decode(PricingPlan.self, from: encoded)
        #expect(decoded == plan)
    }

    // MARK: - covers(date:) — exclusive end (Decision 5)

    @Test
    func coversIsInclusiveOfStartAndExclusiveOfEnd() {
        let plan = makePlan(id: "p", startDate: "2026-08-01", endDate: "2026-09-01")
        #expect(plan.covers(date: "2026-07-31") == false)
        #expect(plan.covers(date: "2026-08-01"))
        #expect(plan.covers(date: "2026-08-31"))
        // The end date is the switch date and belongs to the successor.
        #expect(plan.covers(date: "2026-09-01") == false)
    }

    @Test
    func openEndedPlanCoversEveryDateFromItsStart() {
        let plan = makePlan(id: "p", startDate: "2026-08-01", endDate: nil)
        #expect(plan.covers(date: "2026-07-31") == false)
        #expect(plan.covers(date: "2026-08-01"))
        #expect(plan.covers(date: "2099-01-01"))
    }

    @Test
    func switchDayBelongsToTheSuccessorAndTheDayBeforeToThePredecessor() {
        let predecessor = makePlan(id: "old", startDate: "2026-01-01", endDate: "2026-08-01")
        let successor = makePlan(id: "new", startDate: "2026-08-01", endDate: nil)
        let plans = [predecessor, successor]

        #expect(PricingPlan.plan(for: "2026-07-31", in: plans)?.id == "old")
        #expect(PricingPlan.plan(for: "2026-08-01", in: plans)?.id == "new")
    }

    @Test
    func planForReturnsNilWhenNoPlanCoversTheDate() {
        let plans = [makePlan(id: "p", startDate: "2026-08-01", endDate: "2026-09-01")]
        #expect(PricingPlan.plan(for: "2026-09-02", in: plans) == nil)
    }

    // MARK: - Free window

    @Test
    func freeWindowIsTheFreeSegmentOfThePlan() {
        let plan = makePlan(
            id: "p",
            startDate: "2026-08-01",
            endDate: nil,
            windows: [
                PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil),
                PlanWindow(start: "01:00", end: "06:00", free: false, rate: 0.28)
            ]
        )
        #expect(plan.freeWindow?.start == "10:00")
        #expect(plan.freeWindow?.end == "15:00")
    }

    @Test
    func freeWindowIsNilWhenThePlanHasNoFreeBand() {
        let plan = makePlan(id: "p", startDate: "2026-08-01", endDate: nil, windows: [])
        #expect(plan.freeWindow == nil)
    }

    @Test
    func freeWindowForDateUsesThePlanPricingThatDate() {
        let predecessor = makePlan(
            id: "old",
            startDate: "2026-01-01",
            endDate: "2026-08-01",
            windows: [PlanWindow(start: "11:00", end: "14:00", free: true, rate: nil)]
        )
        let successor = makePlan(
            id: "new",
            startDate: "2026-08-01",
            endDate: nil,
            windows: [PlanWindow(start: "10:00", end: "15:00", free: true, rate: nil)]
        )
        let plans = [predecessor, successor]

        #expect(PricingPlan.freeWindow(for: "2026-07-31", in: plans)?.start == "11:00")
        #expect(PricingPlan.freeWindow(for: "2026-08-01", in: plans)?.start == "10:00")
        #expect(PricingPlan.freeWindow(for: "2025-01-01", in: plans) == nil)
    }

    // MARK: - Band time parsing (mirrors plan.ParseBandTime, Q34)

    @Test
    func bandTimeParsingAcceptsEndOfDayAndRejectsMalformedInput() {
        #expect(PlanWindow.parseBandTime("00:00") == 0)
        #expect(PlanWindow.parseBandTime("10:30") == 630)
        // 24:00 is a valid end-of-day boundary; ParseOffpeakWindow would reject it.
        #expect(PlanWindow.parseBandTime("24:00") == 1440)
        #expect(PlanWindow.parseBandTime("24:01") == nil)
        #expect(PlanWindow.parseBandTime("25:00") == nil)
        #expect(PlanWindow.parseBandTime("10:60") == nil)
        #expect(PlanWindow.parseBandTime("1:00") == nil)
        #expect(PlanWindow.parseBandTime("10-00") == nil)
        #expect(PlanWindow.parseBandTime("aa:bb") == nil)
    }

    // MARK: - Helpers

    private func makePlan(
        id: String,
        startDate: String,
        endDate: String?,
        windows: [PlanWindow] = []
    ) -> PricingPlan {
        PricingPlan(
            id: id,
            startDate: startDate,
            endDate: endDate,
            defaultRate: 0.35,
            windows: windows,
            feedInRate: 0.05,
            savingsReferenceRate: windows.contains(where: \.free) ? 0.35 : nil,
            createdAt: Date(timeIntervalSince1970: 0),
            updatedAt: Date(timeIntervalSince1970: 0)
        )
    }

    private func jsonEncoder() -> JSONEncoder {
        let enc = JSONEncoder()
        enc.dateEncodingStrategy = .iso8601
        return enc
    }

    private func jsonDecoder() -> JSONDecoder {
        let dec = JSONDecoder()
        dec.dateDecodingStrategy = .iso8601
        return dec
    }
}
