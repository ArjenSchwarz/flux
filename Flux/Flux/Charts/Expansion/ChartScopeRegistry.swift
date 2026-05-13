import Foundation
import Observation

@MainActor
@Observable
final class ChartScopeRegistry {
    var current: [ChartKind: ChartScope] = [:]
}
