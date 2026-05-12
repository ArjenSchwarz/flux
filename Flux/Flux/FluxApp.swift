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
                .macOSChartExpansion(registry: chartScopeRegistry)
                .preferredColorScheme(preferredScheme)
        }
        .modelContainer(for: CachedDayEnergy.self)
        .commands {
            FluxKeyboardCommands(coordinator: refreshCoordinator)
        }

        ChartDetailScene()
            .environment(chartScopeRegistry)
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
