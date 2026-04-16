import Foundation
import Observation
import SwiftData

@MainActor @Observable
final class HistoryViewModel {
    private(set) var days: [DayEnergy] = [] {
        didSet {
            rebuildChartData()
        }
    }
    private(set) var selectedDay: DayEnergy?
    private(set) var selectedDayRange = 7
    private(set) var chartDays: [HistoryChartDay] = []
    private(set) var chartEntries: [HistoryChartEntry] = []
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
        selectedDayRange = requestedDays
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

    private func rebuildChartData() {
        guard !days.isEmpty else {
            chartDays = []
            chartEntries = []
            return
        }

        let now = nowProvider()
        var newChartDays: [HistoryChartDay] = []
        newChartDays.reserveCapacity(days.count)

        var newChartEntries: [HistoryChartEntry] = []
        newChartEntries.reserveCapacity(days.count * HistoryChartMetric.allCases.count)

        for day in days {
            guard let parsedDate = DateFormatting.parseDayDate(day.date) else {
                continue
            }

            let isToday = DateFormatting.isToday(day.date, now: now)
            let chartDay = HistoryChartDay(day: day, date: parsedDate, isToday: isToday)
            newChartDays.append(chartDay)

            for metric in HistoryChartMetric.allCases {
                newChartEntries.append(
                    HistoryChartEntry(
                        dayID: day.date,
                        date: parsedDate,
                        metric: metric,
                        value: metric.value(from: day),
                        isToday: isToday
                    )
                )
            }
        }

        chartDays = newChartDays
        chartEntries = newChartEntries
    }
}

extension HistoryViewModel {
    struct HistoryChartDay: Identifiable {
        let day: DayEnergy
        let date: Date
        let isToday: Bool

        var id: String { day.date }
    }

    struct HistoryChartEntry: Identifiable {
        let dayID: String
        let date: Date
        let metric: HistoryChartMetric
        let value: Double
        let isToday: Bool

        var id: String { "\(dayID)-\(metric.rawValue)" }
    }

    enum HistoryChartMetric: String, CaseIterable, Sendable {
        case solar
        case gridImported
        case gridExported
        case charged
        case discharged

        var label: String {
            switch self {
            case .solar: "Solar"
            case .gridImported: "Grid In"
            case .gridExported: "Grid Out"
            case .charged: "Charged"
            case .discharged: "Discharged"
            }
        }

        func value(from day: DayEnergy) -> Double {
            switch self {
            case .solar: day.epv
            case .gridImported: day.eInput
            case .gridExported: day.eOutput
            case .charged: day.eCharge
            case .discharged: day.eDischarge
            }
        }
    }
}
