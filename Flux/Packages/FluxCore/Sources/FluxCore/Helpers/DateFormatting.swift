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

    private static let shortMonthDayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "MMM d"
        formatter.timeZone = sydneyTimeZone
        return formatter
    }()

    /// `"MMM d"` Sydney time (e.g. `"Apr 28"`).
    public static func shortMonthDay(from date: Date) -> String {
        shortMonthDayFormatter.string(from: date)
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

    /// Sydney-calendar 00:00 on the 1st of the month containing `now`.
    public static func startOfMonth(now: Date) -> Date {
        guard let start = sydneyCalendar.dateInterval(of: .month, for: now)?.start else {
            assertionFailure("dateInterval(of: .month) returned nil for a valid date")
            return now
        }
        return start
    }

    /// Sydney-calendar 00:00 on the week start containing `now`.
    /// `firstWeekday` follows Calendar's convention (1 = Sunday … 7 = Saturday);
    /// only the weekday is locale-driven — the date arithmetic is Sydney.
    ///
    /// Mutates a copy of `sydneyCalendar`, never the shared `static let`, which
    /// would corrupt every other date computation.
    public static func startOfWeek(now: Date, firstWeekday: Int) -> Date {
        var calendar = sydneyCalendar
        calendar.firstWeekday = firstWeekday
        guard let start = calendar.dateInterval(of: .weekOfYear, for: now)?.start else {
            assertionFailure("dateInterval(of: .weekOfYear) returned nil for a valid date")
            return now
        }
        return start
    }

    /// Inclusive count of Sydney calendar days from `start` through `end`.
    /// Computed by calendar-day difference (both normalised to Sydney midnight),
    /// never by dividing an elapsed interval, so 23/25-hour DST days don't shift it.
    public static func inclusiveDayCount(from start: Date, through end: Date) -> Int {
        let startMidnight = sydneyCalendar.startOfDay(for: start)
        let endMidnight = sydneyCalendar.startOfDay(for: end)
        let days = sydneyCalendar.dateComponents([.day], from: startMidnight, to: endMidnight).day ?? 0
        return days + 1
    }

    /// Sydney-calendar `YYYY-MM-DD` for the inclusive window start `count` days
    /// back from `now` — i.e. `today-(count-1)`. Single-sources the window-start
    /// formula so the app's offline cache bound matches the backend's
    /// `startDate = now.AddDate(0, 0, -(days-1))`. `count` is an inclusive
    /// day-count (1…31).
    public static func windowStartDateString(inclusiveDays count: Int, now: Date) -> String {
        let today = sydneyCalendar.startOfDay(for: now)
        guard let start = sydneyCalendar.date(byAdding: .day, value: -(count - 1), to: today) else {
            assertionFailure("date(byAdding: .day) returned nil for a valid date")
            return dayDateString(from: today)
        }
        return dayDateString(from: start)
    }
}
