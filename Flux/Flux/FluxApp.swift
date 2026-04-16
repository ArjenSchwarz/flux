//
//  FluxApp.swift
//  Flux
//
//  Created by Arjen Schwarz on 15/4/2026.
//

import SwiftUI
import SwiftData

@main
struct FluxApp: App {
    var body: some Scene {
        WindowGroup {
            AppNavigationView()
        }
        .modelContainer(for: CachedDayEnergy.self)
    }
}
