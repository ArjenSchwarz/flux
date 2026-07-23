import Foundation

/// Aggregated cost totals across a History range. Built by summing the
/// per-day `DayCosts` of every priced day in the range (Requirement 5).
public struct PeriodCosts: Equatable, Sendable {
    public let peakImportsCost: Double
    public let solarFeedInIncome: Double
    public let net: Double
    public let offPeakSavings: Double
    public let pricedDayCount: Int
    public let totalDayCount: Int

    public init(
        peakImportsCost: Double,
        solarFeedInIncome: Double,
        net: Double,
        offPeakSavings: Double,
        pricedDayCount: Int,
        totalDayCount: Int
    ) {
        self.peakImportsCost = peakImportsCost
        self.solarFeedInIncome = solarFeedInIncome
        self.net = net
        self.offPeakSavings = offPeakSavings
        self.pricedDayCount = pricedDayCount
        self.totalDayCount = totalDayCount
    }

    public var hasPartialCoverage: Bool { pricedDayCount < totalDayCount }
}

public extension PeriodCosts {
    /// Sums costs across `days`. Returns `nil` iff no day in `days` is a
    /// priced day (AC 2.7). The `totalDayCount` is the full range (including
    /// unpriced days) so the caller can render the `N of M days priced`
    /// caption from AC 3.7.
    ///
    /// Each day resolves its own tier under the plan pricing it, so a range
    /// spanning a switch date sums days priced by different plans.
    static func compute(days: [DayEnergy], pricing: [PricingPlan]) -> PeriodCosts? {
        guard !days.isEmpty else { return nil }

        var peak: Double = 0
        var feedIn: Double = 0
        var savings: Double = 0
        var net: Double = 0
        var pricedDays = 0

        for day in days {
            guard let cost = day.costs(in: pricing) else { continue }
            peak += cost.peakImportsCost
            feedIn += cost.solarFeedInIncome
            savings += cost.offPeakSavings
            net += cost.net
            pricedDays += 1
        }

        guard pricedDays > 0 else { return nil }

        return PeriodCosts(
            peakImportsCost: peak,
            solarFeedInIncome: feedIn,
            net: net,
            offPeakSavings: savings,
            pricedDayCount: pricedDays,
            totalDayCount: days.count
        )
    }
}
