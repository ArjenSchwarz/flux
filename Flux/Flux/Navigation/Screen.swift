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

    static var sidebarVisible: [Screen] {
        #if os(macOS)
        return Screen.allCases.filter { $0 != .settings }
        #else
        return Screen.allCases
        #endif
    }
}
