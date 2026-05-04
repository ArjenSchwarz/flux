import FluxCore
import Foundation

/// A single row in the shared energy summary card layout, as used by the
/// History and Day Detail screens.
struct EnergySummaryRow: Equatable, Identifiable {
    let title: String
    let value: String

    var id: String { title }
}

/// Builds the ordered list of rows rendered by the shared energy summary
/// card. Shared between `HistoryView` and `DayDetailView` so the two
/// screens stay in lockstep.
enum EnergySummaryFormatter {
    private static func formatKwh(_ value: Double?) -> String {
        guard let value else { return "—" }
        return String(format: "%.2f kWh", value)
    }

    static func rows(
        solar: Double?,
        gridImport: Double?,
        gridExport: Double?,
        batteryCharge: Double?,
        batteryDischarge: Double?,
        offpeakGridImport: Double? = nil
    ) -> [EnergySummaryRow] {
        var rows: [EnergySummaryRow] = [
            EnergySummaryRow(title: "Solar", value: formatKwh(solar))
        ]

        if let offpeak = offpeakGridImport, let total = gridImport {
            let peak = max(0, total - offpeak)
            rows.append(EnergySummaryRow(title: "Grid in (peak)", value: formatKwh(peak)))
            rows.append(EnergySummaryRow(title: "Grid in (off-peak)", value: formatKwh(offpeak)))
            rows.append(EnergySummaryRow(title: "Grid out", value: formatKwh(gridExport)))
        } else {
            rows.append(EnergySummaryRow(
                title: "Grid (import/export)",
                value: "\(formatKwh(gridImport)) / \(formatKwh(gridExport))"
            ))
        }

        rows.append(EnergySummaryRow(
            title: "Battery (+/-)",
            value: "\(formatKwh(batteryCharge)) / \(formatKwh(batteryDischarge))"
        ))

        let load = HouseholdLoad.kwh(
            solar: solar,
            gridImport: gridImport,
            gridExport: gridExport,
            batteryCharge: batteryCharge,
            batteryDischarge: batteryDischarge
        )
        rows.append(EnergySummaryRow(title: "Load", value: formatKwh(load)))
        return rows
    }

    static func rows(for day: DayEnergy) -> [EnergySummaryRow] {
        rows(
            solar: day.epv,
            gridImport: day.eInput,
            gridExport: day.eOutput,
            batteryCharge: day.eCharge,
            batteryDischarge: day.eDischarge,
            offpeakGridImport: day.offpeakGridImportKwh
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

/// Household consumption derived from the energy balance:
/// `solar + grid_import + battery_discharge - grid_export - battery_charge`.
/// All inputs must be present for a meaningful result; any missing value
/// returns `nil` so callers render an em-dash instead of a misleading total.
enum HouseholdLoad {
    static func kwh(
        solar: Double?,
        gridImport: Double?,
        gridExport: Double?,
        batteryCharge: Double?,
        batteryDischarge: Double?
    ) -> Double? {
        guard let solar, let gridImport, let gridExport,
              let batteryCharge, let batteryDischarge
        else { return nil }
        return max(0, solar + gridImport + batteryDischarge - gridExport - batteryCharge)
    }
}
