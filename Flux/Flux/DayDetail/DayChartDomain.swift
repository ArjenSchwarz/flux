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

    static func offpeakRange(for dateString: String) -> (start: Date, end: Date)? {
        guard let startOfDay = DateFormatting.parseDayDate(dateString) else { return nil }
        let calendar = DateFormatting.sydneyCalendar

        guard let offpeakStart = calendar.date(byAdding: .hour, value: 11, to: startOfDay),
              let offpeakEnd = calendar.date(byAdding: .hour, value: 14, to: startOfDay)
        else { return nil }

        return (offpeakStart, offpeakEnd)
    }
}
