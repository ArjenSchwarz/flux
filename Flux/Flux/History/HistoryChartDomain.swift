import Charts
import FluxCore
import Foundation
import SwiftUI

/// Full-period x-axis reservation for the to-date History ranges. A partly
/// elapsed week or month renders its bars left-aligned with empty slots held
/// for the days not yet elapsed, so the layout is consistent regardless of how
/// far into the period we are. Fixed `.days` ranges always span N days ending
/// today, so the view model returns `nil` for them and the charts auto-fit.
struct HistoryChartDomain: Equatable {
    /// `firstSlot ... endExclusive`, where `endExclusive` is Sydney 00:00 of the
    /// day after the period's last day so the final day's bar slot sits fully
    /// inside the plot.
    let span: ClosedRange<Date>
    /// Sydney 00:00 of every day in the period, first through last inclusive.
    /// Rendered as invisible scaffold bars so Swift Charts infers a consistent
    /// one-day bar width even when only one real day has data (the first-day-of
    /// the-period case).
    let slotDates: [Date]

    /// Builds the domain for a to-date range, or `nil` for a fixed `.days`
    /// range (no reservation needed) or if the calendar arithmetic fails.
    static func make(range: HistoryRange, now: Date, firstWeekday: Int) -> HistoryChartDomain? {
        let calendar = DateFormatting.sydneyCalendar
        let start: Date
        let endExclusive: Date
        switch range {
        case .days:
            return nil
        case .weekToDate:
            // A week is always seven calendar days from the locale week start.
            start = DateFormatting.startOfWeek(now: now, firstWeekday: firstWeekday)
            guard let end = calendar.date(byAdding: .day, value: 7, to: start) else { return nil }
            endExclusive = end
        case .monthToDate:
            // The calendar-month interval gives both boundaries without a
            // hard-coded length, so 28–31-day months are handled uniformly.
            guard let interval = calendar.dateInterval(of: .month, for: now) else { return nil }
            start = interval.start
            endExclusive = interval.end
        }
        return build(start: start, endExclusive: endExclusive, calendar: calendar)
    }

    private static func build(start: Date, endExclusive: Date, calendar: Calendar) -> HistoryChartDomain? {
        guard start < endExclusive else { return nil }
        // Step one calendar day at a time (not interval division) so 23/25-hour
        // DST days keep each slot pinned to Sydney midnight.
        var slots: [Date] = []
        var cursor = start
        while cursor < endExclusive {
            slots.append(cursor)
            guard let next = calendar.date(byAdding: .day, value: 1, to: cursor) else { break }
            cursor = next
        }
        guard let first = slots.first else { return nil }
        return HistoryChartDomain(span: first ... endExclusive, slotDates: slots)
    }

    /// Invisible zero-height bars at every slot date. Their presence gives Swift
    /// Charts the one-day spacing it needs to size real bars consistently; they
    /// render nothing and are hidden from VoiceOver. When `slotDates` is empty
    /// (the no-domain case) this contributes nothing to the chart.
    @ChartContentBuilder
    static func scaffold(_ slotDates: [Date]) -> some ChartContent {
        ForEach(slotDates, id: \.self) { date in
            BarMark(
                x: .value("Day", date),
                y: .value("kWh", 0)
            )
            .foregroundStyle(.clear)
            .accessibilityHidden(true)
        }
    }
}

extension View {
    /// Applies `chartXScale(domain:)` when a reservation domain is present, and
    /// is a no-op otherwise so fixed `.days` ranges keep auto-fitting.
    @ViewBuilder
    func historyChartXScale(_ domain: HistoryChartDomain?) -> some View {
        if let domain {
            chartXScale(domain: domain.span)
        } else {
            self
        }
    }
}
