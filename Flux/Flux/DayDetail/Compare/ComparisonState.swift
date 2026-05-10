import Foundation

/// Lifecycle state of the Compare feature. `loading` carries the
/// resolved comparison date so the in-flight fetch is observable for
/// debugging; `ready` carries the snapshot and the period that
/// produced it so VoiceOver labels can name the period without going
/// through the live `@AppStorage` binding.
enum ComparisonState: Sendable, Equatable {
    case off
    case loading(date: String)
    case ready(ComparisonSnapshot, period: ComparePeriod)
    case unavailable(period: ComparePeriod)

    var isUnavailable: Bool {
        if case .unavailable = self { return true }
        return false
    }

    /// Maps the compare lifecycle to the row-level sub-line slot. Single
    /// source of truth for the off → hidden, loading|unavailable →
    /// reserved, ready → delta dispatch used by every supported row.
    func subline(current: Double?, comparison: Double?) -> SublineContent {
        switch self {
        case .off:
            return .hidden
        case .loading, .unavailable:
            return .reserved
        case .ready:
            return DeltaFormatter.sublineContent(current: current, comparison: comparison)
        }
    }
}
