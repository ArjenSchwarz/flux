import FluxCore
import Foundation

enum DayChartDomain {
    static func domain(for dateString: String) -> ClosedRange<Date> {
        guard let startOfDay = DateFormatting.parseDayDate(dateString),
              let endOfDay = DateFormatting.sydneyCalendar.date(byAdding: .day, value: 1, to: startOfDay)
        else {
            let now = Date()
            return now ... now
        }

        return startOfDay ... endOfDay
    }

    /// The free-window shading band for a day's charts. The window comes from
    /// the free band of the plan pricing that day (AC 4.1) rather than a fixed
    /// 11:00–14:00, so it moves with the plan on the switch date. A day with no
    /// plan, or whose plan has no free band, gets no shading (AC 4.4).
    static func offpeakRange(for dateString: String, window: PlanSegment?) -> (start: Date, end: Date)? {
        guard let window,
              let startOfDay = DateFormatting.parseDayDate(dateString),
              let startMinutes = PlanWindow.parseBandTime(window.start),
              let endMinutes = PlanWindow.parseBandTime(window.end)
        else { return nil }

        let calendar = DateFormatting.sydneyCalendar
        guard let offpeakStart = calendar.date(byAdding: .minute, value: startMinutes, to: startOfDay),
              let offpeakEnd = calendar.date(byAdding: .minute, value: endMinutes, to: startOfDay)
        else { return nil }

        return (offpeakStart, offpeakEnd)
    }

    /// Convenience for the call sites that hold the plan list rather than an
    /// already-resolved window.
    static func offpeakRange(for dateString: String, plans: [PricingPlan]) -> (start: Date, end: Date)? {
        offpeakRange(for: dateString, window: PricingPlan.freeWindow(for: dateString, in: plans))
    }
}
