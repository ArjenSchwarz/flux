import Foundation
import Testing
@testable import FluxCore

@MainActor @Suite(.serialized)
struct PricingServiceTests {
    @Test
    func refreshLoadsListAndSortsAscending() async throws {
        let api = MockPricingAPIClient()
        let later = makePeriod(id: "later", start: "2026-07-01", end: nil)
        let earlier = makePeriod(id: "earlier", start: "2026-01-01", end: "2026-06-30")
        api.periodsToReturn = [later, earlier]
        let svc = PricingService()
        svc.bind(apiClient: api)

        try await svc.refresh()

        #expect(svc.periods.count == 2)
        #expect(svc.periods.first?.id == "earlier")
        #expect(svc.periods.last?.id == "later")
        #expect(svc.lastError == nil)
    }

    @Test
    func refreshStoresLastErrorOnFailure() async throws {
        let api = MockPricingAPIClient()
        api.fetchError = FluxAPIError.serverError
        let svc = PricingService()
        svc.bind(apiClient: api)

        do {
            try await svc.refresh()
            Issue.record("expected throw")
        } catch {
            // expected
        }
        #expect(svc.lastError != nil)
    }

    @Test
    func createFoldsResponseIntoLocalListAndTriggersBackgroundRefetch() async throws {
        let api = MockPricingAPIClient()
        let svc = PricingService()
        svc.bind(apiClient: api)

        let draft = PricingPeriodDraft(
            startDate: "2026-08-01",
            endDate: nil,
            peakRate: 0.30,
            feedInRate: 0.06,
            offPeakSavingsRate: 0.12
        )
        let created = try await svc.create(draft)

        #expect(svc.periods.contains(where: { $0.id == created.id }))

        // Wait for fire-and-forget refetch to complete.
        try await waitForRefetch(api: api, expected: 1)
        #expect(api.fetchCallCount == 1)
    }

    @Test
    func updateFoldsUpdatedRowIntoLocalList() async throws {
        let api = MockPricingAPIClient()
        let existing = makePeriod(id: "pp-1", start: "2026-01-01", end: "2026-06-30", peakRate: 0.28)
        api.periodsToReturn = [existing]
        let svc = PricingService()
        svc.bind(apiClient: api)
        try await svc.refresh()

        let draft = PricingPeriodDraft(period: existing).with(peakRate: 0.32)
        let updated = try await svc.update(id: existing.id, draft)
        #expect(updated.peakRate == 0.32)
        #expect(svc.periods.first?.peakRate == 0.32)
    }

    @Test
    func deleteRemovesRowFromLocalList() async throws {
        let api = MockPricingAPIClient()
        let existing = makePeriod(id: "pp-1", start: "2026-01-01", end: "2026-06-30")
        api.periodsToReturn = [existing]
        let svc = PricingService()
        svc.bind(apiClient: api)
        try await svc.refresh()

        try await svc.delete(id: existing.id)
        #expect(svc.periods.isEmpty)
    }

    @Test
    func replaceOpenEndedFoldsBothChanges() async throws {
        let api = MockPricingAPIClient()
        let open = makePeriod(id: "pp-open", start: "2026-01-01", end: nil)
        api.periodsToReturn = [open]
        let svc = PricingService()
        svc.bind(apiClient: api)
        try await svc.refresh()
        // The server returns the new row only — the service must trigger alpha
        // refetch so the local list reflects the closing-row's new endDate.
        let newOpen = makePeriod(id: "pp-new", start: "2026-08-01", end: nil)
        let closed = makePeriod(id: "pp-open", start: "2026-01-01", end: "2026-07-31")
        api.replaceOpenEndedResult = newOpen
        // After the refetch the API returns the final state.
        api.periodsToReturn = [closed, newOpen]

        let draft = PricingPeriodDraft(period: newOpen)
        let result = try await svc.replaceOpenEnded(closingId: "pp-open", with: draft)
        #expect(result.id == "pp-new")
        try await waitForRefetch(api: api, expected: 2)
        let finalPeriods = svc.periods
        #expect(finalPeriods.count == 2)
        #expect(finalPeriods.first?.id == "pp-open")
        #expect(finalPeriods.first?.endDate == "2026-07-31")
    }

