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
}

enum ChartScope: Hashable, Codable {
    case historyRange(days: Int)
    case daySpecific(date: Date)
}
