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
    /// Inclusive day-count resolved on the last load: the requested `N` for the
    /// `.days` form, or the period's calendar day-count for a past range — the
    /// M in the stats card's "N of M days".
    private(set) var resolvedRangeDays: Int = 7
    /// The range whose data is currently in `days`. Drives `chartDomain` so the
    /// charts' x-axis reservation matches the rendered data, not an in-flight
    /// selection.
    private(set) var resolvedRange: HistoryRange = .days(7)
    /// The query whose data is currently in `days` — the resolved counterpart
    /// of `periodAnchor`, set atomically with `days` inside `load()` so
    /// everything user-visible (chart domain, period label, expansion scope,
    /// next-chevron state) reflects rendered data, never an in-flight request.
    private(set) var resolvedQuery: HistoryQuery = .days(7)
    /// Sydney-midnight start of the displayed past period, or `nil` for the
    /// current (to-date) period. Mutated ONLY by the intent methods — the load
    /// path is anchor-agnostic, so refresh can never reset the user's place
    /// (req 1.8).
    private(set) var periodAnchor: Date?

    /// The (range, anchor) pair the in-flight coalescing loop is keyed on, so
    /// a navigation issued mid-load is honoured the same way a range change is.
    private struct RequestedPeriod: Equatable {
        let range: HistoryRange
        let anchor: Date?
    }

    private var lastRequestedPeriod = RequestedPeriod(range: .days(7), anchor: nil)

    private let apiClient: any FluxAPIClient
    private let modelContext: ModelContext
    private let pricingService: PricingService
    // Internal (not private) so the presentation extension in
    // HistoryViewModel+Presentation.swift can derive from the same clock.
    let nowProvider: @Sendable () -> Date
    let firstWeekdayProvider: @Sendable () -> Int
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
        // Record the latest selection before the guard so a range or period
        // chosen during an in-flight load is not dropped — the in-flight load
        // coalesces to it. The anchor is read, never written, here (req 1.8).
        lastRequestedRange = range
        lastRequestedPeriod = RequestedPeriod(range: range, anchor: periodAnchor)
        guard !isLoading else { return }

        isLoading = true
        defer { isLoading = false }

        // Loop so a newer (range, anchor) pair selected mid-load is honoured:
        // after each fetch we re-check `lastRequestedPeriod` and reload if it
        // changed. The latest selection always wins, so the picker, the period
        // header, and the rendered data agree. The loop terminates as soon as
        // no newer request arrived during the last fetch; each iteration is a
        // real network round-trip, so it can only keep looping while requests
        // keep arriving faster than they complete.
        var loadedPeriod = lastRequestedPeriod
        while true {
            await load(loadedPeriod)
            if lastRequestedPeriod == loadedPeriod { break }
            loadedPeriod = lastRequestedPeriod
        }
    }

    /// The query, day-count, and cache window for one load. Resolved before
    /// the fetch; adopted into the resolved snapshot only once the fetch has
    /// settled, atomically with `days`.
    private struct LoadTarget {
        let query: HistoryQuery
        let rangeDays: Int
        let cacheStart: String
        let cacheEnd: String
    }

    private func load(_ period: RequestedPeriod) async {
        let now = nowProvider()
        let target = resolveTarget(for: period, now: now)

        do {
            let response = try await apiClient.fetchHistory(query: target.query)
            adopt(target, range: period.range)
            days = response.days
            error = nil
            selectDefaultDayIfNeeded()
            try cacheHistoricalDays(response.days)
        } catch {
            adopt(target, range: period.range)
            let fallbackDays = loadCachedDays(from: target.cacheStart, through: target.cacheEnd)
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

    private func resolveTarget(for period: RequestedPeriod, now: Date) -> LoadTarget {
        if let anchor = period.anchor, let resolved = self.period(for: period.range, containing: anchor) {
            return LoadTarget(
                query: .dateRange(start: resolved.startDateString, end: resolved.endDateString),
                rangeDays: resolved.dayCount,
                cacheStart: resolved.startDateString,
                cacheEnd: resolved.endDateString
            )
        }
        let resolvedDays = period.range.resolvedDays(now: now, firstWeekday: firstWeekdayProvider())
        return LoadTarget(
            query: .days(resolvedDays),
            rangeDays: resolvedDays,
            cacheStart: DateFormatting.windowStartDateString(inclusiveDays: resolvedDays, now: now),
            cacheEnd: DateFormatting.todayDateString(now: now)
        )
    }

    private func adopt(_ target: LoadTarget, range: HistoryRange) {
        resolvedRangeDays = target.rangeDays
        resolvedRange = range
        resolvedQuery = target.query
    }

    func selectDay(_ day: DayEnergy) {
        selectedDay = day
    }

    func reload() async {
        await loadHistory(range: lastRequestedRange)
    }

    // MARK: - Period-navigation intents

    /// Range picker selection: any range change resets to the current period
    /// for the newly selected range (req 2.3, Decision 4).
    func selectRange(_ range: HistoryRange) async {
        periodAnchor = nil
        await loadHistory(range: range)
    }

    /// Steps to the period immediately before the displayed one (req 1.2).
    func navigatePrevious() async {
        guard let base = navigationBasePeriod() else { return }
        await navigate(to: base.previous(range: lastRequestedRange, firstWeekday: firstWeekdayProvider()))
    }

    /// Steps to the period immediately after the displayed one; landing on the
    /// period containing Sydney-today collapses to the current to-date view
    /// (req 1.3).
    func navigateNext() async {
        // Already at the current period — nothing lies after it. Clamping here
        // (like DayDetailViewModel's isToday guard) matters because the UI's
        // disabled state reads the resolved snapshot, which lags an in-flight
        // load: a rapid double-tap or macOS key-repeat would otherwise issue a
        // future-dated range request the server rejects.
        guard periodAnchor != nil, let base = navigationBasePeriod() else { return }
        await navigate(to: base.next(range: lastRequestedRange, firstWeekday: firstWeekdayProvider()))
    }

    /// Jumps to the week/month containing `date`; a date inside the current
    /// period shows the current to-date view (req 3.1, 3.3).
    func jumpTo(date: Date) async {
        guard let target = period(for: lastRequestedRange, containing: date) else { return }
        await navigate(to: target)
    }

    /// One-tap return to the current to-date period (req 2.1).
    func returnToCurrent() async {
        periodAnchor = nil
        await loadHistory(range: lastRequestedRange)
    }

    private func navigate(to target: HistoryPeriod) async {
        // The current-period check uses Sydney "today" via nowProvider(),
        // re-evaluated on each trigger (requirements Definitions).
        periodAnchor = target.contains(nowProvider()) ? nil : target.start
        await loadHistory(range: lastRequestedRange)
    }

    private func navigationBasePeriod() -> HistoryPeriod? {
        period(for: lastRequestedRange, containing: periodAnchor ?? nowProvider())
    }

    private func period(for range: HistoryRange, containing date: Date) -> HistoryPeriod? {
        switch range {
        case .days:
            return nil
        case .weekToDate:
            return .week(containing: date, firstWeekday: firstWeekdayProvider())
        case .monthToDate:
            return .month(containing: date)
        }
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

    /// Offline fallback bounded by both window ends (req 7.2) — a past period
    /// must not pull in newer cached days, and the current window must not pull
    /// in pre-window ones. The dates are zero-padded `YYYY-MM-DD`, so
    /// lexicographic comparison matches chronological order. Returned ascending
    /// to mirror the online response shape, so `selectDefaultDayIfNeeded`
    /// auto-selects the newest day from `days.last` just as it does online.
    /// For the current window the upper bound is today, which excludes nothing
    /// extra since nothing later than today is ever cached.
    private func loadCachedDays(from startDate: String, through endDate: String) -> [DayEnergy] {
        // Captured as `let`s so the macro can embed them in the predicate.
        let lowerBound = startDate
        let upperBound = endDate
        let descriptor = FetchDescriptor<CachedDayEnergy>(
            predicate: #Predicate<CachedDayEnergy> { cached in
                cached.date >= lowerBound && cached.date <= upperBound
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
