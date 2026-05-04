#if !os(macOS)
import FluxCore
import SwiftData
import SwiftUI

/// iOS root view. Holds the active tab and swaps between the three V5
/// screens. Each screen draws the V5 tab bar at the top of its own header,
/// so there is no system tab bar — the navigation chrome is built from the
/// V5 tokens.
///
/// The Dashboard view-model is held here so its 10s auto-refresh state and
/// last fetched payload survive tab switches; the Today and History
/// view-models reload cheaply when their tabs become visible, so they're
/// allowed to be reconstructed.
///
/// Each tab owns its own `NavigationPath`. Tapping any tab in the tab bar
/// pops that tab back to its root — including taps on the already-selected
/// tab — so a pushed Day Detail (e.g. opened from History) doesn't trap the
/// user.
struct FluxiOSRoot: View {
    @Environment(\.modelContext) private var modelContext

    @State private var dashboardViewModel: DashboardViewModel
    @State private var dashboardPath = NavigationPath()
    @State private var todayPath = NavigationPath()
    @State private var historyPath = NavigationPath()
    @State private var showingSettings = false

    let apiClient: any FluxAPIClient
    @Binding var tab: FluxTab

    init(apiClient: any FluxAPIClient, tab: Binding<FluxTab>) {
        self.apiClient = apiClient
        _tab = tab
        _dashboardViewModel = State(initialValue: DashboardViewModel(apiClient: apiClient))
    }

    var body: some View {
        ZStack {
            FluxTheme.Palette.background.ignoresSafeArea()

            switch tab {
            case .dashboard:
                NavigationStack(path: $dashboardPath) {
                    DashboardView(
                        viewModel: dashboardViewModel,
                        tab: $tab,
                        onSettingsTap: { showingSettings = true },
                        onTabActivate: handleTabActivate
                    )
                }
            case .today:
                NavigationStack(path: $todayPath) {
                    DayDetailView(
                        date: DateFormatting.todayDateString(),
                        apiClient: apiClient,
                        tab: $tab,
                        onSettingsTap: { showingSettings = true },
                        onTabActivate: handleTabActivate
                    )
                }
            case .history:
                NavigationStack(path: $historyPath) {
                    HistoryView(
                        apiClient: apiClient,
                        modelContext: modelContext,
                        tab: $tab,
                        onSettingsTap: { showingSettings = true },
                        onTabActivate: handleTabActivate
                    )
                }
            }
        }
        .preferredColorScheme(.dark)
        .sheet(isPresented: $showingSettings) {
            NavigationStack {
                SettingsView()
                    .toolbar {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button("Done") { showingSettings = false }
                        }
                    }
            }
        }
    }

    /// Resets the activated tab's NavigationStack path. Called on every tap
    /// of the tab bar — the cross-tab switch itself is still handled via the
    /// `$tab` binding. Effect: tapping Dashboard / Today / History always
    /// shows the tab's root view, even after a pushed Day Detail.
    private func handleTabActivate(_ activated: FluxTab) {
        switch activated {
        case .dashboard: dashboardPath = NavigationPath()
        case .today: todayPath = NavigationPath()
        case .history: historyPath = NavigationPath()
        }
    }
}
#endif
