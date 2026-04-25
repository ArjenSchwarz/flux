import FluxCore
import Foundation

enum StatusLineStyle {
    case full
    case short
    case word
}

extension StatusEntry {
    var live: LiveData? { envelope?.status.live }
    var battery: BatteryInfo? { envelope?.status.battery }
    var offpeak: OffpeakData? { envelope?.status.offpeak }
    var rolling15min: RollingAvg? { envelope?.status.rolling15min }

    var soc: Double { live?.soc ?? 0 }
    var pbat: Double { live?.pbat ?? 0 }
    var pload: Double { live?.pload ?? 0 }
    var ppv: Double { live?.ppv ?? 0 }
    var pgrid: Double { live?.pgrid ?? 0 }

    var statusWord: String {
        if staleness == .offline { return "offline" }
        guard let live else { return "idle" }
        if live.pbat > 0 { return "discharging" }
        if live.pbat < 0 { return "charging" }
        return "idle"
    }

    func statusLine(style: StatusLineStyle) -> String {
        if staleness == .offline {
            return "Offline"
        }
        guard let live else { return "No live data" }

        switch style {
        case .word:
            return statusWord
        case .short:
            let word = statusWord.capitalized
            if live.pbat > 0, let cutoff = rolling15min?.estimatedCutoffTime.flatMap(DateFormatting.parseTimestamp) {
                return "\(word) · \(DateFormatting.clockTime(from: cutoff))"
            }
            return word
        case .full:
            let rate = PowerFormatting.format(live.pbat)
            if live.pbat > 0 {
                if let cutoff = rolling15min?.estimatedCutoffTime.flatMap(DateFormatting.parseTimestamp) {
                    return "Discharging at \(rate) · cutoff ~\(DateFormatting.clockTime(from: cutoff))"
                }
                return "Discharging at \(rate)"
            }
            if live.pbat < 0 {
                return "Charging at \(rate)"
            }
            return "Idle"
        }
    }

    var gridTitle: String {
        if pgrid < 0 { return "Grid (export)" }
        if pgrid > 0 { return "Grid (import)" }
        return "Grid"
    }

    var isPlaceholder: Bool { source == .placeholder }
}
