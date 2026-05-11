#if canImport(UIKit)
import FluxCore
import SwiftUI
import Testing
import UIKit
@testable import Flux

// DayInFiveBlocksPanel's compare integration is verified through two layers:
//
// 1. `DayInFiveBlocksPanelCompareMapping` (per-block pure helpers) —
//    exhaustive coverage of the per-block per-column delta logic
//    (solar and total) and the per-row a11y override.
//
// 2. Panel view-level — off-state height byte-identity vs the pre-feature
//    panel (Decision 16 scope), since the off state must not introduce
//    any reserved sub-line slots.
//
// The mapping layer carries the full per-state contract; the view-level
// height check pins layout-stability for the off state.
@MainActor
@Suite
struct DayInFiveBlocksPanelCompareTests {
    // MARK: - off-state height identity

    @Test
    func offStatePanelMatchesPreFeatureHeight() {
        let pre = preFeaturePanel()
        let off = comparePanel(compare: .off)
        #expect(measureHeight(for: pre) == measureHeight(for: off))
    }

    // MARK: - solarValueSub: hidden / reserved / text per state

    @Test
    func solarValueSubReturnsHiddenWhenCompareIsOff() {
        let block = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        let result = DayInFiveBlocksPanelCompareMapping.solarValueSub(for: block, compare: .off)
        #expect(result == .hidden)
    }

    @Test
    func solarValueSubReturnsReservedWhenCompareIsLoading() {
        let block = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        let result = DayInFiveBlocksPanelCompareMapping.solarValueSub(
            for: block,
            compare: .loading(date: "2026-05-08")
        )
        #expect(result == .reserved)
    }

    @Test
    func solarValueSubReturnsReservedWhenCompareIsUnavailable() {
        let block = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        let result = DayInFiveBlocksPanelCompareMapping.solarValueSub(
            for: block,
            compare: .unavailable(period: .yesterday)
        )
        #expect(result == .reserved)
    }

    @Test
    func solarValueSubRendersDeltaWhenReadyAndBothBlocksHaveSolar() {
        let selected = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        let comparisonBlock = makeBlock(kind: .offPeak, totalKwh: 4.5, solarKwh: 1.8)
        let snapshot = makeSnapshot(blocks: [comparisonBlock])

        let result = DayInFiveBlocksPanelCompareMapping.solarValueSub(
            for: selected,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result == .text("▲ 0.6 kWh"))
    }

    @Test
    func solarValueSubFallsBackWhenComparisonBlockSolarIsNil() {
        // AC 4.1 / Delta Formatting per-row fallback: daylight row with
        // nil solarKwh on the comparison side falls back to .reserved.
        let selected = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        let comparisonBlock = makeBlock(kind: .offPeak, totalKwh: 4.5, solarKwh: nil)
        let snapshot = makeSnapshot(blocks: [comparisonBlock])

        let result = DayInFiveBlocksPanelCompareMapping.solarValueSub(
            for: selected,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result == .reserved)
    }

    @Test
    func solarValueSubFallsBackWhenComparisonHasNoBlockOfSameKind() {
        // AC 4.2: missing block of the same kind on comparison side →
        // both solar and total deltas fall back to .reserved.
        let selected = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        let snapshot = makeSnapshot(blocks: [
            makeBlock(kind: .morningPeak, totalKwh: 2.0, solarKwh: 0.5)
        ])

        let result = DayInFiveBlocksPanelCompareMapping.solarValueSub(
            for: selected,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result == .reserved)
    }

    @Test
    func solarValueSubFallsBackWhenSnapshotDailyUsageIsNil() {
        // Partial-availability snapshot (summary present, dailyUsage nil):
        // every Five-Block row independently falls back via per-block helpers.
        let selected = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        let snapshot = makeSnapshot(blocks: nil)

        let result = DayInFiveBlocksPanelCompareMapping.solarValueSub(
            for: selected,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result == .reserved)
    }

    // MARK: - totalValueSub: hidden / reserved / text per state

