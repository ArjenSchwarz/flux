public protocol FluxAPIClient: Sendable {
    func fetchStatus() async throws -> StatusResponse
    func fetchHistory(days: Int) async throws -> HistoryResponse
    func fetchDay(date: String) async throws -> DayDetailResponse
    func saveNote(date: String, text: String) async throws -> NoteResponse

    // Dashboard simulation — dashboard-simulation spec. A SEPARATE method from
    // `fetchStatus()` so the widget timeline and settings-validation call
    // sites stay param-free and can never simulate. Only DashboardViewModel
    // calls this, and only while a simulation is active.
    func fetchStatus(simulateLoadWatts: Int) async throws -> StatusResponse
    func fetchPresets() async throws -> [SimulationPreset]
    func createPreset(_ draft: SimulationPresetDraft) async throws -> SimulationPreset
    func updatePreset(_ preset: SimulationPreset) async throws -> SimulationPreset
    func deletePreset(id: String) async throws

    // SoC alerts — soc-alerts spec.
    func registerDevice(_ registration: DeviceRegistration) async throws -> DeviceItemResponse
    func fetchRules(deviceId: String) async throws -> [SoCAlertRule]
    func createRule(deviceId: String, rule: SoCAlertRuleDraft) async throws -> SoCAlertRule
    func updateRule(deviceId: String, rule: SoCAlertRule) async throws -> SoCAlertRule
    func deleteRule(deviceId: String, ruleId: String) async throws

    // Pricing — daily-costs spec.
    func fetchPricing() async throws -> [PricingPeriod]
    func createPricing(_ draft: PricingPeriodDraft) async throws -> PricingPeriod
    func updatePricing(id: String, _ draft: PricingPeriodDraft) async throws -> PricingPeriod
    func deletePricing(id: String) async throws
    func replaceOpenEndedPricing(
        closingId: String,
        with draft: PricingPeriodDraft
    ) async throws -> ReplaceOpenEndedResult
}

// Default implementations for the SoC-alert endpoints so existing test
// mocks (DashboardViewModelTests, NoteEditorViewModelTests, etc.) do not
// need to implement the new methods. Production conformers
// (URLSessionAPIClient, MockFluxAPIClient) override these with real logic.
public extension FluxAPIClient {
    func registerDevice(_: DeviceRegistration) async throws -> DeviceItemResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchRules(deviceId _: String) async throws -> [SoCAlertRule] {
        throw FluxAPIError.notConfigured
    }

    func createRule(deviceId _: String, rule _: SoCAlertRuleDraft) async throws -> SoCAlertRule {
        throw FluxAPIError.notConfigured
    }

    func updateRule(deviceId _: String, rule _: SoCAlertRule) async throws -> SoCAlertRule {
        throw FluxAPIError.notConfigured
    }

    func deleteRule(deviceId _: String, ruleId _: String) async throws {
        throw FluxAPIError.notConfigured
    }

    func fetchPricing() async throws -> [PricingPeriod] {
        throw FluxAPIError.notConfigured
    }

    func createPricing(_: PricingPeriodDraft) async throws -> PricingPeriod {
        throw FluxAPIError.notConfigured
    }

    func updatePricing(id _: String, _: PricingPeriodDraft) async throws -> PricingPeriod {
        throw FluxAPIError.notConfigured
    }

    func deletePricing(id _: String) async throws {
        throw FluxAPIError.notConfigured
    }

    func replaceOpenEndedPricing(
        closingId _: String,
        with _: PricingPeriodDraft
    ) async throws -> ReplaceOpenEndedResult {
        throw FluxAPIError.notConfigured
    }

    // Default so existing conformers (~30 test mocks, the widget timeline, the
    // settings-validation client) need no change. The non-simulating fallback
    // delegates to `fetchStatus()`, so any conformer that hasn't opted into
    // simulation simply returns its real status. Only URLSessionAPIClient
    // overrides this to actually send the parameter.
    func fetchStatus(simulateLoadWatts _: Int) async throws -> StatusResponse {
        try await fetchStatus()
    }

    func fetchPresets() async throws -> [SimulationPreset] {
        throw FluxAPIError.notConfigured
    }

    func createPreset(_: SimulationPresetDraft) async throws -> SimulationPreset {
        throw FluxAPIError.notConfigured
    }

    func updatePreset(_: SimulationPreset) async throws -> SimulationPreset {
        throw FluxAPIError.notConfigured
    }

    func deletePreset(id _: String) async throws {
        throw FluxAPIError.notConfigured
    }
}
