import Foundation

enum PowerFormatting {
    static func format(_ watts: Double) -> String {
        let absolute = abs(watts)
        if absolute >= 1000 {
            return String(format: "%.2f kW", absolute / 1000)
        }
        return String(format: "%.0f W", absolute)
    }

    static func formatAxis(_ watts: Double) -> String {
        let absolute = abs(watts)
        if absolute >= 1000 {
            let kw = watts / 1000
            if kw == kw.rounded() {
                return String(format: "%.0f kW", kw)
            }
            return String(format: "%.1f kW", kw)
        }
        return String(format: "%.0f W", watts)
    }
}
