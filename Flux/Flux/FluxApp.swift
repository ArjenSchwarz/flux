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
        }
        .modelContainer(for: CachedDayEnergy.self)
        .commands {
            FluxKeyboardCommands(coordinator: refreshCoordinator)
        }

        Settings {
            SettingsView()
                .frame(minWidth: 480, minHeight: 360)
        }
        #else
        WindowGroup {
            AppNavigationView()
        }
        .modelContainer(for: CachedDayEnergy.self)
        #endif
    }
}
