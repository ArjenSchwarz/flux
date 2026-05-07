import FluxCore
import Foundation

enum HistoryStatsFormatters {
    /// Half-up rounded to nearest whole percent. `Int(rounded(.toNearestOrAwayFromZero))`
    /// is half-away-from-zero — equivalent to half-up for the non-negative SoC range.
    /// `considerSocLow` already filters NaN/Inf at the aggregate boundary; this guard
    /// is belt-and-braces so the formatter can be unit-tested in isolation without
    /// crashing on a hand-constructed pathological input.
    static func socPercent(_ soc: Double) -> String {
        guard soc.isFinite else { return "—" }
        return "\(Int(soc.rounded(.toNearestOrAwayFromZero)))%"
    }

    /// "Apr 28" Sydney time.
    static func shortDate(from date: Date) -> String {
        DateFormatting.shortMonthDay(from: date)
    }

    /// "Apr 26 at 06:14" Sydney time. `time` is the full ISO timestamp the
    /// `/history` payload carries on `DayEnergy.socLowTime`. When nil or
    /// unparseable, falls back to `shortDate`.
    static func dateWithTime(from date: Date, time: String?) -> String {
        let prefix = shortDate(from: date)
        guard let time, let parsed = DateFormatting.parseTimestamp(time) else { return prefix }
        return "\(prefix) at \(DateFormatting.clockTime24h(from: parsed))"
    }

    /// Inclusive range covered by `entries`. Uses min/max so a defensively-reversed
    /// response still produces the right chronological extremes. Returns nil when
    /// `entries` is empty.
    static func dateRange(entries: [HistoryViewModel.SolarEntry]) -> String? {
        let dates = entries.map(\.date)
        guard let earliest = dates.min(), let latest = dates.max() else { return nil }
        let lhs = shortDate(from: earliest)
        let rhs = shortDate(from: latest)
        return lhs == rhs ? lhs : "\(lhs) – \(rhs)"
    }

    /// "kilowatt hours" expansion of a kWh string for accessibility labels.
    /// Threshold is `>= 99.95` (not `>= 100`) so values that `%.1f` would round
    /// up to "100.0" — e.g. 99.95 — go through the integer path and read as "100".
    static func accessibleKwh(_ kwh: Double) -> String {
        let format = kwh >= 99.95 ? "%.0f kilowatt hours" : "%.1f kilowatt hours"
        return String(format: format, kwh)
    }

    /// "12 percent" expansion for Lowest SoC accessibility label. Returns
    /// "no data" when `soc` is non-finite (matches em-dash rendering).
    static func accessibleSocPercent(_ soc: Double) -> String {
        guard soc.isFinite else { return "no data" }
        return "\(Int(soc.rounded(.toNearestOrAwayFromZero))) percent"
    }
}
