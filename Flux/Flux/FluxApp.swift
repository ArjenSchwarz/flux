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
                // (T-1342 AC 2.2 / Decision 9).
                .frame(minWidth: 960, minHeight: 600)
        }
        // `.frame(minWidth:minHeight:)` alone constrains the SwiftUI
        // content view but does NOT clamp the NSWindow resize behavior —
        // `.windowResizability(.contentSize)` ties the window's resize
        // limits to the content view's frame constraints. Without it the
        // user can drag the window narrower than 960pt and the content
        // gets clipped, defeating AC 2.2.
        .defaultSize(width: 1200, height: 800)
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
