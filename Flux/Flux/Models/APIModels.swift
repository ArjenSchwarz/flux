import Foundation

// MARK: - /status response

struct StatusResponse: Codable, Sendable {
    let live: LiveData?
    let battery: BatteryInfo?
    let rolling15min: RollingAvg?
    let offpeak: OffpeakData?
    let todayEnergy: TodayEnergy?
}

struct LiveData: Codable, Sendable {
    let ppv: Double
    let pload: Double
    let pbat: Double
    let pgrid: Double
    let pgridSustained: Bool
    let soc: Double
    let timestamp: String
}

struct BatteryInfo: Codable, Sendable {
    let capacityKwh: Double
    let cutoffPercent: Int
    let estimatedCutoffTime: String?
    let low24h: Low24h?
}

struct Low24h: Codable, Sendable {
    let soc: Double
    let timestamp: String
}

struct RollingAvg: Codable, Sendable {
    let avgLoad: Double
    let avgPbat: Double
    let estimatedCutoffTime: String?
}

struct OffpeakData: Codable, Sendable {
    static let defaultWindowStart = "11:00"
    static let defaultWindowEnd = "14:00"

    let windowStart: String
    let windowEnd: String
    let gridUsageKwh: Double?
    let solarKwh: Double?
    let batteryChargeKwh: Double?
    let batteryDischargeKwh: Double?
    let gridExportKwh: Double?
    let batteryDeltaPercent: Double?
}

struct TodayEnergy: Codable, Sendable {
    let epv: Double
    let eInput: Double
    let eOutput: Double
    let eCharge: Double
    let eDischarge: Double
}

// MARK: - /history response

struct HistoryResponse: Codable, Sendable {
    let days: [DayEnergy]
}

struct DayEnergy: Codable, Sendable, Identifiable {
    let date: String
    let epv: Double
    let eInput: Double
    let eOutput: Double
    let eCharge: Double
    let eDischarge: Double

    var id: String { date }
}

// MARK: - /day response

struct PeakPeriod: Codable, Sendable, Identifiable {
    let start: String
    let end: String
    let avgLoadW: Double
    let energyWh: Double

    var id: String { start }
}

struct DayDetailResponse: Codable, Sendable {
    let date: String
    let readings: [TimeSeriesPoint]
    let summary: DaySummary?
    let peakPeriods: [PeakPeriod]?
}

struct TimeSeriesPoint: Codable, Sendable, Identifiable {
    let timestamp: String
    let ppv: Double
    let pload: Double
    let pbat: Double
    let pgrid: Double
    let soc: Double

    var id: String { timestamp }
}

struct DaySummary: Codable, Sendable {
    let epv: Double?
    let eInput: Double?
    let eOutput: Double?
    let eCharge: Double?
    let eDischarge: Double?
    let socLow: Double?
    let socLowTime: String?
}

// MARK: - Error response

struct APIErrorResponse: Codable, Sendable {
    let error: String
}
