import FluxCore
import SwiftUI

extension StatusEntry {
    var effectiveBatteryColor: Color {
        if staleness == .offline { return .secondary }

        if let cutoff = rolling15min?.estimatedCutoffTime.flatMap(DateFormatting.parseTimestamp) {
            let windowStart = offpeak?.windowStart ?? OffpeakData.defaultWindowStart
            return CutoffTimeColor.forCutoff(cutoff, offpeakWindowStart: windowStart, now: date).color
        }

        return BatteryColor.forSOC(soc).color
    }
}
