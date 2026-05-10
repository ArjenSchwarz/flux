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
}
