import Foundation

/// The editable shape used by the pricing editor. The backend assigns id /
/// createdAt / updatedAt on POST, so the draft only carries the writable
/// fields. Server-side validation is authoritative; local validation is for
/// early editor feedback.
public struct PricingPeriodDraft: Codable, Sendable, Equatable {
    public var startDate: String
    public var endDate: String?
    public var peakRate: Double
    public var feedInRate: Double
    public var offPeakSavingsRate: Double

    public init(
        startDate: String = "",
        endDate: String? = nil,
        peakRate: Double = 0,
        feedInRate: Double = 0,
        offPeakSavingsRate: Double = 0
    ) {
        self.startDate = startDate
        self.endDate = endDate
        self.peakRate = peakRate
        self.feedInRate = feedInRate
        self.offPeakSavingsRate = offPeakSavingsRate
    }

    public init(period: PricingPeriod) {
        self.startDate = period.startDate
        self.endDate = period.endDate
        self.peakRate = period.peakRate
        self.feedInRate = period.feedInRate
        self.offPeakSavingsRate = period.offPeakSavingsRate
    }

    /// Reasons a draft can fail validation. Mirrors the backend error codes
    /// (`inverted_dates`, `rate_out_of_range`, `rate_precision`). Overlap
    /// detection runs server-side only.
    public enum ValidationError: Error, Equatable, Sendable {
        case invalidStartDate
        case invertedDates
        case rateOutOfRange
        case ratePrecision
    }

    /// Local pre-flight validation. The server re-validates per Requirement 1
    /// — this is purely for early editor feedback. Returns `nil` when valid.
    public func validate() -> ValidationError? {
        if !Self.isValidDate(startDate) {
            return .invalidStartDate
        }
        if let endDate {
            if !Self.isValidDate(endDate) {
                return .invertedDates
            }
            if endDate < startDate {
                return .invertedDates
            }
        }
        for rate in [peakRate, feedInRate, offPeakSavingsRate] {
            if rate < 0 || rate > 10.0 {
                return .rateOutOfRange
            }
            if !Self.fitsFourDecimalPlaces(rate) {
                return .ratePrecision
            }
        }
        return nil
    }

    /// Returns the rate rounded to exactly four decimal places. The backend
    /// stores rates at 4dp; this helper keeps the wire payload consistent.
    public static func roundedToFourDP(_ rate: Double) -> Double {
        (rate * 10_000).rounded() / 10_000
    }

    private static func isValidDate(_ value: String) -> Bool {
        // YYYY-MM-DD — strictly 10 characters with hyphen positions at 5 and 8.
        guard value.count == 10 else { return false }
        let chars = Array(value)
        guard chars[4] == "-", chars[7] == "-" else { return false }
        let yearString = String(chars[0..<4])
        let monthString = String(chars[5..<7])
        let dayString = String(chars[8..<10])
        guard let year = Int(yearString),
              let month = Int(monthString),
              let day = Int(dayString) else { return false }
        guard year >= 1970, year <= 9999 else { return false }
        guard (1...12).contains(month) else { return false }
        guard (1...31).contains(day) else { return false }
        return true
    }

    private static func fitsFourDecimalPlaces(_ rate: Double) -> Bool {
        // A rate fits 4dp if rate * 10_000 is (numerically) very close to an
        // integer. Float64 noise at 4dp is well below 1e-6, so this is safe.
        let scaled = rate * 10_000
        let rounded = scaled.rounded()
        return abs(scaled - rounded) < 1e-6
    }
}
