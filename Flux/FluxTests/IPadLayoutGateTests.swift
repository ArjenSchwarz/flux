#if os(macOS)
import SwiftUI
import Testing
@testable import Flux

@MainActor
struct IPadLayoutGateTests {
    @Test
    func macOSAlwaysReturnsTrue() {
        // macOS scene `minWidth=960pt` makes the 1-column tier unreachable at
        // runtime; the gate is the single source of truth that drives
        // Dashboard, Day Detail, and History to their adaptive bodies.
        #expect(IPadLayoutGate.isActive(hSizeClass: nil) == true)
        #expect(IPadLayoutGate.isActive(hSizeClass: .regular) == true)
        #expect(IPadLayoutGate.isActive(hSizeClass: .compact) == true)
    }
}
#endif
