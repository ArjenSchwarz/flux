import Foundation

enum ChartKind: String, Hashable, Codable, CaseIterable {
    case historySolar
    case historyGridUsage
    case historyDailyUsage
    case dayPower
    case dayBatteryCombined
}

enum ChartScope: Hashable, Codable {
    case historyRange(days: Int)
    case daySpecific(date: Date)
}
