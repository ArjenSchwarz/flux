import Foundation

// MARK: - /status response

public struct StatusResponse: Codable, Sendable {
    public let live: LiveData?
    public let battery: BatteryInfo?
    public let rolling15min: RollingAvg?
    public let offpeak: OffpeakData?
    public let todayEnergy: TodayEnergy?

    public init(
        live: LiveData?,
        battery: BatteryInfo?,
        rolling15min: RollingAvg?,
        offpeak: OffpeakData?,
        todayEnergy: TodayEnergy?
    ) {
        self.live = live
        self.battery = battery
        self.rolling15min = rolling15min
        self.offpeak = offpeak
        self.todayEnergy = todayEnergy
    }
}

public struct LiveData: Codable, Sendable {
    public let ppv: Double
    public let pload: Double
    public let pbat: Double
    public let pgrid: Double
    public let pgridSustained: Bool
    public let soc: Double
    public let timestamp: String

    public init(
        ppv: Double,
        pload: Double,
        pbat: Double,
        pgrid: Double,
        pgridSustained: Bool,
        soc: Double,
        timestamp: String
    ) {
        self.ppv = ppv
        self.pload = pload
        self.pbat = pbat
        self.pgrid = pgrid
        self.pgridSustained = pgridSustained
        self.soc = soc
        self.timestamp = timestamp
    }
}

public struct BatteryInfo: Codable, Sendable {
    public let capacityKwh: Double
    public let cutoffPercent: Int
    public let estimatedCutoffTime: String?
    public let low24h: Low24h?

    public init(
        capacityKwh: Double,
        cutoffPercent: Int,
        estimatedCutoffTime: String?,
        low24h: Low24h?
    ) {
        self.capacityKwh = capacityKwh
        self.cutoffPercent = cutoffPercent
        self.estimatedCutoffTime = estimatedCutoffTime
        self.low24h = low24h
    }
}

public struct Low24h: Codable, Sendable {
    public let soc: Double
    public let timestamp: String

    public init(soc: Double, timestamp: String) {
        self.soc = soc
        self.timestamp = timestamp
    }
}

public struct RollingAvg: Codable, Sendable {
    public let avgLoad: Double
    public let avgPbat: Double
    public let estimatedCutoffTime: String?

    public init(avgLoad: Double, avgPbat: Double, estimatedCutoffTime: String?) {
        self.avgLoad = avgLoad
        self.avgPbat = avgPbat
        self.estimatedCutoffTime = estimatedCutoffTime
    }
}

public struct OffpeakData: Codable, Sendable {
    public static let defaultWindowStart = "11:00"
    public static let defaultWindowEnd = "14:00"

    /// Lifecycle of the off-peak record for the day. `pending` covers the
    /// in-progress window where deltas are projected against today's
    /// running totals; `complete` is the final post-window record.
    public enum Status: String, Codable, Sendable {
        case pending
        case complete
    }

    public let windowStart: String
    public let windowEnd: String
    public let status: Status?
    public let gridUsageKwh: Double?
    public let solarKwh: Double?
    public let batteryChargeKwh: Double?
    public let batteryDischargeKwh: Double?
    public let gridExportKwh: Double?
    public let batteryDeltaPercent: Double?

    public init(
        windowStart: String,
        windowEnd: String,
        status: Status? = nil,
        gridUsageKwh: Double?,
        solarKwh: Double?,
        batteryChargeKwh: Double?,
        batteryDischargeKwh: Double?,
        gridExportKwh: Double?,
        batteryDeltaPercent: Double?
    ) {
        self.windowStart = windowStart
        self.windowEnd = windowEnd
        self.status = status
        self.gridUsageKwh = gridUsageKwh
        self.solarKwh = solarKwh
        self.batteryChargeKwh = batteryChargeKwh
        self.batteryDischargeKwh = batteryDischargeKwh
        self.gridExportKwh = gridExportKwh
        self.batteryDeltaPercent = batteryDeltaPercent
    }
}

public struct TodayEnergy: Codable, Sendable {
    public let epv: Double
    public let eInput: Double
    public let eOutput: Double
    public let eCharge: Double
    public let eDischarge: Double

    public init(epv: Double, eInput: Double, eOutput: Double, eCharge: Double, eDischarge: Double) {
        self.epv = epv
        self.eInput = eInput
        self.eOutput = eOutput
        self.eCharge = eCharge
        self.eDischarge = eDischarge
    }
}

// MARK: - /history response

public struct HistoryResponse: Codable, Sendable {
    public let days: [DayEnergy]

    public init(days: [DayEnergy]) {
        self.days = days
    }
}

public struct DayEnergy: Codable, Sendable, Identifiable {
    public let date: String
    public let epv: Double
    public let eInput: Double
    public let eOutput: Double
    public let eCharge: Double
    public let eDischarge: Double
    public let offpeakGridImportKwh: Double?
    /// Banked from the API for parity but currently unread by the UI: the
    /// History grid card uses `eOutput` (full-day exports) rather than the
    /// off-peak portion. Kept on the model so the field is available for a
    /// future "off-peak vs peak export" view without another schema change.
    public let offpeakGridExportKwh: Double?

