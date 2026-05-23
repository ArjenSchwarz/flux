import Testing
@testable import Flux

struct SidebarTabSyncTests {
    // MARK: - mappedIosTab

    @Test
    func mappedIosTabReturnsMappedTabWhenSelectedHasOne() {
        for tab in FluxTab.allCases {
            let screen = Screen(tab: tab)
            #expect(mappedIosTab(for: screen, currentTab: .dashboard) == tab)
        }
    }

    @Test
    func mappedIosTabReturnsCurrentTabWhenSelectedIsNil() {
        #expect(mappedIosTab(for: nil, currentTab: .history) == .history)
        #expect(mappedIosTab(for: nil, currentTab: .today) == .today)
    }

    @Test
    func mappedIosTabReturnsCurrentTabWhenSelectedIsSettings() {
        // .settings has no FluxTab counterpart — keep the user's current tab
        // so flipping to Settings doesn't knock the tab bar to .dashboard.
        #expect(mappedIosTab(for: .settings, currentTab: .history) == .history)
    }

    // MARK: - mappedSelectedScreen

    @Test
    func mappedSelectedScreenReturnsCanonicalScreenForTab() {
        for tab in FluxTab.allCases {
            #expect(mappedSelectedScreen(for: tab, currentSelection: nil) == Screen(tab: tab))
        }
    }

    @Test
    func mappedSelectedScreenPreservesSettings() {
        #expect(mappedSelectedScreen(for: .history, currentSelection: .settings) == .settings)
        #expect(mappedSelectedScreen(for: .dashboard, currentSelection: .settings) == .settings)
    }

    @Test
    func mappedSelectedScreenIsIdempotentWhenSelectionAlreadyMatches() {
        for tab in FluxTab.allCases {
            let screen = Screen(tab: tab)
            #expect(mappedSelectedScreen(for: tab, currentSelection: screen) == screen)
        }
    }

    // MARK: - Two-handler convergence

    @Test
    func handlersConvergeOnSidebarClick() {
        // Sidebar click: selectedScreen → .dashboard from prior (.history, .history).
        // The selectedScreen handler writes iosTab; the iosTab handler then writes
        // selectedScreen but the value already matches so no further write.
        var selected: Screen? = .dashboard
        var tab: FluxTab = .history

        // Handler 1: selectedScreen change.
        tab = mappedIosTab(for: selected, currentTab: tab)
        #expect(tab == .dashboard)

        // Handler 2: iosTab change (fired by the write above).
        let next = mappedSelectedScreen(for: tab, currentSelection: selected)
        if next != selected { selected = next }
        #expect(selected == .dashboard)
    }

    @Test
    func handlersConvergeOnTabTap() {
        // Tab tap: iosTab → .history from prior (.dashboard, .dashboard).
        var selected: Screen? = .dashboard
        var tab: FluxTab = .history

        // Handler 2: iosTab change.
        selected = mappedSelectedScreen(for: tab, currentSelection: selected)
        #expect(selected == .history)

        // Handler 1: selectedScreen change.
        let nextTab = mappedIosTab(for: selected, currentTab: tab)
        if nextTab != tab { tab = nextTab }
        #expect(tab == .history)
    }

    @Test
    func everyFluxTabScreenPairRoundTrips() {
        for tab in FluxTab.allCases {
            let screen = Screen(tab: tab)
            #expect(mappedIosTab(for: screen, currentTab: tab) == tab)
            #expect(mappedSelectedScreen(for: tab, currentSelection: screen) == screen)
        }
    }
}
