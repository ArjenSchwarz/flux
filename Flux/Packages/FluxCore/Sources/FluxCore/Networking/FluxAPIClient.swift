public protocol FluxAPIClient: Sendable {
    func fetchStatus() async throws -> StatusResponse
    func fetchHistory(days: Int) async throws -> HistoryResponse
    func fetchDay(date: String) async throws -> DayDetailResponse
    func saveNote(date: String, text: String) async throws -> NoteResponse

    // SoC alerts — soc-alerts spec.
    func registerDevice(_ registration: DeviceRegistration) async throws -> DeviceItemResponse
    func fetchRules(deviceId: String) async throws -> [SoCAlertRule]
    func createRule(deviceId: String, rule: SoCAlertRuleDraft) async throws -> SoCAlertRule
    func updateRule(deviceId: String, rule: SoCAlertRule) async throws -> SoCAlertRule
    func deleteRule(deviceId: String, ruleId: String) async throws
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
}