    public var id: String { date }

    /// Grid imports outside the off-peak window, derived by subtracting the
    /// off-peak portion from the day's total. Returns `nil` when no off-peak
    /// data is available for the day, so callers can distinguish "unknown"
    /// from a true zero.
    public var peakGridImportKwh: Double? {
        guard let offpeak = offpeakGridImportKwh else { return nil }
        return max(0, eInput - offpeak)
    }

    public init(
        date: String,
        epv: Double,
        eInput: Double,
        eOutput: Double,
        eCharge: Double,
        eDischarge: Double,
        offpeakGridImportKwh: Double? = nil,
        offpeakGridExportKwh: Double? = nil
    ) {
        self.date = date
        self.epv = epv
        self.eInput = eInput
        self.eOutput = eOutput
        self.eCharge = eCharge
        self.eDischarge = eDischarge
        self.offpeakGridImportKwh = offpeakGridImportKwh
        self.offpeakGridExportKwh = offpeakGridExportKwh
    }
}

// MARK: - /day response

public struct PeakPeriod: Codable, Sendable, Identifiable {
    public let start: String
    public let end: String
    public let avgLoadW: Double
    public let energyWh: Double

    // Periods in a single day response never overlap, so the RFC 3339 start
    // timestamp is a stable, unique identifier for SwiftUI diffing.
    public var id: String { start }

    public init(start: String, end: String, avgLoadW: Double, energyWh: Double) {
        self.start = start
        self.end = end
        self.avgLoadW = avgLoadW
        self.energyWh = energyWh
    }
}

public struct DayDetailResponse: Codable, Sendable {
    public let date: String
    public let readings: [TimeSeriesPoint]
    public let summary: DaySummary?
    public let peakPeriods: [PeakPeriod]?
    public let eveningNight: EveningNight?

    public init(
        date: String,
        readings: [TimeSeriesPoint],
        summary: DaySummary?,
        peakPeriods: [PeakPeriod]?,
        eveningNight: EveningNight?
    ) {
        self.date = date
        self.readings = readings
        self.summary = summary
        self.peakPeriods = peakPeriods
        self.eveningNight = eveningNight
    }
}

public struct EveningNight: Codable, Sendable {
    public let evening: EveningNightBlock?
    public let night: EveningNightBlock?

    public var hasAnyBlock: Bool { evening != nil || night != nil }

    public init(evening: EveningNightBlock?, night: EveningNightBlock?) {
        self.evening = evening
        self.night = night
    }
}

public struct EveningNightBlock: Codable, Sendable, Identifiable {
    public enum Status: String, Codable, Sendable {
        case complete
        case inProgress = "in-progress"
    }

    public enum BoundarySource: String, Codable, Sendable {
        case readings
        case estimated
    }

    public let start: String
    public let end: String
    public let totalKwh: Double
    public let averageKwhPerHour: Double?
    public let status: Status
    public let boundarySource: BoundarySource

    // The block's RFC 3339 start timestamp is unique within a single response
    // (one evening, one night), so it's a stable identifier for SwiftUI diffing.
    public var id: String { start }

    public init(
        start: String,
        end: String,
        totalKwh: Double,
        averageKwhPerHour: Double?,
        status: Status,
        boundarySource: BoundarySource
    ) {
        self.start = start
        self.end = end
        self.totalKwh = totalKwh
        self.averageKwhPerHour = averageKwhPerHour
        self.status = status
        self.boundarySource = boundarySource
    }
}

public struct TimeSeriesPoint: Codable, Sendable, Identifiable {
    public let timestamp: String
    public let ppv: Double
    public let pload: Double
    public let pbat: Double
    public let pgrid: Double
    public let soc: Double

    public var id: String { timestamp }

    public init(
        timestamp: String,
        ppv: Double,
        pload: Double,
        pbat: Double,
        pgrid: Double,
        soc: Double
    ) {
        self.timestamp = timestamp
        self.ppv = ppv
        self.pload = pload
        self.pbat = pbat
        self.pgrid = pgrid
        self.soc = soc
    }
}

public struct DaySummary: Codable, Sendable {
    public let epv: Double?
    public let eInput: Double?
    public let eOutput: Double?
    public let eCharge: Double?
    public let eDischarge: Double?
    public let socLow: Double?
    public let socLowTime: String?

    public init(
        epv: Double?,
        eInput: Double?,
        eOutput: Double?,
        eCharge: Double?,
        eDischarge: Double?,
        socLow: Double?,
        socLowTime: String?
    ) {
        self.epv = epv
        self.eInput = eInput
        self.eOutput = eOutput
        self.eCharge = eCharge
        self.eDischarge = eDischarge
        self.socLow = socLow
        self.socLowTime = socLowTime
    }
}

// MARK: - Error response

public struct APIErrorResponse: Codable, Sendable {
    public let error: String

    public init(error: String) {
        self.error = error
    }
}
