import Foundation

/// Owns the pricing cache and the mutating CRUD path. View models read
/// `periods` directly; AC 2.7 requires a fetch on every UI entry point and
/// immediately after any mutation. The post-mutation refetch is
/// fire-and-forget so the editor sees instant local feedback while the
/// authoritative list lands on the next tick.
@MainActor
@Observable
public final class PricingService {
    public static let shared = PricingService()

    public private(set) var periods: [PricingPeriod] = []
    public private(set) var lastError: Error?

    private var apiClient: (any FluxAPIClient)?

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
            periods = remote.sorted { $0.startDate < $1.startDate }
            lastError = nil
        } catch {
            lastError = error
            throw error
        }
    }

    @discardableResult
    public func create(_ draft: PricingPeriodDraft) async throws -> PricingPeriod {
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
    public func update(id: String, _ draft: PricingPeriodDraft) async throws -> PricingPeriod {
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
            periods.removeAll { $0.id == id }
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
        with draft: PricingPeriodDraft
    ) async throws -> PricingPeriod {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            let result = try await apiClient.replaceOpenEndedPricing(closingId: closingId, with: draft)
            foldInsert(result.closing)
            foldInsert(result.newPeriod)
            lastError = nil
            scheduleRefetch()
            return result.newPeriod
        } catch {
            lastError = error
            throw error
        }
    }

    private func foldInsert(_ period: PricingPeriod) {
        if let idx = periods.firstIndex(where: { $0.id == period.id }) {
            periods[idx] = period
        } else {
            periods.append(period)
        }
        periods.sort { $0.startDate < $1.startDate }
    }

    private func foldReplace(_ period: PricingPeriod) {
        if let idx = periods.firstIndex(where: { $0.id == period.id }) {
            periods[idx] = period
            periods.sort { $0.startDate < $1.startDate }
        } else {
            foldInsert(period)
        }
    }

    private func scheduleRefetch() {
        Task { @MainActor [weak self] in
            try? await self?.refresh()
        }
    }
}
