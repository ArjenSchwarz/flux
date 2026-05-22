import Testing
@testable import Flux

struct SidebarTabSyncTests {
    @Test
    func identityInputsAreFixedPoints() {
        for tab in FluxTab.allCases {
            let screen = Screen(tab: tab)
            let (outScreen, outTab) = syncedState(selected: screen, tab: tab)
            #expect(outScreen == screen)
            #expect(outTab == tab)
        }
    }

    @Test
    func mismatchedInputsConvergeInOneStep() {
        // Screen .dashboard with tab .history: tab wins by canonical rule —
        // sidebar mirrors the tab so iPhone-compact → iPad-regular preserves
        // the user's most-recent navigation intent. The pair becomes
        // (.history, .history) after one step.
        let (outScreen, outTab) = syncedState(selected: .dashboard, tab: .history)
        #expect(outScreen == .history)
        #expect(outTab == .history)

        // Idempotency: feed the result back in, nothing changes.
        let (outScreen2, outTab2) = syncedState(selected: outScreen, tab: outTab)
        #expect(outScreen2 == outScreen)
        #expect(outTab2 == outTab)
    }

    @Test
    func nilSelectedScreenAdoptsTab() {
        let (outScreen, outTab) = syncedState(selected: nil, tab: .today)
        #expect(outScreen == .today)
        #expect(outTab == .today)
    }

    @Test
    func settingsSelectionLeavesTabUnchanged() {
        // .settings has no FluxTab counterpart. The sync must not destroy
        // the user's tab selection or crash on the nil mapping.
        let (outScreen, outTab) = syncedState(selected: .settings, tab: .history)
        #expect(outScreen == .settings)
        #expect(outTab == .history)
    }

    @Test
    func everyFluxTabScreenPairRoundTrips() {
        for tab in FluxTab.allCases {
            let screen = Screen(tab: tab)
            let (outScreen, outTab) = syncedState(selected: screen, tab: tab)
            #expect(outScreen == screen, "round-trip screen for \(tab)")
            #expect(outTab == tab, "round-trip tab for \(tab)")
        }
    }
}
