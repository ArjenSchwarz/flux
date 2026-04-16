import SwiftData
import SwiftUI

@MainActor
struct AppNavigationView: View {
    @Environment(\.modelContext) private var modelContext
    @Environment(\.scenePhase) private var scenePhase

    @State private var selectedScreen: Screen? = .dashboard
    @State private var navigationPath = NavigationPath()
    @State private var preferredCompactColumn: NavigationSplitViewColumn = .detail
    @State private var keychainService = KeychainService()
    @State private var apiClient: (any FluxAPIClient)?

    var body: some View {
        NavigationSplitView(preferredCompactColumn: $preferredCompactColumn) {
            SidebarView(selection: $selectedScreen)
        } detail: {
            NavigationStack(path: $navigationPath) {
                currentScreenView
            }
        }
        .onAppear(perform: reloadDependencies)
        .onChange(of: selectedScreen) { _, _ in
            navigationPath = NavigationPath()
        }
        .onChange(of: scenePhase) { _, newPhase in
            if newPhase == .active {
                reloadDependencies()
            }
        }
    }

    @ViewBuilder
    private var currentScreenView: some View {
        switch effectiveScreen {
        case .dashboard:
            if let apiClient {
                DashboardView(apiClient: apiClient)
            } else {
                SettingsView(onSaved: handleSettingsSaved)
            }
        case .history:
            if let apiClient {
                HistoryView(apiClient: apiClient, modelContext: modelContext)
            } else {
                SettingsView(onSaved: handleSettingsSaved)
            }
        case .settings:
            SettingsView(onSaved: handleSettingsSaved)
        }
    }

    private var effectiveScreen: Screen {
        if apiClient == nil {
            return .settings
        }
        return selectedScreen ?? .dashboard
    }

    private func handleSettingsSaved() {
        reloadDependencies()
    }

    private func reloadDependencies() {
        apiClient = makeAPIClient()
        selectedScreen = apiClient == nil ? .settings : (selectedScreen ?? .dashboard)
    }

    private func makeAPIClient() -> (any FluxAPIClient)? {
        guard let urlString = UserDefaults.standard.apiURL?.trimmingCharacters(in: .whitespacesAndNewlines),
              let url = URL(string: urlString),
              keychainService.loadToken()?.isEmpty == false
        else {
            return nil
        }

        return URLSessionAPIClient(baseURL: url, keychainService: keychainService)
    }
}

#Preview {
    AppNavigationView()
        .modelContainer(for: CachedDayEnergy.self, inMemory: true)
}
