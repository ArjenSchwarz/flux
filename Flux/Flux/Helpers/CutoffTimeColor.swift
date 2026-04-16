import Foundation

enum CutoffTimeColor {
    static func forCutoff(
        _ cutoffTime: Date,
        offpeakWindowStart: String,
        now: Date = .now
    ) -> ColorTier {
        if cutoffTime.timeIntervalSince(now) < 2 * 60 * 60 {
            return .red
        }

        guard let offpeakStart = DateFormatting.parseWindowTime(offpeakWindowStart, on: now) else {
            return .normal
        }

        if cutoffTime < offpeakStart {
            return .orange
        }

        return .normal
    }
}
