import FluxCore
import Foundation
import SwiftData
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct HistoryViewModelRangeTests {
    // MARK: - Default selection

    @Test
    func defaultRangeResolvesToSevenDays() throws {
        // [6.4] Before any load, the view model defaults to the 7d range.
        let modelContext = try makeModelContext()
        let viewModel = HistoryViewModel(apiClient: RecordingHistoryAPIClient(), modelContext: modelContext)

        #expect(viewModel.lastRequestedRange == .days(7))
        #expect(viewModel.resolvedRangeDays == 7)
    }

    // MARK: - Wk / Mo resolution

    @Test
    func monthToDateResolvesInclusiveDayCount() async throws {
        let modelContext = try makeModelContext()
        let apiClient = RecordingHistoryAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: []))

        // 02:00 UTC 2026-04-15 = 12:00 AEST 2026-04-15, so Sydney "today" is the
        // 15th and month-to-date is days 1…15 = 15.
        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 2, minute: 0)
        let viewModel = HistoryViewModel(
            apiClient: apiClient,
            modelContext: modelContext,
            nowProvider: { now },
            firstWeekdayProvider: { 2 }
        )

        await viewModel.loadHistory(range: .monthToDate)

        #expect(viewModel.resolvedRangeDays == 15)
        #expect(apiClient.requestedDays == [15])
    }

    @Test
    func weekToDateResolvesUsingInjectedFirstWeekday() async throws {
        let modelContext = try makeModelContext()
        let apiClient = RecordingHistoryAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: []))

        // 2026-04-15 is a Wednesday; Monday-start week → Mon/Tue/Wed = 3.
        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 2, minute: 0)
        let viewModel = HistoryViewModel(
            apiClient: apiClient,
            modelContext: modelContext,
            nowProvider: { now },
            firstWeekdayProvider: { 2 }
        )

        await viewModel.loadHistory(range: .weekToDate)

        #expect(viewModel.resolvedRangeDays == 3)
        #expect(apiClient.requestedDays == [3])
    }

    // MARK: - reload re-resolves across midnight

    @Test
    func reloadReResolvesAfterNowAdvancesPastSydneyMidnight() async throws {
        let modelContext = try makeModelContext()
        let apiClient = RecordingHistoryAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: []))

        // Mutable now: starts on the 1st (month-to-date = 1), then advances to
        // the 2nd (month-to-date = 2) before reload().
        let nowBox = NowBox(makeUTCDate(year: 2026, month: 4, day: 1, hour: 2, minute: 0))
        let viewModel = HistoryViewModel(
            apiClient: apiClient,
            modelContext: modelContext,
            nowProvider: { nowBox.value },
            firstWeekdayProvider: { 2 }
        )

        await viewModel.loadHistory(range: .monthToDate)
        #expect(viewModel.resolvedRangeDays == 1)

        nowBox.value = makeUTCDate(year: 2026, month: 4, day: 2, hour: 2, minute: 0)
        await viewModel.reload()

        #expect(viewModel.resolvedRangeDays == 2)
        #expect(apiClient.requestedDays == [1, 2])
    }

    // MARK: - Date-bounded offline fallback

    @Test
    func offlineFallbackExcludesPreStartDaysAndAutoSelectsNewest() async throws {
        let modelContext = try makeModelContext()
        // Sydney "today" = 2026-04-15; a 7-day window starts 2026-04-09.
        // 2026-04-07 is before the start and must be excluded; the gap on
        // 2026-04-12/13 must not pull in older days.
        for date in ["2026-04-07", "2026-04-10", "2026-04-11", "2026-04-14"] {
            modelContext.insert(CachedDayEnergy(from: DayEnergy(
                date: date, epv: 5.0, eInput: 1.0, eOutput: 0.3, eCharge: 1.5, eDischarge: 2.0
            )))
        }
        try modelContext.save()

        let apiClient = RecordingHistoryAPIClient()
        apiClient.historyResult = .failure(FluxAPIError.networkError("offline"))

        let now = makeUTCDate(year: 2026, month: 4, day: 14, hour: 18, minute: 0)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })

        await viewModel.loadHistory(range: .days(7))

        #expect(viewModel.days.map(\.date) == ["2026-04-10", "2026-04-11", "2026-04-14"])
        #expect(viewModel.error == nil)
        // Ascending order means the newest day is last and is auto-selected.
        #expect(viewModel.selectedDay?.date == "2026-04-14")
    }

    @Test
    func offlineFallbackBoundedByWeekStart() async throws {
        let modelContext = try makeModelContext()
        // Sydney "today" = 2026-04-15 (Wed). Monday-start week begins 2026-04-13,
        // so only rows on/after 2026-04-13 may appear.
        for date in ["2026-04-11", "2026-04-13", "2026-04-15"] {
            modelContext.insert(CachedDayEnergy(from: DayEnergy(
                date: date, epv: 5.0, eInput: 1.0, eOutput: 0.3, eCharge: 1.5, eDischarge: 2.0
            )))
        }
        try modelContext.save()

        let apiClient = RecordingHistoryAPIClient()
        apiClient.historyResult = .failure(FluxAPIError.networkError("offline"))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 2, minute: 0)
        let viewModel = HistoryViewModel(
            apiClient: apiClient,
            modelContext: modelContext,
            nowProvider: { now },
            firstWeekdayProvider: { 2 }
        )

        await viewModel.loadHistory(range: .weekToDate)

        #expect(viewModel.days.map(\.date) == ["2026-04-13", "2026-04-15"])
        #expect(viewModel.selectedDay?.date == "2026-04-15")
    }

    // MARK: - Coalescing

    @Test
    func midLoadRangeSwitchCoalescesToLatest() async throws {
        let modelContext = try makeModelContext()
        let apiClient = GatedHistoryAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: []))

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 2, minute: 0)
        let viewModel = HistoryViewModel(
            apiClient: apiClient,
            modelContext: modelContext,
            nowProvider: { now },
            firstWeekdayProvider: { 2 }
        )

        // First load parks inside fetchHistory.
        let firstLoad = Task { await viewModel.loadHistory(range: .days(7)) }
        await apiClient.waitForFirstFetch()

        // A newer selection arrives during the in-flight load. It returns early
        // at the isLoading guard but records lastRequestedRange = .days(30).
        await viewModel.loadHistory(range: .days(30))

        // Release the parked fetch; the loop then re-resolves to .days(30).
        apiClient.releaseFirstFetch()
        await firstLoad.value

        #expect(viewModel.resolvedRangeDays == 30)
        #expect(apiClient.requestedDays == [7, 30])
    }

    // MARK: - Failure with empty cache

    @Test
    func failedFetchWithEmptyCacheYieldsErrorAndEmptyDays() async throws {
        let modelContext = try makeModelContext()
        let apiClient = RecordingHistoryAPIClient()
        apiClient.historyResult = .failure(FluxAPIError.serverError)

        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 2, minute: 0)
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext, nowProvider: { now })

        await viewModel.loadHistory(range: .monthToDate)

        #expect(viewModel.days.isEmpty)
        #expect(viewModel.error == .serverError)
        #expect(viewModel.selectedDay == nil)
    }

    // MARK: - Helpers

    private func makeModelContext() throws -> ModelContext {
        let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
        let container = try ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
        return ModelContext(container)
    }

    private func makeUTCDate(year: Int, month: Int, day: Int, hour: Int, minute: Int) -> Date {
        let calendar = Calendar(identifier: .gregorian)
        return calendar.date(from: DateComponents(
            timeZone: TimeZone(secondsFromGMT: 0),
            year: year,
            month: month,
            day: day,
            hour: hour,
            minute: minute
        ))!
    }
}

