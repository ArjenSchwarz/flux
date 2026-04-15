import Foundation

enum DayChartDomain {
    private static let dayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.timeZone = DateFormatting.sydneyTimeZone
        return formatter
    }()

    private static let sydneyCalendar: Calendar = {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = DateFormatting.sydneyTimeZone
        return calendar
    }()

    static func domain(for dateString: String) -> ClosedRange<Date> {
        guard let startOfDay = dayFormatter.date(from: dateString),
              let endOfDay = sydneyCalendar.date(byAdding: .day, value: 1, to: startOfDay)
        else {
            let now = Date()
            return now ... now
        }

        return startOfDay ... endOfDay
    }
}
