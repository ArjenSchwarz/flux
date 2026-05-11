import Foundation

/// Comparison period the user selects via the Compare period chip.
/// `dayOffset` is applied to the selected day in the site-local
/// (Sydney) calendar to resolve the comparison day.
enum ComparePeriod: String, CaseIterable, Identifiable, Sendable {
    case yesterday
    case sevenDaysAgo

    var id: String { rawValue }
    var dayOffset: Int { self == .yesterday ? -1 : -7 }
    var displayName: String { self == .yesterday ? "Yesterday" : "7 days ago" }

    /// Used by `@AppStorage`'s rawValue codec when a future build introduced
    /// new cases that the current build doesn't know about. Returning the
    /// default keeps the toggle/chip operable instead of crashing.
    static func parseOrDefault(_ raw: String?) -> ComparePeriod {
        ComparePeriod(rawValue: raw ?? "") ?? .yesterday
    }
}
