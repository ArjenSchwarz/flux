import Testing
@testable import Flux

struct ScreenTests {
    @Test
    func everyFluxTabRoundTripsThroughScreen() {
        for tab in FluxTab.allCases {
            let screen = Screen(tab: tab)
            #expect(screen.tab == tab, "Screen(tab: \(tab)).tab should equal \(tab)")
        }
    }

    @Test
    func dashboardTabMapsToDashboardScreen() {
        #expect(Screen(tab: .dashboard) == .dashboard)
    }

    @Test
    func todayTabMapsToTodayScreen() {
        #expect(Screen(tab: .today) == .today)
    }

    @Test
    func historyTabMapsToHistoryScreen() {
        #expect(Screen(tab: .history) == .history)
    }

    @Test
    func sidebarVisibleExcludesSettings() {
        #expect(!Screen.sidebarVisible.contains(.settings))
    }

    #if !os(macOS)
    @Test
    func sidebarVisibleOnIOSIncludesDashboardTodayHistory() {
        #expect(Screen.sidebarVisible == [.dashboard, .today, .history])
    }
    #endif

    #if os(macOS)
    @Test
    func sidebarVisibleOnMacOSIncludesDashboardTodayHistory() {
        #expect(Screen.sidebarVisible == [.dashboard, .today, .history])
    }
    #endif
}
