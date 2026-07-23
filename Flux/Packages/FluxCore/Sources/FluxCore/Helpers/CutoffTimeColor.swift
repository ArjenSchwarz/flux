import Foundation

public enum CutoffTimeColor {
    /// The window start is optional because a day whose plan has no free band
    /// has no window to run out of charge before (Q35). Absent takes the same
    /// path as an unparseable window: no escalation beyond the two-hour rule.
    public static func forCutoff(
        _ cutoffTime: Date,
        offpeakWindowStart: String?,
        now: Date = .now
    ) -> ColorTier {
        if cutoffTime.timeIntervalSince(now) < 2 * 60 * 60 {
            return .red
        }

        guard let offpeakWindowStart,
              let offpeakStart = DateFormatting.parseWindowTime(offpeakWindowStart, on: now) else {
            return .normal
        }

        if cutoffTime < offpeakStart {
            return .orange
        }

        return .normal
    }
}
