import FluxCore
import Foundation
import Testing
@testable import Flux

@MainActor
@Suite(.serialized)
struct SoCAlertsViewModelTests {
    @Test
    func draftIsValidWithDefaultValues() {
        let vm = makeViewModel()
        vm.beginCreate()
        #expect(vm.canSave, "default draft (threshold 40, 17:00-00:00) must be valid")
    }

    @Test
    func saveDisabledWhenThresholdOutOfRange() {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.thresholdPercent = 0
        #expect(vm.canSave == false)
        vm.draft.thresholdPercent = 100
        #expect(vm.canSave == false)
    }

    @Test
    func saveDisabledWhenStartEqualsEnd() {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.windowStart = "17:00"
        vm.draft.windowEnd = "17:00"
        #expect(vm.canSave == false)
    }

    @Test
    func saveDisabledWhenLabelExceeds40Chars() {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.label = String(repeating: "a", count: 41)
        #expect(vm.canSave == false)
    }

    @Test
    func addAffordanceDisabledAtCap() async throws {
        let (service, _) = makeServiceAndAPI()
        let vm = SoCAlertsViewModel(service: service)
        vm.beginCreate()
        for _ in 0 ..< 10 {
            _ = try await vm.save()
            vm.beginCreate()
        }
        #expect(vm.rules.count == 10)
        #expect(vm.addAffordanceEnabled == false,
                "the 11th add affordance must be disabled (AC 1.5)")
    }

    @Test
    func saveCreatesNewRuleWhenEditorWasOpenedForCreate() async throws {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.thresholdPercent = 35
        try await vm.save()
        #expect(vm.rules.count == 1)
        #expect(vm.rules.first?.thresholdPercent == 35)
    }

    @Test
    func saveUpdatesExistingRuleWhenEditorWasOpenedForEdit() async throws {
        let vm = makeViewModel()
        vm.beginCreate()
        vm.draft.thresholdPercent = 30
        try await vm.save()
        let existing = try #require(vm.rules.first)
        vm.beginEdit(existing)
        vm.draft.thresholdPercent = 45
        try await vm.save()
        #expect(vm.rules.first?.thresholdPercent == 45)
        #expect(vm.rules.count == 1)
    }

    @Test
    func deleteRemovesRuleLocally() async throws {
        let vm = makeViewModel()
        vm.beginCreate()
        try await vm.save()
        let rule = try #require(vm.rules.first)
        try await vm.delete(rule)
        #expect(vm.rules.isEmpty)
    }

    @Test
    func showsErrorBannerWhenServiceReportsLastError() async {
        let (service, _) = makeServiceAndAPI(failOnRefresh: FluxAPIError.serverError)
        try? await service.refresh()
        let vm = SoCAlertsViewModel(service: service)
        #expect(vm.showsErrorBanner == true,
                "view model must reflect SoCAlertsService.lastError as a banner")
    }

    @Test
    func clearErrorDismissesBanner() async {
        let (service, _) = makeServiceAndAPI(failOnRefresh: FluxAPIError.serverError)
        try? await service.refresh()
        let vm = SoCAlertsViewModel(service: service)
        #expect(vm.showsErrorBanner)
        vm.clearError()
        #expect(vm.showsErrorBanner == false)
    }

    // MARK: - helpers

    private func makeViewModel() -> SoCAlertsViewModel {
        let (service, _) = makeServiceAndAPI()
        return SoCAlertsViewModel(service: service)
    }

    private func makeServiceAndAPI(failOnRefresh error: Error? = nil) -> (SoCAlertsService, ViewModelTestAPIClient) {
        let suite = "flux.test.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)
        let identifier = DeviceIdentifier(userDefaults: defaults)
        let service = SoCAlertsService(deviceIdentifier: identifier, registrationCache: defaults)
        let api = ViewModelTestAPIClient(refreshError: error)
        service.bind(apiClient: api)
        return (service, api)
    }
}

@MainActor
final class ViewModelTestAPIClient: FluxAPIClient, @unchecked Sendable {
    private var rules: [SoCAlertRule] = []
    private var nextID = 1
    private let refreshError: Error?

    init(refreshError: Error? = nil) { self.refreshError = refreshError }

    nonisolated func fetchStatus() async throws -> StatusResponse {
        StatusResponse(live: nil, battery: nil, rolling15min: nil, offpeak: nil, todayEnergy: nil, note: nil)
    }
    nonisolated func fetchHistory(days _: Int) async throws -> HistoryResponse { HistoryResponse(days: []) }
    nonisolated func fetchDay(date: String) async throws -> DayDetailResponse {
        DayDetailResponse(date: date, readings: [], summary: nil, peakPeriods: nil, dailyUsage: nil, note: nil)
    }
    nonisolated func saveNote(date: String, text _: String) async throws -> NoteResponse {
        NoteResponse(date: date, text: "", updatedAt: nil)
    }

    nonisolated func registerDevice(_ registration: DeviceRegistration) async throws -> DeviceItemResponse {
        DeviceItemResponse(
            deviceId: registration.deviceId,
            platform: registration.platform,
            apnsToken: registration.apnsToken,
            tzIdentifier: registration.tzIdentifier,
            tzUpdatedAt: registration.tzUpdatedAt,
            tokenStatus: "active",
            lastRegisteredAt: nil
        )
    }

    func fetchRules(deviceId _: String) async throws -> [SoCAlertRule] {
        if let refreshError { throw refreshError }
        return rules
    }

    func createRule(deviceId _: String, rule: SoCAlertRuleDraft) async throws -> SoCAlertRule {
        let now = Date()
        let r = SoCAlertRule(
            id: "test-\(nextID)",
            thresholdPercent: rule.thresholdPercent,
            windowStart: rule.windowStart,
            windowEnd: rule.windowEnd,
            enabled: rule.enabled,
            label: rule.label,
            createdAt: now,
            updatedAt: now
        )
        nextID += 1
        rules.append(r)
        return r
    }

    func updateRule(deviceId _: String, rule: SoCAlertRule) async throws -> SoCAlertRule {
        if let idx = rules.firstIndex(where: { $0.id == rule.id }) {
            var u = rule
            u.updatedAt = Date()
            rules[idx] = u
            return u
        }
        throw FluxAPIError.badRequest("not found")
    }

    func deleteRule(deviceId _: String, ruleId: String) async throws {
        rules.removeAll { $0.id == ruleId }
    }
}
