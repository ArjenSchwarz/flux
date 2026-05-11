import Foundation

public enum PowerFormatting {
    public static func format(_ watts: Double) -> String {
        let absolute = abs(watts)
        if absolute >= 1000 {
            return String(format: "%.2f kW", absolute / 1000)
        }
        return String(format: "%.0f W", absolute)
    }

    /// Returns the formatted numeric portion and the unit separately so the
    /// caller can render them at different sizes (used by the V5 live trio).
    public static func split(_ watts: Double) -> (value: String, unit: String) {
        let absolute = abs(watts)
        if absolute >= 1000 {
            return (String(format: "%.2f", absolute / 1000), "kW")
        }
        return (String(format: "%.0f", absolute), "W")
    }

    public static func formatAxis(_ watts: Double) -> String {
        let absolute = abs(watts)
        if absolute >= 1000 {
            let kilowatts = watts / 1000
            if kilowatts == kilowatts.rounded() {
                return String(format: "%.0f kW", kilowatts)
            }
            return String(format: "%.1f kW", kilowatts)
        }
        return String(format: "%.0f W", watts)
    }
}

public enum EnergyFormatting {
    /// Formats a kilowatt-hour value, falling back to Wh when below 1 kWh.
    /// Returns "—" for nil.
    public static func format(_ kilowattHours: Double?) -> String {
        guard let value = kilowattHours else { return "—" }
        if abs(value) >= 1 {
            return String(format: "%.2f kWh", value)
        }
        return String(format: "%.0f Wh", value * 1000)
    }

    /// Spoken form of `format(_:)` for VoiceOver labels. VoiceOver reads
    /// "kWh" as the letters k-W-h, so accessibility labels that include
    /// the spoken unit must spell it out. Returns "—" for nil so the
    /// glyph still reads naturally.
    public static func formatSpoken(_ kilowattHours: Double?) -> String {
        guard let value = kilowattHours else { return "—" }
        if abs(value) >= 1 {
            return String(format: "%.2f kilowatt-hours", value)
        }
        return String(format: "%.0f watt-hours", value * 1000)
    }
}
