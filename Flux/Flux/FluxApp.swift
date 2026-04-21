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
    init() {
        SettingsSuiteMigrator.run()
        KeychainAccessibilityMigrator.run()
    }

    var body: some Scene {
        WindowGroup {
            AppNavigationView()
        }
        .modelContainer(for: CachedDayEnergy.self)
    }
}
