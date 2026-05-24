import Foundation

/// Per-day cost breakdown rendered on the Day Detail costs card. Computed on
/// read from the stored kWh values and the current pricing list — there is
/// no persisted snapshot (Decision 8).
public struct DayCosts: Equatable, Sendable {
    public let peakImportsCost: Double
    public let solarFeedInIncome: Double
    public let net: Double
    public let offPeakSavings: Double

    public init(
        peakImportsCost: Double,
        solarFeedInIncome: Double,
        net: Double,
        offPeakSavings: Double
    ) {
        self.peakImportsCost = peakImportsCost
        self.solarFeedInIncome = solarFeedInIncome
        self.net = net
        self.offPeakSavings = offPeakSavings
    }
}

public extension DaySummary {
    /// Returns the cost breakdown for `date` if a pricing period covers it.
    /// Returns `nil` when no period covers `date` (AC 4.6).
    ///
    /// Zero kWh fields produce zero cost lines and do NOT make the day
    /// unpriced (Decision 18). When `offpeakGridImportKwh` is `nil`, all of
    /// `eInput` is billed as peak (Decision 23) and off-peak savings is `0`.
    func costs(forDate date: String, in pricing: [PricingPeriod]) -> DayCosts? {
        guard let period = pricing.first(where: { $0.covers(date: date) }) else {
            return nil
        }

        let solarKwh = eOutput ?? 0
        let peakKwh: Double
        let offPeakKwh: Double
        if let off = offpeakGridImportKwh {
            offPeakKwh = off
            peakKwh = max(0, (eInput ?? 0) - off)
        } else {
            offPeakKwh = 0
            peakKwh = eInput ?? 0
        }

        let peakCost = peakKwh * period.peakRate
        let feedIn = solarKwh * period.feedInRate
        let savings = offPeakKwh * period.offPeakSavingsRate
        return DayCosts(
            peakImportsCost: peakCost,
            solarFeedInIncome: feedIn,
            net: peakCost - feedIn,
            offPeakSavings: savings
        )
    }
}

public extension DayEnergy {
    /// Convenience for History per-day costing. Forwards to the
    /// `DaySummary` extension using `self.date` and a transient `DaySummary`
    /// built from the day's fields.
    func costs(in pricing: [PricingPeriod]) -> DayCosts? {
        let summary = DaySummary(
            epv: epv,
            eInput: eInput,
            eOutput: eOutput,
            eCharge: eCharge,
            eDischarge: eDischarge,
            socLow: socLow,
            socLowTime: socLowTime,
            offpeakGridImportKwh: offpeakGridImportKwh,
            offpeakGridExportKwh: offpeakGridExportKwh
        )
        return summary.costs(forDate: date, in: pricing)
    }
}
