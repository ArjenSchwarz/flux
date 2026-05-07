import FluxCore
import Foundation
import SwiftData
import Testing
@testable import Flux

@MainActor @Suite
struct HistoryStatsOverviewCardTests {
    // MARK: - Labels

    @Test("Tile labels match the §2 specification")
    func labelsMatchSpec() {
        #expect(HistoryStatsOverviewCard.label(for: .totalUsage) == "Total usage")
        #expect(HistoryStatsOverviewCard.label(for: .totalSolar) == "Total solar")
        #expect(HistoryStatsOverviewCard.label(for: .exported) == "Exported")
        #expect(HistoryStatsOverviewCard.label(for: .peakImports) == "Peak imports")
        #expect(HistoryStatsOverviewCard.label(for: .avgNight) == "Avg night")
        #expect(HistoryStatsOverviewCard.label(for: .mostUsage) == "Most usage")
        #expect(HistoryStatsOverviewCard.label(for: .mostSolar) == "Most solar")
        #expect(HistoryStatsOverviewCard.label(for: .lowestSoc) == "Lowest SoC")
    }

    // MARK: - Em-dash placeholder per cohort

    @Test("Each tile renders em-dash on its own empty cohort")
    func emDashPerEmptyCohort() {
        let empty = HistoryViewModel.PeriodSummary.empty

        #expect(HistoryStatsOverviewCard.valueText(for: .totalUsage, summary: empty) == "—")
        #expect(HistoryStatsOverviewCard.valueText(for: .totalSolar, summary: empty) == "—")
        #expect(HistoryStatsOverviewCard.valueText(for: .exported, summary: empty) == "—")
        #expect(HistoryStatsOverviewCard.valueText(for: .peakImports, summary: empty) == "—")
        #expect(HistoryStatsOverviewCard.valueText(for: .avgNight, summary: empty) == "—")
        #expect(HistoryStatsOverviewCard.valueText(for: .mostUsage, summary: empty) == "—")
        #expect(HistoryStatsOverviewCard.valueText(for: .mostSolar, summary: empty) == "—")
        #expect(HistoryStatsOverviewCard.valueText(for: .lowestSoc, summary: empty) == "—")
    }

    @Test("Peak imports renders zero, not em-dash, when at least one day-with-offpeak has zero peak")
    func peakImportsZeroIsNotEmDash() {
        let summary = makeSummary(peakImportTotalKwh: 0, gridDayCount: 1)
        #expect(HistoryStatsOverviewCard.valueText(for: .peakImports, summary: summary) == "0.0 kWh")
    }

    // MARK: - Day-record date line

    @Test("Most usage / most solar tiles render MMM d date line")
    func dateLineForKwhRecords() {
        let date = parsedDay("2026-04-28")
        let summary = makeSummary(
            mostUsage: HistoryViewModel.DayKwhRecord(dayID: "2026-04-28", date: date, kwh: 18.7),
            mostSolar: HistoryViewModel.DayKwhRecord(dayID: "2026-04-28", date: date, kwh: 12.3)
        )
        #expect(HistoryStatsOverviewCard.dateLineText(for: .mostUsage, summary: summary) == "Apr 28")
        #expect(HistoryStatsOverviewCard.dateLineText(for: .mostSolar, summary: summary) == "Apr 28")
    }

    @Test("Lowest SoC date line includes time when socLowTime is non-nil")
    func lowestSocDateLineWithTime() {
        let date = parsedDay("2026-04-26")
        let summary = makeSummary(
            lowestSoc: HistoryViewModel.LowestSocRecord(
                dayID: "2026-04-26", date: date, soc: 12.0,
                socLowTime: "2026-04-25T20:14:00Z" // 06:14 AEST
            )
        )
        #expect(HistoryStatsOverviewCard.dateLineText(for: .lowestSoc, summary: summary) == "Apr 26 at 06:14")
    }

    @Test("Em-dash record tiles return nil dateLineText")
    func emDashRecordTilesHaveNoDateLine() {
        let empty = HistoryViewModel.PeriodSummary.empty
        #expect(HistoryStatsOverviewCard.dateLineText(for: .mostUsage, summary: empty) == nil)
        #expect(HistoryStatsOverviewCard.dateLineText(for: .mostSolar, summary: empty) == nil)
        #expect(HistoryStatsOverviewCard.dateLineText(for: .lowestSoc, summary: empty) == nil)
    }

