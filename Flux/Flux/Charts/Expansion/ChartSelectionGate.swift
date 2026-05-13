import Foundation

@MainActor
protocol ChartSelectionGate<Snapshot>: AnyObject {
    associatedtype Snapshot
    var isStale: Bool { get }
    var onApply: ((Snapshot) -> Void)? { get set }
    func adopt(_ snapshot: Snapshot)
}

@MainActor
final class HistoryDragGate<Snapshot>: ChartSelectionGate {
    private(set) var dragging: Bool = false
    private var pending: Snapshot?
    var onApply: ((Snapshot) -> Void)?

    var isStale: Bool { dragging }

    func beginDrag() {
        dragging = true
    }

    func endDrag() {
        dragging = false
        if let snapshot = pending {
            pending = nil
            onApply?(snapshot)
        }
    }

    func adopt(_ snapshot: Snapshot) {
        if dragging {
            pending = snapshot
        } else {
            onApply?(snapshot)
        }
    }
}

@MainActor
final class XSelectionQuiescenceGate<Snapshot>: ChartSelectionGate {
    private let quietWindow: TimeInterval
    private let clock: () -> Date
    private var lastSelectionChange: Date = .distantPast
    private var pending: Snapshot?
    var onApply: ((Snapshot) -> Void)?

    init(quietWindow: TimeInterval = 0.4, clock: @escaping () -> Date = Date.init) {
        self.quietWindow = quietWindow
        self.clock = clock
    }

    var isStale: Bool {
        clock().timeIntervalSince(lastSelectionChange) < quietWindow
    }

    func noteSelectionChange() {
        lastSelectionChange = clock()
    }

    func noteSelectionCleared() {
        lastSelectionChange = .distantPast
        flushPending()
    }

    func tick() {
        if !isStale {
            flushPending()
        }
    }

    func adopt(_ snapshot: Snapshot) {
        if isStale {
            pending = snapshot
        } else {
            onApply?(snapshot)
        }
    }

    private func flushPending() {
        if let snapshot = pending {
            pending = nil
            onApply?(snapshot)
        }
    }
}
