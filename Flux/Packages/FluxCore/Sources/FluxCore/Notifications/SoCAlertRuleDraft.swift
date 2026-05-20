import Foundation

/// The editable shape used by the rule editor sheet. The backend assigns id
/// / createdAt / updatedAt on POST, so the draft only carries the writable
/// fields and a local validation method.
public struct SoCAlertRuleDraft: Sendable, Equatable {
    public var thresholdPercent: Int
    public var windowStart: String
    public var windowEnd: String
    public var enabled: Bool
    public var label: String?

    public init(
        thresholdPercent: Int = 40,
        windowStart: String = "17:00",
        windowEnd: String = "00:00",
        enabled: Bool = true,
        label: String? = nil
    ) {
        self.thresholdPercent = thresholdPercent
        self.windowStart = windowStart
        self.windowEnd = windowEnd
        self.enabled = enabled
        self.label = label
    }

    /// Convenience: seed a draft from an existing rule for the edit sheet.
    public init(rule: SoCAlertRule) {
        self.thresholdPercent = rule.thresholdPercent
        self.windowStart = rule.windowStart
        self.windowEnd = rule.windowEnd
        self.enabled = rule.enabled
        self.label = rule.label
    }

    /// Reasons a draft can fail validation. Map back to user-visible text
    /// in the view model so this enum can stay localisation-agnostic.
    public enum ValidationError: Error, Equatable, Sendable {
        case thresholdOutOfRange
        case invalidWindowStart
        case invalidWindowEnd
        case startEqualsEnd
        case labelTooLong
    }

    /// Local pre-flight validation matching AC 1.3 / 1.2. Returns nil when
    /// the draft is valid. The backend re-validates server-side; this is
    /// purely for early feedback in the editor.
    public func validate() -> ValidationError? {
        if thresholdPercent < 1 || thresholdPercent > 99 {
            return .thresholdOutOfRange
        }
        if !Self.isValidHHMM(windowStart) {
            return .invalidWindowStart
        }
        if !Self.isValidHHMM(windowEnd) {
            return .invalidWindowEnd
        }
        if windowStart == windowEnd {
            return .startEqualsEnd
        }
        if let label, label.count > 40 {
            return .labelTooLong
        }
        return nil
    }

    private static func isValidHHMM(_ s: String) -> Bool {
        let parts = s.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        guard parts.count == 2,
              let h = Int(parts[0]), let m = Int(parts[1]),
              (0...23).contains(h), (0...59).contains(m)
        else { return false }
        return true
    }
}
