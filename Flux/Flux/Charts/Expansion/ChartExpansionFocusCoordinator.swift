import SwiftUI

@MainActor
@Observable
final class ChartExpansionFocusCoordinator {
    struct Request: Equatable, Sendable {
        let kind: ChartKind
        let token: Int
    }

    private(set) var pendingRequest: Request?
    private var nextToken: Int = 0

    func requestRestore(for kind: ChartKind) {
        nextToken &+= 1
        pendingRequest = Request(kind: kind, token: nextToken)
    }

    func consume(_ request: Request) {
        if pendingRequest == request {
            pendingRequest = nil
        }
    }

    func shouldRestoreFocus(for kind: ChartKind) -> Bool {
        pendingRequest?.kind == kind
    }
}

extension EnvironmentValues {
    @Entry var chartExpansionFocus: ChartExpansionFocusCoordinator = ChartExpansionFocusCoordinator()
}
