import Foundation

public enum SOCFormatting {
    public static func format(_ soc: Double) -> String {
        if soc >= 99.95 {
            return "100%"
        }
        return String(format: "%.1f%%", soc)
    }

    public static func symbol(for soc: Double) -> String {
        switch soc {
        case ..<13: return "battery.0percent"
        case ..<38: return "battery.25percent"
        case ..<63: return "battery.50percent"
        case ..<88: return "battery.75percent"
        default: return "battery.100percent"
        }
    }
}
