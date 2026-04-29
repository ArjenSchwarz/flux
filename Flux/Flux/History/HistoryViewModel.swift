import FluxCore
import Foundation
import Observation
import SwiftData

@MainActor @Observable
final class HistoryViewModel {
    private(set) var days: [DayEnergy] = []
    private(set) var selectedDay: DayEnergy?
    private(set) var isLoading = false
    private(set) var error: FluxAPIError?

    private let apiClient: any FluxAPIClient
    private let modelContext: ModelContext
    private let nowProvider: @Sendable () -> Date

    init(
        apiClient: any FluxAPIClient,
        modelContext: ModelContext,
        nowProvider: @escaping @Sendable () -> Date = { .now }
    ) {
        self.apiClient = apiClient
        self.modelContext = modelContext
        self.nowProvider = nowProvider
    }

    func loadHistory(days requestedDays: Int) async {
        guard !isLoading else { return }

        isLoading = true
        defer { isLoading = false }

        do {
            let response = try await apiClient.fetchHistory(days: requestedDays)
            days = response.days
            error = nil
            selectDefaultDayIfNeeded()
            try cacheHistoricalDays(response.days)
        } catch {
            let fallbackDays = loadCachedDays(limit: requestedDays)
            if fallbackDays.isEmpty {
                self.error = FluxAPIError.from(error)
                days = []
                selectedDay = nil
            } else {
                days = fallbackDays
                self.error = nil
                selectDefaultDayIfNeeded()
            }
        }
    }

    func selectDay(_ day: DayEnergy) {
        selectedDay = day
    }

    private func cacheHistoricalDays(_ dayEnergies: [DayEnergy]) throws {
        let now = nowProvider()
        let datesToCache = dayEnergies
            .filter { !DateFormatting.isToday($0.date, now: now) }
            .map(\.date)
        guard !datesToCache.isEmpty else { return }
        let descriptor = FetchDescriptor<CachedDayEnergy>(
            predicate: #Predicate<CachedDayEnergy> { cached in
                datesToCache.contains(cached.date)
            }
        )
        let cachedDays = try modelContext.fetch(descriptor)
        var cachedByDate = Dictionary(uniqueKeysWithValues: cachedDays.map { ($0.date, $0) })

        for day in dayEnergies where !DateFormatting.isToday(day.date, now: now) {
            if let cached = cachedByDate[day.date] {
                cached.epv = day.epv
                cached.eInput = day.eInput
                cached.eOutput = day.eOutput
                cached.eCharge = day.eCharge
                cached.eDischarge = day.eDischarge
                cached.offpeakGridImportKwh = day.offpeakGridImportKwh
                cached.offpeakGridExportKwh = day.offpeakGridExportKwh
                cached.note = day.note
            } else {
                let newCachedDay = CachedDayEnergy(from: day)
                modelContext.insert(newCachedDay)
                cachedByDate[day.date] = newCachedDay
            }
        }

        if modelContext.hasChanges {
            try modelContext.save()
        }
    }

    private func loadCachedDays(limit: Int) -> [DayEnergy] {
        var descriptor = FetchDescriptor<CachedDayEnergy>(
            sortBy: [SortDescriptor(\CachedDayEnergy.date, order: .reverse)]
        )
        descriptor.fetchLimit = limit

        guard let cachedDays = try? modelContext.fetch(descriptor), !cachedDays.isEmpty else {
            return []
        }

        return cachedDays.map(\.asDayEnergy)
    }

    private func selectDefaultDayIfNeeded() {
        guard let selectedDay else {
            self.selectedDay = days.last
            return
        }

        self.selectedDay = days.first(where: { $0.date == selectedDay.date }) ?? days.last
    }
}

extension HistoryViewModel {
    /// Series and period summary derived from `days`. With at most 30
    /// entries the recomputation is cheap; storing the result would just
    /// add cache-invalidation work. Callers (notably the View) should
    /// capture this once per render rather than reading the convenience
    /// accessors below repeatedly.
    var derived: DerivedState {
        DerivedState(days: days, now: nowProvider())
    }

    /// Convenience accessors for tests and previews. Each rebuilds
    /// `DerivedState` independently — production callers should read
    /// `derived` once and destructure instead.
    var solarSeries: [SolarEntry] { derived.solar }
    var gridSeries: [GridEntry] { derived.grid }
    var batterySeries: [BatteryEntry] { derived.battery }
    var summary: PeriodSummary { derived.summary }
}

extension HistoryViewModel {
    struct SolarEntry: Identifiable, Equatable {
        let date: Date
        let dayID: String
        let kwh: Double
        let isToday: Bool

        var id: String { dayID }
    }

    struct GridEntry: Identifiable, Equatable {
        let date: Date
        let dayID: String
        let peakImportKwh: Double
        let offpeakImportKwh: Double
        let exportKwh: Double
        let isToday: Bool

