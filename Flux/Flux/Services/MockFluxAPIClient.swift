import Foundation

final actor MockFluxAPIClient: FluxAPIClient {
    static let preview = MockFluxAPIClient()
    static let previewDate = "2026-04-15"

    private static let calendar: Calendar = {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = DateFormatting.sydneyTimeZone
        return calendar
    }()

    static let statusResponse = StatusResponse(
        live: LiveData(
            ppv: 2400,
            pload: 750,
            pbat: 400,
            pgrid: -100,
            pgridSustained: false,
            soc: 62.4,
            timestamp: "\(previewDate)T10:00:00Z"
        ),
        battery: BatteryInfo(
            capacityKwh: 13.3,
            cutoffPercent: 10,
            estimatedCutoffTime: "\(previewDate)T18:30:00Z",
            low24h: Low24h(soc: 38.2, timestamp: "\(previewDate)T08:45:00Z")
        ),
        rolling15min: RollingAvg(
            avgLoad: 243,
            avgPbat: 320,
            estimatedCutoffTime: "2026-04-16T03:00:00Z"
        ),
        offpeak: OffpeakData(
            windowStart: "11:00",
            windowEnd: "14:00",
            gridUsageKwh: 6.1,
            solarKwh: 2.3,
            batteryChargeKwh: 5.0,
            batteryDischargeKwh: 4.2,
            gridExportKwh: 1.4,
            batteryDeltaPercent: 42.3
        ),
        todayEnergy: TodayEnergy(
            epv: 14.3,
            eInput: 0.25,
            eOutput: 5.94,
            eCharge: 5.7,
            eDischarge: 6.8
        )
    )

    static let historyDays: [DayEnergy] = {
        guard let baseDate = DateFormatting.parseDayDate(previewDate) else {
            return []
        }

        var days: [DayEnergy] = []
        days.reserveCapacity(30)

        for dayOffset in 0 ..< 30 {
            guard let date = calendar.date(byAdding: .day, value: -dayOffset, to: baseDate) else {
                continue
            }
            let trend = Double(dayOffset)
            days.append(
                DayEnergy(
                    date: DateFormatting.dayDateString(from: date),
                    epv: max(0, 14.2 - trend * 0.17),
                    eInput: max(0.2, 2.3 - trend * 0.03),
                    eOutput: max(0.5, 5.1 - trend * 0.09),
                    eCharge: max(0.8, 5.4 - trend * 0.08),
                    eDischarge: max(1.0, 6.2 - trend * 0.10)
                )
            )
        }

        return days
    }()

    static let historyResponse = HistoryResponse(days: historyDays)

    static func dayDetailResponse(for date: String = previewDate) -> DayDetailResponse {
        var readings: [TimeSeriesPoint] = []
        readings.reserveCapacity(24 * 12)

        for minuteOfDay in stride(from: 0, to: 24 * 60, by: 5) {
            let hour = Double(minuteOfDay) / 60
            let hourInt = minuteOfDay / 60
            let minuteInt = minuteOfDay % 60

            let solar = max(0, 2400 * sin((hour - 6) * .pi / 12))
            let load = 520 + 160 * sin(hour * .pi / 6)
            let battery = 300 * sin((hour - 2) * .pi / 8)
            let grid = load - solar - battery
            let soc = max(10, min(100, 72 - hour * 2.1 + 6 * sin(hour * .pi / 12)))

            readings.append(
                TimeSeriesPoint(
                    timestamp: "\(date)T\(String(format: "%02d", hourInt)):\(String(format: "%02d", minuteInt)):00Z",
                    ppv: solar,
                    pload: max(0, load),
                    pbat: battery,
                    pgrid: grid,
                    soc: soc
                )
            )
        }

        let summary = DaySummary(
            epv: 13.4,
            eInput: 2.2,
            eOutput: 4.9,
            eCharge: 5.1,
            eDischarge: 6.2,
            socLow: 18.3,
            socLowTime: "\(date)T19:00:00Z"
        )

        return DayDetailResponse(date: date, readings: readings, summary: summary)
    }

    func fetchStatus() async throws -> StatusResponse {
        Self.statusResponse
    }

    func fetchHistory(days: Int) async throws -> HistoryResponse {
        let clampedDays = max(1, days)
        let selectedDays = Array(Self.historyDays.prefix(clampedDays))
        return HistoryResponse(days: selectedDays)
    }

    func fetchDay(date: String) async throws -> DayDetailResponse {
        Self.dayDetailResponse(for: date)
    }
}
