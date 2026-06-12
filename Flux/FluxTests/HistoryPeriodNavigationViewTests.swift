import FluxCore
import Foundation
import SwiftData
import Testing
@testable import Flux

/// Snapshot-free view checks for period navigation: the header's visibility
/// gate and label formats, the "Current" button gate, the stats card's
/// coverage subtitle, and the empty-past-period rendering split.
@MainActor @Suite(.serialized)
struct HistoryPeriodNavigationViewTests {
    // MARK: - Header visibility gate (req 1.5)

    @Test
    func displayedPeriodIsNilForFixedRangesSoTheHeaderIsHidden() async throws {
        let viewModel = try makeViewModel()
        await viewModel.loadHistory(range: .days(7))

        #expect(viewModel.displayedPeriod == nil)
    }

    @Test
    func displayedPeriodExistsForWeekAndMonthRanges() async throws {
        let viewModel = try makeViewModel()

        await viewModel.loadHistory(range: .weekToDate)
        let week = try #require(viewModel.displayedPeriod)
        #expect(week.startDateString == "2026-04-13")

        await viewModel.selectRange(.monthToDate)
        let month = try #require(viewModel.displayedPeriod)
        #expect(month.startDateString == "2026-04-01")
    }

    // MARK: - "Current" button gate (req 2.1/2.2, Decision 8)

    @Test
    func currentButtonGateFollowsViewedPeriod() async throws {
        let viewModel = try makeViewModel()
        await viewModel.loadHistory(range: .weekToDate)

        // Current period: the button is hidden, the next chevron disabled.
        #expect(viewModel.isViewingCurrentPeriod)
        #expect(viewModel.periodAnchor == nil)

        await viewModel.navigatePrevious()
        #expect(!viewModel.isViewingCurrentPeriod)
        #expect(viewModel.periodAnchor != nil)
    }

    // MARK: - Period label formats (req 4.1)

    @Test
    func weekLabelWithinOneMonthUsesCompactForm() {
        let period = HistoryPeriod.week(containing: sydneyDate(2026, 4, 15), firstWeekday: 2)
        #expect(HistoryPeriodHeader.label(for: .weekToDate, period: period) == "Apr 13 – 19")
    }

    @Test
    func weekLabelAcrossMonthsNamesBothMonths() {
        // Monday-start week containing Thu 2026-04-30 is Apr 27 – May 3.
        let period = HistoryPeriod.week(containing: sydneyDate(2026, 4, 30), firstWeekday: 2)
        #expect(HistoryPeriodHeader.label(for: .weekToDate, period: period) == "Apr 27 – May 3")
    }

    @Test
    func monthLabelShowsMonthAndYear() {
        let period = HistoryPeriod.month(containing: sydneyDate(2026, 5, 10))
        #expect(HistoryPeriodHeader.label(for: .monthToDate, period: period) == "May 2026")
    }

    // MARK: - Stats card coverage subtitle (req 6.2, 6.3)

    @Test
    func subtitleShowsCoverageOnlyForPartialPastPeriods() {
        #expect(
            HistoryStatsOverviewCard.periodCoverageSubtitle(dayCount: 11, periodDays: 30) == "11 of 30 days"
        )
        #expect(
            HistoryStatsOverviewCard.periodCoverageSubtitle(dayCount: 30, periodDays: 30) == nil,
            "a fully recorded past period needs no indicator"
        )
        #expect(
            HistoryStatsOverviewCard.periodCoverageSubtitle(dayCount: 11, periodDays: nil) == nil,
            "the current period is unchanged (req 6.3)"
        )
    }

    // MARK: - Empty past period keeps the cards rendered (req 1.6)

    @Test
    func emptyPastPeriodReservesFullAxisInsteadOfEmptyState() async throws {
        let viewModel = try makeViewModel()
        await viewModel.loadHistory(range: .monthToDate)
        await viewModel.navigatePrevious()

        // The view keys the replace-everything emptyState off this flag being
        // false; here it is true, so the cards stay rendered with the
        // scaffold axis spanning the whole past month.
        #expect(viewModel.showsEmptyPeriodNotice)
        let domain = try #require(viewModel.chartDomain)
        #expect(domain.slotDates.count == 31)
        #expect(domain.slotDates.first == sydneyDate(2026, 3, 1))
    }

    // MARK: - Helpers

    /// 02:00 UTC on 2026-04-15 = 12:00 AEST Wednesday; Monday-start weeks.
    private func makeViewModel() throws -> HistoryViewModel {
        let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
        let container = try ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
        let now = Calendar(identifier: .gregorian).date(from: DateComponents(
            timeZone: TimeZone(secondsFromGMT: 0),
            year: 2026, month: 4, day: 15, hour: 2, minute: 0
        ))!
        return HistoryViewModel(
            apiClient: EmptyHistoryAPIClient(),
            modelContext: ModelContext(container),
            nowProvider: { now },
            firstWeekdayProvider: { 2 }
        )
    }

    private func sydneyDate(_ year: Int, _ month: Int, _ day: Int) -> Date {
        DateFormatting.sydneyCalendar.date(from: DateComponents(
            timeZone: DateFormatting.sydneyTimeZone,
            year: year, month: month, day: day
        ))!
    }
}

/// Succeeds with an empty day set for both query forms.
private final class EmptyHistoryAPIClient: FluxAPIClient, @unchecked Sendable {
    func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    func fetchHistory(days _: Int) async throws -> HistoryResponse { HistoryResponse(days: []) }
    func fetchHistory(query _: HistoryQuery) async throws -> HistoryResponse { HistoryResponse(days: []) }
    func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }
    func saveNote(date _: String, text _: String) async throws -> NoteResponse { throw FluxAPIError.notConfigured }
}
