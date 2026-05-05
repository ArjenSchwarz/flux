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
    #endif

    @AppStorage(UserDefaults.themeIdentifierKey, store: UserDefaults.fluxAppGroup)
    private var themeRaw: String = ""

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
                .preferredColorScheme(preferredScheme)
        }
        .modelContainer(for: CachedDayEnergy.self)
        .commands {
            FluxKeyboardCommands(coordinator: refreshCoordinator)
        }

        Settings {
            SettingsView()
                .frame(minWidth: 480, minHeight: 360)
                .preferredColorScheme(preferredScheme)
        }
        #else
        WindowGroup {
            AppNavigationView()
                .preferredColorScheme(preferredScheme)
        }
        .modelContainer(for: CachedDayEnergy.self)
        #endif
    }
}
