import FluxCore
import Foundation

// MARK: - Presentation-facing derived state
//
// Lives in its own file to keep `HistoryViewModel.swift` under the SwiftLint
// file-length cap. Everything here is derived from the resolved snapshot
// (`resolvedRange`/`resolvedQuery`) or `days`, never from in-flight state.

extension HistoryViewModel {
    /// Series and period summary derived from `days`. With at most 30
    /// entries the recomputation is cheap; storing the result would just
    /// add cache-invalidation work. Callers (notably the View) should
    /// capture this once per render rather than reading the convenience
    /// accessors below repeatedly.
    var derived: DerivedState {
        DerivedState(days: days, now: nowProvider())
    }

    /// Full-period x-axis reservation for the Wk/Mo ranges (current or past
    /// period — past periods always reserve the full week/month, req 1.4), or
    /// `nil` for the fixed `.days` ranges, which always span N days ending
    /// today and so need no reservation.
    var chartDomain: HistoryChartDomain? {
        HistoryChartDomain.make(
            range: resolvedRange,
            referenceDate: chartReferenceDate,
            firstWeekday: firstWeekdayProvider()
        )
    }

    /// An instant inside the rendered period: the period start for a resolved
    /// past range, falling back to `now` for the `.days` form.
    private var chartReferenceDate: Date {
        if case let .dateRange(start, _) = resolvedQuery,
           let date = DateFormatting.parseDayDate(start) {
            return date
        }
        return nowProvider()
    }

    /// The expansion scope's query — the rendered window, so an enlarged chart
    /// can never disagree with the card it came from (Decision 13).
    var periodQuery: HistoryQuery { resolvedQuery }

    /// True when the rendered period contains Sydney today — i.e. the last
    /// load resolved to the `.days` form. Keyed off `resolvedQuery` rather
    /// than `periodAnchor` so the next-chevron disabled state stays correct
    /// across a midnight period-rollover until the next load re-resolves.
    var isViewingCurrentPeriod: Bool {
        if case .days = resolvedQuery { return true }
        return false
    }

    /// True when a past period fetched successfully but holds no data: the
    /// cards stay rendered with the full-period axis and a compact notice
    /// (req 1.6). Distinct from the fetch-error state (req 7.3), which sets
    /// `error` instead.
    var showsEmptyPeriodNotice: Bool {
        days.isEmpty && error == nil && !isViewingCurrentPeriod
    }

    /// The rendered period for the header label — `nil` for fixed `.days`
    /// ranges, where the header is not shown (req 1.5).
    var displayedPeriod: HistoryPeriod? {
        switch resolvedRange {
        case .days:
            return nil
        case .weekToDate:
            return .week(containing: chartReferenceDate, firstWeekday: firstWeekdayProvider())
        case .monthToDate:
            return .month(containing: chartReferenceDate)
        }
    }

    /// Upper bound for the jump picker: the last instant of the Sydney day
    /// containing now, so no date after Sydney-today is selectable (req 3.2).
    var sydneyTodayEnd: Date {
        let calendar = DateFormatting.sydneyCalendar
        let startOfToday = calendar.startOfDay(for: nowProvider())
        guard let startOfTomorrow = calendar.date(byAdding: .day, value: 1, to: startOfToday) else {
            return startOfToday
        }
        return startOfTomorrow.addingTimeInterval(-1)
    }

    /// Convenience accessors for tests and previews. Each rebuilds
    /// `DerivedState` independently — production callers should read
    /// `derived` once and destructure instead.
    var solarSeries: [SolarEntry] { derived.solar }
    var gridSeries: [GridEntry] { derived.grid }
    var batterySeries: [BatteryEntry] { derived.battery }
    var dailyUsageSeries: [DailyUsageEntry] { derived.dailyUsage }
    var summary: PeriodSummary { derived.summary }
}
