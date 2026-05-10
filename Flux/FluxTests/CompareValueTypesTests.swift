import FluxCore
import Foundation
import Testing
@testable import Flux

// Covers the behaviour of the Compare value types (ComparePeriod,
// ComparisonState, ComparisonSnapshot, SublineContent) introduced in
// the stat-comparisons spec. These types are pure value types with
// no SwiftUI / view dependencies, so the tests sit in the host-app
// target like the rest of the model-level tests.

// MARK: - ComparePeriod

@Suite
struct ComparePeriodTests {
    @Test
    func rawValuesAreStable() {
        // Stored under @AppStorage as rawValue; the strings are part of
        // the persisted contract.
        #expect(ComparePeriod.yesterday.rawValue == "yesterday")
        #expect(ComparePeriod.sevenDaysAgo.rawValue == "sevenDaysAgo")
    }

    @Test
    func dayOffsetMatchesDesignTable() {
        #expect(ComparePeriod.yesterday.dayOffset == -1)
        #expect(ComparePeriod.sevenDaysAgo.dayOffset == -7)
    }

    @Test
    func displayNamesMatchRequirementCopy() {
        // AC 2.1 — chip copy.
        #expect(ComparePeriod.yesterday.displayName == "Yesterday")
        #expect(ComparePeriod.sevenDaysAgo.displayName == "7 days ago")
    }

    @Test
    func parseOrDefaultReturnsYesterdayForNil() {
        #expect(ComparePeriod.parseOrDefault(nil) == .yesterday)
    }

    @Test
    func parseOrDefaultReturnsYesterdayForEmptyString() {
        #expect(ComparePeriod.parseOrDefault("") == .yesterday)
    }

    @Test
    func parseOrDefaultReturnsYesterdayForUnknownRawValue() {
        // Forward-compatibility: a future build's value (e.g. "lastMonth")
        // read by the current build must fall back rather than crash.
        #expect(ComparePeriod.parseOrDefault("lastMonth") == .yesterday)
        #expect(ComparePeriod.parseOrDefault("not-a-period") == .yesterday)
    }

    @Test
    func parseOrDefaultReturnsExactCaseForKnownRawValues() {
        #expect(ComparePeriod.parseOrDefault("yesterday") == .yesterday)
        #expect(ComparePeriod.parseOrDefault("sevenDaysAgo") == .sevenDaysAgo)
    }
}

// MARK: - ComparisonState

@Suite
struct ComparisonStateTests {
    @Test
    func offIsNotUnavailable() {
        #expect(ComparisonState.off.isUnavailable == false)
    }

    @Test
    func loadingIsNotUnavailable() {
        #expect(ComparisonState.loading(date: "2026-05-09").isUnavailable == false)
    }

    @Test
    func readyIsNotUnavailable() {
        let snapshot = ComparisonSnapshot(
            date: "2026-05-09",
            solar: 14.0,
            gridImport: 1.2,
            gridExport: 3.4,
            batteryCharge: 5.0,
            batteryDischarge: 4.0,
            offpeakGridImport: 0.5,
            dailyUsage: nil
        )
        #expect(ComparisonState.ready(snapshot, period: .yesterday).isUnavailable == false)
    }

    @Test
    func unavailableIsUnavailable() {
        #expect(ComparisonState.unavailable(period: .yesterday).isUnavailable == true)
        #expect(ComparisonState.unavailable(period: .sevenDaysAgo).isUnavailable == true)
    }
}

// MARK: - ComparisonSnapshot.from(date:response:)

@Suite
struct ComparisonSnapshotFromResponseTests {
    @Test
    func snapshotFromSummaryAndDailyUsageIsReady() {
        let response = makeResponse(
            summary: makeSummary(values: .all),
            dailyUsage: DailyUsage(blocks: [
                makeBlock(kind: .morningPeak, totalKwh: 2.4, solarKwh: 1.0)
            ])
        )

        let snapshot = ComparisonSnapshot.from(date: "2026-05-09", response: response)
        let unwrapped = try? #require(snapshot)
        #expect(unwrapped?.date == "2026-05-09")
        #expect(unwrapped?.solar == 14.8)
        #expect(unwrapped?.gridImport == 4.2)
        #expect(unwrapped?.gridExport == 1.1)
        #expect(unwrapped?.batteryCharge == 6.0)
        #expect(unwrapped?.batteryDischarge == 3.5)
        #expect(unwrapped?.offpeakGridImport == 0.6)
        #expect(unwrapped?.dailyUsage?.blocks.count == 1)
    }

