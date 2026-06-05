import FluxCore
import Foundation

/// A History range selection. Fixed `.days` cases carry their length directly;
/// the to-date cases resolve to an inclusive day-count ending on `now`,
/// computed against the Sydney calendar with a locale-derived first weekday.
enum HistoryRange: Hashable {
    case days(Int) // 7, 14, 30
    case weekToDate
    case monthToDate

    /// The only UI string for this range. The boundary math is delegated to
    /// the FluxCore helpers.
    var pickerLabel: String {
        switch self {
        case let .days(count):
            return "\(count)d"
        case .weekToDate:
            return "Wk"
        case .monthToDate:
            return "Mo"
        }
    }

    /// Inclusive day-count ending on `now` (1…31). Fixed cases return their N;
    /// to-date cases resolve via the Sydney-calendar FluxCore helpers.
    /// `firstWeekday` follows Calendar's convention (1 = Sunday … 7 = Saturday)
    /// and is consulted only for `weekToDate`.
    func resolvedDays(now: Date, firstWeekday: Int) -> Int {
        switch self {
        case let .days(count):
            return count
        case .weekToDate:
            let start = DateFormatting.startOfWeek(now: now, firstWeekday: firstWeekday)
            return DateFormatting.inclusiveDayCount(from: start, through: now)
        case .monthToDate:
            let start = DateFormatting.startOfMonth(now: now)
            return DateFormatting.inclusiveDayCount(from: start, through: now)
        }
    }
}
