import Foundation

// MARK: - Simulation (dashboard-simulation spec)

extension URLSessionAPIClient {
    /// Simulated status. Sends the added load as a single `simulateLoadWatts`
    /// query item; the backend returns a what-if status. Kept separate from
    /// `fetchStatus()` so non-Dashboard call sites (widget timeline, settings
    /// validation) can never accidentally simulate.
    public func fetchStatus(simulateLoadWatts: Int) async throws -> StatusResponse {
        try await performRequest(
            path: "status",
            queryItems: [URLQueryItem(name: "simulateLoadWatts", value: String(simulateLoadWatts))]
        )
    }

    public func fetchPresets() async throws -> [SimulationPreset] {
        let response: SimulationPresetListResponse = try await performRequest(
            path: "simulation-presets",
            queryItems: []
        )
        return response.presets
    }

    public func createPreset(_ draft: SimulationPresetDraft) async throws -> SimulationPreset {
        let body = try encoder.encode(SimulationPresetPayload(draft: draft))
        return try await performRequest(
            path: "simulation-presets",
            queryItems: [],
            method: "POST",
            body: body
        )
    }

    public func updatePreset(_ preset: SimulationPreset) async throws -> SimulationPreset {
        let body = try encoder.encode(SimulationPresetPayload(preset: preset))
        return try await performRequest(
            path: "simulation-presets/\(preset.id)",
            queryItems: [],
            method: "PUT",
            body: body
        )
    }

    public func deletePreset(id: String) async throws {
        let _: EmptySimulationPresetResponse = try await performRequest(
            path: "simulation-presets/\(id)",
            queryItems: [],
            method: "DELETE"
        )
    }

    private struct SimulationPresetPayload: Encodable {
        let label: String
        let watts: Int

        init(draft: SimulationPresetDraft) {
            self.label = draft.label
            self.watts = draft.watts
        }

        init(preset: SimulationPreset) {
            self.label = preset.label
            self.watts = preset.watts
        }
    }

    private struct SimulationPresetListResponse: Decodable {
        let presets: [SimulationPreset]
    }

    private struct EmptySimulationPresetResponse: Decodable {
        init(from _: Decoder) throws {}
    }
}
