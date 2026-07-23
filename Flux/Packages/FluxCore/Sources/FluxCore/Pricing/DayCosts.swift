import Foundation

/// One rated band's stored grid import for a day. Each entry snapshots the
/// geometry it was captured under (Q23), so a later plan-window edit is
/// detectable as a mismatch rather than silently mispricing the day.
///
/// Only rated bands appear: the free band's import lives on the off-peak row,
/// which owns that quantity exclusively (Q31). Mirrors `plan.BandImport`.
public struct BandImport: Codable, Sendable, Equatable, Hashable {
    public let start: String
    public let end: String
    public let kwh: Double

    public init(start: String, end: String, kwh: Double) {
        self.start = start
        self.end = end
        self.kwh = kwh
    }
}

/// The off-peak row's contribution to costing — the only source of the free
/// band's kWh. Mirrors `plan.OffpeakRow`.
public struct OffpeakImport: Sendable, Equatable {
    /// The window every pre-feature off-peak row was integrated under. Rows
    /// predating the geometry snapshot carry no window, and this is the only
    /// window they can have had.
    public static let legacyWindowStart = "11:00"
    public static let legacyWindowEnd = "14:00"

    public let gridImportKwh: Double
    /// The geometry the row was integrated under; `nil` on a pre-feature row.
    public let windowStart: String?
    public let windowEnd: String?
    /// Integration provenance. A row integrated from readings but with no
    /// samples is a zero-delta artifact, not a measured zero.
    public let integratedAt: String?
    public let sampleCount: Int

    public init(
        gridImportKwh: Double,
        windowStart: String? = nil,
        windowEnd: String? = nil,
        integratedAt: String? = nil,
        sampleCount: Int = 0
    ) {
        self.gridImportKwh = gridImportKwh
        self.windowStart = windowStart
        self.windowEnd = windowEnd
        self.integratedAt = integratedAt
        self.sampleCount = sampleCount
    }

    /// Whether the row's import is a real measurement. Rows predating the
    /// integration path (no `integratedAt`) are snapshot deltas and stay usable.
    public var isUsable: Bool {
        integratedAt == nil || sampleCount > 0
    }

    /// The window the row was integrated under, substituting the pre-feature
    /// window when the row carries no snapshot.
    public var geometry: (start: String, end: String) {
        guard let windowStart, let windowEnd, !windowStart.isEmpty, !windowEnd.isEmpty else {
            return (Self.legacyWindowStart, Self.legacyWindowEnd)
        }
        return (windowStart, windowEnd)
    }
}

/// One day's stored energy, as cost resolution sees it. Optionals distinguish
/// "never recorded" from a measured zero — the distinction the single-rate
/// formula turns on. Mirrors `plan.DayEnergy`.
public struct DayCostInputs: Sendable, Equatable {
    public let eInput: Double?
    public let eOutput: Double?
    public let peakGridImportKwh: Double?
    public let offpeak: OffpeakImport?
    public let bandImports: [BandImport]?

    public init(
        eInput: Double?,
        eOutput: Double?,
        peakGridImportKwh: Double?,
        offpeak: OffpeakImport?,
        bandImports: [BandImport]?
    ) {
        self.eInput = eInput
        self.eOutput = eOutput
        self.peakGridImportKwh = peakGridImportKwh
        self.offpeak = offpeak
        self.bandImports = bandImports
    }
}

/// Which resolution path produced a `DayCosts`. Exposed so tests can assert on
/// the path, not just the number. Mirrors `plan.Tier`.
public enum CostTier: Int, Sendable, Equatable {
    /// Each rated band priced at its own rate from the stored split.
    case banded = 1
    /// The pre-band formula, applicable whenever the plan's rated segments
    /// share one rate — which every migrated legacy plan does.
    case singleRate = 2
    /// All import at the plan's highest rate with no savings (AC 3.6).
    /// Reachable only for multi-rate plans.
    case fallback = 3
}

/// Per-day cost breakdown rendered on the Day Detail costs card. Computed on
/// read from the stored kWh values and the current plan list — there is no
/// persisted snapshot (Decision 8).
public struct DayCosts: Equatable, Sendable {
    public let peakImportsCost: Double
    public let solarFeedInIncome: Double
    public let net: Double
    public let offPeakSavings: Double
    public let tier: CostTier

    public init(
        peakImportsCost: Double,
        solarFeedInIncome: Double,
        net: Double,
        offPeakSavings: Double,
        tier: CostTier = .singleRate
    ) {
        self.peakImportsCost = peakImportsCost
        self.solarFeedInIncome = solarFeedInIncome
        self.net = net
        self.offPeakSavings = offPeakSavings
        self.tier = tier
    }
}

