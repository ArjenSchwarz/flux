import FluxCore
import Foundation
import SwiftData
import Testing
@testable import Flux

/// Period-navigation behaviour: intent methods drive the expected
/// `HistoryQuery`, the anchor is only mutated by intents (req 1.8), the
/// resolved snapshot reflects rendered data, and the offline cache fallback is
/// bounded by both period ends (req 7.2).
///
/// Clock for every test: 02:00 UTC on 2026-04-15 = 12:00 AEST Wednesday.
/// With Monday-start weeks (firstWeekday 2): current week Apr 13–19 (to-date
/// count 3), previous week Apr 6–12; current month Apr 1–30 (to-date 15),
/// previous month Mar 1–31.
@MainActor @Suite(.serialized)
struct HistoryViewModelNavigationTests {
    private static let previousWeekQuery = HistoryQuery.dateRange(start: "2026-04-06", end: "2026-04-12")

    // MARK: - Intent methods issue the expected query

    @Test
    func navigatePreviousRequestsThePreviousCalendarWeek() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        await viewModel.navigatePrevious()

        #expect(apiClient.requestedQueries == [.days(3), Self.previousWeekQuery])
        #expect(viewModel.periodAnchor == sydneyMidnight(year: 2026, month: 4, day: 6))
        #expect(viewModel.resolvedQuery == Self.previousWeekQuery)
        #expect(viewModel.resolvedRangeDays == 7)
    }

    @Test
    func navigatePreviousRequestsThePreviousCalendarMonth() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .monthToDate)
        await viewModel.navigatePrevious()

        let march = HistoryQuery.dateRange(start: "2026-03-01", end: "2026-03-31")
        #expect(apiClient.requestedQueries == [.days(15), march])
        #expect(viewModel.resolvedRangeDays == 31)
    }

    @Test
    func navigateNextFromPreviousWeekCollapsesToCurrent() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        await viewModel.navigatePrevious()
        await viewModel.navigateNext()

        // The period containing Sydney-today is requested via the days form,
        // never as a date range (Decision 15).
        #expect(apiClient.requestedQueries.last == .days(3))
        #expect(viewModel.periodAnchor == nil)
        #expect(viewModel.isViewingCurrentPeriod)
    }

    @Test
    func navigateNextAtCurrentPeriodIsANoOp() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        await viewModel.navigateNext()

        // No future period exists after the current one: no request, no
        // anchor movement.
        #expect(apiClient.requestedQueries == [.days(3)])
        #expect(viewModel.periodAnchor == nil)
        #expect(viewModel.isViewingCurrentPeriod)
    }

    @Test
    func rapidDoubleNavigateNextStopsAtTheCurrentPeriod() async throws {
        let context = try makeModelContext()
        let apiClient = GatedQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        await viewModel.navigatePrevious()

        // First navigateNext collapses to current; park its fetch in-flight.
        apiClient.armGate()
        let firstNext = Task { await viewModel.navigateNext() }
        await apiClient.waitForGatedFetch()

        // A second navigateNext lands mid-load (rapid double-tap or macOS
        // key-repeat). The anchor is already nil, so it must be a no-op —
        // not a step into the future week the server would reject.
        await viewModel.navigateNext()

        apiClient.release()
        await firstNext.value

        #expect(apiClient.requestedQueries == [.days(3), Self.previousWeekQuery, .days(3)])
        #expect(viewModel.periodAnchor == nil)
        #expect(viewModel.isViewingCurrentPeriod)
    }

    @Test
    func rapidBackwardNavigationAccumulatesPeriods() async throws {
        let context = try makeModelContext()
        let apiClient = GatedQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)

        // First navigatePrevious steps to the previous week; park its fetch.
        apiClient.armGate()
        let firstPrev = Task { await viewModel.navigatePrevious() }
        await apiClient.waitForGatedFetch()

        // A second navigatePrevious lands mid-load. Back-navigation is
        // unbounded (req 1.6): it steps a further week from the already-moved
        // anchor rather than collapsing to a single step, so two taps land two
        // weeks back, not one.
        await viewModel.navigatePrevious()

        apiClient.release()
        await firstPrev.value

        let twoWeeksBack = HistoryQuery.dateRange(start: "2026-03-30", end: "2026-04-05")
        #expect(apiClient.requestedQueries == [.days(3), Self.previousWeekQuery, twoWeeksBack])
        #expect(viewModel.periodAnchor == sydneyMidnight(year: 2026, month: 3, day: 30))
        #expect(viewModel.resolvedQuery == twoWeeksBack)
    }

    @Test
    func jumpToPastDateRequestsTheContainingPeriod() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .monthToDate)
        await viewModel.jumpTo(date: sydneyMidnight(year: 2026, month: 3, day: 10))

        #expect(apiClient.requestedQueries.last == .dateRange(start: "2026-03-01", end: "2026-03-31"))
        #expect(viewModel.periodAnchor == sydneyMidnight(year: 2026, month: 3, day: 1))
    }

    @Test
    func jumpToDateInCurrentMonthGivesNilAnchor() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .monthToDate)
        await viewModel.jumpTo(date: sydneyMidnight(year: 2026, month: 4, day: 2))

        // Req 3.3: a date inside the current period shows the to-date view.
        #expect(viewModel.periodAnchor == nil)
        #expect(apiClient.requestedQueries.last == .days(15))
    }

    @Test
    func jumpToDateInsideTheDisplayedPeriodIssuesNoRequest() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        await viewModel.navigatePrevious()

        // The picker lands on another date inside the already-displayed past
        // week: nothing would change, so no request is issued and the anchor
        // stays where it is.
        await viewModel.jumpTo(date: sydneyMidnight(year: 2026, month: 4, day: 9))
        #expect(apiClient.requestedQueries == [.days(3), Self.previousWeekQuery])
        #expect(viewModel.periodAnchor == sydneyMidnight(year: 2026, month: 4, day: 6))

        // Same for re-selecting a date inside the displayed current period.
        await viewModel.returnToCurrent()
        await viewModel.jumpTo(date: sydneyMidnight(year: 2026, month: 4, day: 14))
        #expect(apiClient.requestedQueries == [.days(3), Self.previousWeekQuery, .days(3)])
        #expect(viewModel.periodAnchor == nil)
    }

    @Test
    func jumpToSamePeriodRefetchesFromErrorState() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        await viewModel.navigatePrevious()

        // A reload of that same week then fails, leaving the view model in an
        // error state while the previous week is still the rendered snapshot.
        apiClient.historyResult = .failure(FluxAPIError.serverError)
        await viewModel.reload()
        #expect(viewModel.error == .serverError)

        // Retrying via the picker (a date inside the rendered-but-errored week)
        // must bypass the already-rendered short-circuit and re-issue the
        // request — the error state needs its retry path, not a no-op.
        apiClient.historyResult = .success(HistoryResponse(days: []))
        await viewModel.jumpTo(date: sydneyMidnight(year: 2026, month: 4, day: 9))

        #expect(apiClient.requestedQueries == [
            .days(3), Self.previousWeekQuery, Self.previousWeekQuery, Self.previousWeekQuery,
        ])
        #expect(viewModel.error == nil)
        #expect(viewModel.resolvedQuery == Self.previousWeekQuery)
    }

    @Test
    func returnToCurrentClearsAnchorAndRequestsDaysForm() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        await viewModel.navigatePrevious()
        await viewModel.returnToCurrent()

        #expect(viewModel.periodAnchor == nil)
        #expect(apiClient.requestedQueries.last == .days(3))
    }

    // MARK: - Anchor mutation rules (req 1.8, 2.3)

    @Test
    func reloadAndLoadHistoryKeepTheAnchor() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        await viewModel.navigatePrevious()

        await viewModel.reload()
        #expect(viewModel.periodAnchor == sydneyMidnight(year: 2026, month: 4, day: 6))
        #expect(apiClient.requestedQueries.last == Self.previousWeekQuery)

        await viewModel.loadHistory(range: .weekToDate)
        #expect(viewModel.periodAnchor == sydneyMidnight(year: 2026, month: 4, day: 6))
        #expect(apiClient.requestedQueries.last == Self.previousWeekQuery)
    }

    @Test
    func selectRangeResetsTheAnchor() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        await viewModel.navigatePrevious()
        await viewModel.selectRange(.monthToDate)

        #expect(viewModel.periodAnchor == nil)
        #expect(apiClient.requestedQueries.last == .days(15))
    }

    // MARK: - Coalescing on the (range, anchor) pair

    @Test
    func navigationIssuedMidLoadIsHonoured() async throws {
        let context = try makeModelContext()
        let apiClient = GatedQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        // First load parks inside fetchHistory(query:).
        apiClient.armGate()
        let firstLoad = Task { await viewModel.loadHistory(range: .weekToDate) }
        await apiClient.waitForGatedFetch()

        // A navigation arrives during the in-flight load. It returns early at
        // the isLoading guard but records the requested (range, anchor) pair.
        await viewModel.navigatePrevious()

        apiClient.release()
        await firstLoad.value

        #expect(apiClient.requestedQueries == [.days(3), Self.previousWeekQuery])
        #expect(viewModel.resolvedQuery == Self.previousWeekQuery)
    }

    // MARK: - Resolved snapshot reflects rendered data

    @Test
    func resolvedSnapshotIgnoresInFlightNavigation() async throws {
        let context = try makeModelContext()
        let apiClient = GatedQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)
        #expect(viewModel.resolvedQuery == .days(3))

        // Park the navigation's fetch: the rendered data is still the current
        // week, so the resolved snapshot must not move yet.
        apiClient.armGate()
        let navigation = Task { await viewModel.navigatePrevious() }
        await apiClient.waitForGatedFetch()

        #expect(viewModel.resolvedQuery == .days(3))
        #expect(viewModel.isViewingCurrentPeriod)
        let inFlightDomain = try #require(viewModel.chartDomain)
        #expect(inFlightDomain.slotDates.first == sydneyMidnight(year: 2026, month: 4, day: 13))

        apiClient.release()
        await navigation.value

        #expect(viewModel.resolvedQuery == Self.previousWeekQuery)
        #expect(!viewModel.isViewingCurrentPeriod)
        let renderedDomain = try #require(viewModel.chartDomain)
        #expect(renderedDomain.slotDates.first == sydneyMidnight(year: 2026, month: 4, day: 6))
        #expect(renderedDomain.slotDates.count == 7)
    }

    // MARK: - Partial past period averages (req 6.1)

    @Test
    func pastPeriodAveragesDivideByRecordedDaysNotPeriodLength() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)
        await viewModel.loadHistory(range: .monthToDate)

        // March 2026 spans 31 days but only 11 of them have recorded data.
        let recordedDays = (1 ... 11).map { day in
            DayEnergy(
                date: String(format: "2026-03-%02d", day),
                epv: 4.0, eInput: 1.0, eOutput: 0.5, eCharge: 2.0, eDischarge: 3.0
            )
        }
        apiClient.historyResult = .success(HistoryResponse(days: recordedDays))
        await viewModel.navigatePrevious()

        // Req 6.1: per-day averages divide by the 11 recorded days — the
        // 31-day period length only feeds the "N of M days" subtitle.
        let summary = viewModel.derived.summary
        #expect(viewModel.resolvedRangeDays == 31)
        #expect(summary.dayCount == 11)
        #expect(summary.solarPerDayKwh == 4.0)
        #expect(summary.dischargePerDayKwh == 3.0)
    }

    // MARK: - Cache fallback bounded both ends (req 7.2)

    @Test
    func offlineFallbackForPastPeriodIsBoundedByBothEnds() async throws {
        let context = try makeModelContext()
        // Rows before the period start, inside the period, and after the
        // period end (inside the current week). Only the inside rows may show.
        for date in ["2026-04-04", "2026-04-08", "2026-04-12", "2026-04-14"] {
            context.insert(CachedDayEnergy(from: DayEnergy(
                date: date, epv: 5.0, eInput: 1.0, eOutput: 0.3, eCharge: 1.5, eDischarge: 2.0
            )))
        }
        try context.save()

        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)
        await viewModel.loadHistory(range: .weekToDate)

        apiClient.historyResult = .failure(FluxAPIError.networkError("offline"))
        await viewModel.navigatePrevious()

        #expect(viewModel.days.map(\.date) == ["2026-04-08", "2026-04-12"])
        #expect(viewModel.error == nil)
    }

    // MARK: - Error vs no-data states (req 1.6, 7.3)

    @Test
    func failedPastFetchWithoutCacheShowsErrorState() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)
        await viewModel.loadHistory(range: .weekToDate)

        apiClient.historyResult = .failure(FluxAPIError.serverError)
        await viewModel.navigatePrevious()

        #expect(viewModel.days.isEmpty)
        #expect(viewModel.error == .serverError)
        #expect(!viewModel.showsEmptyPeriodNotice, "error and no-data states are distinct")
    }

    @Test
    func successfulEmptyPastFetchShowsNoDataState() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)
        await viewModel.loadHistory(range: .weekToDate)

        await viewModel.navigatePrevious()

        #expect(viewModel.days.isEmpty)
        #expect(viewModel.error == nil)
        #expect(viewModel.showsEmptyPeriodNotice)
        // Req 1.6: the full-period axis is still reserved for the empty period.
        let domain = try #require(viewModel.chartDomain)
        #expect(domain.slotDates.count == 7)
        #expect(domain.slotDates.first == sydneyMidnight(year: 2026, month: 4, day: 6))
    }

    @Test
    func currentPeriodEmptyFetchDoesNotShowPeriodNotice() async throws {
        let context = try makeModelContext()
        let apiClient = RecordingQueryAPIClient()
        let viewModel = makeViewModel(apiClient: apiClient, modelContext: context)

        await viewModel.loadHistory(range: .weekToDate)

        #expect(viewModel.days.isEmpty)
        #expect(!viewModel.showsEmptyPeriodNotice, "the current period keeps the existing empty state")
    }

    // MARK: - Helpers

    /// 02:00 UTC on 2026-04-15 = 12:00 AEST Wednesday.
    private var now: Date {
        Calendar(identifier: .gregorian).date(from: DateComponents(
            timeZone: TimeZone(secondsFromGMT: 0),
            year: 2026, month: 4, day: 15, hour: 2, minute: 0
        ))!
    }

    private func makeViewModel(apiClient: any FluxAPIClient, modelContext: ModelContext) -> HistoryViewModel {
        let now = self.now
        return HistoryViewModel(
            apiClient: apiClient,
            modelContext: modelContext,
            nowProvider: { now },
            firstWeekdayProvider: { 2 }
        )
    }

    private func makeModelContext() throws -> ModelContext {
        let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
        let container = try ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
        return ModelContext(container)
    }

    private func sydneyMidnight(year: Int, month: Int, day: Int) -> Date {
        DateFormatting.sydneyCalendar.date(from: DateComponents(
            timeZone: DateFormatting.sydneyTimeZone,
            year: year, month: month, day: day
        ))!
    }
}

