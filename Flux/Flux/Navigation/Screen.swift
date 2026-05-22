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
        case .today: "Details"
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

    /// macOS uses the Settings scene (⌘,); iPad regular uses the per-screen
    /// settings affordance. Neither shell wants a Settings sidebar row.
    static var sidebarVisible: [Screen] {
        Screen.allCases.filter { $0 != .settings }
    }

    var tab: FluxTab? {
        switch self {
        case .dashboard: .dashboard
        case .today: .today
        case .history: .history
        case .settings: nil
        }
    }

    init(tab: FluxTab) {
        switch tab {
        case .dashboard: self = .dashboard
        case .today: self = .today
        case .history: self = .history
        }
    }
}
