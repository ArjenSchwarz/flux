import Foundation

public enum GridColor {
    /// The window is optional because a day whose plan has no free band — or
    /// which no plan prices — genuinely has no off-peak window (Q35). Absent
    /// is treated as "outside the window", the same as any other time of day;
    /// substituting a default would falsely excuse sustained import.
    public static func forGrid(
        pgrid: Double,
        pgridSustained: Bool,
        offpeakWindowStart: String?,
        offpeakWindowEnd: String?,
        now: Date = .now
    ) -> ColorTier {
        if pgrid < 0 {
            return .green
        }

        let inOffpeak: Bool
        if let offpeakWindowStart, let offpeakWindowEnd {
            inOffpeak = DateFormatting.isInOffpeakWindow(start: offpeakWindowStart, end: offpeakWindowEnd, now: now)
        } else {
            inOffpeak = false
        }

        if pgrid > 500 && pgridSustained && !inOffpeak {
            return .red
        }

        return .normal
    }
}
