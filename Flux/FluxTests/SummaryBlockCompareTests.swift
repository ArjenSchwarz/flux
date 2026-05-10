#if canImport(UIKit)
import SwiftUI
import Testing
import UIKit
@testable import Flux

// SummaryBlock's compare integration is verified through two layers:
//
// 1. `SummaryBlockCompareMapping` (per-row pure helpers) — exhaustive
//    state coverage at the value-type level. The block calls these
//    helpers for each SR row, so per-row deltas, fallbacks, and
//    accessibility overrides are pinned by these tests.
//
// 2. SummaryBlock view-level — off-state height byte-identity vs the
//    pre-feature block (Decision 16 scope), since the off state must
//    not introduce any reserved sub-line slots.
//
// Inspecting the rendered view tree of a non-trivial SwiftUI view in
// unit tests is fragile (UILabels are not always materialized when the
// host has no window scene). The mapping layer captures the same
// contract without that fragility.
@MainActor
@Suite
struct SummaryBlockCompareTests {
    // MARK: - off-state height identity

    @Test
    func offStateBlockMatchesPreFeatureHeight() {
        let pre = preFeatureBlock()
        let off = compareBlock(compare: .off)
        #expect(measureHeight(for: pre) == measureHeight(for: off))
    }

    // MARK: - SummaryBlockCompareMapping.valueSub

