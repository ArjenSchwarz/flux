import FluxCore
import Foundation
import Observation

struct ParsedReading: Identifiable {
    let id: String
    let date: Date
    let point: TimeSeriesPoint
}

extension Array where Element == ParsedReading {
    func nearestReading(to target: Date) -> ParsedReading? {
        guard !isEmpty else { return nil }
        return self.min(by: { abs($0.date.timeIntervalSince(target)) < abs($1.date.timeIntervalSince(target)) })
    }
}

@MainActor @Observable
final class DayDetailViewModel {
    private(set) var date: String
    private(set) var readings: [TimeSeriesPoint] = []
    private(set) var parsedReadings: [ParsedReading] = []
    private(set) var summary: DaySummary?
    private(set) var isLoading = false
    private(set) var error: FluxAPIError?
    private(set) var hasPowerData = true
    private(set) var peakPeriods: [PeakPeriod] = []

    private let apiClient: any FluxAPIClient
    private let nowProvider: @Sendable () -> Date

    init(
        date: String,
        apiClient: any FluxAPIClient,
        nowProvider: @escaping @Sendable () -> Date = { .now }
    ) {
        self.date = date
        self.apiClient = apiClient
        self.nowProvider = nowProvider
    }

    var isToday: Bool {
        DateFormatting.isToday(date, now: nowProvider())
    }

    func loadDay() async {
        guard !isLoading else { return }

        isLoading = true
        defer { isLoading = false }

        do {
            let response = try await apiClient.fetchDay(date: date)
            readings = response.readings
            parsedReadings = parseReadings(response.readings)
            summary = response.summary
            hasPowerData = !isFallbackData(response.readings)
            peakPeriods = response.peakPeriods ?? []
            error = nil
        } catch {
            readings = []
            parsedReadings = []
            summary = nil
            hasPowerData = true
            peakPeriods = []
            self.error = FluxAPIError.from(error)
        }
    }

    func navigatePrevious() {
        guard let previous = shiftDate(by: -1) else { return }
        date = previous
    }

    func navigateNext() {
        guard !isToday, let next = shiftDate(by: 1) else { return }
        date = next
    }

    private func shiftDate(by dayOffset: Int) -> String? {
        guard let currentDate = DateFormatting.parseDayDate(date),
              let newDate = DateFormatting.sydneyCalendar.date(byAdding: .day, value: dayOffset, to: currentDate)
        else {
            return nil
        }

        return DateFormatting.dayDateString(from: newDate)
    }

    private func parseReadings(_ readings: [TimeSeriesPoint]) -> [ParsedReading] {
        var parsed: [ParsedReading] = []
        parsed.reserveCapacity(readings.count)
        for reading in readings {
            guard let parsedDate = DateFormatting.parseTimestamp(reading.timestamp) else { continue }
            parsed.append(ParsedReading(id: reading.id, date: parsedDate, point: reading))
        }
        return parsed
    }

    private func isFallbackData(_ readings: [TimeSeriesPoint]) -> Bool {
        guard !readings.isEmpty else { return false }

        return readings.first {
            $0.ppv != 0 || $0.pload != 0 || $0.pbat != 0 || $0.pgrid != 0
        } == nil
    }
}
