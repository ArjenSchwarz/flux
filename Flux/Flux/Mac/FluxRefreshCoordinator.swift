import Foundation
import Observation

@MainActor @Observable
final class FluxRefreshCoordinator {
    var refresh: (@MainActor () -> Void)?

    init() {}
}
