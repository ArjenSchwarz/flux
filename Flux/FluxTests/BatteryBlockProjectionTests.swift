import FluxCore
import Testing
@testable import Flux

// Verifies BatteryBlock's off-peak row selection (Decision 9): the projection
// row takes precedence over the "Charged during off-peak" delta row, and the
// two are mutually exclusive. The contract is exposed through internal
// computed properties so it is testable without view-rendering infrastructure
// (and so it runs on macOS, where UIKit hosting is unavailable).
@MainActor
@Suite
struct BatteryBlockProjectionTests {
    private func block(
        projected: Double?,
        windowEnd: String?,
        delta: Double? = nil,
        showsDelta: Bool = true
    ) -> BatteryBlock {
        BatteryBlock(
            batteryCharge: 6.2,
            batteryDischarge: 5.4,
            lowestSOC: 38,
            lowestSOCTimestamp: nil,
            offpeakBatteryDeltaPercent: delta,
            showsOffpeakDelta: showsDelta,
            projectedOffpeakEndSoc: projected,
            offpeakWindowEnd: windowEnd
        )
    }

    @Test
    func projectionRowLabelUsesWindowEnd() {
        let battery = block(projected: 97.5, windowEnd: "14:00")
        let row = battery.offpeakRow
        #expect(row?.label == "Projected at 14:00")
        #expect(row?.value == SOCFormatting.format(97.5))
    }

    @Test
    func projectionRowFallsBackToOffPeakEndWhenWindowEndNil() {
        let battery = block(projected: 80, windowEnd: nil)
        #expect(battery.offpeakRow?.label == "Projected at off-peak end")
    }

    @Test
    func projectionSuppressesDeltaRow() {
        // Projection present + a realised delta also present: only the
        // projection row renders (mutually exclusive, projection wins).
        let battery = block(projected: 97.5, windowEnd: "14:00", delta: 42)
        let row = battery.offpeakRow
        #expect(row?.label == "Projected at 14:00")
        #expect(row?.value == SOCFormatting.format(97.5))
        #expect(row?.label != "Charged during off-peak")
    }

    @Test
    func deltaRowRendersWhenProjectionNil() {
        // No projection: behaviour unchanged — the delta row renders per the
        // existing showsOffpeakDelta logic.
        let battery = block(projected: nil, windowEnd: "14:00", delta: 42)
        let row = battery.offpeakRow
        #expect(row?.label == "Charged during off-peak")
        #expect(row?.value == "+42%")
    }

    @Test
    func deltaRowShowsDashWhenProjectionNilAndDeltaNil() {
        // No projection, no delta yet, but showsOffpeakDelta keeps the row.
        let battery = block(projected: nil, windowEnd: "14:00", delta: nil)
        let row = battery.offpeakRow
        #expect(row?.label == "Charged during off-peak")
        #expect(row?.value == "—")
    }

    @Test
    func noOffpeakRowWhenProjectionNilAndDeltaHidden() {
        // Neither projection nor delta context (Day Detail / History style).
        let battery = block(projected: nil, windowEnd: nil, delta: nil, showsDelta: false)
        #expect(battery.offpeakRow == nil)
    }
}
