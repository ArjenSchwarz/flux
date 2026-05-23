import Foundation

/// Server-assigned pricing period. The id and timestamps are populated by the
/// backend; the client treats them as opaque. Dates are kept as YYYY-MM-DD
/// strings to match `DayEnergy.date` and so day-membership tests can use
/// lexicographic string comparison (Decision 22).
public struct PricingPeriod: Identifiable, Codable, Sendable, Equatable, Hashable {
    public let id: String
    public let startDate: String
    public let endDate: String?
    public let peakRate: Double
    public let feedInRate: Double
    public let offPeakSavingsRate: Double
    public let createdAt: Date
    public let updatedAt: Date

    public init(
        id: String,
        startDate: String,
        endDate: String?,
        peakRate: Double,
        feedInRate: Double,
        offPeakSavingsRate: Double,
        createdAt: Date,
        updatedAt: Date
    ) {
        self.id = id
        self.startDate = startDate
        self.endDate = endDate
        self.peakRate = peakRate
        self.feedInRate = feedInRate
        self.offPeakSavingsRate = offPeakSavingsRate
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    /// Returns true iff `date` (YYYY-MM-DD) falls within `[startDate, endDate]`,
    /// inclusive on both ends. An absent endDate means open-ended.
    public func covers(date: String) -> Bool {
        guard date >= startDate else { return false }
        if let endDate {
            return date <= endDate
        }
        return true
    }
}

/// Server response from POST /pricing/replace-open-ended: the closing row
/// (with a freshly-assigned end date) and the new open-ended row.
public struct ReplaceOpenEndedResult: Sendable, Equatable {
    public let closing: PricingPeriod
    public let newPeriod: PricingPeriod

    public init(closing: PricingPeriod, newPeriod: PricingPeriod) {
        self.closing = closing
        self.newPeriod = newPeriod
    }
}
