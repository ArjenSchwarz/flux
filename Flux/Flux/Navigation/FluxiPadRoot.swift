#if !os(macOS)
import FluxCore
import SwiftData
import SwiftUI

/// iPad regular-size-class shell. A two-column `NavigationSplitView` with a
/// sidebar (Dashboard / Today / History) and a detail column hosting the
/// selected screen. Settings stays as a sheet, opened via a toolbar gear in
/// the detail column.
///
/// View-models are injected by `AppNavigationView`. They outlive a size-class
/// flip between this shell and `FluxiOSRoot` so cached fetch state and
/// in-flight refresh timers survive the transition (AC 6.4).
///
/// Compact-width fallbacks (Slide Over, narrow Split View) are not handled
/// here — `AppNavigationView.iOSRoot` gates on `usesPadShell` and renders
/// `FluxiOSRoot` at compact widths.
struct FluxiPadRoot: View {
    @Environment(\.modelContext) private var modelContext

    let apiClient: any FluxAPIClient
    @Binding var selectedScreen: Screen?
    @Binding var navigationPath: NavigationPath
    let today: String
    let dashboardViewModel: DashboardViewModel
    let historyViewModel: HistoryViewModel
    let todayDayDetailViewModel: DayDetailViewModel

    @State private var preferredCompactColumn: NavigationSplitViewColumn = .detail
    @State private var showingSettings = false

    var body: some View {
        NavigationSplitView(preferredCompactColumn: $preferredCompactColumn) {
            SidebarView(selection: $selectedScreen)
        } detail: {
            NavigationStack(path: $navigationPath) {
                detailContent
                    .toolbar {
                        ToolbarItem(placement: .primaryAction) {
                            Button {
                                showingSettings = true
                            } label: {
                                Label("Settings", systemImage: "gearshape")
                            }
                        }
                    }
            }
        }
        .navigationSplitViewStyle(.balanced)
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

    @ViewBuilder
    private var detailContent: some View {
        switch selectedScreen ?? .dashboard {
        case .dashboard:
            DashboardView(viewModel: dashboardViewModel)
        case .today:
            DayDetailView(viewModel: todayDayDetailViewModel)
        case .history:
            HistoryView(
                viewModel: historyViewModel,
                makeDayDetailViewModel: { date in
                    DayDetailViewModel(date: date, apiClient: apiClient)
                }
            )
        case .settings:
            // Settings sheet is presented via the toolbar gear above; this
            // case should be unreachable on iPad regular because
            // Screen.sidebarVisible filters .settings out. Render a small
            // placeholder defensively rather than crashing.
            ContentUnavailableView(
                "Settings",
                systemImage: "gearshape",
                description: Text("Open Settings from the toolbar.")
            )
        }
    }
}
#endif
