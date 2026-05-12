#if canImport(UIKit) && !os(macOS)
import Foundation
import Testing
import UIKit
@testable import Flux

@MainActor
@Suite
struct OrientationLockTests {
    @Test("Default mask is portrait")
    func defaultMaskIsPortrait() {
        let lock = OrientationLock()
        #expect(lock.mask == .portrait)
        #expect(lock.depth == 0)
    }

    @Test("enter applies the requested mask and increments depth")
    func enterAppliesMask() {
        let lock = OrientationLock()
        lock.enter(.allButUpsideDown)

        #expect(lock.mask == .allButUpsideDown)
        #expect(lock.depth == 1)
    }

    @Test("Balanced enter/exit restores the default mask")
    func balancedEnterExitRestoresDefault() {
        let lock = OrientationLock()
        lock.enter(.allButUpsideDown)
        lock.exit()

        #expect(lock.mask == .portrait)
        #expect(lock.depth == 0)
    }

    @Test("Nested enter calls keep the latest mask until depth returns to zero")
    func nestedEnterKeepsLatestMask() {
        let lock = OrientationLock()
        lock.enter(.allButUpsideDown)
        lock.enter(.landscape)

        #expect(lock.depth == 2)
        #expect(lock.mask == .landscape)

        lock.exit()
        #expect(lock.depth == 1)
        #expect(lock.mask == .landscape)

        lock.exit()
        #expect(lock.depth == 0)
        #expect(lock.mask == .portrait)
    }

    @Test("Extra exit calls do not underflow the depth or change the mask")
    func extraExitIsNoOp() {
        let lock = OrientationLock()
        lock.exit()
        lock.exit()

        #expect(lock.depth == 0)
        #expect(lock.mask == .portrait)

        lock.enter(.allButUpsideDown)
        lock.exit()
        lock.exit()

        #expect(lock.depth == 0)
        #expect(lock.mask == .portrait)
    }

    @Test("Custom default mask is restored after balanced enter/exit")
    func customDefaultMaskRestored() {
        let lock = OrientationLock(defaultMask: .all)
        #expect(lock.mask == .all)

        lock.enter(.portrait)
        #expect(lock.mask == .portrait)

        lock.exit()
        #expect(lock.mask == .all)
    }
}
#endif
