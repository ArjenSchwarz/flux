import FluxCore
import Foundation

enum ChartKind: String, Hashable, Codable, CaseIterable, Identifiable {
    case historySolar
    case historyGridUsage
    case historyDailyUsage
    case dayPower
    case dayBatteryCombined

    var id: String { rawValue }

    enum HostKind: Hashable {
        case history
        case day
    }

    var hostKind: HostKind {
        switch self {
        case .historySolar, .historyGridUsage, .historyDailyUsage:
            return .history
        case .dayPower, .dayBatteryCombined:
            return .day
        }
    }

    /// Human-readable name spoken by VoiceOver and shown in user-facing
    /// failure messages. The `rawValue` is a camelCase identifier and
    /// would be read verbatim ("history solar" announced as
    /// "historySolar"), so a dedicated display string is needed.
    var displayName: String {
        switch self {
        case .historySolar:
            return "solar history"
        case .historyGridUsage:
            return "grid usage history"
        case .historyDailyUsage:
            return "daily usage history"
        case .dayPower:
            return "day power"
        case .dayBatteryCombined:
            return "day battery"
        }
    }
}

enum ChartScope: Hashable, Codable {
    /// Carries the full `HistoryQuery` rather than a day count so an enlarged
    /// chart fetches the same window as the card it came from — including a
    /// navigated past period (Decision 13).
    case historyRange(HistoryQuery)
    case daySpecific(date: Date)
}
