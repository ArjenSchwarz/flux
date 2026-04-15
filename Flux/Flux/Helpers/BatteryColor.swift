import SwiftUI

nonisolated enum ColorTier: Sendable, Equatable {
    case green, red, orange, amber, normal

    var color: Color {
        switch self {
        case .green: .green
        case .red: .red
        case .orange: .orange
        case .amber: .yellow
        case .normal: .primary
        }
    }
}

enum BatteryColor {
    static func forSOC(_ soc: Double) -> ColorTier {
        if soc > 60 {
            return .green
        }
        if soc >= 30 {
            return .normal
        }
        if soc >= 15 {
            return .orange
        }
        return .red
    }
}
