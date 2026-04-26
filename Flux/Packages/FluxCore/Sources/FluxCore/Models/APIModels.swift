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

    public static let statusComplete = "complete"
    public static let statusPending = "pending"

    public let windowStart: String
    public let windowEnd: String
    public let status: String?
    public let gridUsageKwh: Double?
    public let solarKwh: Double?
    public let batteryChargeKwh: Double?
    public let batteryDischargeKwh: Double?
    public let gridExportKwh: Double?
    public let batteryDeltaPercent: Double?

    public var isInProgress: Bool { status == Self.statusPending }

    public init(
        windowStart: String,
        windowEnd: String,
        status: String? = nil,
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

    public init(
        date: String,
        readings: [TimeSeriesPoint],
        summary: DaySummary?,
        peakPeriods: [PeakPeriod]?
    ) {
        self.date = date
        self.readings = readings
        self.summary = summary
        self.peakPeriods = peakPeriods
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