/// A mutable now holder so a test can advance the injected clock between loads.
/// Lock-guarded so the `@Sendable nowProvider` closure may read it safely.
private final class NowBox: @unchecked Sendable {
    private let lock = NSLock()
    private var stored: Date

    init(_ value: Date) { stored = value }

    var value: Date {
        get { lock.withLock { stored } }
        set { lock.withLock { stored = newValue } }
    }
}

private final class RecordingHistoryAPIClient: FluxAPIClient, @unchecked Sendable {
    var historyResult: Result<HistoryResponse, Error> = .failure(FluxAPIError.notConfigured)
    private(set) var requestedDays: [Int] = []

    func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }

    func fetchHistory(days: Int) async throws -> HistoryResponse {
        requestedDays.append(days)
        return try historyResult.get()
    }

    func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }
    func saveNote(date _: String, text _: String) async throws -> NoteResponse { throw FluxAPIError.notConfigured }
}

/// Parks the first `fetchHistory` call until released so a test can switch
/// range mid-load and exercise the coalescing loop.
@MainActor
private final class GatedHistoryAPIClient: FluxAPIClient {
    var historyResult: Result<HistoryResponse, Error> = .failure(FluxAPIError.notConfigured)
    private(set) var requestedDays: [Int] = []

    private var firstFetchStarted: CheckedContinuation<Void, Never>?
    private var firstFetchRelease: CheckedContinuation<Void, Never>?
    private var didStartFirstFetch = false
    private var didReleaseFirstFetch = false

    nonisolated func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    nonisolated func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }
    nonisolated func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days: Int) async throws -> HistoryResponse {
        let isFirst = requestedDays.isEmpty
        requestedDays.append(days)
        if isFirst {
            didStartFirstFetch = true
            firstFetchStarted?.resume()
            firstFetchStarted = nil
            if !didReleaseFirstFetch {
                await withCheckedContinuation { firstFetchRelease = $0 }
            }
        }
        return try historyResult.get()
    }

    func waitForFirstFetch() async {
        if didStartFirstFetch { return }
        await withCheckedContinuation { firstFetchStarted = $0 }
    }

    func releaseFirstFetch() {
        didReleaseFirstFetch = true
        firstFetchRelease?.resume()
        firstFetchRelease = nil
    }
}
