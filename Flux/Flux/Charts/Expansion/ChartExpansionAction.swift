import SwiftUI

struct ChartExpansionAction {
    let expand: (ChartKind, ChartScope) -> Void

    func callAsFunction(_ kind: ChartKind, scope: ChartScope) {
        expand(kind, scope)
    }
}

extension EnvironmentValues {
    @Entry var chartExpansion: ChartExpansionAction = .init { _, _ in }
}
