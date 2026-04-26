import Foundation

enum HistoryFormatters {
    /// All current call sites pass non-negative period totals, so the
    /// branch chooses precision from `value` directly.
    static func kwh(_ value: Double) -> String {
        let format = value >= 100 ? "%.0f kWh" : "%.1f kWh"
        return String(format: format, value)
    }
}