    @Test("Non-record tiles have no dateLineText regardless of summary")
    func nonRecordTilesHaveNoDateLine() {
        let summary = makeSummary(solarTotalKwh: 50.0, solarDayCount: 5)
        #expect(HistoryStatsOverviewCard.dateLineText(for: .totalUsage, summary: summary) == nil)
        #expect(HistoryStatsOverviewCard.dateLineText(for: .totalSolar, summary: summary) == nil)
        #expect(HistoryStatsOverviewCard.dateLineText(for: .exported, summary: summary) == nil)
        #expect(HistoryStatsOverviewCard.dateLineText(for: .peakImports, summary: summary) == nil)
        #expect(HistoryStatsOverviewCard.dateLineText(for: .avgNight, summary: summary) == nil)
    }

    // MARK: - Tap-target invariant

    @Test("Em-dash tiles are non-tappable")
    func emDashTilesNonTappable() {
        let empty = HistoryViewModel.PeriodSummary.empty
        for tile in HistoryStatsOverviewCard.TileKey.allCases {
            #expect(HistoryStatsOverviewCard.isTappable(tile: tile, summary: empty) == false)
        }
    }

    @Test("Total / Avg / Peak / Exported tiles never tappable, even when populated")
    func nonRecordTilesNeverTappable() {
        let summary = makeSummary(
            solarTotalKwh: 100, solarDayCount: 5,
            peakImportTotalKwh: 12, gridDayCount: 5
        )
        let nonRecord: [HistoryStatsOverviewCard.TileKey] =
            [.totalUsage, .totalSolar, .exported, .peakImports, .avgNight]
        for tile in nonRecord {
            #expect(HistoryStatsOverviewCard.isTappable(tile: tile, summary: summary) == false)
        }
    }

    @Test("Populated record tiles are tappable")
    func populatedRecordTilesAreTappable() {
        let date = parsedDay("2026-04-13")
        let summary = makeSummary(
            mostUsage: HistoryViewModel.DayKwhRecord(dayID: "2026-04-13", date: date, kwh: 7.5),
            mostSolar: HistoryViewModel.DayKwhRecord(dayID: "2026-04-13", date: date, kwh: 12.0),
            lowestSoc: HistoryViewModel.LowestSocRecord(
                dayID: "2026-04-13", date: date, soc: 18.5, socLowTime: nil
            )
        )
        #expect(HistoryStatsOverviewCard.isTappable(tile: .mostUsage, summary: summary) == true)
        #expect(HistoryStatsOverviewCard.isTappable(tile: .mostSolar, summary: summary) == true)
        #expect(HistoryStatsOverviewCard.isTappable(tile: .lowestSoc, summary: summary) == true)
    }

    // MARK: - Accessibility labels

