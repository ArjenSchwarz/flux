//
//  FluxApp.swift
//  Flux
//
//  Created by Arjen Schwarz on 15/4/2026.
//

import FluxCore
import SwiftUI
import SwiftData

@main
struct FluxApp: App {
    #if os(macOS)
    @NSApplicationDelegateAdaptor(FluxAppDelegate.self) private var appDelegate
    @State private var refreshCoordinator = FluxRefreshCoordinator()
    #else
    @UIApplicationDelegateAdaptor(FluxiOSAppDelegate.self) private var appDelegate
    #endif

    @State private var chartScopeRegistry = ChartScopeRegistry()
    @State private var chartExpansionFocus = ChartExpansionFocusCoordinator()

    @AppStorage(UserDefaults.themeIdentifierKey, store: UserDefaults.fluxAppGroup)
    private var themeRaw: String = ""

    @AppStorage(UserDefaults.appFontFamilyKey, store: UserDefaults.fluxAppGroup)
    private var appFontFamily: String = ""

    private var preferredScheme: ColorScheme? {
        (ThemeChoice(rawValue: themeRaw) ?? .default).colorScheme
    }

    init() {
        SettingsSuiteMigrator.run()
        KeychainAccessibilityMigrator.run()
        iCloudURLMirror.shared.start()
    }

    var body: some Scene {
        #if os(macOS)
        WindowGroup {
            AppNavigationView()
                .environment(refreshCoordinator)
                .environment(chartScopeRegistry)
                .macOSChartExpansion(registry: chartScopeRegistry, focus: chartExpansionFocus)
                .preferredColorScheme(preferredScheme)
                // Content-view minimum sized so the NavigationSplitView
                // detail column always exceeds the 700pt 2-column threshold
                // for AdaptiveColumnsLayout and the Day Detail Grid
                // (see Decision 9).
                .frame(minWidth: 960, minHeight: 600)
        }
        .defaultSize(width: 1200, height: 800)
        // .windowResizability(.contentSize) is required — .frame alone doesn't clamp NSWindow.
        .windowResizability(.contentSize)
        .modelContainer(for: CachedDayEnergy.self)
        .commands {
            FluxKeyboardCommands(coordinator: refreshCoordinator)
        }

        ChartDetailScene()
            .environment(chartScopeRegistry)
            .environment(\.chartExpansionFocus, chartExpansionFocus)
            .modelContainer(for: CachedDayEnergy.self)

        Settings {
            // The Settings scene is a top-level SwiftUI Scene — not a
            // descendant of AppNavigationView — so it does NOT inherit the
            // \.appFontFamily environment value injected there. Inject it
            // explicitly so the Settings form itself honours the user's
            // chosen font on macOS.
            SettingsView()
                .frame(minWidth: 480, minHeight: 360)
                .preferredColorScheme(preferredScheme)
                .environment(\.appFontFamily, appFontFamily.appFontFamilyEnvironmentValue)
        }
        #else
        WindowGroup {
            RootView()
                .environment(chartScopeRegistry)
                .preferredColorScheme(preferredScheme)
        }
        .modelContainer(for: CachedDayEnergy.self)
        #endif
    }
}
