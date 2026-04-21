import Foundation

public enum SOCFormatting {
    public static func format(_ soc: Double) -> String {
        if soc >= 99.95 {
            return "100%"
        }
        return String(format: "%.1f%%", soc)
    }
}
