import Foundation
import Observation

@MainActor @Observable
final class DayDetailViewModel {
    private(set) var date: String
    private(set) var readings: [TimeSeriesPoint] = []
    private(set) var summary: DaySummary?
    private(set) var isLoading = false
    private(set) var error: FluxAPIError?
    private(set) var hasPowerData = true

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
            summary = response.summary
            hasPowerData = !isFallbackData(response.readings)
            error = nil
        } catch {
            readings = []
            summary = nil
            hasPowerData = true
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

    private func isFallbackData(_ readings: [TimeSeriesPoint]) -> Bool {
        guard !readings.isEmpty else { return false }

        return readings.first {
            $0.ppv != 0 || $0.pload != 0 || $0.pbat != 0 || $0.pgrid != 0
        } == nil
    }
}
