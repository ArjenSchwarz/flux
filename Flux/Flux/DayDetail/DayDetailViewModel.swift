import FluxCore
import Foundation
import Observation

struct ParsedReading: Identifiable {
    let id: String
    let date: Date
    let point: TimeSeriesPoint

    static func parse(_ readings: [TimeSeriesPoint]) -> [ParsedReading] {
        var parsed: [ParsedReading] = []
        parsed.reserveCapacity(readings.count)
        for reading in readings {
            guard let parsedDate = DateFormatting.parseTimestamp(reading.timestamp) else { continue }
            parsed.append(ParsedReading(id: reading.id, date: parsedDate, point: reading))
        }
        return parsed
    }
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
    private(set) var dailyUsage: DailyUsage?
    private(set) var note: String?
    /// Derived off-peak stats computed once per `loadDay()` rather than
    /// on every SwiftUI body re-render. `OffpeakReadingStats.compute` is
    /// O(n) over `parsedReadings`, so caching it here saves work on
    /// frequent re-renders (selection cursor, scroll, etc.).
    private(set) var offpeakStats: OffpeakReadingStats = .empty
    private(set) var comparisonState: ComparisonState = .off

    private let apiClient: any FluxAPIClient
    private let nowProvider: @Sendable () -> Date
    // `nonisolated(unsafe)` so `deinit` can cancel the in-flight task
    // without crossing actor isolation. `@ObservationIgnored` opts the
    // task handle out of `@Observable` storage — it's not observable
    // state.
    //
    // INVARIANT (compiler no longer enforces this): the only access to
    // this property from a non-MainActor context must be the
    // `cancel()` call in `deinit`. All read/write paths from
    // `updateCompare` and friends run on the main actor. If you add a
    // new access point, audit it against this rule — `nonisolated(unsafe)`
    // is the discipline that makes the storage safe; Swift no longer
    // checks it for you.
    @ObservationIgnored
    nonisolated(unsafe) private var comparisonTask: Task<Void, Never>?

    init(
        date: String,
        apiClient: any FluxAPIClient,
        nowProvider: @escaping @Sendable () -> Date = { .now }
    ) {
        self.date = date
        self.apiClient = apiClient
        self.nowProvider = nowProvider
    }

    deinit {
        // The comparison Task captures `self` strongly so it can write
        // back to `comparisonState` on completion. If the view is
        // dismissed mid-fetch, cancel the in-flight task so the awaited
        // network request is not left running.
        comparisonTask?.cancel()
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
            parsedReadings = ParsedReading.parse(response.readings)
            summary = response.summary
            hasPowerData = !isFallbackData(response.readings)
            peakPeriods = response.peakPeriods ?? []
            dailyUsage = response.dailyUsage
            note = response.note
            offpeakStats = OffpeakReadingStats.compute(date: date, readings: parsedReadings)
            error = nil
        } catch {
            readings = []
            parsedReadings = []
            summary = nil
            hasPowerData = true
            peakPeriods = []
            dailyUsage = nil
            note = nil
            offpeakStats = .empty
            self.error = FluxAPIError.from(error)
        }
    }

    /// Drives the Compare lifecycle. Cancels any in-flight comparison
    /// fetch, then either resets to `.off`, short-circuits to
    /// `.unavailable` on date-resolution failure, or kicks off a new
    /// fetch whose outcome maps to `.ready` / `.unavailable`. The
    /// `Task.isCancelled` guards before each state mutation are
    /// load-bearing — without them a slow fetch could overwrite a
    /// newer state after the period chip or selected day changed.
    func updateCompare(enabled: Bool, period: ComparePeriod) {
        comparisonTask?.cancel()
        guard enabled else {
            comparisonState = .off
            return
        }
        guard let target = resolveCompareDate(period: period) else {
            comparisonState = .unavailable(period: period)
            return
        }
        comparisonState = .loading(date: target)
        comparisonTask = Task { [apiClient] in
            let result: ComparisonState
            do {
                let response = try await apiClient.fetchDay(date: target)
                if Task.isCancelled { return }
                if let snapshot = ComparisonSnapshot.from(date: target, response: response) {
                    result = .ready(snapshot, period: period)
                } else {
                    result = .unavailable(period: period)
                }
            } catch {
                if Task.isCancelled { return }
                result = .unavailable(period: period)
            }
            if Task.isCancelled { return }
            self.comparisonState = result
        }
    }

    private func resolveCompareDate(period: ComparePeriod) -> String? {
        guard let parsed = DateFormatting.parseDayDate(date),
              let target = DateFormatting.sydneyCalendar
                  .date(byAdding: .day, value: period.dayOffset, to: parsed)
        else {
            return nil
        }
        return DateFormatting.dayDateString(from: target)
    }

    func saveNote(_ rawText: String) async throws {
        let normalised = NoteText.normalised(rawText)
        guard normalised.count <= NoteText.maxGraphemes else {
            throw FluxAPIError.badRequest("Note must be 200 characters or fewer")
        }
        let response = try await apiClient.saveNote(date: date, text: normalised)
        note = response.text.isEmpty ? nil : response.text
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