    @Test
    func totalValueSubReturnsHiddenWhenCompareIsOff() {
        let block = makeBlock(kind: .night, totalKwh: 3.1, solarKwh: nil)
        let result = DayInFiveBlocksPanelCompareMapping.totalValueSub(for: block, compare: .off)
        #expect(result == .hidden)
    }

    @Test
    func totalValueSubReturnsReservedWhenCompareIsLoading() {
        let block = makeBlock(kind: .night, totalKwh: 3.1, solarKwh: nil)
        let result = DayInFiveBlocksPanelCompareMapping.totalValueSub(
            for: block,
            compare: .loading(date: "2026-05-08")
        )
        #expect(result == .reserved)
    }

    @Test
    func totalValueSubRendersDeltaWhenReadyAndBothBlocksPresent() {
        let selected = makeBlock(kind: .night, totalKwh: 3.1, solarKwh: nil)
        let comparisonBlock = makeBlock(kind: .night, totalKwh: 2.5, solarKwh: nil)
        let snapshot = makeSnapshot(blocks: [comparisonBlock])

        let result = DayInFiveBlocksPanelCompareMapping.totalValueSub(
            for: selected,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result == .text("▲ 0.6 kWh"))
    }

    @Test
    func totalValueSubFallsBackWhenComparisonHasNoBlockOfSameKind() {
        let selected = makeBlock(kind: .night, totalKwh: 3.1, solarKwh: nil)
        let snapshot = makeSnapshot(blocks: [
            makeBlock(kind: .evening, totalKwh: 2.0, solarKwh: nil)
        ])

        let result = DayInFiveBlocksPanelCompareMapping.totalValueSub(
            for: selected,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result == .reserved)
    }

    @Test
    func totalAndSolarSublinesAreIndependentOnDaylightRow() {
        // AC 4.1: daylight rows show two independent sub-lines. Total and
        // solar can resolve to different states.
        let selected = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        // Comparison block has total but missing solar.
        let comparisonBlock = makeBlock(kind: .offPeak, totalKwh: 4.5, solarKwh: nil)
        let snapshot = makeSnapshot(blocks: [comparisonBlock])

        let total = DayInFiveBlocksPanelCompareMapping.totalValueSub(
            for: selected,
            compare: .ready(snapshot, period: .yesterday)
        )
        let solar = DayInFiveBlocksPanelCompareMapping.solarValueSub(
            for: selected,
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(total == .text("▲ 0.5 kWh"))
        #expect(solar == .reserved)
    }

    // MARK: - row accessibility override

    @Test
    func rowAccessibilityOverrideReturnsNilWhenCompareIsOff() {
        let block = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        let result = DayInFiveBlocksPanelCompareMapping.rowAccessibilityOverride(
            for: block,
            rowLabel: "Off-peak",
            timeRange: "10:00–14:00",
            compare: .off
        )
        #expect(result == nil)
    }

    @Test
    func rowAccessibilityOverrideOmitsComparisonClauseWhenLoading() {
        let block = makeBlock(kind: .night, totalKwh: 3.1, solarKwh: nil)
        let result = DayInFiveBlocksPanelCompareMapping.rowAccessibilityOverride(
            for: block,
            rowLabel: "Night",
            timeRange: "00:00–06:00",
            compare: .loading(date: "2026-05-08")
        )
        // Fallback label includes row label, time range, and total only.
        // Spoken "kilowatt-hours" so VoiceOver doesn't read "k-W-h".
        #expect(result?.contains("Night") == true)
        #expect(result?.contains("3.10 kilowatt-hours") == true)
        #expect(result?.contains("versus") == false)
    }

    @Test
    func rowAccessibilityOverrideFallbackOnDaylightRowWithNilSolarOmitsSolar() {
        // Daylight blocks normally expose both total and solar in the
        // fallback label, but a daylight block with no solar reading
        // should drop the solar clause rather than reading "—".
        let block = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: nil)
        let result = DayInFiveBlocksPanelCompareMapping.rowAccessibilityOverride(
            for: block,
            rowLabel: "Off-peak",
            timeRange: "10:00–14:00",
            compare: .loading(date: "2026-05-08")
        )
        #expect(result?.contains("Off-peak") == true)
        #expect(result?.contains("5.00 kilowatt-hours") == true)
        #expect(result?.contains("solar") == false)
    }

