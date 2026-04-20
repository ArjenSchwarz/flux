import Foundation
import WidgetKit

public enum WidgetAccessibility {
    public static func label(for entry: StatusEntry, family: WidgetFamily) -> String {
        guard let envelope = entry.envelope else {
            return "Flux battery data unavailable"
        }

        let base = baseLabel(envelope: envelope, family: family)
        if entry.staleness == .offline {
            return "Offline. " + base
        }
        return base
    }

    private static func baseLabel(
        envelope: StatusSnapshotEnvelope,
        family: WidgetFamily
    ) -> String {
        let live = envelope.status.live
        let socInt = Int((live?.soc ?? 0).rounded())
        let verb = statusVerb(live: live)

        switch family {
        case .accessoryInline:
            return "Battery \(socInt) percent, \(verb)"
        case .accessoryCircular:
            return "Battery \(socInt) percent"
        case .accessoryRectangular:
            return "Battery \(socInt) percent, \(verb)"
        case .systemSmall:
            if let live {
                return "Battery \(socInt) percent, \(verb). Load \(watts(live.pload))."
            }
            return "Battery \(socInt) percent, \(verb)."
        case .systemMedium, .systemLarge:
            if let live {
                return "Battery \(socInt) percent, \(verb). Solar \(watts(live.ppv)), load \(watts(live.pload)), grid \(gridPhrase(live))."
            }
            return "Battery \(socInt) percent, \(verb)."
        @unknown default:
            return "Battery \(socInt) percent, \(verb)."
        }
    }

    private static func statusVerb(live: LiveData?) -> String {
        guard let live else { return "no live data" }
        if live.soc >= 100, live.pbat <= 0 { return "full" }
        if live.pbat > 0 { return "discharging" }
        if live.pbat < 0 { return "charging" }
        return "idle"
    }

    private static func watts(_ value: Double) -> String {
        let absolute = abs(value)
        if absolute >= 1000 {
            return String(format: "%.1f kilowatts", absolute / 1000)
        }
        return String(format: "%.0f watts", absolute)
    }

    private static func gridPhrase(_ live: LiveData) -> String {
        if live.pgrid < 0 {
            return "exporting \(watts(live.pgrid))"
        }
        if live.pgrid > 0 {
            return "importing \(watts(live.pgrid))"
        }
        return "idle"
    }
}