    @Test
    func notConfiguredWhenAPIClientNotBound() async throws {
        let svc = PricingService()
        do {
            try await svc.refresh()
            Issue.record("expected throw")
        } catch let error as FluxAPIError {
            #expect(error == .notConfigured)
        }
    }

    @Test
    func clearErrorResetsLastError() async throws {
        let api = MockPricingAPIClient()
        api.fetchError = .serverError
        let svc = PricingService()
        svc.bind(apiClient: api)
        _ = try? await svc.refresh()
        #expect(svc.lastError != nil)
        svc.clearError()
        #expect(svc.lastError == nil)
    }

    // MARK: - helpers

    /// Poll for the API mock's fetch counter to reach the expected value.
    /// Used because the post-mutation refetch is fire-and-forget.
    private func waitForRefetch(api: MockPricingAPIClient, expected: Int) async throws {
        for _ in 0..<50 {
            if api.fetchCallCount >= expected { return }
            try await Task.sleep(nanoseconds: 5_000_000) // 5ms
        }
        Issue.record("refetch did not complete in time (got \(api.fetchCallCount), expected \(expected))")
    }

    private func makePeriod(id: String, start: String, end: String?, peakRate: Double = 0.30) -> PricingPeriod {
        PricingPeriod(
            id: id,
            startDate: start,
            endDate: end,
            peakRate: peakRate,
            feedInRate: 0.05,
            offPeakSavingsRate: 0.12,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
        )
    }
}

private extension PricingPeriodDraft {
    func with(peakRate: Double) -> PricingPeriodDraft {
        var copy = self
        copy.peakRate = peakRate
        return copy
    }
}

// MARK: - Test doubles

final class MockPricingAPIClient: FluxAPIClient, @unchecked Sendable {
    var periodsToReturn: [PricingPeriod] = []
    var fetchError: FluxAPIError?
    var fetchCallCount = 0
    var replaceOpenEndedResult: PricingPeriod?

    func fetchStatus() async throws -> StatusResponse {
        StatusResponse(live: nil, battery: nil, rolling15min: nil, offpeak: nil, todayEnergy: nil, note: nil)
    }
    func fetchHistory(days _: Int) async throws -> HistoryResponse { HistoryResponse(days: []) }
    func fetchDay(date: String) async throws -> DayDetailResponse {
        DayDetailResponse(date: date, readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil, note: nil)
    }
    func saveNote(date: String, text _: String) async throws -> NoteResponse {
        NoteResponse(date: date, text: "", updatedAt: nil)
    }

    func fetchPricing() async throws -> [PricingPeriod] {
        fetchCallCount += 1
        if let fetchError { throw fetchError }
        return periodsToReturn
    }

    func createPricing(_ draft: PricingPeriodDraft) async throws -> PricingPeriod {
        let now = Date()
        let period = PricingPeriod(
            id: "mock-\(UUID().uuidString)",
            startDate: draft.startDate,
            endDate: draft.endDate,
            peakRate: draft.peakRate,
            feedInRate: draft.feedInRate,
            offPeakSavingsRate: draft.offPeakSavingsRate,
            createdAt: now,
            updatedAt: now
        )
        periodsToReturn.append(period)
        return period
    }

    func updatePricing(id: String, _ draft: PricingPeriodDraft) async throws -> PricingPeriod {
        guard let idx = periodsToReturn.firstIndex(where: { $0.id == id }) else {
            throw FluxAPIError.notFound
        }
        let now = Date()
        let period = PricingPeriod(
            id: id,
            startDate: draft.startDate,
            endDate: draft.endDate,
            peakRate: draft.peakRate,
            feedInRate: draft.feedInRate,
            offPeakSavingsRate: draft.offPeakSavingsRate,
            createdAt: periodsToReturn[idx].createdAt,
            updatedAt: now
        )
        periodsToReturn[idx] = period
        return period
    }

    func deletePricing(id: String) async throws {
        periodsToReturn.removeAll { $0.id == id }
    }

    func replaceOpenEndedPricing(closingId _: String, with _: PricingPeriodDraft) async throws -> PricingPeriod {
        if let result = replaceOpenEndedResult { return result }
        throw FluxAPIError.serverError
    }
}