/// Records every `HistoryQuery` the view model issues; the result is settable
/// per test so navigation can succeed, fail, or return an empty period.
@MainActor
private final class RecordingQueryAPIClient: FluxAPIClient {
    var historyResult: Result<HistoryResponse, Error> = .success(HistoryResponse(days: []))
    private(set) var requestedQueries: [HistoryQuery] = []

    func fetchHistory(query: HistoryQuery) async throws -> HistoryResponse {
        requestedQueries.append(query)
        return try historyResult.get()
    }

    nonisolated func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    nonisolated func fetchHistory(days _: Int) async throws -> HistoryResponse { throw FluxAPIError.notConfigured }
    nonisolated func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }
    nonisolated func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}

/// Parks the next `fetchHistory(query:)` after `armGate()` until released, so
/// tests can interleave navigation with an in-flight load.
@MainActor
private final class GatedQueryAPIClient: FluxAPIClient {
    var historyResult: Result<HistoryResponse, Error> = .success(HistoryResponse(days: []))
    private(set) var requestedQueries: [HistoryQuery] = []

    private var gateArmed = false
    private var didStartGatedFetch = false
    private var didRelease = false
    private var started: CheckedContinuation<Void, Never>?
    private var parked: CheckedContinuation<Void, Never>?

    func armGate() {
        gateArmed = true
        didStartGatedFetch = false
        didRelease = false
    }

    func fetchHistory(query: HistoryQuery) async throws -> HistoryResponse {
        requestedQueries.append(query)
        if gateArmed {
            gateArmed = false
            didStartGatedFetch = true
            started?.resume()
            started = nil
            if !didRelease {
                await withCheckedContinuation { parked = $0 }
            }
        }
        return try historyResult.get()
    }

    func waitForGatedFetch() async {
        if didStartGatedFetch { return }
        await withCheckedContinuation { started = $0 }
    }

    func release() {
        didRelease = true
        parked?.resume()
        parked = nil
    }

    nonisolated func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    nonisolated func fetchHistory(days _: Int) async throws -> HistoryResponse { throw FluxAPIError.notConfigured }
    nonisolated func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }
    nonisolated func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}
