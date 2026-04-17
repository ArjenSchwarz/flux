import Foundation

/// A single row in the shared energy summary card layout, as used by the
/// History and Day Detail screens.
struct EnergySummaryRow: Equatable {
    let title: String
    let value: String
}

/// Builds the ordered list of rows rendered by the shared energy summary
/// card. Both `HistoryView` and `DayDetailView` render these rows so the
/// two screens stay in lockstep — see T-842.
enum EnergySummaryFormatter {
    static func formatKwh(_ value: Double?) -> String {
        guard let value else { return "—" }
        return String(format: "%.2f kWh", value)
    }

    static func rows(
        solar: Double?,
        gridImport: Double?,
        gridExport: Double?,
        batteryCharge: Double?,
        batteryDischarge: Double?
    ) -> [EnergySummaryRow] {
        [
            EnergySummaryRow(title: "Solar", value: formatKwh(solar)),
            EnergySummaryRow(
                title: "Grid (import/export)",
                value: "\(formatKwh(gridImport)) / \(formatKwh(gridExport))"
            ),
            EnergySummaryRow(
                title: "Battery (+/-)",
                value: "\(formatKwh(batteryCharge)) / \(formatKwh(batteryDischarge))"
            )
        ]
    }

    static func rows(for day: DayEnergy) -> [EnergySummaryRow] {
        rows(
            solar: day.epv,
            gridImport: day.eInput,
            gridExport: day.eOutput,
            batteryCharge: day.eCharge,
            batteryDischarge: day.eDischarge
        )
    }

    static func rows(for summary: DaySummary?) -> [EnergySummaryRow] {
        rows(
            solar: summary?.epv,
            gridImport: summary?.eInput,
            gridExport: summary?.eOutput,
            batteryCharge: summary?.eCharge,
            batteryDischarge: summary?.eDischarge
        )
    }
}