        var id: String { dayID }
    }

    struct BatteryEntry: Identifiable, Equatable {
        let date: Date
        let dayID: String
        let chargeKwh: Double
        let dischargeKwh: Double
        let isToday: Bool

        var id: String { dayID }
    }

    struct PeriodSummary: Equatable {
        let solarTotalKwh: Double
        /// Excludes today (today is partial; including it would skew daily
        /// averages). `batteryDayCount` follows the same rule.
        let solarDayCount: Int
        let peakImportTotalKwh: Double
        let offpeakImportTotalKwh: Double
        let exportTotalKwh: Double
        /// Includes today when an off-peak record exists. Off-peak imports
        /// are the actionable headline so showing today's in-progress
        /// number matters more than a clean daily average.
        let gridDayCount: Int
        let chargeTotalKwh: Double
        let dischargeTotalKwh: Double
        let batteryDayCount: Int

        static let empty = PeriodSummary(
            solarTotalKwh: 0,
            solarDayCount: 0,
            peakImportTotalKwh: 0,
            offpeakImportTotalKwh: 0,
            exportTotalKwh: 0,
            gridDayCount: 0,
            chargeTotalKwh: 0,
            dischargeTotalKwh: 0,
            batteryDayCount: 0
        )

        var solarPerDayKwh: Double? {
            solarDayCount > 0 ? solarTotalKwh / Double(solarDayCount) : nil
        }

        var dischargePerDayKwh: Double? {
            batteryDayCount > 0 ? dischargeTotalKwh / Double(batteryDayCount) : nil
        }
    }

    struct DerivedState {
        let solar: [SolarEntry]
        let grid: [GridEntry]
        let battery: [BatteryEntry]
        let summary: PeriodSummary

        init(days: [DayEnergy], now: Date) {
            guard !days.isEmpty else {
                solar = []
                grid = []
                battery = []
                summary = .empty
                return
            }

            var solar: [SolarEntry] = []
            var grid: [GridEntry] = []
            var battery: [BatteryEntry] = []
            var totals = Totals()
            solar.reserveCapacity(days.count)
            grid.reserveCapacity(days.count)
            battery.reserveCapacity(days.count)

            for day in days {
                guard let parsedDate = DateFormatting.parseDayDate(day.date) else { continue }
                let isToday = DateFormatting.isToday(day.date, now: now)
                solar.append(SolarEntry(date: parsedDate, dayID: day.date, kwh: day.epv, isToday: isToday))
                battery.append(BatteryEntry(
                    date: parsedDate, dayID: day.date,
                    chargeKwh: day.eCharge, dischargeKwh: day.eDischarge, isToday: isToday
                ))
                if let entry = Self.gridEntry(day: day, parsedDate: parsedDate, isToday: isToday) {
                    grid.append(entry)
                    totals.addGrid(entry)
                }
                if !isToday {
                    totals.addCompleteDay(day)
                }
            }

            self.solar = solar
            self.grid = grid
            self.battery = battery
            self.summary = totals.snapshot
        }

        private static func gridEntry(day: DayEnergy, parsedDate: Date, isToday: Bool) -> GridEntry? {
            guard let offpeakImport = day.offpeakGridImportKwh else { return nil }
            let peak = max(0, day.eInput - offpeakImport)
            return GridEntry(
                date: parsedDate,
                dayID: day.date,
                peakImportKwh: peak,
                offpeakImportKwh: offpeakImport,
                exportKwh: day.eOutput,
                isToday: isToday
            )
        }
    }

    private struct Totals {
        var solarTotal = 0.0
        var peakImportTotal = 0.0
        var offpeakImportTotal = 0.0
        var exportTotal = 0.0
        var chargeTotal = 0.0
        var dischargeTotal = 0.0
        var completeDayCount = 0
        var gridDayCount = 0

        mutating func addGrid(_ entry: GridEntry) {
            peakImportTotal += entry.peakImportKwh
            offpeakImportTotal += entry.offpeakImportKwh
            exportTotal += entry.exportKwh
            gridDayCount += 1
        }

        mutating func addCompleteDay(_ day: DayEnergy) {
            solarTotal += day.epv
            chargeTotal += day.eCharge
            dischargeTotal += day.eDischarge
            completeDayCount += 1
        }

        var snapshot: PeriodSummary {
            PeriodSummary(
                solarTotalKwh: solarTotal,
                solarDayCount: completeDayCount,
                peakImportTotalKwh: peakImportTotal,
                offpeakImportTotalKwh: offpeakImportTotal,
                exportTotalKwh: exportTotal,
                gridDayCount: gridDayCount,
                chargeTotalKwh: chargeTotal,
                dischargeTotalKwh: dischargeTotal,
                batteryDayCount: completeDayCount
            )
        }
    }
}
