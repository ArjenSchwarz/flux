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
    @State private var iosTab: FluxTab = .dashboard
    @State private var pendingAuto: PendingAutoPresentation?
    @State private var didEvaluateAutoPresentation = false
    @State private var canonicalInstalledVersion: String = ""

    @AppStorage(UserDefaults.appFontFamilyKey, store: .fluxAppGroup)
    private var appFontFamily: String = ""

    #if os(macOS)
    @SceneStorage("flux.sidebar.selectedScreen") private var storedSelection: String = Screen.dashboard.rawValue
    #endif

    var body: some View {
        rootView
            .environment(\.appFontFamily, appFontFamily.appFontFamilyEnvironmentValue)
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
                    // Replay any pending SoC alert registration (AC 1.7,
                    // 2.4): a failed POST in registerDeviceIfNeeded leaves
                    // the device record stashed locally; foregroundHook
                    // re-attempts and clears lastError on success.
                    Task { @MainActor in
                        await SoCAlertsService.shared.foregroundHook()
                    }
                }
            }
            .onOpenURL { url in
                switch DeepLinkHandler.handle(url) {
                case let .navigate(screen):
                    selectedScreen = screen
                    navigationPath = NavigationPath()
                    iosTab = screen.tab ?? .dashboard
                case .none:
                    break
                }
            }
            .onReceive(NotificationCenter.default.publisher(for: .fluxCredentialsChanged)) { _ in
                reloadDependencies()
            }
            .task { evaluateWhatsNewAutoPresentation() }
            .sheet(item: $pendingAuto, onDismiss: handleWhatsNewDismiss) { item in
                WhatsNewSheet(releases: item.releases)
            }
    }

    private func evaluateWhatsNewAutoPresentation() {
        guard !didEvaluateAutoPresentation else { return }
        didEvaluateAutoPresentation = true

        guard let coordinator = WhatsNewCoordinator.forCurrentInstall() else { return }
        canonicalInstalledVersion = coordinator.canonicalInstalledVersion

        switch coordinator.autoDecision() {
        case .skip:
            break
        case .silentSet(let version):
            UserDefaults.fluxAppGroup.lastSeenWhatsNewVersion = version
        case .present(let releases):
            pendingAuto = PendingAutoPresentation(releases: releases)
        }
    }

    private func handleWhatsNewDismiss() {
        guard !canonicalInstalledVersion.isEmpty else { return }
        UserDefaults.fluxAppGroup.lastSeenWhatsNewVersion = canonicalInstalledVersion
    }

    @ViewBuilder
    private var rootView: some View {
        #if os(macOS)
        macOSRoot
        #else
        iOSRoot
        #endif
    }

    #if os(macOS)
    @ViewBuilder
    private var macOSRoot: some View {
        NavigationSplitView(preferredCompactColumn: $preferredCompactColumn) {
            SidebarView(selection: $selectedScreen)
        } detail: {
            NavigationStack(path: $navigationPath) {
                currentScreenView
                    .scrollContentBackground(.hidden)
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
                MacUnconfiguredView()
            }
        case .today:
            if let apiClient {
                DayDetailView(date: DateFormatting.todayDateString(), apiClient: apiClient)
            } else {
                MacUnconfiguredView()
            }
        case .history:
            if let apiClient {
                HistoryView(apiClient: apiClient, modelContext: modelContext)
            } else {
                MacUnconfiguredView()
            }
        case .settings:
            MacUnconfiguredView()
        }
    }
    #endif

    #if !os(macOS)
    @ViewBuilder
    private var iOSRoot: some View {
        if let apiClient {
            FluxiOSRoot(apiClient: apiClient, tab: $iosTab)
                .modelContext(modelContext)
        } else {
            SettingsView(onSaved: handleSettingsSaved)
        }
    }
    #endif

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
        let client = makeAPIClient()
        apiClient = client
        // SoC alerts share the same backend client. Without this bind the
        // SoCAlertsService.shared methods all throw .notConfigured from
        // their guard clauses — which fires before lastError is set, so
        // the Settings → Alerts editor's Save button has no visible effect:
        // the sheet just stays open with no banner.
        if let client {
            SoCAlertsService.shared.bind(apiClient: client)
        }
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

private extension Screen {
    var tab: FluxTab? {
        switch self {
        case .dashboard: .dashboard
        case .today: .today
        case .history: .history
        case .settings: nil
        }
    }
}

#if os(macOS)
private struct MacUnconfiguredView: View {
    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "gearshape")
                .appFont(.largeTitle)
                .foregroundStyle(.secondary)
            Text("Flux is not configured")
                .appFont(.headline)
            Text("Open Settings to enter your API URL and token.")
                .appFont(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            // ⌘, is registered globally by the Settings scene; no explicit
            // .keyboardShortcut is needed (and adding one duplicates it).
            SettingsLink {
                Text("Open Settings…")
            }
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
