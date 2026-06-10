import FluxCore
import SwiftUI
import Testing
@testable import Flux

// The hero panel composes the "can't empty before off-peak" indicator
// alongside the existing status line. The contract worth pinning is the
// VoiceOver announcement — it is the user-facing signal the requirement
// commits to (see requirements.md §3.5). The string is sourced from
// `DashboardHeroPanel<EmptyView>.cantEmptyBeforeOffpeakAccessibilityLabel` so both
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
        // Scope of this test: pin the contract string only. SwiftUI view
        // bodies are not exercised in unit tests, so the panel construction
        // below verifies the call-site binding compiles but does not
        // assert that the rendered `.accessibilityLabel(...)` modifier
        // actually uses this helper. Behavioural coverage would need
        // ViewInspector or a UI snapshot test — out of scope here.
        _ = DashboardHeroPanel(
            live: live,
            rolling15min: nil,
            battery: battery,
            offpeakWindowStart: "23:00"
        )

        #expect(
            DashboardHeroPanel<EmptyView>.cantEmptyBeforeOffpeakAccessibilityLabel(offpeakWindowStart: "23:00")
                == "Battery won't empty before off-peak at 23:00"
        )
    }

    // The visible indicator leads with the live power-flow rate so it carries
    // the same context as the "empty by" status line. With a prefix the line
    // continues mid-sentence ("… · won't empty before …"); without one (idle /
    // awaiting data) it stands alone with a leading capital.
    @Test
    func visibleTextLeadsWithPowerFlowPrefixWhenPresent() {
        #expect(
            DashboardHeroPanel<EmptyView>.cantEmptyBeforeOffpeakVisibleText(
                prefix: "Discharging · 400 W",
                offpeakWindowStart: "11:00"
            ) == "Discharging · 400 W · won't empty before 11:00"
        )
    }

    @Test
    func visibleTextFallsBackToBareFormWithoutPrefix() {
        #expect(
            DashboardHeroPanel<EmptyView>.cantEmptyBeforeOffpeakVisibleText(
                prefix: nil,
                offpeakWindowStart: "11:00"
            ) == "Won't empty before 11:00"
        )
    }

    // Pin the shared power-flow labels: the "·" separator and the W/kW
    // threshold are part of the assembled indicator and status-line strings,
    // so a formatting change here must be a deliberate one.
    @Test
    func powerFlowLabelsFormatWattsAndKilowatts() {
        #expect(DashboardHeroPanel<EmptyView>.dischargingLabel(400) == "Discharging · 400 W")
        #expect(DashboardHeroPanel<EmptyView>.dischargingLabel(2400) == "Discharging · 2.40 kW")
        #expect(DashboardHeroPanel<EmptyView>.chargingLabel(1200) == "Charging · 1.20 kW")
    }
}
