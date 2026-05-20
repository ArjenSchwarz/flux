import Foundation
import Testing
@testable import FluxCore

@MainActor
@Suite(.serialized)
struct SoCAlertsServiceTests {
    @Test
    func registerDeviceIfNeededPostsFirstCall() async throws {
        let api = TestAPIClient()
        let svc = makeService(api: api)

        try await svc.registerDeviceIfNeeded(token: Data([0xde, 0xad, 0xbe, 0xef]), tz: TimeZone(identifier: "Australia/Sydney")!)
        #expect(await api.registrationCalls.count == 1)
        let call = try #require(await api.registrationCalls.first)
        #expect(call.apnsToken == "deadbeef")
        #expect(call.tzIdentifier == "Australia/Sydney")
    }

    @Test
    func registerDeviceIfNeededIsIdempotentWhenNothingChanged() async throws {
        let api = TestAPIClient()
        let svc = makeService(api: api)

        let token = Data([0xde, 0xad, 0xbe, 0xef])
        try await svc.registerDeviceIfNeeded(token: token, tz: TimeZone(identifier: "Australia/Sydney")!)
        try await svc.registerDeviceIfNeeded(token: token, tz: TimeZone(identifier: "Australia/Sydney")!)
        #expect(await api.registrationCalls.count == 1,
                "second call with same token+tz must not POST again")
    }

    @Test
    func registerDeviceIfNeededRePostsOnTokenChange() async throws {
        let api = TestAPIClient()
        let svc = makeService(api: api)

        try await svc.registerDeviceIfNeeded(token: Data([0x01, 0x02]), tz: TimeZone(identifier: "Australia/Sydney")!)
        try await svc.registerDeviceIfNeeded(token: Data([0x03, 0x04]), tz: TimeZone(identifier: "Australia/Sydney")!)
        #expect(await api.registrationCalls.count == 2)
    }

    @Test
    func registerDeviceIfNeededPostsWithoutToken() async throws {
        let api = TestAPIClient()
        let svc = makeService(api: api)
        try await svc.registerDeviceIfNeeded(token: nil, tz: TimeZone(identifier: "Australia/Sydney")!)
        let call = try #require(await api.registrationCalls.first)
        #expect(call.apnsToken == nil, "denial path: backend gets the row without a token")
    }

    @Test
    func createRuleIsOptimisticAndAppendsToLocalRules() async throws {
        let api = TestAPIClient()
        let svc = makeService(api: api)
        try await svc.registerDeviceIfNeeded(token: Data([0xab]), tz: TimeZone(identifier: "UTC")!)

        let draft = SoCAlertRuleDraft(thresholdPercent: 30, windowStart: "17:00", windowEnd: "18:00", enabled: true)
        let created = try await svc.create(draft)
        #expect(svc.rules.contains(where: { $0.id == created.id }))
    }

    @Test
    func createRuleFailureRecordsLastError() async throws {
        let api = TestAPIClient()
        api.createRuleResult = .failure(FluxAPIError.ruleCapReached)
        let svc = makeService(api: api)
        try await svc.registerDeviceIfNeeded(token: Data([0xab]), tz: TimeZone(identifier: "UTC")!)

        do {
            _ = try await svc.create(SoCAlertRuleDraft(thresholdPercent: 30, windowStart: "17:00", windowEnd: "18:00", enabled: true))
            Issue.record("expected create to throw")
        } catch {
            #expect(svc.lastError != nil)
        }
    }

    @Test
    func refreshLoadsRulesFromBackend() async throws {
        let api = TestAPIClient()
        let now = Date()
        api.rules = [
            SoCAlertRule(id: "r1", thresholdPercent: 30, windowStart: "17:00", windowEnd: "18:00", enabled: true, label: nil, createdAt: now, updatedAt: now)
        ]
        let svc = makeService(api: api)
        try await svc.registerDeviceIfNeeded(token: Data([0x01]), tz: TimeZone(identifier: "UTC")!)
        try await svc.refresh()
        #expect(svc.rules.count == 1)
        #expect(svc.rules.first?.id == "r1")
    }

    @Test
    func foregroundHookRetriesPendingRegistration() async throws {
        let api = TestAPIClient()
        api.registerFailures = 1
        let svc = makeService(api: api)
        do {
            try await svc.registerDeviceIfNeeded(token: Data([0x01]), tz: TimeZone(identifier: "UTC")!)
        } catch {
            // expected
        }
        #expect(svc.lastError != nil)
        // Foreground hook fires; backend now succeeds.
        await svc.foregroundHook()
        #expect(await api.registrationCalls.count == 2,
                "foreground hook must replay the pending registration")
        #expect(svc.lastError == nil, "successful retry must clear lastError")
    }

    @Test
    func clearErrorResetsLastError() async throws {
        let api = TestAPIClient()
        api.createRuleResult = .failure(.serverError)
        let svc = makeService(api: api)
        try await svc.registerDeviceIfNeeded(token: Data([0x01]), tz: TimeZone(identifier: "UTC")!)
        _ = try? await svc.create(SoCAlertRuleDraft(thresholdPercent: 30, windowStart: "17:00", windowEnd: "18:00", enabled: true))
        #expect(svc.lastError != nil)
        svc.clearError()
        #expect(svc.lastError == nil)
    }

    // MARK: - helpers

    private func makeService(api: TestAPIClient) -> SoCAlertsService {
        let suite = "flux.test.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)
        let identifier = DeviceIdentifier(userDefaults: defaults)
        let service = SoCAlertsService(deviceIdentifier: identifier, registrationCache: defaults)
        service.bind(apiClient: api)
        return service
    }
}

