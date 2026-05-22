import FluxCore
import SwiftData
import SwiftUI

@MainActor
struct AppNavigationView: View {
    @Environment(\.modelContext) private var modelContext
    @Environment(\.scenePhase) private var scenePhase
    #if !os(macOS)
    @Environment(\.horizontalSizeClass) private var hSizeClass
    #endif

    @State private var selectedScreen: Screen? = .dashboard
    @State private var navigationPath = NavigationPath()
    @State private var preferredCompactColumn: NavigationSplitViewColumn = .detail
    @State private var keychainService = KeychainService()
    @State private var apiClient: (any FluxAPIClient)?
    @State private var iosTab: FluxTab = .dashboard
    @State private var today: String = DateFormatting.todayDateString()
    @State private var dashboardViewModel: DashboardViewModel?
    @State private var historyViewModel: HistoryViewModel?
    @State private var todayDayDetailViewModel: DayDetailViewModel?
    @State private var credentialFingerprint: String?
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
                let mapped = mappedIosTab(for: newScreen, currentTab: iosTab)
                if mapped != iosTab { iosTab = mapped }
            }
            .onChange(of: iosTab) { _, newTab in
                let mapped = mappedSelectedScreen(for: newTab, currentSelection: selectedScreen)
                if mapped != selectedScreen { selectedScreen = mapped }
            }
            .onChange(of: scenePhase) { _, newPhase in
                if newPhase == .active {
                    reloadDependencies()
                    rolloverToday()
                    // Replay any pending SoC alert registration (AC 1.7,
                    // 2.4): a failed POST in registerDeviceIfNeeded leaves
                    // the device record stashed locally; foregroundHook
                    // re-attempts and clears lastError on success.
                    Task { @MainActor in
                        await SoCAlertsService.shared.foregroundHook()
                    }
                }
            }
            .task {
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(60))
                    if Task.isCancelled { return }
                    rolloverToday()
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
        if let apiClient, let dashboardViewModel, let historyViewModel, let todayDayDetailViewModel {
            if usesPadShell {
                FluxiPadRoot(
                    apiClient: apiClient,
                    selectedScreen: $selectedScreen,
                    navigationPath: $navigationPath,
                    dashboardViewModel: dashboardViewModel,
                    historyViewModel: historyViewModel,
                    todayDayDetailViewModel: todayDayDetailViewModel
                )
                .modelContext(modelContext)
            } else {
                FluxiOSRoot(
                    apiClient: apiClient,
                    tab: $iosTab,
                    dashboardViewModel: dashboardViewModel,
                    historyViewModel: historyViewModel
                )
                .modelContext(modelContext)
            }
        } else {
            SettingsView(onSaved: handleSettingsSaved)
        }
    }

    private var usesPadShell: Bool { IPadLayoutGate.isActive(hSizeClass: hSizeClass) }
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

    private func rolloverToday() {
        let now = DateFormatting.todayDateString()
        guard now != today else { return }
        today = now
        if let todayDayDetailViewModel {
            Task { @MainActor in
                await todayDayDetailViewModel.setDate(now)
            }
        }
    }

    private func reloadDependencies() {
        // Fingerprint the credentials so we only rebuild the API client and
        // the three hoisted VMs when the URL or token actually changes —
        // otherwise every scenePhase → .active foreground would discard
        // cached state and in-flight refresh timers (violating AC 6.4).
        let newFingerprint = currentCredentialFingerprint()
        let credentialsChanged = newFingerprint != credentialFingerprint
        credentialFingerprint = newFingerprint

        if credentialsChanged {
            let client = makeAPIClient()
            apiClient = client
            if let client {
                dashboardViewModel = DashboardViewModel(apiClient: client)
                historyViewModel = HistoryViewModel(apiClient: client, modelContext: modelContext)
                todayDayDetailViewModel = DayDetailViewModel(date: today, apiClient: client)
            } else {
                dashboardViewModel = nil
                historyViewModel = nil
                todayDayDetailViewModel = nil
            }
        }

        // Also binds SoCAlertsService so its CRUD calls don't throw .notConfigured.
        if let apiClient {
            SoCAlertsService.shared.bind(apiClient: apiClient)
        }
        selectedScreen = apiClient == nil ? .settings : (selectedScreen ?? .dashboard)
    }

    /// Joins the trimmed API URL and the keychain token into a single
    /// string so `reloadDependencies()` can detect a credentials change
    /// by comparison instead of by API-client reference identity (the
    /// client is a class instance and `makeAPIClient()` returns a fresh
    /// one every call).
    private func currentCredentialFingerprint() -> String? {
        guard let urlString = UserDefaults.fluxAppGroup.apiURL?
                .trimmingCharacters(in: .whitespacesAndNewlines),
              !urlString.isEmpty,
              let token = keychainService.loadToken(),
              !token.isEmpty
        else {
            return nil
        }
        return "\(urlString)|\(token)"
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