    @Test
    func rowAccessibilityOverrideIncludesComparisonClauseWhenReady() {
        let selected = makeBlock(kind: .night, totalKwh: 3.1, solarKwh: nil)
        let comparisonBlock = makeBlock(kind: .night, totalKwh: 2.5, solarKwh: nil)
        let snapshot = makeSnapshot(blocks: [comparisonBlock])

        let result = DayInFiveBlocksPanelCompareMapping.rowAccessibilityOverride(
            for: selected,
            rowLabel: "Night",
            timeRange: "00:00–06:00",
            compare: .ready(snapshot, period: .yesterday)
        )
        #expect(result?.contains("up 0.6 kilowatt-hours") == true)
        #expect(result?.contains("versus yesterday") == true)
    }

    @Test
    func rowAccessibilityOverrideOnDaylightRowMentionsBothColumns() {
        // Daylight rows have both solar and total — the a11y label should
        // expose both so VoiceOver users hear both deltas in one element.
        let selected = makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4)
        let comparisonBlock = makeBlock(kind: .offPeak, totalKwh: 4.5, solarKwh: 1.8)
        let snapshot = makeSnapshot(blocks: [comparisonBlock])

        let result = DayInFiveBlocksPanelCompareMapping.rowAccessibilityOverride(
            for: selected,
            rowLabel: "Off-peak",
            timeRange: "10:00–14:00",
            compare: .ready(snapshot, period: .yesterday)
        )
        // The label format is implementation-defined; assert it contains
        // both deltas so VoiceOver users hear them.
        #expect(result?.contains("Off-peak") == true)
        #expect(result?.contains("solar") == true || result?.contains("Solar") == true)
        #expect(result?.contains("0.6 kilowatt-hours") == true)
        #expect(result?.contains("0.5 kilowatt-hours") == true)
    }

    // MARK: - Helpers

    private func measureHeight<V: View>(for view: V) -> CGFloat {
        let controller = UIHostingController(rootView: view.frame(width: 360))
        controller.view.setNeedsLayout()
        controller.view.layoutIfNeeded()
        return controller.sizeThatFits(in: CGSize(width: 360, height: CGFloat.infinity)).height
    }

    private func preFeaturePanel() -> some View {
        DayInFiveBlocksPanel(dailyUsage: fixture)
    }

    private func comparePanel(compare: ComparisonState) -> some View {
        DayInFiveBlocksPanel(dailyUsage: fixture, compare: compare)
    }

    private var fixture: DailyUsage {
        DailyUsage(blocks: [
            makeBlock(kind: .night, totalKwh: 3.1, solarKwh: nil),
            makeBlock(kind: .morningPeak, totalKwh: 2.1, solarKwh: 0.6),
            makeBlock(kind: .offPeak, totalKwh: 5.0, solarKwh: 2.4),
            makeBlock(kind: .afternoonPeak, totalKwh: 4.5, solarKwh: 1.8),
            makeBlock(kind: .evening, totalKwh: 2.2, solarKwh: nil)
        ])
    }

    private func makeBlock(kind: DailyUsageBlock.Kind, totalKwh: Double, solarKwh: Double?) -> DailyUsageBlock {
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

    private func makeSnapshot(blocks: [DailyUsageBlock]?) -> ComparisonSnapshot {
        ComparisonSnapshot(
            date: "2026-05-08",
            solar: nil,
            gridImport: nil,
            gridExport: nil,
            batteryCharge: nil,
            batteryDischarge: nil,
            offpeakGridImport: nil,
            dailyUsage: blocks.map { DailyUsage(blocks: $0) }
        )
    }
}
#endif
