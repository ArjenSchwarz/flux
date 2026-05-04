import Foundation

/// The three primary screens reachable from the V5 tab bar.
enum FluxTab: String, CaseIterable, Identifiable, Hashable {
    case dashboard
    case today
    case history

    var id: String { rawValue }

    var title: String {
        switch self {
        case .dashboard: "Dashboard"
        case .today: "Details"
        case .history: "History"
        }
    }
}
