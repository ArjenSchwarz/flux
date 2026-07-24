import Foundation

/// One exception to a plan's default rate: a half-open `[start, end)` slice of
/// the day that is either free or carries its own rate. Boundaries are "HH:MM"
/// in Sydney local time; `end` may be "24:00". `rate` is absent on a free
/// window — the domain ignores it there by contract.
///
/// Mirrors `plan.Window` on the backend.
public struct PlanWindow: Codable, Sendable, Equatable, Hashable {
    public var start: String
    public var end: String
    public var free: Bool
    public var rate: Double?

    public init(start: String, end: String, free: Bool, rate: Double?) {
        self.start = start
        self.end = end
        self.free = free
        self.rate = rate
    }
}

public extension PlanWindow {
    /// The exclusive upper bound of a band boundary. Boundaries are
    /// minute-of-day values in `0...1440`; 1440 ("24:00") is end-of-day and is
    /// only ever a window or segment end.
    static let minutesPerDay = 24 * 60

    /// Converts an "HH:MM" band boundary to minutes since midnight, accepting
    /// "24:00" as end-of-day. Returns `nil` for anything malformed or beyond
    /// the day (Q34 — the off-peak window parser rejects `h > 23` and cannot
    /// be reused for bands).
    static func parseBandTime(_ value: String) -> Int? {
        let chars = Array(value)
        guard chars.count == 5, chars[2] == ":" else { return nil }
        for index in [0, 1, 3, 4] where !chars[index].isASCII || !chars[index].isNumber {
            return nil
        }
        guard let hours = Int(String(chars[0 ..< 2])),
              let minutes = Int(String(chars[3 ..< 5])),
              minutes <= 59 else { return nil }
        let total = hours * 60 + minutes
        guard total <= minutesPerDay else { return nil }
        return total
    }

    /// The inverse of `parseBandTime`, rendering 1440 as "24:00".
    static func formatBandTime(_ minutes: Int) -> String {
        String(format: "%02d:%02d", minutes / 60, minutes % 60)
    }
}

/// A window whose boundaries have been resolved to minutes of the day. Shared
/// by segmentation and draft validation, which both need the same "is this
/// window well-formed" answer before they can do anything with it.
struct ParsedBand {
    let start: Int
    let end: Int
    let free: Bool
    let rate: Double

    /// Fails when the boundaries are unparseable or not in order — the cases
    /// `PricingPlanDraft.validate` reports as `bandWindowInvalid`.
    init?(_ window: PlanWindow) {
        guard let start = PlanWindow.parseBandTime(window.start),
              let end = PlanWindow.parseBandTime(window.end),
              start < end else { return nil }
        self.start = start
        self.end = end
        self.free = window.free
        self.rate = window.rate ?? 0
    }
}

/// One band of the derived full-day segmentation. The segments of a plan tile
/// 00:00–24:00 exactly (AC 1.1). Mirrors `plan.Segment`.
public struct PlanSegment: Sendable, Equatable, Hashable {
    public let start: String
    public let end: String
    public let free: Bool
    public let rate: Double

    public init(start: String, end: String, free: Bool, rate: Double) {
        self.start = start
        self.end = end
        self.free = free
        self.rate = rate
    }
}

/// Server-assigned pricing plan. The id and timestamps are populated by the
/// backend; the client treats them as opaque. Dates are kept as YYYY-MM-DD
/// strings to match `DayEnergy.date` and so day-membership tests can use
/// lexicographic string comparison.
///
/// The plan is stored as entered — a default rate plus the exception windows
/// that deviate from it (Decision 4) — and `endDate` is the exclusive switch
/// date (Decision 5). Mirrors `dynamo.PricingItem` / `plan.Plan`.
public struct PricingPlan: Identifiable, Codable, Sendable, Equatable, Hashable {
    public let id: String
    public let startDate: String
    /// Exclusive: the plan prices `[startDate, endDate)`, so a plan ending on
    /// the date its successor starts hands the switch day to the successor.
    /// `nil` means open-ended.
    public let endDate: String?
    public let defaultRate: Double
    public let windows: [PlanWindow]
    public let feedInRate: Double
    /// Present iff the plan has a free window; the rate free-window energy is
    /// valued at.
    public let savingsReferenceRate: Double?
    public let createdAt: Date
    public let updatedAt: Date

