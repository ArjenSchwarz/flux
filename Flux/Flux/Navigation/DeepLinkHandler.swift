import FluxCore
import Foundation

enum DeepLinkHandler {
    enum Action: Equatable {
        case navigate(Screen)
        case none
    }

    static func handle(_ url: URL) -> Action {
        guard let destination = WidgetDeepLink.parse(url) else {
            return .none
        }

        switch destination {
        case .dashboard:
            return .navigate(.dashboard)
        }
    }
}
