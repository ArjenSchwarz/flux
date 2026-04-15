import Foundation
import Observation
import SwiftData

@MainActor @Observable
final class HistoryViewModel {
    private(set) var days: [DayEnergy] = []
    private(set) var selectedDay: DayEnergy?
    private(set) var selectedDayRange = 7
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
                self.error = mapError(error)
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

        for day in dayEnergies where !DateFormatting.isToday(day.date, now: now) {
            modelContext.insert(CachedDayEnergy(from: day))
        }

        if modelContext.hasChanges {
            try modelContext.save()
        }
    }

    private func loadCachedDays(limit: Int) -> [DayEnergy] {
        let descriptor = FetchDescriptor<CachedDayEnergy>(
            sortBy: [SortDescriptor(\CachedDayEnergy.date, order: .reverse)]
        )

        guard let cachedDays = try? modelContext.fetch(descriptor), !cachedDays.isEmpty else {
            return []
        }

        if cachedDays.count <= limit {
            return cachedDays.map(\.asDayEnergy)
        }

        return Array(cachedDays.prefix(limit)).map(\.asDayEnergy)
    }

    private func selectDefaultDayIfNeeded() {
        guard let selectedDay else {
            self.selectedDay = days.first
            return
        }

        self.selectedDay = days.first(where: { $0.date == selectedDay.date }) ?? days.first
    }

    private func mapError(_ error: Error) -> FluxAPIError {
        if let apiError = error as? FluxAPIError {
            return apiError
        }

        return .networkError(error.localizedDescription)
    }
}
