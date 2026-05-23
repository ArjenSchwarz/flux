import FluxCore
import SwiftUI
import Testing
@testable import Flux

// The hero panel composes the "can't empty before off-peak" indicator
// alongside the existing status line. The contract worth pinning is the
// VoiceOver announcement — it is the user-facing signal the requirement
// commits to (see requirements.md §3.5). The string is sourced from
// `DashboardHeroPanel.cantEmptyBeforeOffpeakAccessibilityLabel` so both
// the view body and this test consume the same expression.
@MainActor
@Suite
struct DashboardHeroPanelTests {
    @Test
    func cantEmptyIndicatorAccessibilityLabelMatchesSpec() {
        let live = LiveData(
            ppv: 2400,
            pload: 750,
            pbat: 400,
            pgrid: -100,
            pgridSustained: false,
            soc: 62.4,
            timestamp: "2026-04-15T10:00:00Z"
        )
        let battery = BatteryInfo(
            capacityKwh: 13.3,
            cutoffPercent: 10,
            estimatedCutoffTime: nil,
            low24h: nil,
            cantEmptyBeforeOffpeak: true
        )
        // Construct the panel with the new inputs to keep the binding
        // surface under test even though the assertion is on the static
        // helper that drives the rendered accessibility label.
        _ = DashboardHeroPanel(
            live: live,
            rolling15min: nil,
            battery: battery,
            offpeakWindowStart: "23:00"
        )

        #expect(
            DashboardHeroPanel.cantEmptyBeforeOffpeakAccessibilityLabel(offpeakWindowStart: "23:00")
                == "Battery won't empty before off-peak at 23:00"
        )
    }
}
