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
}
