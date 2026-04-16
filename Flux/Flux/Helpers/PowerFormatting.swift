import Foundation

enum PowerFormatting {
    static func format(_ watts: Double) -> String {
        let absolute = abs(watts)
        if absolute >= 1000 {
            return String(format: "%.2f kW", absolute / 1000)
        }
        return String(format: "%.0f W", absolute)
    }
}