    @Test("Em-dash tiles announce 'no data'")
    func emDashAccessibilityLabel() {
        let empty = HistoryViewModel.PeriodSummary.empty
        #expect(HistoryStatsOverviewCard.accessibilityLabel(tile: .peakImports, summary: empty)
            == "Peak imports, no data")
        #expect(HistoryStatsOverviewCard.accessibilityLabel(tile: .lowestSoc, summary: empty)
            == "Lowest SoC, no data")
    }

    @Test("Stat tile spells out kilowatt hours")
    func statTileAccessibilityLabel() {
        let summary = makeSummary(solarTotalKwh: 98.4, solarDayCount: 5)
        #expect(HistoryStatsOverviewCard.accessibilityLabel(tile: .totalSolar, summary: summary)
            == "Total solar, 98.4 kilowatt hours")
    }

    @Test("Day-record kWh tile spells out unit and uses long month")
    func kwhRecordAccessibilityLabel() {
        let date = parsedDay("2026-04-28")
        let summary = makeSummary(
            mostUsage: HistoryViewModel.DayKwhRecord(dayID: "2026-04-28", date: date, kwh: 18.7)
        )
        #expect(HistoryStatsOverviewCard.accessibilityLabel(tile: .mostUsage, summary: summary)
            == "Most usage, 18.7 kilowatt hours, April 28")
    }

    @Test("Lowest SoC accessibility label spells percent and includes time")
    func lowestSocAccessibilityLabel() {
        let date = parsedDay("2026-04-26")
        let summary = makeSummary(
            lowestSoc: HistoryViewModel.LowestSocRecord(
                dayID: "2026-04-26", date: date, soc: 12.0,
                socLowTime: "2026-04-25T20:14:00Z"
            )
        )
        #expect(HistoryStatsOverviewCard.accessibilityLabel(tile: .lowestSoc, summary: summary)
            == "Lowest SoC, 12 percent, April 26 at 06:14")
    }

    // MARK: - Tap action plumbing

    @Test("Tapping mostUsage tile invokes onSelect with the record's dayID")
    func tapInvokesOnSelectWithDayID() {
        let date = parsedDay("2026-04-13")
        let summary = makeSummary(
            mostUsage: HistoryViewModel.DayKwhRecord(dayID: "2026-04-13", date: date, kwh: 7.5)
        )
        var captured: String?
        let action = HistoryStatsOverviewCard.tapAction(
            for: .mostUsage, summary: summary, onSelect: { captured = $0 }
        )
        action?()
        #expect(captured == "2026-04-13")
    }

    @Test("Tapping a non-tappable tile returns nil action")
    func nonTappableTilesReturnNilAction() {
        let empty = HistoryViewModel.PeriodSummary.empty
        let action = HistoryStatsOverviewCard.tapAction(
            for: .mostUsage, summary: empty, onSelect: { _ in }
        )
        #expect(action == nil)
    }

    @Test("Lowest SoC tap when record is today still routes through selectDay")
    func lowestSocTapWhenTodayRoutesSelection() async throws {
        let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
        let container = try ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
        let modelContext = ModelContext(container)

        let today = DayEnergy(
            date: "2026-04-16",
            epv: 5.0, eInput: 1.0, eOutput: 0.5, eCharge: 0.5, eDischarge: 0.5,
            socLow: 22.0, socLowTime: nil
        )
        let apiClient = StaticHistoryClient(days: [today])
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)
        await viewModel.loadHistory(days: 7)

        let action = HistoryStatsOverviewCard.tapAction(
            for: .lowestSoc, summary: viewModel.summary,
            onSelect: { dayID in
                if let day = viewModel.days.first(where: { $0.date == dayID }) {
                    viewModel.selectDay(day)
                }
            }
        )
        action?()

        #expect(viewModel.selectedDay?.date == "2026-04-16")
    }

    // MARK: - Helpers

    private func parsedDay(_ dayID: String) -> Date {
        DateFormatting.parseDayDate(dayID)!
    }

    private func makeSummary(
        solarTotalKwh: Double = 0,
        solarDayCount: Int = 0,
        peakImportTotalKwh: Double = 0,
        offpeakImportTotalKwh: Double = 0,
        exportTotalKwh: Double = 0,
        gridDayCount: Int = 0,
        chargeTotalKwh: Double = 0,
        dischargeTotalKwh: Double = 0,
        batteryDayCount: Int = 0,
        dailyUsageTotalKwh: Double = 0,
        dailyUsageDayCount: Int = 0,
        dailyUsageLargestKind: DailyUsageBlock.Kind? = nil,
        dailyUsageLargestKindTotalKwh: Double = 0,
        nightTotalKwh: Double = 0,
        nightBlockDayCount: Int = 0,
        mostUsage: HistoryViewModel.DayKwhRecord? = nil,
        mostSolar: HistoryViewModel.DayKwhRecord? = nil,
        lowestSoc: HistoryViewModel.LowestSocRecord? = nil
    ) -> HistoryViewModel.PeriodSummary {
        HistoryViewModel.PeriodSummary(
            solarTotalKwh: solarTotalKwh,
            solarDayCount: solarDayCount,
            peakImportTotalKwh: peakImportTotalKwh,
            offpeakImportTotalKwh: offpeakImportTotalKwh,
            exportTotalKwh: exportTotalKwh,
            gridDayCount: gridDayCount,
            chargeTotalKwh: chargeTotalKwh,
            dischargeTotalKwh: dischargeTotalKwh,
            batteryDayCount: batteryDayCount,
            dailyUsageTotalKwh: dailyUsageTotalKwh,
            dailyUsageDayCount: dailyUsageDayCount,
            dailyUsageLargestKind: dailyUsageLargestKind,
            dailyUsageLargestKindTotalKwh: dailyUsageLargestKindTotalKwh,
            nightTotalKwh: nightTotalKwh,
            nightBlockDayCount: nightBlockDayCount,
            mostUsageDay: mostUsage,
            mostSolarDay: mostSolar,
            lowestSocDay: lowestSoc
        )
    }
}

private final class StaticHistoryClient: FluxAPIClient, @unchecked Sendable {
    let days: [DayEnergy]
    init(days: [DayEnergy]) { self.days = days }

    func fetchStatus() async throws -> StatusResponse { throw FluxAPIError.notConfigured }
    func fetchHistory(days _: Int) async throws -> HistoryResponse { HistoryResponse(days: days) }
    func fetchDay(date _: String) async throws -> DayDetailResponse { throw FluxAPIError.notConfigured }
    func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}
