import Foundation
import SwiftUI
import Testing
@testable import Flux

@MainActor
@Suite
struct ExpansionAccessibilityTests {
    @Test("Coordinator starts with no pending focus restoration")
    func initialState() {
        let coordinator = ChartExpansionFocusCoordinator()
        #expect(coordinator.pendingRequest == nil)
    }

    @Test("requestRestore records the kind so the matching container can react")
    func requestRestoreRecordsKind() {
        let coordinator = ChartExpansionFocusCoordinator()
        coordinator.requestRestore(for: .dayPower)
        #expect(coordinator.pendingRequest?.kind == .dayPower)
    }

    @Test("Repeated requestRestore for the same kind advances the token so SwiftUI sees a change")
    func repeatedRequestAdvancesToken() {
        let coordinator = ChartExpansionFocusCoordinator()
        coordinator.requestRestore(for: .historySolar)
        let first = coordinator.pendingRequest
        coordinator.requestRestore(for: .historySolar)
        let second = coordinator.pendingRequest
        #expect(first != nil)
        #expect(second != nil)
        #expect(first != second)
        #expect(first?.kind == second?.kind)
    }

    @Test("requestRestore replaces the pending kind when a different chart is dismissed")
    func laterRequestReplacesPending() {
        let coordinator = ChartExpansionFocusCoordinator()
        coordinator.requestRestore(for: .historyGridUsage)
        coordinator.requestRestore(for: .dayBatteryCombined)
        #expect(coordinator.pendingRequest?.kind == .dayBatteryCombined)
    }

    @Test("consume clears the pending request for the matching kind")
    func consumeClearsForMatch() {
        let coordinator = ChartExpansionFocusCoordinator()
        coordinator.requestRestore(for: .dayPower)
        let request = coordinator.pendingRequest
        #expect(request != nil)
        coordinator.consume(request!)
        #expect(coordinator.pendingRequest == nil)
    }

    @Test("consume is a no-op when the token does not match (defends against stale callbacks)")
    func consumeIgnoresStaleToken() {
        let coordinator = ChartExpansionFocusCoordinator()
        coordinator.requestRestore(for: .dayPower)
        let stale = coordinator.pendingRequest
        coordinator.requestRestore(for: .dayPower)
        coordinator.consume(stale!)
        #expect(coordinator.pendingRequest != nil)
    }

    @Test("shouldRestoreFocus(for:) is true only for the kind matching the pending request")
    func shouldRestoreFocusOnlyForMatchingKind() {
        let coordinator = ChartExpansionFocusCoordinator()
        coordinator.requestRestore(for: .dayPower)
        #expect(coordinator.shouldRestoreFocus(for: .dayPower) == true)
        #expect(coordinator.shouldRestoreFocus(for: .historySolar) == false)
        #expect(coordinator.shouldRestoreFocus(for: .historyGridUsage) == false)
    }

    @Test("shouldRestoreFocus(for:) is false when there is no pending request")
    func shouldRestoreFocusFalseWhenIdle() {
        let coordinator = ChartExpansionFocusCoordinator()
        for kind in ChartKind.allCases {
            #expect(coordinator.shouldRestoreFocus(for: kind) == false)
        }
    }

    @Test("Restore is requested through the environment-injected coordinator on dismissal")
    func environmentCoordinatorReceivesRestoreOnDismissal() {
        // Simulates the platform-specific dismissal flow: after the
        // presentation closes, the dismissal site invokes the coordinator
        // with the kind that was open. The container's binding then reacts.
        let coordinator = ChartExpansionFocusCoordinator()
        let openKind: ChartKind = .historyDailyUsage
        coordinator.requestRestore(for: openKind)
        #expect(coordinator.shouldRestoreFocus(for: openKind) == true)
    }

    @Test("Each ChartKind has a focus-restoration cycle that does not leak into other kinds")
    func eachKindHasIndependentRestoreCycle() {
        let coordinator = ChartExpansionFocusCoordinator()
        for kind in ChartKind.allCases {
            coordinator.requestRestore(for: kind)
            #expect(coordinator.shouldRestoreFocus(for: kind) == true)
            for other in ChartKind.allCases where other != kind {
                #expect(coordinator.shouldRestoreFocus(for: other) == false)
            }
            coordinator.consume(coordinator.pendingRequest!)
            #expect(coordinator.pendingRequest == nil)
        }
    }
}