// MARK: - Test doubles

final class TestAPIClient: FluxAPIClient, @unchecked Sendable {
    var registrationCalls: [DeviceRegistration] = []
    var rules: [SoCAlertRule] = []
    var registerFailures: Int = 0
    var createRuleResult: Result<SoCAlertRule, FluxAPIError>?

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

    func registerDevice(_ registration: DeviceRegistration) async throws -> DeviceItemResponse {
        // Record every attempt (including failures) so tests can assert on
        // retry behaviour without surprising semantics.
        registrationCalls.append(registration)
        if registerFailures > 0 {
            registerFailures -= 1
            throw FluxAPIError.serverError
        }
        return DeviceItemResponse(
            deviceId: registration.deviceId,
            platform: registration.platform,
            apnsToken: registration.apnsToken,
            tzIdentifier: registration.tzIdentifier,
            tzUpdatedAt: registration.tzUpdatedAt,
            tokenStatus: "active",
            lastRegisteredAt: nil
        )
    }

    func fetchRules(deviceId _: String) async throws -> [SoCAlertRule] { rules }
    func createRule(deviceId _: String, rule: SoCAlertRuleDraft) async throws -> SoCAlertRule {
        if let result = createRuleResult {
            switch result {
            case .success(let r): return r
            case .failure(let e): throw e
            }
        }
        let now = Date()
        let id = "mock-\(UUID().uuidString)"
        let created = SoCAlertRule(
            id: id,
            thresholdPercent: rule.thresholdPercent,
            windowStart: rule.windowStart,
            windowEnd: rule.windowEnd,
            enabled: rule.enabled,
            label: rule.label,
            createdAt: now,
            updatedAt: now
        )
        rules.append(created)
        return created
    }

    func updateRule(deviceId _: String, rule: SoCAlertRule) async throws -> SoCAlertRule {
        if let idx = rules.firstIndex(where: { $0.id == rule.id }) {
            var r = rule
            r.updatedAt = Date()
            rules[idx] = r
            return r
        }
        throw FluxAPIError.badRequest("not found")
    }

    func deleteRule(deviceId _: String, ruleId: String) async throws {
        rules.removeAll { $0.id == ruleId }
    }
}
