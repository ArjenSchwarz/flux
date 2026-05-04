import Foundation

enum Screen: String, CaseIterable, Identifiable {
    case dashboard
    case history
    case settings

    var id: String { rawValue }

    var title: String {
        switch self {
        case .dashboard: "Dashboard"
        case .history: "History"
        case .settings: "Settings"
        }
    }

    var systemImage: String {
        switch self {
        case .dashboard: "speedometer"
        case .history: "chart.bar.xaxis"
        case .settings: "gearshape"
        }
    }
}