    public init(
        id: String,
        startDate: String,
        endDate: String?,
        defaultRate: Double,
        windows: [PlanWindow],
        feedInRate: Double,
        savingsReferenceRate: Double?,
        createdAt: Date,
        updatedAt: Date
    ) {
        self.id = id
        self.startDate = startDate
        self.endDate = endDate
        self.defaultRate = defaultRate
        self.windows = windows
        self.feedInRate = feedInRate
        self.savingsReferenceRate = savingsReferenceRate
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    /// Returns true iff `date` (YYYY-MM-DD) falls within `[startDate, endDate)`.
    /// The end date is exclusive (Decision 5), so the switch day belongs to the
    /// successor.
    public func covers(date: String) -> Bool {
        guard date >= startDate else { return false }
        guard let endDate else { return true }
        return date < endDate
    }
}

public extension PricingPlan {
    /// The plan's contiguous full-day band list, derived from the default rate
    /// and the exception windows. The result always starts at "00:00", ends at
    /// "24:00", and contains no gaps, overlaps, or zero-width entries.
    ///
    /// Abutting segments carrying the same rate are deliberately not merged
    /// (Q26): the stored per-band split joins to this geometry, so it has to
    /// stay stable against a merge that would depend on rate equality.
    ///
    /// Windows that fail to parse are skipped — `PricingPlanDraft.validate` is
    /// where they are reported, so this stays a total function.
    var segments: [PlanSegment] {
        let bands = windows
            .compactMap(ParsedBand.init)
            .sorted { $0.start < $1.start }

        var segments: [PlanSegment] = []
        segments.reserveCapacity(bands.count * 2 + 1)

        func appendDefault(fromMinute: Int, toMinute: Int) {
            guard fromMinute < toMinute else { return }
            segments.append(PlanSegment(
                start: PlanWindow.formatBandTime(fromMinute),
                end: PlanWindow.formatBandTime(toMinute),
                free: false,
                rate: defaultRate
            ))
        }

        var cursor = 0
        for band in bands {
            // An overlapping window is invalid (validate reports bandOverlap);
            // skipping it keeps the tiling invariant intact rather than
            // emitting an inverted segment.
            guard band.start >= cursor else { continue }
            appendDefault(fromMinute: cursor, toMinute: band.start)
            segments.append(PlanSegment(
                start: PlanWindow.formatBandTime(band.start),
                end: PlanWindow.formatBandTime(band.end),
                free: band.free,
                rate: band.free ? 0 : band.rate
            ))
            cursor = band.end
        }
        appendDefault(fromMinute: cursor, toMinute: PlanWindow.minutesPerDay)
        return segments
    }

    /// `segments` filtered to the non-free bands — the geometry stored in a
    /// day's `bandImports`. Free-window import lives on the off-peak row, which
    /// owns it exclusively (Q31).
    var ratedSegments: [PlanSegment] {
        segments.filter { !$0.free }
    }

    /// The plan's free band, or `nil` when it has none. Callers must treat
    /// `nil` as "no window" and never substitute a default window (AC 4.4).
    var freeWindow: PlanSegment? {
        segments.first { $0.free }
    }

    /// The plan pricing the given date. At most one plan covers any date
    /// (AC 2.1) — the validation rules make overlapping ranges unstorable — so
    /// the first match is the only match.
    static func plan(for date: String, in plans: [PricingPlan]) -> PricingPlan? {
        plans.first { $0.covers(date: date) }
    }

    /// The free window of the plan pricing the given date (AC 4.1). `nil` when
    /// no plan covers the date or the covering plan has no free band — the two
    /// "no window" outcomes callers treat alike.
    static func freeWindow(for date: String, in plans: [PricingPlan]) -> PlanSegment? {
        plan(for: date, in: plans)?.freeWindow
    }
}

/// Server response from POST /pricing/replace-open-ended: the closing plan
/// (its exclusive end date set to the successor's start date) and the new
/// open-ended plan.
public struct ReplaceOpenEndedResult: Sendable, Equatable {
    public let closing: PricingPlan
    public let newPlan: PricingPlan

    public init(closing: PricingPlan, newPlan: PricingPlan) {
        self.closing = closing
        self.newPlan = newPlan
    }
}
