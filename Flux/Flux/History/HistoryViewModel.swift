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
    private let warn: @Sendable (String) -> Void

    init(
        apiClient: any FluxAPIClient,
        modelContext: ModelContext,
        nowProvider: @escaping @Sendable () -> Date = { .now },
        warn: @escaping @Sendable (String) -> Void = HistoryCacheLog.defaultWarn
    ) {
        self.apiClient = apiClient
        self.modelContext = modelContext
        self.nowProvider = nowProvider
        self.warn = warn
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
                warnIfClearing(cached: cached, day: day)
                cached.dailyUsage = day.dailyUsage
                cached.socLow = day.socLow
                cached.socLowTime = day.socLowTime
                cached.peakPeriods = day.peakPeriods
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

    private func warnIfClearing(cached: CachedDayEnergy, day: DayEnergy) {
        if cached.dailyUsage != nil, day.dailyUsage == nil {
            warn("Clearing cached dailyUsage for \(day.date)")
        }
        if cached.socLow != nil, day.socLow == nil {
            warn("Clearing cached socLow for \(day.date)")
        }
        if cached.socLowTime != nil, day.socLowTime == nil {
            warn("Clearing cached socLowTime for \(day.date)")
        }
        if cached.peakPeriods != nil, day.peakPeriods == nil {
            warn("Clearing cached peakPeriods for \(day.date)")
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
    var dailyUsageSeries: [DailyUsageEntry] { derived.dailyUsage }
    var summary: PeriodSummary { derived.summary }
}
