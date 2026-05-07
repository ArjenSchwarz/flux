import FluxCore
import Foundation
import Testing
@testable import Flux

@Suite
struct HistoryStatsFormattersTests {
    // MARK: - socPercent

    @Test("(n) socPercent half-up rounding boundaries")
    func socPercentHalfUpBoundaries() {
        #expect(HistoryStatsFormatters.socPercent(11.5) == "12%")
        #expect(HistoryStatsFormatters.socPercent(11.4) == "11%")
        #expect(HistoryStatsFormatters.socPercent(0.5) == "1%")
        #expect(HistoryStatsFormatters.socPercent(99.5) == "100%")
    }

    @Test("(n′) socPercent finiteness guard returns em-dash")
    func socPercentNonFiniteReturnsDash() {
        #expect(HistoryStatsFormatters.socPercent(.nan) == "—")
        #expect(HistoryStatsFormatters.socPercent(.infinity) == "—")
        #expect(HistoryStatsFormatters.socPercent(-.infinity) == "—")
    }

    // MARK: - dateRange

    @Test("dateRange chronologically reversed input matches forward output")
    func dateRangeReversedMatchesForward() {
        let entries = [
            entry(dayID: "2026-04-30"),
            entry(dayID: "2026-05-06")
        ]
        let reversed = Array(entries.reversed())
        let forward = HistoryStatsFormatters.dateRange(entries: entries)
        let backward = HistoryStatsFormatters.dateRange(entries: reversed)
        #expect(forward == backward)
        #expect(forward == "Apr 30 – May 6")
    }

    @Test("dateRange single-day range returns one date")
    func dateRangeSingleDay() {
        let entries = [entry(dayID: "2026-04-30")]
        #expect(HistoryStatsFormatters.dateRange(entries: entries) == "Apr 30")
    }

    @Test("dateRange empty entries returns nil")
    func dateRangeEmptyReturnsNil() {
        #expect(HistoryStatsFormatters.dateRange(entries: []) == nil)
    }

    // MARK: - shortDate / dateWithTime

    @Test("shortDate returns MMM d Sydney time")
    func shortDateReturnsMonthDay() {
        let date = DateFormatting.parseDayDate("2026-04-28")!
        #expect(HistoryStatsFormatters.shortDate(from: date) == "Apr 28")
    }

    @Test("dateWithTime non-nil time returns MMM d at HH:mm Sydney")
    func dateWithTimeFormatsTime() {
        let date = DateFormatting.parseDayDate("2026-04-26")!
        // 20:14 UTC on 2026-04-25 is 06:14 AEST (UTC+10) on 2026-04-26.
        #expect(HistoryStatsFormatters.dateWithTime(from: date, time: "2026-04-25T20:14:00Z") == "Apr 26 at 06:14")
    }

    @Test("dateWithTime nil time falls back to shortDate")
    func dateWithTimeNilFallsBack() {
        let date = DateFormatting.parseDayDate("2026-04-26")!
        #expect(HistoryStatsFormatters.dateWithTime(from: date, time: nil) == "Apr 26")
    }

    // MARK: - Accessibility helpers

    @Test("accessibleKwh spells out unit")
    func accessibleKwhSpellsOutUnit() {
        #expect(HistoryStatsFormatters.accessibleKwh(98.4) == "98.4 kilowatt hours")
    }

    @Test("accessibleSocPercent spells out unit and rounds")
    func accessibleSocPercentSpellsOutUnit() {
        #expect(HistoryStatsFormatters.accessibleSocPercent(11.5) == "12 percent")
        #expect(HistoryStatsFormatters.accessibleSocPercent(.nan) == "no data")
    }

    // MARK: - Helpers

    private func entry(dayID: String) -> HistoryViewModel.SolarEntry {
        HistoryViewModel.SolarEntry(
            date: DateFormatting.parseDayDate(dayID)!,
            dayID: dayID,
            kwh: 0,
            isToday: false
        )
    }
}
