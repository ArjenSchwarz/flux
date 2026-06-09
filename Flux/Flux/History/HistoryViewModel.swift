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
    private(set) var lastRequestedRange: HistoryRange = .days(7)
    /// Inclusive day-count resolved from `lastRequestedRange` on the last load.
    /// Read by `HistoryView` for the cards' `rangeDays:` (which carries the
    /// expansion scope's `N`).
    private(set) var resolvedRangeDays: Int = 7
    /// The range whose data is currently in `days`. Drives `chartDomain` so the
    /// charts' x-axis reservation matches the rendered data, not an in-flight
    /// selection.
    private(set) var resolvedRange: HistoryRange = .days(7)

    private let apiClient: any FluxAPIClient
    private let modelContext: ModelContext
    private let pricingService: PricingService
    private let nowProvider: @Sendable () -> Date
    private let firstWeekdayProvider: @Sendable () -> Int
    private let warn: (String) -> Void

    init(
        apiClient: any FluxAPIClient,
        modelContext: ModelContext,
        pricingService: PricingService = .shared,
        nowProvider: @escaping @Sendable () -> Date = { .now },
        firstWeekdayProvider: @escaping @Sendable () -> Int = { Calendar.current.firstWeekday },
        warn: @escaping (String) -> Void = HistoryCacheLog.defaultWarn
    ) {
        self.apiClient = apiClient
        self.modelContext = modelContext
        self.pricingService = pricingService
        self.nowProvider = nowProvider
        self.firstWeekdayProvider = firstWeekdayProvider
        self.warn = warn
    }

    /// Costs for the currently-loaded range. Computed lazily — recomputes
    /// whenever the underlying `days` or `pricingService.periods` change,
    /// thanks to `@Observable` tracking on both.
    var periodCosts: PeriodCosts? {
        PeriodCosts.compute(days: days, pricing: pricingService.periods)
    }

    /// AC 2.7 requires a refetch on every History range change. Called from
    /// the view's task modifier and onChange handler.
    func refreshPricing() async {
        try? await pricingService.refresh()
    }

    func loadHistory(range: HistoryRange) async {
        // Record the latest selection before the guard so a range chosen during
        // an in-flight load is not dropped — the in-flight load coalesces to it.
        lastRequestedRange = range
        guard !isLoading else { return }

        isLoading = true
        defer { isLoading = false }

        // Loop so a newer range selected mid-load is honoured: after each fetch
        // we re-check `lastRequestedRange` and reload if it changed. The latest
        // selection always wins, so the picker and the rendered data agree. The
        // loop terminates as soon as no newer range arrived during the last
        // fetch; each iteration is a real network round-trip, so it can only
        // keep looping while selections keep arriving faster than they complete.
        var loadedRange = range
        while true {
            await load(loadedRange)
            if lastRequestedRange == loadedRange { break }
            loadedRange = lastRequestedRange
        }
    }

    private func load(_ range: HistoryRange) async {
        let now = nowProvider()
        let resolvedDays = range.resolvedDays(now: now, firstWeekday: firstWeekdayProvider())
        resolvedRangeDays = resolvedDays
        resolvedRange = range

        do {
            let response = try await apiClient.fetchHistory(days: resolvedDays)
            days = response.days
            error = nil
            selectDefaultDayIfNeeded()
            try cacheHistoricalDays(response.days)
        } catch {
            let startDate = DateFormatting.windowStartDateString(inclusiveDays: resolvedDays, now: now)
            let fallbackDays = loadCachedDays(onOrAfter: startDate)
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

    func reload() async {
        await loadHistory(range: lastRequestedRange)
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
                cached.peakGridImportKwh = day.peakGridImportKwh
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

    /// Offline fallback bounded by the resolved window's start date. The dates
    /// are zero-padded `YYYY-MM-DD`, so a lexicographic `>=` matches chronological
    /// order. Returned ascending to mirror the online response shape, so
    /// `selectDefaultDayIfNeeded` auto-selects the newest (today) day from
    /// `days.last` just as it does online.
    private func loadCachedDays(onOrAfter startDate: String) -> [DayEnergy] {
        // Captured as a `let` so the macro can embed it in the predicate.
        let lowerBound = startDate
        let descriptor = FetchDescriptor<CachedDayEnergy>(
            predicate: #Predicate<CachedDayEnergy> { cached in
                cached.date >= lowerBound
            },
            sortBy: [SortDescriptor(\CachedDayEnergy.date, order: .forward)]
        )

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

    /// Full-period x-axis reservation for the to-date ranges (Wk → full week,
    /// Mo → full calendar month), or `nil` for the fixed `.days` ranges, which
    /// always span N days ending today and so need no reservation.
    var chartDomain: HistoryChartDomain? {
        HistoryChartDomain.make(
            range: resolvedRange,
            now: nowProvider(),
            firstWeekday: firstWeekdayProvider()
        )
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
