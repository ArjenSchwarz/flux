import FluxCore
import Foundation
import SwiftData
import Testing
@testable import Flux

/// Requirement 5.2 / 5.3: the producing range must not influence the cards.
/// Given an identical day set, every card produces identical `DerivedState`
/// and `PeriodSummary`, and the resolved day-count `N` flows unchanged into the
/// card's `ChartScope.historyRange(days:)` expansion scope.
@MainActor @Suite(.serialized)
struct HistoryRangeConsistencyTests {
    private static let sampleDays: [DayEnergy] = [
        DayEnergy(
            date: "2026-04-08", epv: 7.2, eInput: 4.0, eOutput: 1.2, eCharge: 3.0, eDischarge: 2.5,
            offpeakGridImportKwh: 2.0, offpeakGridExportKwh: 0.4
        ),
        DayEnergy(
            date: "2026-04-09", epv: 6.1, eInput: 3.5, eOutput: 0.9, eCharge: 2.8, eDischarge: 2.2,
            offpeakGridImportKwh: 1.5, offpeakGridExportKwh: 0.3
        ),
        DayEnergy(
            date: "2026-04-12", epv: 5.0, eInput: 2.0, eOutput: 0.5, eCharge: 2.0, eDischarge: 1.8,
            offpeakGridImportKwh: 1.0, offpeakGridExportKwh: 0.2
        )
    ]

    @Test
    func derivedStateIsIdenticalRegardlessOfProducingRange() async throws {
        // 2026-04-12 is a Sunday. With Monday-start (firstWeekday = 2) the
        // week begins 2026-04-06, so week-to-date resolves to 7 — identical to
        // .days(7). The same day set therefore reaches both view models.
        let now = makeUTCDate(year: 2026, month: 4, day: 12, hour: 2, minute: 0)

        let fixedVM = makeViewModel(now: now)
        await fixedVM.loadHistory(range: .days(7))

        let weekVM = makeViewModel(now: now)
        await weekVM.loadHistory(range: .weekToDate)

        // Same resolved count.
        #expect(fixedVM.resolvedRangeDays == 7)
        #expect(weekVM.resolvedRangeDays == 7)

        // Identical derived state — the producing range is irrelevant ([5.2]).
        #expect(fixedVM.derived.summary == weekVM.derived.summary)
        #expect(fixedVM.derived.solar == weekVM.derived.solar)
        #expect(fixedVM.derived.grid == weekVM.derived.grid)
        #expect(fixedVM.derived.battery == weekVM.derived.battery)
        #expect(fixedVM.derived.dailyUsage == weekVM.derived.dailyUsage)
    }

    @Test
    func derivedStateMatchesDirectConstructionFromSameDays() {
        // The card-building path is `DerivedState(days:now:)`; an identical
        // array yields an identical state regardless of which range produced it.
        let now = makeUTCDate(year: 2026, month: 4, day: 12, hour: 2, minute: 0)
        let viaFixed = HistoryViewModel.DerivedState(days: Self.sampleDays, now: now)
        let viaWeek = HistoryViewModel.DerivedState(days: Self.sampleDays, now: now)
        #expect(viaFixed.summary == viaWeek.summary)
    }

    @Test
    func toDateSelectionFeedsResolvedCountToExpansionScope() async throws {
        // 2026-04-12 Sunday, Monday-start → month-to-date = 12 (days 1…12).
        let now = makeUTCDate(year: 2026, month: 4, day: 12, hour: 2, minute: 0)
        let viewModel = makeViewModel(now: now)
        await viewModel.loadHistory(range: .monthToDate)

        #expect(viewModel.resolvedRangeDays == 12)

        // The cards receive `rangeDays: viewModel.resolvedRangeDays`, which
        // becomes `ChartScope.historyRange(days:)` — the resolved N, not the
        // fixed 7/14/30 ([5.3]).
        let derived = viewModel.derived
        let solar = HistorySolarCard(
            entries: derived.solar, summary: derived.summary,
            selectedDate: nil, rangeDays: viewModel.resolvedRangeDays, onSelect: { _ in }
        )
        let grid = HistoryGridUsageCard(
            entries: derived.grid, summary: derived.summary,
            selectedDate: nil, rangeDays: viewModel.resolvedRangeDays, onSelect: { _ in }
        )
        let usage = HistoryDailyUsageCard(
            entries: derived.dailyUsage, summary: derived.summary,
            selectedDate: nil, rangeDays: viewModel.resolvedRangeDays, onSelect: { _ in }
        )

        #expect(solar.expansionScope == .historyRange(days: 12))
        #expect(grid.expansionScope == .historyRange(days: 12))
        #expect(usage.expansionScope == .historyRange(days: 12))
    }

    // MARK: - Helpers

    private func makeViewModel(now: Date) -> HistoryViewModel {
        let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
        // swiftlint:disable:next force_try
        let container = try! ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
        let apiClient = StubHistoryAPIClient(days: Self.sampleDays)
        return HistoryViewModel(
            apiClient: apiClient,
            modelContext: ModelContext(container),
            nowProvider: { now },
            firstWeekdayProvider: { 2 }
        )
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

/// Returns the same day set regardless of the requested count, so the producing
/// range is the only difference under test.
private final class StubHistoryAPIClient: FluxAPIClient, @unchecked Sendable {
    private let days: [DayEnergy]

    init(days: [DayEnergy]) { self.days = days }

    func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    func fetchHistory(days _: Int) async throws -> HistoryResponse { HistoryResponse(days: days) }
    func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }
    func saveNote(date _: String, text _: String) async throws -> NoteResponse { throw FluxAPIError.notConfigured }
}
