import Foundation

enum HistoryFormatters {
    static func kwh(_ value: Double) -> String {
        let absValue = abs(value)
        let format = absValue >= 100 ? "%.0f kWh" : "%.1f kWh"
        return String(format: format, value)
    }
}
