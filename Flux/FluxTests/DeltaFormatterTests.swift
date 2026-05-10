import Foundation
import Testing
@testable import Flux

// Pins the delta-string formatting and VoiceOver label composition for
// the Compare feature. The rounding boundary cases are example-tested
// rather than property-tested because String(format:) defines the exact
// behaviour we want to lock down.
@Suite
struct DeltaFormatterTests {
    // MARK: - sublineContent: nil-input fallback

    @Test
    func sublineContentReservesWhenComparisonIsNil() {
        #expect(DeltaFormatter.sublineContent(current: 10.0, comparison: nil) == .reserved)
    }

    @Test
    func sublineContentReservesWhenCurrentIsNil() {
        #expect(DeltaFormatter.sublineContent(current: nil, comparison: 10.0) == .reserved)
    }

    @Test
    func sublineContentReservesWhenBothAreNil() {
        #expect(DeltaFormatter.sublineContent(current: nil, comparison: nil) == .reserved)
    }

    // MARK: - sublineContent: rounding boundaries (pinned by example)

    @Test
    func sublineContentRendersUnchangedWhenRoundedDeltaIsZero() {
        // 10.04 - 10.0 = 0.04, rounds to 0.0 at one decimal → "—"
        #expect(
            DeltaFormatter.sublineContent(current: 10.04, comparison: 10.0)
            == .text("— kWh")
        )
    }

    @Test
    func sublineContentRendersUpAtSmallestPositiveBoundary() {
        // 10.05 - 10.0 = 0.05, %.1f rounds to 0.1 → "▲ 0.1 kWh"
        #expect(
            DeltaFormatter.sublineContent(current: 10.05, comparison: 10.0)
            == .text("▲ 0.1 kWh")
        )
    }

    @Test
    func sublineContentRendersDownAtSmallestNegativeBoundary() {
        // 10.0 - 10.05 = -0.05, %.1f rounds to -0.1 → "▼ 0.1 kWh"
        #expect(
            DeltaFormatter.sublineContent(current: 10.0, comparison: 10.05)
            == .text("▼ 0.1 kWh")
        )
    }

    @Test
    func sublineContentRendersUpForLargerPositiveDelta() {
        #expect(
            DeltaFormatter.sublineContent(current: 14.8, comparison: 13.6)
            == .text("▲ 1.2 kWh")
        )
    }

    @Test
    func sublineContentRendersDownForLargerNegativeDelta() {
        #expect(
            DeltaFormatter.sublineContent(current: 4.0, comparison: 6.4)
            == .text("▼ 2.4 kWh")
        )
    }

    @Test
    func sublineContentRendersUnchangedForExactEquality() {
        #expect(
            DeltaFormatter.sublineContent(current: 7.5, comparison: 7.5)
            == .text("— kWh")
        )
    }

    // MARK: - voiceOverLabel composition

    @Test
    func voiceOverLabelMatchesAcceptanceCriterionExample() {
        // AC 7.1: "Solar produced: 14.8 kilowatt-hours, up 1.2 kilowatt-hours versus yesterday"
        let label = DeltaFormatter.voiceOverLabel(
            rowLabel: "Solar produced",
            labelSub: nil,
            primaryValue: "14.8 kilowatt-hours",
            current: 14.8,
            comparison: 13.6,
            period: .yesterday
        )
        #expect(label == "Solar produced: 14.8 kilowatt-hours, up 1.2 kilowatt-hours versus yesterday")
    }

    @Test
    func voiceOverLabelIncludesPaidLabelSubClause() {
        let label = DeltaFormatter.voiceOverLabel(
            rowLabel: "Grid in (peak)",
            labelSub: "paid",
            primaryValue: "4.2 kilowatt-hours",
            current: 4.2,
            comparison: 3.6,
            period: .yesterday
        )
        #expect(label == "Grid in (peak), paid: 4.2 kilowatt-hours, up 0.6 kilowatt-hours versus yesterday")
    }

    @Test
    func voiceOverLabelIncludesFreeLabelSubClause() {
        let label = DeltaFormatter.voiceOverLabel(
            rowLabel: "Grid in (off-peak)",
            labelSub: "free",
            primaryValue: "1.0 kilowatt-hours",
            current: 1.0,
            comparison: 1.5,
            period: .sevenDaysAgo
        )
        #expect(label == "Grid in (off-peak), free: 1.0 kilowatt-hours, down 0.5 kilowatt-hours versus 7 days ago")
    }

    @Test
    func voiceOverLabelUsesUnchangedWhenRoundedDeltaIsZero() {
        let label = DeltaFormatter.voiceOverLabel(
            rowLabel: "House used",
            labelSub: nil,
            primaryValue: "10.0 kilowatt-hours",
            current: 10.04,
            comparison: 10.0,
            period: .yesterday
        )
        #expect(label == "House used: 10.0 kilowatt-hours, unchanged versus yesterday")
    }

    @Test
    func voiceOverLabelOmitsComparisonClauseWhenComparisonMissing() {
        // Per-row fallback within .ready: comparison field nil for this row.
        // AC 7.2: omit the comparison clause; read only "{row label}: {primary value}".
        let label = DeltaFormatter.voiceOverLabel(
            rowLabel: "Solar produced",
            labelSub: nil,
            primaryValue: "14.8 kilowatt-hours",
            current: 14.8,
            comparison: nil,
            period: .yesterday
        )
        #expect(label == "Solar produced: 14.8 kilowatt-hours")
    }

    @Test
    func voiceOverLabelOmitsComparisonClauseWhenCurrentMissing() {
        let label = DeltaFormatter.voiceOverLabel(
            rowLabel: "Solar produced",
            labelSub: nil,
            primaryValue: "— kWh",
            current: nil,
            comparison: 13.6,
            period: .yesterday
        )
        #expect(label == "Solar produced: — kWh")
    }

    @Test
    func voiceOverLabelHonoursLabelSubInFallbackPath() {
        // labelSub still surfaces when comparison is nil — preserves the
        // "paid"/"free" qualifier on the Grid In rows.
        let label = DeltaFormatter.voiceOverLabel(
            rowLabel: "Grid in (peak)",
            labelSub: "paid",
            primaryValue: "4.2 kilowatt-hours",
            current: 4.2,
            comparison: nil,
            period: .yesterday
        )
        #expect(label == "Grid in (peak), paid: 4.2 kilowatt-hours")
    }

    // MARK: - voiceOverFallbackLabel composition

    @Test
    func voiceOverFallbackLabelOmitsComparisonClause() {
        let label = DeltaFormatter.voiceOverFallbackLabel(
            rowLabel: "Solar produced",
            labelSub: nil,
            primaryValue: "14.8 kilowatt-hours"
        )
        #expect(label == "Solar produced: 14.8 kilowatt-hours")
    }

    @Test
    func voiceOverFallbackLabelIncludesLabelSubClause() {
        let label = DeltaFormatter.voiceOverFallbackLabel(
            rowLabel: "Grid in (peak)",
            labelSub: "paid",
            primaryValue: "4.2 kilowatt-hours"
        )
        #expect(label == "Grid in (peak), paid: 4.2 kilowatt-hours")
    }

    @Test
    func voiceOverFallbackLabelOmitsEmptyLabelSub() {
        // Defensive: an empty-string labelSub should behave like nil so the
        // accessibility label doesn't read "Solar produced, : ...".
        let label = DeltaFormatter.voiceOverFallbackLabel(
            rowLabel: "Solar produced",
            labelSub: "",
            primaryValue: "14.8 kilowatt-hours"
        )
        #expect(label == "Solar produced: 14.8 kilowatt-hours")
    }
}
