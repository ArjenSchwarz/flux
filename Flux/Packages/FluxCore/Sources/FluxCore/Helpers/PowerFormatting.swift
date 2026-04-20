import Foundation

public enum PowerFormatting {
    public static func format(_ watts: Double) -> String {
        let absolute = abs(watts)
        if absolute >= 1000 {
            return String(format: "%.2f kW", absolute / 1000)
        }
        return String(format: "%.0f W", absolute)
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
