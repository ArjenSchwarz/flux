import Foundation
import Testing
@testable import FluxCore

@Suite
struct PricingPeriodTests {
    @Test
    func decodeFromBackendShape() throws {
        let jsonString = """
        {
          "id": "pp-1",
          "startDate": "2026-01-01",
          "endDate": "2026-06-30",
          "peakRate": 0.2873,
          "feedInRate": 0.0500,
          "offPeakSavingsRate": 0.1234,
          "createdAt": "2026-05-19T08:00:00Z",
          "updatedAt": "2026-05-19T08:00:00Z"
        }
        """
        let json = Data(jsonString.utf8)
        let period = try jsonDecoder().decode(PricingPeriod.self, from: json)
        #expect(period.id == "pp-1")
        #expect(period.startDate == "2026-01-01")
        #expect(period.endDate == "2026-06-30")
        #expect(period.peakRate == 0.2873)
        #expect(period.feedInRate == 0.05)
        #expect(period.offPeakSavingsRate == 0.1234)
    }

    @Test
    func decodeOpenEndedPeriodWithoutEndDate() throws {
        let jsonString = """
        {
          "id": "pp-open",
          "startDate": "2026-07-01",
          "peakRate": 0.30,
          "feedInRate": 0.06,
          "offPeakSavingsRate": 0.12,
          "createdAt": "2026-07-01T00:00:00Z",
          "updatedAt": "2026-07-01T00:00:00Z"
        }
        """
        let json = Data(jsonString.utf8)
        let period = try jsonDecoder().decode(PricingPeriod.self, from: json)
        #expect(period.endDate == nil)
    }

    @Test
    func encodeRoundTripPreservesFields() throws {
        let period = PricingPeriod(
            id: "pp-1",
            startDate: "2026-01-01",
            endDate: "2026-06-30",
            peakRate: 0.2873,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12,
            createdAt: Date(timeIntervalSince1970: 1_715_000_000),
            updatedAt: Date(timeIntervalSince1970: 1_715_100_000)
        )
        let data = try jsonEncoder().encode(period)
        let back = try jsonDecoder().decode(PricingPeriod.self, from: data)
        #expect(back == period)
    }

    @Test
    func encodeOpenEndedOmitsEndDate() throws {
        let period = PricingPeriod(
            id: "pp-open",
            startDate: "2026-07-01",
            endDate: nil,
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
        )
        let data = try jsonEncoder().encode(period)
        let dict = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(dict["endDate"] == nil)
    }

    @Test
    func draftEncodesAllFields() throws {
        let draft = PricingPeriodDraft(
            startDate: "2026-01-01",
            endDate: "2026-06-30",
            peakRate: 0.2873,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12
        )
        let data = try jsonEncoder().encode(draft)
        let dict = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(dict["startDate"] as? String == "2026-01-01")
        #expect(dict["endDate"] as? String == "2026-06-30")
        #expect((dict["peakRate"] as? NSNumber)?.doubleValue == 0.2873)
    }

    @Test
    func iso8601DateStringsSortLexicographicallyAsExpected() {
        // YYYY-MM-DD strings compare correctly chronologically with string compare.
        let earlier = "2026-01-15"
        let later = "2026-02-01"
        #expect(earlier < later)
        #expect("2026-12-31" < "2027-01-01")
        #expect("2026-01-01" < "2026-01-02")
    }

    @Test
    func coversReturnsTrueWithinRange() {
        let period = makePeriod(start: "2026-01-01", end: "2026-06-30")
        #expect(period.covers(date: "2026-01-01"))
        #expect(period.covers(date: "2026-04-15"))
        #expect(period.covers(date: "2026-06-30"))
    }

    @Test
    func coversReturnsFalseOutsideRange() {
        let period = makePeriod(start: "2026-01-01", end: "2026-06-30")
        #expect(!period.covers(date: "2025-12-31"))
        #expect(!period.covers(date: "2026-07-01"))
    }

    @Test
    func coversOpenEndedExtendsForever() {
        let period = makePeriod(start: "2026-07-01", end: nil)
        #expect(period.covers(date: "2026-07-01"))
        #expect(period.covers(date: "2030-01-01"))
        #expect(period.covers(date: "9999-12-31"))
        #expect(!period.covers(date: "2026-06-30"))
    }

    @Test
    func equalityCoversAllFields() {
        let alpha = makePeriod(start: "2026-01-01", end: "2026-06-30")
        let beta = makePeriod(start: "2026-01-01", end: "2026-06-30")
        #expect(alpha == beta)
        let gamma = makePeriod(start: "2026-01-02", end: "2026-06-30")
        #expect(alpha != gamma)
    }

    @Test
    func hashableAcrossSet() {
        let alpha = makePeriod(start: "2026-01-01", end: "2026-06-30")
        let beta = alpha
        let gamma = makePeriod(start: "2026-02-01", end: nil)
        var set: Set<PricingPeriod> = []
        set.insert(alpha)
        set.insert(beta)
        set.insert(gamma)
        #expect(set.count == 2)
    }

    // MARK: - helpers

    private func makePeriod(
        start: String,
        end: String?,
        id: String = "pp",
        peak: Double = 0.30,
        feedIn: Double = 0.05,
        offPeak: Double = 0.12
    ) -> PricingPeriod {
        PricingPeriod(
            id: id,
            startDate: start,
            endDate: end,
            peakRate: peak,
            feedInRate: feedIn,
            offPeakSavingsRate: offPeak,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
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
