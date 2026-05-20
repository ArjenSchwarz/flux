import Foundation
import Testing
@testable import FluxCore

@Suite
struct SoCAlertRuleTests {
    @Test
    func decodeFromBackendShape() throws {
        let json = """
        {
          "id": "rule-uuid",
          "thresholdPercent": 40,
          "windowStart": "17:00",
          "windowEnd": "00:00",
          "enabled": true,
          "label": "Evening cooking",
          "createdAt": "2026-05-19T08:00:00Z",
          "updatedAt": "2026-05-19T08:00:00Z"
        }
        """.data(using: .utf8)!

        let rule = try jsonDecoder().decode(SoCAlertRule.self, from: json)
        #expect(rule.id == "rule-uuid")
        #expect(rule.thresholdPercent == 40)
        #expect(rule.windowStart == "17:00")
        #expect(rule.windowEnd == "00:00")
        #expect(rule.enabled == true)
        #expect(rule.label == "Evening cooking")
    }

    @Test
    func encodeRoundTrip() throws {
        let rule = SoCAlertRule(
            id: "r1",
            thresholdPercent: 35,
            windowStart: "22:00",
            windowEnd: "06:00",
            enabled: true,
            label: nil,
            createdAt: Date(timeIntervalSince1970: 1_715_000_000),
            updatedAt: Date(timeIntervalSince1970: 1_715_100_000)
        )
        let data = try jsonEncoder().encode(rule)
        let back = try jsonDecoder().decode(SoCAlertRule.self, from: data)
        #expect(back.id == rule.id)
        #expect(back.thresholdPercent == rule.thresholdPercent)
        #expect(back.label == rule.label)
    }

    @Test
    func draftValidationThresholdRange() {
        let base = SoCAlertRuleDraft(thresholdPercent: 40, windowStart: "17:00", windowEnd: "18:00", enabled: true, label: nil)
        #expect(base.validate() == nil)

        var d = base
        d.thresholdPercent = 0
        #expect(d.validate() == .thresholdOutOfRange)
        d.thresholdPercent = 100
        #expect(d.validate() == .thresholdOutOfRange)
    }

    @Test
    func draftValidationHHMM() {
        var d = SoCAlertRuleDraft(thresholdPercent: 40, windowStart: "25:00", windowEnd: "18:00", enabled: true)
        #expect(d.validate() == .invalidWindowStart)

        d = SoCAlertRuleDraft(thresholdPercent: 40, windowStart: "17:00", windowEnd: "99:99", enabled: true)
        #expect(d.validate() == .invalidWindowEnd)
    }

    @Test
    func draftValidationStartEqualsEnd() {
        let d = SoCAlertRuleDraft(thresholdPercent: 40, windowStart: "17:00", windowEnd: "17:00", enabled: true)
        #expect(d.validate() == .startEqualsEnd)
    }

    @Test
    func draftValidationLabelLengthCap() {
        let longLabel = String(repeating: "a", count: 41)
        let d = SoCAlertRuleDraft(thresholdPercent: 40, windowStart: "17:00", windowEnd: "18:00", enabled: true, label: longLabel)
        #expect(d.validate() == .labelTooLong)
    }

    @Test
    func draftValidationLabelAt40CharsOK() {
        let label = String(repeating: "a", count: 40)
        let d = SoCAlertRuleDraft(thresholdPercent: 40, windowStart: "17:00", windowEnd: "18:00", enabled: true, label: label)
        #expect(d.validate() == nil)
    }

    @Test
    func ruleIsEquatableAndHashable() {
        let r1 = SoCAlertRule(id: "x", thresholdPercent: 30, windowStart: "00:00", windowEnd: "01:00", enabled: true, label: nil, createdAt: Date(timeIntervalSince1970: 1), updatedAt: Date(timeIntervalSince1970: 1))
        let r2 = r1
        let r3 = SoCAlertRule(id: "y", thresholdPercent: 30, windowStart: "00:00", windowEnd: "01:00", enabled: true, label: nil, createdAt: Date(timeIntervalSince1970: 1), updatedAt: Date(timeIntervalSince1970: 1))
        #expect(r1 == r2)
        #expect(r1 != r3)
        var set: Set<SoCAlertRule> = []
        set.insert(r1)
        set.insert(r2)
        set.insert(r3)
        #expect(set.count == 2)
    }

    // MARK: - helpers

    private func jsonEncoder() -> JSONEncoder {
        let enc = JSONEncoder()
        enc.dateEncodingStrategy = .iso8601
        return enc
    }

    private func jsonDecoder() -> JSONDecoder {
        let dec = JSONDecoder()
        dec.dateDecodingStrategy = .iso8601
        return dec
    }
}
