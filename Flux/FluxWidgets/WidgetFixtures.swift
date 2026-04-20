#if DEBUG
import FluxCore
import Foundation

enum WidgetFixtures {
    static func envelope(
        soc: Double = 73.4,
        pbat: Double = 2100,
        pload: Double = 412,
        ppv: Double = 1800,
        pgrid: Double = 210,
        ageMinutes: Double = 0
    ) -> StatusSnapshotEnvelope {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        return StatusSnapshotEnvelope(
            fetchedAt: now.addingTimeInterval(-ageMinutes * 60),
            status: StatusResponse(
                live: LiveData(
                    ppv: ppv,
                    pload: pload,
                    pbat: pbat,
                    pgrid: pgrid,
                    pgridSustained: false,
                    soc: soc,
                    timestamp: "2026-04-20T10:00:00Z"
                ),
                battery: BatteryInfo(
                    capacityKwh: 10,
                    cutoffPercent: 10,
                    estimatedCutoffTime: "2026-04-20T17:12:00Z",
                    low24h: Low24h(soc: 38.2, timestamp: "2026-04-20T06:00:00Z")
                ),
                rolling15min: RollingAvg(
                    avgLoad: 400,
                    avgPbat: 2000,
                    estimatedCutoffTime: "2026-04-20T17:12:00Z"
                ),
                offpeak: OffpeakData(
                    windowStart: OffpeakData.defaultWindowStart,
                    windowEnd: OffpeakData.defaultWindowEnd,
                    gridUsageKwh: 1.2,
                    solarKwh: nil,
                    batteryChargeKwh: nil,
                    batteryDischargeKwh: nil,
                    gridExportKwh: nil,
                    batteryDeltaPercent: 8.5
                ),
                todayEnergy: nil
            )
        )
    }

    static func entry(
        staleness: Staleness = .fresh,
        source: StatusEntry.Source = .live,
        soc: Double = 73.4,
        pbat: Double = 2100,
        pload: Double = 412,
        ppv: Double = 1800,
        pgrid: Double = 210,
        ageMinutes: Double = 0
    ) -> StatusEntry {
        let env = envelope(
            soc: soc,
            pbat: pbat,
            pload: pload,
            ppv: ppv,
            pgrid: pgrid,
            ageMinutes: ageMinutes
        )
        return StatusEntry(
            date: Date(timeIntervalSince1970: 1_800_000_000),
            envelope: env,
            staleness: staleness,
            source: source
        )
    }

    static var placeholderEntry: StatusEntry {
        let date = Date(timeIntervalSince1970: 1_800_000_000)
        return StatusEntry(
            date: date,
            envelope: StatusTimelineLogic.placeholderEnvelope(now: date),
            staleness: .fresh,
            source: .placeholder
        )
    }
}
#endif