    @Test
    func snapshotFromEmptyResponseReturnsNil() {
        let response = makeResponse(summary: nil, dailyUsage: nil)
        #expect(ComparisonSnapshot.from(date: "2026-05-09", response: response) == nil)
    }

    @Test
    func snapshotFromAllNilSummaryStillReady() {
        // Per design: a present-but-all-nil-SR-fields summary stays .ready;
        // per-row fallback handles each row.
        let response = makeResponse(
            summary: makeSummary(values: .allNil),
            dailyUsage: nil
        )

        let snapshot = ComparisonSnapshot.from(date: "2026-05-09", response: response)
        let unwrapped = try? #require(snapshot)
        #expect(unwrapped?.solar == nil)
        #expect(unwrapped?.gridImport == nil)
        #expect(unwrapped?.gridExport == nil)
        #expect(unwrapped?.batteryCharge == nil)
        #expect(unwrapped?.batteryDischarge == nil)
        #expect(unwrapped?.offpeakGridImport == nil)
        #expect(unwrapped?.dailyUsage == nil)
    }

    @Test
    func snapshotWithSummaryButNoDailyUsageIsReady() {
        // Partial availability: SummaryBlock rows render their deltas;
        // Five-Block rows individually fall back via per-block helpers.
        let response = makeResponse(
            summary: makeSummary(values: .partial),
            dailyUsage: nil
        )

        let snapshot = ComparisonSnapshot.from(date: "2026-05-09", response: response)
        #expect(snapshot != nil)
        #expect(snapshot?.solar == 11.0)
        #expect(snapshot?.dailyUsage == nil)
    }

    @Test
    func snapshotWithDailyUsageButNoSummaryIsReady() {
        // Mirror of the partial-availability case: dailyUsage present,
        // summary missing. Snapshot is non-nil so Five-Block deltas render;
        // SummaryBlock rows individually fall back.
        let response = makeResponse(
            summary: nil,
            dailyUsage: DailyUsage(blocks: [makeBlock(kind: .offPeak, totalKwh: 1.0, solarKwh: nil)])
        )

        let snapshot = ComparisonSnapshot.from(date: "2026-05-09", response: response)
        #expect(snapshot != nil)
        #expect(snapshot?.solar == nil)
        #expect(snapshot?.dailyUsage?.blocks.count == 1)
    }
}

// MARK: - ComparisonSnapshot derived fields

@Suite
struct ComparisonSnapshotDerivedFieldTests {
    @Test
    func houseUsedMatchesHouseholdLoadFormula() {
        let snapshot = ComparisonSnapshot(
            date: "2026-05-09",
            solar: 14.0,
            gridImport: 2.0,
            gridExport: 1.0,
            batteryCharge: 5.0,
            batteryDischarge: 4.0,
            offpeakGridImport: nil,
            dailyUsage: nil
        )
        let expected = HouseholdLoad.kwh(
            solar: 14.0, gridImport: 2.0, gridExport: 1.0,
            batteryCharge: 5.0, batteryDischarge: 4.0
        )
        #expect(snapshot.houseUsed == expected)
        #expect(snapshot.houseUsed == 14.0) // 14 + 2 + 4 - 1 - 5 = 14
    }

    @Test
    func houseUsedReturnsNilForEachMissingInput() {
        // Drive the same nil-sensitivity check across every input by
        // replacing each one in turn.
        let basis = ComparisonSnapshot.fixture()
        #expect(basis.houseUsed != nil)

        #expect(basis.with(solar: nil).houseUsed == nil)
        #expect(basis.with(gridImport: nil).houseUsed == nil)
        #expect(basis.with(gridExport: nil).houseUsed == nil)
        #expect(basis.with(batteryCharge: nil).houseUsed == nil)
        #expect(basis.with(batteryDischarge: nil).houseUsed == nil)
    }

    @Test
    func peakGridImportSubtractsOffpeakFromTotal() {
        let snapshot = ComparisonSnapshot(
            date: "2026-05-09",
            solar: nil,
            gridImport: 4.2,
            gridExport: nil,
            batteryCharge: nil,
            batteryDischarge: nil,
            offpeakGridImport: 1.2,
            dailyUsage: nil
        )
        #expect(snapshot.peakGridImport == 3.0)
    }

    @Test
    func peakGridImportClampsToZeroWhenOffpeakExceedsTotal() {
        // Defensive: shouldn't happen in practice but DayEnergy.peakGridImportKwh
        // uses max(0, ...). Stay consistent.
        let snapshot = ComparisonSnapshot(
            date: "2026-05-09",
            solar: nil,
            gridImport: 0.5,
            gridExport: nil,
            batteryCharge: nil,
            batteryDischarge: nil,
            offpeakGridImport: 1.2,
            dailyUsage: nil
        )
        #expect(snapshot.peakGridImport == 0)
    }

