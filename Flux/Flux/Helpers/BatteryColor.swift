import SwiftUI

enum BatteryColor {
    static func forSOC(_ soc: Double) -> Color {
        if soc > 60 {
            return .green
        }
        if soc >= 30 {
            return .primary
        }
        if soc >= 15 {
            return .orange
        }
        return .red
    }
}
