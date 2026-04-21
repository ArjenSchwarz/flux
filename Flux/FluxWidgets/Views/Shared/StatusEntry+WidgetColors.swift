import FluxCore
import SwiftUI

extension StatusEntry {
    /// Ring tint: level-driven (BatteryColor) unless a cutoff is predicted, in which case
    /// the cutoff-risk tier (red/orange) escalates regardless of SOC.
    var effectiveBatteryColor: Color {
        if staleness == .offline { return .secondary }
        if let cutoffColor = cutoffRiskColor { return cutoffColor }
        return BatteryColor.forSOC(soc).color
    }

    /// Battery-state pill tint: action-driven. Green for charging, primary for discharging,
    /// secondary for idle/full/offline. Cutoff-risk tier (red/orange) escalates when present.
    var batteryStateColor: Color {
        if staleness == .offline { return .secondary }
        guard let live else { return .secondary }
        if live.soc >= 100, live.pbat <= 0 { return .secondary } // Full
        if let cutoffColor = cutoffRiskColor { return cutoffColor }
        if live.pbat < 0 { return .green }
        if live.pbat > 0 { return .primary }
        return .secondary // Idle
    }

    private var cutoffRiskColor: Color? {
        guard let cutoff = rolling15min?.estimatedCutoffTime.flatMap(DateFormatting.parseTimestamp) else {
            return nil
        }
        // Only escalate when the cutoff is actually close — distant predictions
        // would otherwise paint the ring orange all afternoon on a normal day.
        if cutoff.timeIntervalSince(date) > 6 * 60 * 60 { return nil }
        let windowStart = offpeak?.windowStart ?? OffpeakData.defaultWindowStart
        return CutoffTimeColor.forCutoff(cutoff, offpeakWindowStart: windowStart, now: date).color
    }
}
