import SwiftUI

enum GridColor {
    static func forGrid(
        pgrid: Double,
        pgridSustained: Bool,
        offpeakWindowStart: String,
        offpeakWindowEnd: String,
        now: Date = .now
    ) -> Color {
        if pgrid < 0 {
            return .green
        }

        if pgrid > 500 &&
            pgridSustained &&
            !DateFormatting.isInOffpeakWindow(start: offpeakWindowStart, end: offpeakWindowEnd, now: now) {
            return .red
        }

        return .primary
    }
}
