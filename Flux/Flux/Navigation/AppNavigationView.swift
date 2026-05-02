import FluxCore
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

    #if os(macOS)
    @SceneStorage("flux.sidebar.selectedScreen") private var storedSelection: String = Screen.dashboard.rawValue
    #endif

    var body: some View {
        NavigationSplitView(preferredCompactColumn: $preferredCompactColumn) {
            SidebarView(selection: $selectedScreen)
        } detail: {
            NavigationStack(path: $navigationPath) {
                currentScreenView
                    #if os(macOS)
                    .scrollContentBackground(.hidden)
                    #endif
            }
        }
        .onAppear {
            #if os(macOS)
            if let restored = Screen(rawValue: storedSelection), restored != .settings {
                selectedScreen = restored
            }
            #endif
            reloadDependencies()
        }
        .onChange(of: selectedScreen) { _, newScreen in
            navigationPath = NavigationPath()
            #if os(macOS)
            if let newScreen, newScreen != .settings {
                storedSelection = newScreen.rawValue
            }
            #endif
        }
        .onChange(of: scenePhase) { _, newPhase in
            if newPhase == .active {
                reloadDependencies()
            }
        }
        .onOpenURL { url in
            switch DeepLinkHandler.handle(url) {
            case let .navigate(screen):
                selectedScreen = screen
                navigationPath = NavigationPath()
            case .none:
                break
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .fluxCredentialsChanged)) { _ in
            reloadDependencies()
        }
    }

    @ViewBuilder
    private var currentScreenView: some View {
        switch effectiveScreen {
        case .dashboard:
            if let apiClient {
                DashboardView(apiClient: apiClient)
            } else {
                unconfiguredView
            }
        case .today:
            if let apiClient {
                DayDetailView(date: DateFormatting.todayDateString(), apiClient: apiClient)
            } else {
                unconfiguredView
            }
        case .history:
            if let apiClient {
                HistoryView(apiClient: apiClient, modelContext: modelContext)
            } else {
                unconfiguredView
            }
        case .settings:
            #if os(macOS)
            unconfiguredView
            #else
            SettingsView(onSaved: handleSettingsSaved)
            #endif
        }
    }

    @ViewBuilder
    private var unconfiguredView: some View {
        #if os(macOS)
        MacUnconfiguredView()
        #else
        SettingsView(onSaved: handleSettingsSaved)
        #endif
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
        guard let urlString = UserDefaults.fluxAppGroup.apiURL?.trimmingCharacters(in: .whitespacesAndNewlines),
              let url = URL(string: urlString),
              keychainService.loadToken()?.isEmpty == false
        else {
            return nil
        }

        return URLSessionAPIClient(baseURL: url, keychainService: keychainService)
    }
}

#if os(macOS)
private struct MacUnconfiguredView: View {
    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "gearshape")
                .font(.largeTitle)
                .foregroundStyle(.secondary)
            Text("Flux is not configured")
                .font(.headline)
            Text("Open Settings to enter your API URL and token.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            SettingsLink {
                Text("Open Settings…")
            }
            .keyboardShortcut(",", modifiers: .command)
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
#endif

#Preview {
    AppNavigationView()
        .modelContainer(for: CachedDayEnergy.self, inMemory: true)
}