    @Test
    func peakGridImportReturnsNilWhenGridImportMissing() {
        let snapshot = ComparisonSnapshot(
            date: "2026-05-09",
            solar: nil,
            gridImport: nil,
            gridExport: nil,
            batteryCharge: nil,
            batteryDischarge: nil,
            offpeakGridImport: 1.2,
            dailyUsage: nil
        )
        #expect(snapshot.peakGridImport == nil)
    }

    @Test
    func peakGridImportReturnsNilWhenOffpeakMissing() {
        // Decision 10 says production data always carries the off-peak split,
        // but the wire type is optional so the guard still fires defensively.
        let snapshot = ComparisonSnapshot(
            date: "2026-05-09",
            solar: nil,
            gridImport: 4.2,
            gridExport: nil,
            batteryCharge: nil,
            batteryDischarge: nil,
            offpeakGridImport: nil,
            dailyUsage: nil
        )
        #expect(snapshot.peakGridImport == nil)
    }
}

// MARK: - SublineContent

@Suite
struct SublineContentTests {
    @Test
    func equalityCoversEachCase() {
        // SublineContent drives view layout decisions; equality is used by
        // diffing in SwiftUI and by panel-integration tests, so pin it here.
        #expect(SublineContent.hidden == SublineContent.hidden)
        #expect(SublineContent.reserved == SublineContent.reserved)
        #expect(SublineContent.text("▲ 0.6 kWh") == SublineContent.text("▲ 0.6 kWh"))
        #expect(SublineContent.text("▲ 0.6 kWh") != SublineContent.text("▼ 0.6 kWh"))
        #expect(SublineContent.hidden != SublineContent.reserved)
    }
}

// MARK: - Fixtures

private enum SummaryFieldShape {
    case all
    case allNil
    case partial
}

private func makeResponse(
    summary: DaySummary?,
    dailyUsage: DailyUsage?
) -> DayDetailResponse {
    DayDetailResponse(
        date: "2026-05-09",
        readings: [],
        summary: summary,
        peakPeriods: nil,
        dailyUsage: dailyUsage
    )
}

private func makeSummary(values: SummaryFieldShape) -> DaySummary {
    switch values {
    case .all:
        return DaySummary(
            epv: 14.8, eInput: 4.2, eOutput: 1.1,
            eCharge: 6.0, eDischarge: 3.5,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: 0.6, offpeakGridExportKwh: nil
        )
    case .allNil:
        return DaySummary(
            epv: nil, eInput: nil, eOutput: nil,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: nil, offpeakGridExportKwh: nil
        )
    case .partial:
        return DaySummary(
            epv: 11.0, eInput: 2.0, eOutput: 0.5,
            eCharge: 4.0, eDischarge: 3.0,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: 0.4, offpeakGridExportKwh: nil
        )
    }
}

private func makeBlock(
    kind: DailyUsageBlock.Kind,
    totalKwh: Double,
    solarKwh: Double?
) -> DailyUsageBlock {
    DailyUsageBlock(
        kind: kind,
        start: "2026-05-09T00:00:00+10:00",
        end: "2026-05-09T05:00:00+10:00",
        totalKwh: totalKwh,
        solarKwh: solarKwh,
        averageKwhPerHour: nil,
        percentOfDay: 20,
        status: .complete,
        boundarySource: .readings
    )
}

private extension ComparisonSnapshot {
    static func fixture() -> ComparisonSnapshot {
        ComparisonSnapshot(
            date: "2026-05-09",
            solar: 14.0,
            gridImport: 2.0,
            gridExport: 1.0,
            batteryCharge: 5.0,
            batteryDischarge: 4.0,
            offpeakGridImport: nil,
            dailyUsage: nil
        )
    }

    func with(
        solar: Double?? = nil,
        gridImport: Double?? = nil,
        gridExport: Double?? = nil,
        batteryCharge: Double?? = nil,
        batteryDischarge: Double?? = nil
    ) -> ComparisonSnapshot {
        ComparisonSnapshot(
            date: date,
            solar: solar ?? self.solar,
            gridImport: gridImport ?? self.gridImport,
            gridExport: gridExport ?? self.gridExport,
            batteryCharge: batteryCharge ?? self.batteryCharge,
            batteryDischarge: batteryDischarge ?? self.batteryDischarge,
            offpeakGridImport: offpeakGridImport,
            dailyUsage: dailyUsage
        )
    }
}
