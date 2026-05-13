import Foundation
import Testing
@testable import Flux

@MainActor
@Suite
struct XSelectionQuiescenceGateTests {
    private final class TestClock {
        private var now: Date = Date(timeIntervalSince1970: 1_700_000_000)

        func current() -> Date { now }

        func advance(by seconds: TimeInterval) {
            now = now.addingTimeInterval(seconds)
        }
    }

    @Test("Default quiet window is 400 ms")
    func defaultQuietWindowIs400ms() {
        let clock = TestClock()
        let gate = XSelectionQuiescenceGate<Int>(clock: clock.current)
        gate.noteSelectionChange()

        clock.advance(by: 0.399)
        #expect(gate.isStale == true)

        clock.advance(by: 0.002)
        #expect(gate.isStale == false)
    }

    @Test("Without any selection activity, adopt applies snapshots immediately")
    func adoptFiresImmediatelyWhenIdle() {
        let clock = TestClock()
        let gate = XSelectionQuiescenceGate<Int>(clock: clock.current)
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.adopt(5)
        #expect(applied == [5])
    }

    @Test("Adopting during the quiet window pends the snapshot")
    func adoptDuringQuietPends() {
        let clock = TestClock()
        let gate = XSelectionQuiescenceGate<Int>(clock: clock.current)
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.noteSelectionChange()
        clock.advance(by: 0.1)
        gate.adopt(42)

        #expect(applied.isEmpty)
    }

    @Test("Pending snapshot is flushed by tick after the quiet window elapses")
    func tickAfterQuietFlushesPending() {
        let clock = TestClock()
        let gate = XSelectionQuiescenceGate<Int>(clock: clock.current)
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.noteSelectionChange()
        clock.advance(by: 0.1)
        gate.adopt(42)
        clock.advance(by: 0.05)
        gate.tick()
        #expect(applied.isEmpty)

        clock.advance(by: 0.3)
        gate.tick()
        #expect(applied == [42])
    }

    @Test("Multiple snapshots during a quiet window collapse to the latest on flush")
    func multipleSnapshotsCollapseToLatest() {
        let clock = TestClock()
        let gate = XSelectionQuiescenceGate<Int>(clock: clock.current)
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.noteSelectionChange()
        clock.advance(by: 0.05)
        gate.adopt(1)
        clock.advance(by: 0.05)
        gate.adopt(2)
        clock.advance(by: 0.05)
        gate.adopt(3)
        clock.advance(by: 0.4)
        gate.tick()

        #expect(applied == [3])
    }

    @Test("Selection change during a pending state extends the quiet window")
    func selectionChangeExtendsQuietWindow() {
        let clock = TestClock()
        let gate = XSelectionQuiescenceGate<Int>(clock: clock.current)
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.noteSelectionChange()
        clock.advance(by: 0.1)
        gate.adopt(7)

        clock.advance(by: 0.3)
        gate.noteSelectionChange()

        clock.advance(by: 0.3)
        gate.tick()
        #expect(applied.isEmpty)

        clock.advance(by: 0.2)
        gate.tick()
        #expect(applied == [7])
    }

    @Test("Selection cleared flushes pending and unblocks subsequent updates")
    func clearFlushesAndUnblocks() {
        let clock = TestClock()
        let gate = XSelectionQuiescenceGate<Int>(clock: clock.current)
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.noteSelectionChange()
        clock.advance(by: 0.1)
        gate.adopt(11)
        #expect(applied.isEmpty)

        gate.noteSelectionCleared()
        #expect(applied == [11])
        #expect(gate.isStale == false)

        gate.adopt(22)
        #expect(applied == [11, 22])
    }

    @Test("Clearing with no pending snapshot is a no-op for onApply")
    func clearWithoutPendingIsNoOp() {
        let clock = TestClock()
        let gate = XSelectionQuiescenceGate<Int>(clock: clock.current)
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.noteSelectionChange()
        gate.noteSelectionCleared()

        #expect(applied.isEmpty)
        #expect(gate.isStale == false)
    }
}
