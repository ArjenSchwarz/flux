#if canImport(UIKit) && !os(macOS)
import Foundation
import SwiftUI
import Testing
import UIKit
@testable import Flux

@MainActor
@Suite
// swiftlint:disable:next type_name
struct iOSExpandIntegrationTests {
    @Test("Activating expand on every ChartKind writes scope to the registry and sets the binding")
    func activatingEveryKindWritesRegistryAndBinding() {
        let registry = ChartScopeRegistry()
        var current: ChartKind?

        let action = ChartExpansionAction { kind, scope in
            registry.current[kind] = scope
            current = kind
        }

        let cases: [(ChartKind, ChartScope)] = [
            (.historySolar, .historyRange(days: 7)),
            (.historyGridUsage, .historyRange(days: 14)),
            (.historyDailyUsage, .historyRange(days: 30)),
            (.dayPower, .daySpecific(date: Date(timeIntervalSince1970: 1_700_000_000))),
            (.dayBatteryCombined, .daySpecific(date: Date(timeIntervalSince1970: 1_710_000_000)))
        ]

        for (kind, scope) in cases {
            action(kind, scope: scope)
            #expect(current == kind)
            #expect(registry.current[kind] == scope)
        }
    }

    @Test("Clearing the binding does not clear the registry")
    func clearingBindingPreservesRegistry() {
        let registry = ChartScopeRegistry()
        var current: ChartKind? = .dayPower

        let action = ChartExpansionAction { kind, scope in
            registry.current[kind] = scope
            current = kind
        }

        action(.dayPower, scope: .daySpecific(date: Date(timeIntervalSince1970: 1_700_000_000)))
        current = nil

        #expect(current == nil)
        #expect(registry.current[.dayPower] != nil)
    }

    @Test("Cover lifecycle: orientation lock enters landscape on open and resets on close")
    func coverLifecycleResetsOrientationLock() {
        let lock = OrientationLock()
        #expect(lock.mask == .portrait)

        lock.enter(.allButUpsideDown)
        #expect(lock.mask == .allButUpsideDown)
        #expect(lock.depth == 1)

        lock.exit()
        #expect(lock.mask == .portrait)
        #expect(lock.depth == 0)
    }

    @Test("Belt-and-braces: a second exit (e.g. SwiftUI onDisappear after viewWillDisappear) is a no-op")
    func extraExitIsSafe() {
        let lock = OrientationLock()
        lock.enter(.allButUpsideDown)
        lock.exit()
        lock.exit()

        #expect(lock.depth == 0)
        #expect(lock.mask == .portrait)
    }

    @Test("Tab-switch teardown: belt-and-braces resets the lock if viewWillDisappear never fired")
    func tabSwitchTeardownResetsLockWithoutViewWillDisappear() {
        let lock = OrientationLock()
        lock.enter(.allButUpsideDown)
        #expect(lock.depth == 1)
        #expect(lock.mask == .allButUpsideDown)

        // Simulates SwiftUI .onDisappear belt-and-braces firing without the
        // controller's viewWillDisappear path running (e.g. parent unmount
        // during tab switch).
        lock.exit()

        #expect(lock.depth == 0)
        #expect(lock.mask == .portrait)
    }

    @Test("Re-expanding a kind after dismissal restarts the cycle cleanly")
    func reopeningAfterDismissalRestarts() {
        let lock = OrientationLock()
        let registry = ChartScopeRegistry()
        var current: ChartKind?

        let action = ChartExpansionAction { kind, scope in
            registry.current[kind] = scope
            current = kind
        }

        action(.dayPower, scope: .daySpecific(date: Date(timeIntervalSince1970: 1_700_000_000)))
        lock.enter(.allButUpsideDown)
        lock.exit()
        current = nil

        action(.dayPower, scope: .daySpecific(date: Date(timeIntervalSince1970: 1_800_000_000)))
        lock.enter(.allButUpsideDown)

        #expect(current == .dayPower)
        #expect(lock.mask == .allButUpsideDown)
        #expect(registry.current[.dayPower] == .daySpecific(date: Date(timeIntervalSince1970: 1_800_000_000)))

        lock.exit()
        #expect(lock.mask == .portrait)
    }

    @Test("ChartKind is Identifiable so it can drive fullScreenCover(item:)")
    func chartKindIsIdentifiable() {
        for kind in ChartKind.allCases {
            #expect(kind.id == kind.rawValue)
        }
    }
}
#endif
