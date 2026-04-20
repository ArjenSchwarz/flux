import Foundation

public enum DateFormatting {
    public static let sydneyTimeZone = TimeZone(identifier: "Australia/Sydney")!

    public static let sydneyCalendar: Calendar = {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = sydneyTimeZone
        return calendar
    }()

    private static let isoFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    private static let isoFormatterNoFractionalSeconds: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    public static let dayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.timeZone = sydneyTimeZone
        return formatter
    }()

    private static let clockFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeStyle = .short
        formatter.dateStyle = .none
        formatter.timeZone = sydneyTimeZone
        return formatter
    }()

    public static func parseTimestamp(_ string: String) -> Date? {
        isoFormatter.date(from: string) ?? isoFormatterNoFractionalSeconds.date(from: string)
    }

    public static func clockTime(from date: Date) -> String {
        clockFormatter.string(from: date)
    }

    private static let clock24hFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "HH:mm"
        formatter.timeZone = sydneyTimeZone
        return formatter
    }()

    public static func clockTime24h(from date: Date) -> String {
        clock24hFormatter.string(from: date)
    }

    public static func todayDateString(now: Date = .now) -> String {
        dayFormatter.string(from: now)
    }

    public static func parseDayDate(_ dateString: String) -> Date? {
        dayFormatter.date(from: dateString)
    }

    public static func dayDateString(from date: Date) -> String {
        dayFormatter.string(from: date)
    }

    public static func parseWindowTime(_ timeString: String, on date: Date = .now) -> Date? {
        let parts = timeString.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count == 2,
              let hour = Int(parts[0]),
              let minute = Int(parts[1]),
              (0 ... 23).contains(hour),
              (0 ... 59).contains(minute)
        else {
            return nil
        }
        return sydneyCalendar.date(bySettingHour: hour, minute: minute, second: 0, of: date)
    }

    public static func isInOffpeakWindow(start: String, end: String, now: Date = .now) -> Bool {
        guard let startDate = parseWindowTime(start, on: now),
              let endDate = parseWindowTime(end, on: now) else {
            return false
        }
        return now >= startDate && now < endDate
    }

    public static func isToday(_ dateString: String, now: Date = .now) -> Bool {
        dateString == todayDateString(now: now)
    }
}