    @Test
    func valueSubReturnsHiddenWhenCompareIsOff() {
        #expect(
            SummaryBlockCompareMapping.valueSub(current: 14.8, comparison: 13.6, compare: .off)
            == .hidden
        )
    }

    @Test
    func valueSubReturnsReservedWhenCompareIsLoading() {
        #expect(
            SummaryBlockCompareMapping.valueSub(
                current: 14.8,
                comparison: 13.6,
                compare: .loading(date: "2026-05-08")
            ) == .reserved
        )
    }

    @Test
    func valueSubReturnsReservedWhenCompareIsUnavailable() {
        #expect(
            SummaryBlockCompareMapping.valueSub(
                current: 14.8,
                comparison: 13.6,
                compare: .unavailable(period: .yesterday)
            ) == .reserved
        )
    }

    @Test
    func valueSubReturnsReservedWhenReadyButComparisonIsNil() {
        // Per-row fallback: if the comparison day's value is nil for this
        // row, the sub-line slot stays reserved (no glyph).
        let snapshot = makeSnapshot(solar: nil)
        let result = SummaryBlockCompareMapping.valueSub(
            current: 14.8,
            comparison: snapshot.solar,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result == .reserved)
    }

    @Test
    func valueSubReturnsTextDeltaWhenReadyAndBothPresent() {
        let snapshot = makeSnapshot(solar: 13.6)
        let result = SummaryBlockCompareMapping.valueSub(
            current: 14.8,
            comparison: snapshot.solar,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result == .text("▲ 1.2 kWh"))
    }

    // MARK: - SummaryBlockCompareMapping.accessibilityOverride

    @Test
    func accessibilityOverrideReturnsNilWhenCompareIsOff() {
        let result = SummaryBlockCompareMapping.accessibilityOverride(
            rowLabel: "Solar produced",
            labelSub: nil,
            primary: "14.8 kWh",
            current: 14.8,
            comparison: 13.6,
            compare: .off
        )
        #expect(result == nil)
    }

    @Test
    func accessibilityOverrideOmitsComparisonClauseWhenLoading() {
        // AC 7.2: fallback label is just "{label}: {value}".
        let result = SummaryBlockCompareMapping.accessibilityOverride(
            rowLabel: "Solar produced",
            labelSub: nil,
            primary: "14.8 kWh",
            current: 14.8,
            comparison: 13.6,
            compare: .loading(date: "2026-05-08")
        )
        #expect(result == "Solar produced: 14.8 kWh")
    }

    @Test
    func accessibilityOverrideOmitsComparisonClauseWhenUnavailable() {
        let result = SummaryBlockCompareMapping.accessibilityOverride(
            rowLabel: "Solar produced",
            labelSub: nil,
            primary: "14.8 kWh",
            current: 14.8,
            comparison: 13.6,
            compare: .unavailable(period: .yesterday)
        )
        #expect(result == "Solar produced: 14.8 kWh")
    }

    @Test
    func accessibilityOverrideIncludesComparisonClauseWhenReady() {
        // AC 7.1 example: "{label}: {value}, {dir} {abs} kilowatt-hours versus {period}"
        let snapshot = makeSnapshot(solar: 13.6)
        let result = SummaryBlockCompareMapping.accessibilityOverride(
            rowLabel: "Solar produced",
            labelSub: nil,
            primary: "14.8 kWh",
            current: 14.8,
            comparison: snapshot.solar,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result == "Solar produced: 14.8 kWh, up 1.2 kilowatt-hours versus yesterday")
    }

    @Test
    func accessibilityOverrideIncludesPaidQualifierForGridInPeak() {
        // AC 7.1 example combines labelSub ("paid"/"free") with the row label.
        let snapshot = makeSnapshot(gridImport: 4.2, offpeakGridImport: 3.2)
        let result = SummaryBlockCompareMapping.accessibilityOverride(
            rowLabel: "Grid in (peak)",
            labelSub: "paid",
            primary: "0.84 kWh",
            current: 0.84,
            comparison: snapshot.peakGridImport,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result?.starts(with: "Grid in (peak), paid: 0.84 kWh,") == true)
    }

    @Test
    func accessibilityOverrideIncludesFreeQualifierForGridInOffPeak() {
        let snapshot = makeSnapshot(offpeakGridImport: 3.2)
        let result = SummaryBlockCompareMapping.accessibilityOverride(
            rowLabel: "Grid in (off-peak)",
            labelSub: "free",
            primary: "3.42 kWh",
            current: 3.42,
            comparison: snapshot.offpeakGridImport,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result?.starts(with: "Grid in (off-peak), free: 3.42 kWh,") == true)
    }

    @Test
    func accessibilityOverridePeriodNameMatchesSelectedPeriod() {
        let snapshot = makeSnapshot(solar: 13.6)
        let result = SummaryBlockCompareMapping.accessibilityOverride(
            rowLabel: "Solar produced",
            labelSub: nil,
            primary: "14.8 kWh",
            current: 14.8,
            comparison: snapshot.solar,
            compare: .ready(snapshot, period: .sevenDaysAgo)
        )
        #expect(result?.contains("versus 7 days ago") == true)
    }

    // MARK: - House used uses HouseholdLoad.kwh on snapshot fields

    @Test
    func houseUsedSnapshotFieldMatchesHouseholdLoadFormula() {
        // The "House used" comparison value is `snapshot.houseUsed`,
        // which itself derives from HouseholdLoad.kwh on the snapshot's
        // fields. Verifying the snapshot's accessor directly is enough
        // — SummaryBlock's row passes `snapshot?.houseUsed` straight
        // through to `valueSub`.
        let snapshot = ComparisonSnapshot(
            date: "2026-05-08",
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
    }

    // MARK: - Helpers

    private func measureHeight<V: View>(for view: V) -> CGFloat {
        let controller = UIHostingController(rootView: view.frame(width: 360))
        controller.view.setNeedsLayout()
        controller.view.layoutIfNeeded()
        return controller.sizeThatFits(in: CGSize(width: 360, height: CGFloat.infinity)).height
    }

    private func preFeatureBlock() -> some View {
        SummaryBlock(
            title: "Power",
            trailing: "today",
            solar: 14.82,
            gridImport: 4.26,
            gridExport: 1.10,
            offpeakGridImport: 3.42,
            batteryCharge: 6.20,
            batteryDischarge: 5.40,
            showsBatteryCycle: false
        )
    }

    private func compareBlock(compare: ComparisonState) -> some View {
        SummaryBlock(
            title: "Power",
            trailing: "today",
            solar: 14.82,
            gridImport: 4.26,
            gridExport: 1.10,
            offpeakGridImport: 3.42,
            batteryCharge: 6.20,
            batteryDischarge: 5.40,
            showsBatteryCycle: false,
            compare: compare
        )
    }

    private func makeSnapshot(
        solar: Double? = 14.82,
        gridImport: Double? = 4.26,
        gridExport: Double? = 1.10,
        batteryCharge: Double? = 6.20,
        batteryDischarge: Double? = 5.40,
        offpeakGridImport: Double? = 3.42
    ) -> ComparisonSnapshot {
        ComparisonSnapshot(
            date: "2026-05-08",
            solar: solar,
            gridImport: gridImport,
            gridExport: gridExport,
            batteryCharge: batteryCharge,
            batteryDischarge: batteryDischarge,
            offpeakGridImport: offpeakGridImport,
            dailyUsage: nil
        )
    }
}
#endif
