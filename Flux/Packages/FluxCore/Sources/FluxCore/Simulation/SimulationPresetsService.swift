import Foundation

/// Owns the simulation-presets cache and the server-confirmed-then-apply CRUD
/// path. View models read `presets` directly. Mirrors `SoCAlertsService`'s
/// failure model (Decision 9): a change is applied locally only after the
/// server confirms it; on failure the list is left unchanged and `lastError`
/// is surfaced for a banner.
@MainActor
@Observable
public final class SimulationPresetsService {
    public static let shared = SimulationPresetsService()

    public private(set) var presets: [SimulationPreset] = []
    public private(set) var lastError: Error?

    private var apiClient: (any FluxAPIClient)?

    public init() {}

    /// Wires the API client. Call once from the app's startup path.
    public func bind(apiClient: any FluxAPIClient) {
        self.apiClient = apiClient
    }

    /// Drops the in-memory lastError; the editor sheet calls this on dismiss
    /// so the banner clears.
    public func clearError() {
        lastError = nil
    }

    public func refresh() async throws {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            let remote = try await apiClient.fetchPresets()
            // Bail if a fresher refresh has already overtaken us.
            try Task.checkCancellation()
            presets = remote.sorted { $0.createdAt < $1.createdAt }
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
    public func create(_ draft: SimulationPresetDraft) async throws -> SimulationPreset {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            let created = try await apiClient.createPreset(draft)
            foldInsert(created)
            lastError = nil
            return created
        } catch {
            lastError = error
            throw error
        }
    }

    @discardableResult
    public func update(_ preset: SimulationPreset) async throws -> SimulationPreset {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            let updated = try await apiClient.updatePreset(preset)
            if let idx = presets.firstIndex(where: { $0.id == updated.id }) {
                presets[idx] = updated
            } else {
                foldInsert(updated)
            }
            lastError = nil
            return updated
        } catch {
            lastError = error
            throw error
        }
    }

    public func delete(_ presetId: String) async throws {
        guard let apiClient else {
            lastError = FluxAPIError.notConfigured
            throw FluxAPIError.notConfigured
        }
        do {
            try await apiClient.deletePreset(id: presetId)
            presets.removeAll { $0.id == presetId }
            lastError = nil
        } catch {
            lastError = error
            throw error
        }
    }

    private func foldInsert(_ preset: SimulationPreset) {
        if let idx = presets.firstIndex(where: { $0.id == preset.id }) {
            presets[idx] = preset
        } else {
            presets.append(preset)
        }
        presets.sort { $0.createdAt < $1.createdAt }
    }
}