public extension DayCosts {
    /// Resolves one day's costs under the plan pricing that day. Resolution
    /// order is banded → single-rate → fallback (Decision 6); the Go side runs
    /// the identical order in `plan.DayCosts`, pinned by the shared vectors in
    /// `internal/api/testdata/pricing_costs.json`.
    static func resolve(plan: PricingPlan, day: DayCostInputs) -> DayCosts {
        let feedIn = (day.eOutput ?? 0) * plan.feedInRate
        func finish(importCost: Double, savings: Double, tier: CostTier) -> DayCosts {
            DayCosts(
                peakImportsCost: importCost,
                solarFeedInIncome: feedIn,
                net: importCost - feedIn,
                offPeakSavings: savings,
                tier: tier
            )
        }

        let rated = plan.ratedSegments

        if let banded = bandedCosts(plan: plan, rated: rated, day: day) {
            return finish(importCost: banded.importCost, savings: banded.savings, tier: .banded)
        }
        if let rate = singleRate(of: rated) {
            let resolved = singleRateCosts(plan: plan, day: day, rate: rate)
            return finish(importCost: resolved.importCost, savings: resolved.savings, tier: .singleRate)
        }
        // AC 3.6: an unresolvable split prices everything at the highest rate
        // and shows no savings — the conservative overestimate every screen
        // must agree on.
        let highest = rated.map(\.rate).max() ?? 0
        return finish(importCost: (day.eInput ?? 0) * highest, savings: 0, tier: .fallback)
    }

    /// Prices the day from the stored split. Applies only when the split's
    /// geometry exactly matches the plan's rated segments AND the free band's
    /// import is resolvable — a partially known split is unavailable (AC 3.6),
    /// not partially used.
    private static func bandedCosts(
        plan: PricingPlan,
        rated: [PlanSegment],
        day: DayCostInputs
    ) -> (importCost: Double, savings: Double)? {
        guard let bands = day.bandImports, !rated.isEmpty, bands.count == rated.count else { return nil }

        var importCost = 0.0
        for (index, segment) in rated.enumerated() {
            guard bands[index].start == segment.start, bands[index].end == segment.end else { return nil }
            importCost += bands[index].kwh * segment.rate
        }

        guard let free = plan.freeWindow else {
            // No free band: the rated segments are the whole day and there is
            // nothing to value as savings.
            return (importCost, 0)
        }
        guard let offpeak = day.offpeak, offpeak.isUsable else { return nil }
        let geometry = offpeak.geometry
        guard geometry.start == free.start, geometry.end == free.end else { return nil }
        guard let savingsRate = plan.savingsReferenceRate else { return (importCost, 0) }
        return (importCost, offpeak.gridImportKwh * savingsRate)
    }

    /// The pre-band formula, unchanged. Peak kWh prefers the server-computed
    /// value over the `eInput − off-peak` residual: the two differ by ~1.5% by
    /// design (a shared sampling artifact), and pricing the measured value is
    /// what keeps migrated history identical (Q30).
    private static func singleRateCosts(
        plan: PricingPlan,
        day: DayCostInputs,
        rate: Double
    ) -> (importCost: Double, savings: Double) {
        let total = day.eInput ?? 0
        guard let offpeak = day.offpeak else {
            return ((day.peakGridImportKwh ?? total) * rate, 0)
        }

        let off = offpeak.gridImportKwh
        let peak = day.peakGridImportKwh ?? max(0, total - off)
        let savings = plan.savingsReferenceRate.map { off * $0 } ?? 0
        return (peak * rate, savings)
    }

    /// The rate shared by every rated segment, or `nil` when they carry more
    /// than one — the only case that can reach the fallback tier.
    private static func singleRate(of rated: [PlanSegment]) -> Double? {
        guard let first = rated.first else { return nil }
        return rated.allSatisfy { $0.rate == first.rate } ? first.rate : nil
    }
}

public extension DaySummary {
    /// Returns the cost breakdown for `date` if a plan covers it, or `nil` when
    /// none does (AC 2.7).
    ///
    /// Zero kWh fields produce zero cost lines and do NOT make the day
    /// unpriced (Decision 18).
    func costs(forDate date: String, in pricing: [PricingPlan]) -> DayCosts? {
        guard let plan = PricingPlan.plan(for: date, in: pricing) else { return nil }
        return DayCosts.resolve(plan: plan, day: costInputs)
    }

    /// The day's energy as cost resolution sees it. The off-peak row is
    /// reconstructed from the flat wire fields; a day with no off-peak import
    /// has no row at all, which is the distinction the single-rate formula
    /// turns on.
    var costInputs: DayCostInputs {
        DayCostInputs(
            eInput: eInput,
            eOutput: eOutput,
            peakGridImportKwh: peakGridImportKwh,
            offpeak: offpeakGridImportKwh.map {
                OffpeakImport(
                    gridImportKwh: $0,
                    windowStart: offpeakWindowStart,
                    windowEnd: offpeakWindowEnd,
                    integratedAt: offpeakIntegratedAt,
                    sampleCount: offpeakSampleCount ?? 0
                )
            },
            bandImports: bandImports
        )
    }
}

public extension DayEnergy {
    /// Convenience for History per-day costing, using `self.date` and the same
    /// resolution the Day Detail card uses so both screens agree (AC 3.4).
    func costs(in pricing: [PricingPlan]) -> DayCosts? {
        guard let plan = PricingPlan.plan(for: date, in: pricing) else { return nil }
        return DayCosts.resolve(plan: plan, day: costInputs)
    }

    var costInputs: DayCostInputs {
        DayCostInputs(
            eInput: eInput,
            eOutput: eOutput,
            peakGridImportKwh: peakGridImportKwh,
            offpeak: offpeakGridImportKwh.map {
                OffpeakImport(
                    gridImportKwh: $0,
                    windowStart: offpeakWindowStart,
                    windowEnd: offpeakWindowEnd,
                    integratedAt: offpeakIntegratedAt,
                    sampleCount: offpeakSampleCount ?? 0
                )
            },
            bandImports: bandImports
        )
    }
}
