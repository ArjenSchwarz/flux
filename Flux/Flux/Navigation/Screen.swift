import Foundation

enum Screen: String, CaseIterable, Identifiable {
    case dashboard
    case today
    case history
    case settings

    var id: String { rawValue }

    var title: String {
        switch self {
        case .dashboard: "Dashboard"
        case .today: "Today"
        case .history: "History"
        case .settings: "Settings"
        }
    }

    var systemImage: String {
        switch self {
        case .dashboard: "speedometer"
        case .today: "sun.max"
        case .history: "chart.bar.xaxis"
        case .settings: "gearshape"
        }
    }

    static var sidebarVisible: [Screen] {
        #if os(macOS)
        // macOS uses the Settings scene (⌘,) instead of an inline entry.
        return Screen.allCases.filter { $0 != .settings }
        #else
        // `.today` is a macOS-only sidebar entry per T-1081 polish; iOS
        // continues to reach Day Detail via the Dashboard's "Today detail"
        // button.
        return Screen.allCases.filter { $0 != .today }
        #endif
    }
}
