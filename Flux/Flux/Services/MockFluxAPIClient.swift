#if DEBUG
import FluxCore
import Foundation

// swiftlint:disable:next type_body_length
final actor MockFluxAPIClient: FluxAPIClient {
    static let preview = MockFluxAPIClient()
    static let previewDate = "2026-04-15"
    static let previewNote = "Away in Bali — minimal load expected."

    private(set) var lastSaveNoteCall: (date: String, text: String)?

    // SoC alerts mock state. Stored per device id, matches the cap behaviour.
    private var registeredDevices: [String: DeviceItemResponse] = [:]
    private var rulesByDevice: [String: [SoCAlertRule]] = [:]
    private var nextRuleSeq = 1
    private static let socAlertRuleCap = 10

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
        ),
        note: previewNote
    )

    /// Variant of `statusResponse` with the server-computed
    /// `cantEmptyBeforeOffpeak` flag set to `true`. Used by the Dashboard
    /// hero preview and tests to exercise the indicator subview.
    static let statusResponseCantEmpty = StatusResponse(
        live: statusResponse.live,
        battery: BatteryInfo(
            capacityKwh: 13.3,
            cutoffPercent: 10,
            estimatedCutoffTime: "\(previewDate)T18:30:00Z",
            low24h: Low24h(soc: 38.2, timestamp: "\(previewDate)T08:45:00Z"),
            cantEmptyBeforeOffpeak: true
        ),
        rolling15min: statusResponse.rolling15min,
        offpeak: statusResponse.offpeak,
        todayEnergy: statusResponse.todayEnergy,
        note: previewNote
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
            let dateString = DateFormatting.dayDateString(from: date)
            let trend = Double(dayOffset)
            let eInput = max(0.2, 2.3 - trend * 0.03)
            let offpeakImport = min(eInput * 0.6, max(0.1, 1.4 - trend * 0.02))
            let hour = String(format: "%02d", dayOffset % 24)
            let minute = String(format: "%02d", (dayOffset * 7) % 60)
            days.append(
                DayEnergy(
                    date: dateString,
                    epv: max(0, 14.2 - trend * 0.17),
                    eInput: eInput,
                    eOutput: max(0.5, 5.1 - trend * 0.09),
                    eCharge: max(0.8, 5.4 - trend * 0.08),
                    eDischarge: max(1.0, 6.2 - trend * 0.10),
                    offpeakGridImportKwh: offpeakImport,
                    offpeakGridExportKwh: max(0.1, 0.6 - trend * 0.01),
                    note: dayOffset == 0 ? previewNote : nil,
                    dailyUsage: dayDailyUsage(for: dateString),
                    socLow: max(8.0, 38.0 - trend * 0.6),
                    socLowTime: "\(dateString)T\(hour):\(minute):00Z"
                )
            )
        }

        return days
    }()

    static let historyResponse = HistoryResponse(days: historyDays)

    static func dayDetailResponse(for date: String = previewDate) -> DayDetailResponse {
        DayDetailResponse(
            date: date,
            readings: dayReadings(for: date),
            summary: DaySummary(
                epv: 13.4,
                eInput: 2.2,
                eOutput: 4.9,
                eCharge: 5.1,
                eDischarge: 6.2,
                socLow: 18.3,
                socLowTime: "\(date)T19:00:00Z"
            ),
            peakPeriods: [
                PeakPeriod(start: "\(date)T17:30:00Z", end: "\(date)T18:15:00Z", avgLoadW: 3800.1, energyWh: 2850),
                PeakPeriod(start: "\(date)T07:15:00Z", end: "\(date)T07:45:00Z", avgLoadW: 4200.3, energyWh: 2100),
                PeakPeriod(start: "\(date)T12:00:00Z", end: "\(date)T12:20:00Z", avgLoadW: 2900.5, energyWh: 967)
            ],
            dailyUsage: dayDailyUsage(for: date),
            note: date == previewDate ? previewNote : nil
        )
    }

    private static func dayReadings(for date: String) -> [TimeSeriesPoint] {
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
        return readings
    }

    // Exercises all five blocks for the preview, including a mix of readings-
    // and estimated-derived boundaries to render the caption variants.
    // swiftlint:disable:next function_body_length
    private static func dayDailyUsage(for date: String) -> DailyUsage {
        let utc = sydneyClockToUTC(date: date)
        return DailyUsage(blocks: [
            DailyUsageBlock(
                kind: .night,
                start: utc(0, 0),
                end: utc(6, 30),
                totalKwh: 3.1,
                averageKwhPerHour: 0.48,
                percentOfDay: 18,
                status: .complete,
                boundarySource: .readings
            ),
            DailyUsageBlock(
                kind: .morningPeak,
                start: utc(6, 30),
                end: utc(11, 0),
                totalKwh: 2.1,
                averageKwhPerHour: 0.47,
                percentOfDay: 12,
                status: .complete,
                boundarySource: .estimated
            ),
            DailyUsageBlock(
                kind: .offPeak,
                start: utc(11, 0),
                end: utc(14, 0),
                totalKwh: 5.0,
                averageKwhPerHour: 1.67,
                percentOfDay: 30,
                status: .complete,
                boundarySource: .readings
            ),
            DailyUsageBlock(
                kind: .afternoonPeak,
                start: utc(14, 0),
                end: utc(18, 42),
                totalKwh: 4.5,
                averageKwhPerHour: 0.96,
                percentOfDay: 27,
                status: .complete,
                boundarySource: .estimated
            ),
            DailyUsageBlock(
                kind: .evening,
                start: utc(18, 42),
                end: utc(24, 0),
                totalKwh: 2.7,
                averageKwhPerHour: 1.08,
                percentOfDay: 13,
                status: .inProgress,
                boundarySource: .estimated
            )
        ])
    }

    private static let isoUTCFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        return formatter
    }()

    // Returns a closure that converts a Sydney-local (hour, minute) on the
    // given date to its UTC ISO8601 timestamp, so preview block boundaries
    // remain chronologically valid regardless of date or DST offset.
    private static func sydneyClockToUTC(date: String) -> (Int, Int) -> String {
        let baseDay = DateFormatting.parseDayDate(date) ?? Date()
        return { hour, minute in
            let target = calendar.date(byAdding: .minute, value: hour * 60 + minute, to: baseDay) ?? baseDay
            return isoUTCFormatter.string(from: target)
        }
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

    func saveNote(date: String, text: String) async throws -> NoteResponse {
        lastSaveNoteCall = (date, text)
        return NoteResponse(
            date: date,
            text: text,
            updatedAt: text.isEmpty ? nil : Self.isoUTCFormatter.string(from: Date())
        )
    }

    // MARK: - SoC Alerts

    func registerDevice(_ registration: DeviceRegistration) async throws -> DeviceItemResponse {
        let response = DeviceItemResponse(
            deviceId: registration.deviceId,
            platform: registration.platform,
            apnsToken: registration.apnsToken,
            tzIdentifier: registration.tzIdentifier,
            tzUpdatedAt: registration.tzUpdatedAt,
            tokenStatus: "active",
            lastRegisteredAt: Self.isoUTCFormatter.string(from: Date())
        )
        registeredDevices[registration.deviceId] = response
        return response
    }

    func fetchRules(deviceId: String) async throws -> [SoCAlertRule] {
        (rulesByDevice[deviceId] ?? []).sorted { $0.createdAt < $1.createdAt }
    }

    func createRule(deviceId: String, rule: SoCAlertRuleDraft) async throws -> SoCAlertRule {
        let existing = rulesByDevice[deviceId] ?? []
        if existing.count >= Self.socAlertRuleCap {
            throw FluxAPIError.ruleCapReached
        }
        let now = Date()
        let created = SoCAlertRule(
            id: "mock-rule-\(nextRuleSeq)",
            thresholdPercent: rule.thresholdPercent,
            windowStart: rule.windowStart,
            windowEnd: rule.windowEnd,
            enabled: rule.enabled,
            label: rule.label,
            createdAt: now,
            updatedAt: now
        )
        nextRuleSeq += 1
        rulesByDevice[deviceId] = existing + [created]
        return created
    }

    func updateRule(deviceId: String, rule: SoCAlertRule) async throws -> SoCAlertRule {
        var rules = rulesByDevice[deviceId] ?? []
        guard let idx = rules.firstIndex(where: { $0.id == rule.id }) else {
            throw FluxAPIError.badRequest("rule not found")
        }
        var updated = rule
        updated.updatedAt = Date()
        rules[idx] = updated
        rulesByDevice[deviceId] = rules
        return updated
    }

    func deleteRule(deviceId: String, ruleId: String) async throws {
        rulesByDevice[deviceId] = (rulesByDevice[deviceId] ?? []).filter { $0.id != ruleId }
    }
}
#endif
