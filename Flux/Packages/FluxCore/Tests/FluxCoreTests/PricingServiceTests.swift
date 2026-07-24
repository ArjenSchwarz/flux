import Foundation
import Testing
@testable import FluxCore

@MainActor @Suite(.serialized)
struct PricingServiceTests {
    @Test
    func refreshLoadsListAndSortsAscending() async throws {
        let api = MockPricingAPIClient()
        let later = makePlan(id: "later", start: "2026-07-01", end: nil)
        let earlier = makePlan(id: "earlier", start: "2026-01-01", end: "2026-07-01")
        api.plansToReturn = [later, earlier]
        let svc = PricingService()
        svc.bind(apiClient: api)

        try await svc.refresh()

        #expect(svc.plans.count == 2)
        #expect(svc.plans.first?.id == "earlier")
        #expect(svc.plans.last?.id == "later")
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

        let draft = PricingPlanDraft(
            startDate: "2026-08-01",
            endDate: nil,
            defaultRate: 0.30,
            windows: [PlanWindow(start: "11:00", end: "14:00", free: true, rate: nil)],
            feedInRate: 0.06,
            savingsReferenceRate: 0.12
        )
        let created = try await svc.create(draft)

        #expect(svc.plans.contains(where: { $0.id == created.id }))

        // Wait for fire-and-forget refetch to complete.
        try await waitForRefetch(api: api, expected: 1)
        #expect(api.fetchCallCount == 1)
    }

    @Test
    func updateFoldsUpdatedRowIntoLocalList() async throws {
        let api = MockPricingAPIClient()
        let existing = makePlan(id: "pp-1", start: "2026-01-01", end: "2026-07-01", defaultRate: 0.28)
        api.plansToReturn = [existing]
        let svc = PricingService()
        svc.bind(apiClient: api)
        try await svc.refresh()

        let draft = PricingPlanDraft(plan: existing).with(defaultRate: 0.32)
        let updated = try await svc.update(id: existing.id, draft)
        #expect(updated.defaultRate == 0.32)
        #expect(svc.plans.first?.defaultRate == 0.32)
    }

    @Test
    func deleteRemovesRowFromLocalList() async throws {
        let api = MockPricingAPIClient()
        let existing = makePlan(id: "pp-1", start: "2026-01-01", end: "2026-07-01")
        api.plansToReturn = [existing]
        let svc = PricingService()
        svc.bind(apiClient: api)
        try await svc.refresh()

        try await svc.delete(id: existing.id)
        #expect(svc.plans.isEmpty)
    }

    @Test
    func replaceOpenEndedFoldsBothChanges() async throws {
        let api = MockPricingAPIClient()
        let open = makePlan(id: "pp-open", start: "2026-01-01", end: nil)
        api.plansToReturn = [open]
        let svc = PricingService()
        svc.bind(apiClient: api)
        try await svc.refresh()
        // The server returns the new row only — the service must trigger alpha
        // refetch so the local list reflects the closing-row's new endDate.
        let newOpen = makePlan(id: "pp-new", start: "2026-08-01", end: nil)
        let closed = makePlan(id: "pp-open", start: "2026-01-01", end: "2026-08-01")
        api.replaceOpenEndedResult = ReplaceOpenEndedResult(closing: closed, newPlan: newOpen)
        // After the refetch the API returns the final state.
        api.plansToReturn = [closed, newOpen]

        let draft = PricingPlanDraft(plan: newOpen)
        let result = try await svc.replaceOpenEnded(closingId: "pp-open", with: draft)
        #expect(result.id == "pp-new")
        try await waitForRefetch(api: api, expected: 2)
        let finalPlans = svc.plans
        #expect(finalPlans.count == 2)
        #expect(finalPlans.first?.id == "pp-open")
        #expect(finalPlans.first?.endDate == "2026-08-01")
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

    private func makePlan(id: String, start: String, end: String?, defaultRate: Double = 0.30) -> PricingPlan {
        PricingPlan(
            id: id,
            startDate: start,
            endDate: end,
            defaultRate: defaultRate,
            windows: [PlanWindow(start: "11:00", end: "14:00", free: true, rate: nil)],
            feedInRate: 0.05,
            savingsReferenceRate: 0.12,
            createdAt: Date(timeIntervalSince1970: 1),
            updatedAt: Date(timeIntervalSince1970: 1)
        )
    }
}

private extension PricingPlanDraft {
    func with(defaultRate: Double) -> PricingPlanDraft {
        var copy = self
        copy.defaultRate = defaultRate
        return copy
    }
}

// MARK: - Test doubles

final class MockPricingAPIClient: FluxAPIClient, @unchecked Sendable {
    var plansToReturn: [PricingPlan] = []
    var fetchError: FluxAPIError?
    var fetchCallCount = 0
    var replaceOpenEndedResult: ReplaceOpenEndedResult?

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

    func fetchPricing() async throws -> [PricingPlan] {
        fetchCallCount += 1
        if let fetchError { throw fetchError }
        return plansToReturn
    }

    func createPricing(_ draft: PricingPlanDraft) async throws -> PricingPlan {
        let now = Date()
        let plan = PricingPlan(
            id: "mock-\(UUID().uuidString)",
            startDate: draft.startDate,
            endDate: draft.endDate,
            defaultRate: draft.defaultRate,
            windows: draft.windows,
            feedInRate: draft.feedInRate,
            savingsReferenceRate: draft.savingsReferenceRate,
            createdAt: now,
            updatedAt: now
        )
        plansToReturn.append(plan)
        return plan
    }

    func updatePricing(id: String, _ draft: PricingPlanDraft) async throws -> PricingPlan {
        guard let idx = plansToReturn.firstIndex(where: { $0.id == id }) else {
            throw FluxAPIError.notFound
        }
        let now = Date()
        let plan = PricingPlan(
            id: id,
            startDate: draft.startDate,
            endDate: draft.endDate,
            defaultRate: draft.defaultRate,
            windows: draft.windows,
            feedInRate: draft.feedInRate,
            savingsReferenceRate: draft.savingsReferenceRate,
            createdAt: plansToReturn[idx].createdAt,
            updatedAt: now
        )
        plansToReturn[idx] = plan
        return plan
    }

    func deletePricing(id: String) async throws {
        plansToReturn.removeAll { $0.id == id }
    }

    func replaceOpenEndedPricing(
        closingId _: String,
        with _: PricingPlanDraft
    ) async throws -> ReplaceOpenEndedResult {
        if let result = replaceOpenEndedResult { return result }
        throw FluxAPIError.serverError
    }
}
