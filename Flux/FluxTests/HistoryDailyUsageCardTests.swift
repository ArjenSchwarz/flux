import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor
@Suite
struct HistoryDailyUsageCardTests {
    @Test
    func opacityIsHalfForTodayAndFullForOtherDays() {
        let today = makeEntry(dayID: "2026-04-15", isToday: true)
        let yesterday = makeEntry(dayID: "2026-04-14", isToday: false)

        #expect(HistoryDailyUsageCard.opacity(for: today) == 0.5)
        #expect(HistoryDailyUsageCard.opacity(for: yesterday) == 1.0)
    }

    @Test
    func placeholderCopyMatchesPattern() {
        let summary = makeSummary(largest: nil, avg: nil, totalKwh: 0, dayCount: 0)
        #expect(HistoryDailyUsageCard.placeholderCopy == "No load breakdown available for this range.")
        #expect(HistoryDailyUsageCard.shouldShowPlaceholder(summary: summary))
        #expect(HistoryDailyUsageCard.kpi(for: summary) == "—")
        #expect(HistoryDailyUsageCard.subtitle(for: summary) == nil)
    }

    @Test
    func chartRendersWhenOnlyTodayHasBlocks() {
        // Today has a breakdown but no complete day does: the chart shows
        // today's bar (display count > 0), so the placeholder is suppressed.
        // The KPI/subtitle stay em-dash because the per-day average needs a
        // complete day.
        let summary = makeSummary(largest: nil, avg: nil, totalKwh: 4.0, dayCount: 0, displayDayCount: 1)
        #expect(HistoryDailyUsageCard.shouldShowPlaceholder(summary: summary) == false)
        #expect(HistoryDailyUsageCard.kpi(for: summary) == "—")
        #expect(HistoryDailyUsageCard.subtitle(for: summary) == nil)
    }

    @Test
    func subtitleFormatsLargestKindAndAverage() {
        let summary = makeSummary(largest: .evening, avg: 3.4, totalKwh: 17.0, dayCount: 5, eveningSum: 17.0)
        #expect(HistoryDailyUsageCard.subtitle(for: summary) == "Evening largest at 3.4 kWh/day average")
    }

    @Test
    func kpiFormatsAverageKwh() {
        let summary = makeSummary(largest: .evening, avg: 3.4, totalKwh: 17.0, dayCount: 5, eveningSum: 17.0)
        #expect(HistoryDailyUsageCard.kpi(for: summary) == "3.4 kWh")
    }

    @Test
    func accessibilitySummaryTieBreakMatchesPeriodSummary() {
        // All five blocks tied at 1.0 kWh: per AC 1.8 the earliest chronological
        // kind (.night) wins, matching `PeriodSummary.dailyUsageLargestKind`.
        let entry = makeEntry(dayID: "2026-04-14", isToday: false)
        #expect(entry.accessibilitySummary.contains("Night largest"))
    }

    @Test
    func accessibilityElementsCountMatchesEntries() {
        let entries = [
            makeEntry(dayID: "2026-04-13", isToday: false),
            makeEntry(dayID: "2026-04-14", isToday: false),
            makeEntry(dayID: "2026-04-15", isToday: true)
        ]
        let summaries = entries.map(\.accessibilitySummary)
        #expect(summaries.count == entries.count, "one a11y element per day, not per BarMark")
    }

    private func makeEntry(dayID: String, isToday: Bool) -> HistoryViewModel.DailyUsageEntry {
        let date = DateFormatting.parseDayDate(dayID) ?? Date()
        return HistoryViewModel.DailyUsageEntry(
            date: date,
            dayID: dayID,
            blocks: [
                .init(kind: .night, totalKwh: 1.0),
                .init(kind: .morningPeak, totalKwh: 1.0),
                .init(kind: .offPeak, totalKwh: 1.0),
                .init(kind: .afternoonPeak, totalKwh: 1.0),
                .init(kind: .evening, totalKwh: 1.0)
            ],
            stackedTotalKwh: 5.0,
            isToday: isToday
        )
    }

    // swiftlint:disable:next function_default_parameter_at_end
    private func makeSummary(
        largest: DailyUsageBlock.Kind?,
        avg _: Double?,
        totalKwh: Double,
        dayCount: Int,
        displayDayCount: Int = 0,
        eveningSum: Double = 0
    ) -> HistoryViewModel.PeriodSummary {
        HistoryViewModel.PeriodSummary(
            solarTotalKwh: 0,
            solarCompleteTotalKwh: 0,
            solarDayCount: 0,
            dayCount: 0,
            peakImportTotalKwh: 0,
            offpeakImportTotalKwh: 0,
            exportTotalKwh: 0,
            gridDayCount: 0,
            chargeTotalKwh: 0,
            dischargeTotalKwh: 0,
            dischargeCompleteTotalKwh: 0,
            batteryDayCount: 0,
            dailyUsageTotalKwh: totalKwh,
            dailyUsageCompleteTotalKwh: totalKwh,
            dailyUsageDayCount: dayCount,
            dailyUsageDisplayDayCount: displayDayCount,
            dailyUsageLargestKind: largest,
            dailyUsageLargestKindTotalKwh: eveningSum,
            nightTotalKwh: 0,
            nightBlockDayCount: 0,
            mostUsageDay: nil,
            mostSolarDay: nil,
            lowestSocDay: nil
        )
    }
}
