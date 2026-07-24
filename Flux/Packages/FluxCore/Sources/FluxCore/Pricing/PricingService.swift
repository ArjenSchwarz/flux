import Foundation

/// Owns the pricing cache and the mutating CRUD path. View models read
/// `plans` directly; AC 2.7 requires a fetch on every UI entry point and
/// immediately after any mutation. The post-mutation refetch is
/// fire-and-forget so the editor sees instant local feedback while the
/// authoritative list lands on the next tick.
@MainActor
@Observable
public final class PricingService {
    public static let shared = PricingService()

    public private(set) var plans: [PricingPlan] = []
    public private(set) var lastError: Error?

    private var apiClient: (any FluxAPIClient)?
    private var refetchTask: Task<Void, Never>?

    public init() {}

    public func bind(apiClient: any FluxAPIClient) {
        self.apiClient = apiClient
    }

    public func clearError() {
        lastError = nil
    }

    public func refresh() async throws {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            let remote = try await apiClient.fetchPricing()
            // Bail if the task was cancelled mid-flight — a fresher
            // refresh has already overtaken us.
            try Task.checkCancellation()
            plans = remote.sorted { $0.startDate < $1.startDate }
            lastError = nil
        } catch is CancellationError {
            // Cancelled refreshes are not failures; leave state alone.
            return
        } catch {
            lastError = error
            throw error
        }
    }

    @discardableResult
    public func create(_ draft: PricingPlanDraft) async throws -> PricingPlan {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            let created = try await apiClient.createPricing(draft)
            foldInsert(created)
            lastError = nil
            scheduleRefetch()
            return created
        } catch {
            lastError = error
            throw error
        }
    }

    @discardableResult
    public func update(id: String, _ draft: PricingPlanDraft) async throws -> PricingPlan {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            let updated = try await apiClient.updatePricing(id: id, draft)
            foldReplace(updated)
            lastError = nil
            scheduleRefetch()
            return updated
        } catch {
            lastError = error
            throw error
        }
    }

    public func delete(id: String) async throws {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            try await apiClient.deletePricing(id: id)
            plans.removeAll { $0.id == id }
            lastError = nil
            scheduleRefetch()
        } catch {
            lastError = error
            throw error
        }
    }

    @discardableResult
    public func replaceOpenEnded(
        closingId: String,
        with draft: PricingPlanDraft
    ) async throws -> PricingPlan {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            let result = try await apiClient.replaceOpenEndedPricing(closingId: closingId, with: draft)
            // Sort once after both folds rather than re-sorting per insert.
            foldInsert(result.closing, sort: false)
            foldInsert(result.newPlan, sort: false)
            plans.sort { $0.startDate < $1.startDate }
            lastError = nil
            scheduleRefetch()
            return result.newPlan
        } catch {
            lastError = error
            throw error
        }
    }

    private func foldInsert(_ plan: PricingPlan, sort: Bool = true) {
        if let idx = plans.firstIndex(where: { $0.id == plan.id }) {
            plans[idx] = plan
        } else {
            plans.append(plan)
        }
        if sort {
            plans.sort { $0.startDate < $1.startDate }
        }
    }

    private func foldReplace(_ plan: PricingPlan) {
        if let idx = plans.firstIndex(where: { $0.id == plan.id }) {
            plans[idx] = plan
            plans.sort { $0.startDate < $1.startDate }
        } else {
            foldInsert(plan)
        }
    }

    /// Cancels any prior fire-and-forget refetch before scheduling a new
    /// one. Stored as a single in-flight handle so a fast sequence of
    /// mutations can't have an older response clobber a newer one, and so
    /// the task isn't leaked across teardown.
    private func scheduleRefetch() {
        refetchTask?.cancel()
        refetchTask = Task { @MainActor [weak self] in
            try? await self?.refresh()
        }
    }
}
