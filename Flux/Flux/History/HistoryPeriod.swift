import FluxCore
import Foundation

/// A resolved calendar period (one week or one month) with all the period date
/// math in one place, composed from the existing `DateFormatting` helpers
/// (Sydney calendar, locale-derived first weekday per the T-1361 rules).
struct HistoryPeriod: Equatable {
    /// Sydney midnight on the period's first day.
    let start: Date
    /// Sydney midnight on the day after the period's last day.
    let endExclusive: Date

    /// The calendar week containing `date`. `firstWeekday` follows Calendar's
    /// convention (1 = Sunday … 7 = Saturday).
    static func week(containing date: Date, firstWeekday: Int) -> HistoryPeriod {
        let start = DateFormatting.startOfWeek(now: date, firstWeekday: firstWeekday)
        guard let end = DateFormatting.sydneyCalendar.date(byAdding: .day, value: 7, to: start) else {
            assertionFailure("date(byAdding: .day) returned nil for a valid date")
            return HistoryPeriod(start: start, endExclusive: start)
        }
        return HistoryPeriod(start: start, endExclusive: end)
    }

    /// The calendar month containing `date`. The month interval gives both
    /// boundaries without a hard-coded length, so 28–31-day months are
    /// handled uniformly (same rule as `HistoryChartDomain`).
    static func month(containing date: Date) -> HistoryPeriod {
        guard let interval = DateFormatting.sydneyCalendar.dateInterval(of: .month, for: date) else {
            assertionFailure("dateInterval(of: .month) returned nil for a valid date")
            return HistoryPeriod(start: date, endExclusive: date)
        }
        return HistoryPeriod(start: interval.start, endExclusive: interval.end)
    }

    /// The period immediately before this one. Shifts `start` by calendar
    /// arithmetic — never interval division — so DST days don't drift.
    ///
    /// Asserts on `.days` ranges: navigation is undefined there and the
    /// controls are never shown (req 1.5); a silent self-return would hide a
    /// wiring bug.
    func previous(range: HistoryRange, firstWeekday: Int) -> HistoryPeriod {
        shifted(by: -1, range: range, firstWeekday: firstWeekday)
    }

    /// The period immediately after this one. See `previous` for the rules.
    func next(range: HistoryRange, firstWeekday: Int) -> HistoryPeriod {
        shifted(by: 1, range: range, firstWeekday: firstWeekday)
    }

    private func shifted(by direction: Int, range: HistoryRange, firstWeekday: Int) -> HistoryPeriod {
        let calendar = DateFormatting.sydneyCalendar
        switch range {
        case .days:
            assertionFailure("Period navigation is undefined for fixed .days ranges")
            return self
        case .weekToDate:
            guard let newStart = calendar.date(byAdding: .day, value: direction * 7, to: start) else {
                assertionFailure("date(byAdding: .day) returned nil for a valid date")
                return self
            }
            return .week(containing: newStart, firstWeekday: firstWeekday)
        case .monthToDate:
            guard let newStart = calendar.date(byAdding: .month, value: direction, to: start) else {
                assertionFailure("date(byAdding: .month) returned nil for a valid date")
                return self
            }
            return .month(containing: newStart)
        }
    }

    /// True when `date` falls inside `start ..< endExclusive`.
    func contains(_ date: Date) -> Bool {
        date >= start && date < endExclusive
    }

    /// Number of calendar days in the period — the M in "N of M days".
    var dayCount: Int {
        DateFormatting.inclusiveDayCount(from: start, through: lastDay)
    }

    /// `YYYY-MM-DD` of the period's first day.
    var startDateString: String {
        DateFormatting.dayDateString(from: start)
    }

    /// `YYYY-MM-DD` of the period's last day (inclusive bound).
    var endDateString: String {
        DateFormatting.dayDateString(from: lastDay)
    }

    /// Sydney midnight on the period's last day (inclusive bound). Used by the
    /// header label formatters alongside the date strings.
    var lastDay: Date {
        guard let day = DateFormatting.sydneyCalendar.date(byAdding: .day, value: -1, to: endExclusive) else {
            assertionFailure("date(byAdding: .day) returned nil for a valid date")
            return start
        }
        return day
    }
}
