import Foundation
import Testing
@testable import Flux

@MainActor
@Suite
struct HistoryDragGateTests {
    @Test("New gate is not stale and applies adopted snapshots immediately")
    func notStaleBeforeDragStart() {
        let gate = HistoryDragGate<Int>()
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        #expect(gate.isStale == false)

        gate.adopt(42)

        #expect(applied == [42])
    }

    @Test("beginDrag flips isStale to true; endDrag flips it back")
    func dragLifecycleTogglesStale() {
        let gate = HistoryDragGate<Int>()

        gate.beginDrag()
        #expect(gate.isStale == true)

        gate.endDrag()
        #expect(gate.isStale == false)
    }

    @Test("Snapshot adopted during a drag is held until endDrag, then applied once")
    func pendingFlushedOnDragEnd() {
        let gate = HistoryDragGate<Int>()
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.beginDrag()
        gate.adopt(7)
        #expect(applied.isEmpty)

        gate.endDrag()
        #expect(applied == [7])
    }

    @Test("Multiple snapshots during a drag collapse to the latest one on flush")
    func onlyLatestPendingFlushed() {
        let gate = HistoryDragGate<Int>()
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.beginDrag()
        gate.adopt(1)
        gate.adopt(2)
        gate.adopt(3)
        #expect(applied.isEmpty)

        gate.endDrag()
        #expect(applied == [3])
    }

    @Test("endDrag with no pending snapshot does not call onApply")
    func endDragWithoutPendingIsNoOp() {
        let gate = HistoryDragGate<Int>()
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.beginDrag()
        gate.endDrag()

        #expect(applied.isEmpty)
    }

    @Test("Pending snapshots do not leak across drags")
    func pendingDoesNotLeakAcrossDrags() {
        let gate = HistoryDragGate<Int>()
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.beginDrag()
        gate.adopt(11)
        gate.endDrag()
        #expect(applied == [11])

        gate.beginDrag()
        gate.endDrag()

        #expect(applied == [11])
    }

    @Test("Snapshot adopted between drags is applied immediately")
    func adoptBetweenDragsFiresImmediately() {
        let gate = HistoryDragGate<Int>()
        var applied: [Int] = []
        gate.onApply = { applied.append($0) }

        gate.beginDrag()
        gate.endDrag()
        gate.adopt(99)

        #expect(applied == [99])
    }
}
