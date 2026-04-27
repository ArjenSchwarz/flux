import FluxCore
import Foundation
import Testing
@testable import Flux

@Suite
struct EnergySummaryFormatterTests {
    // Locks in the shared row structure (Solar row plus paired Grid and
    // Battery rows) so the History and Day Detail summary cards stay
    // consistent.

    @Test
    func rowsUsePairedGridAndBatteryLayout() {
        let rows = EnergySummaryFormatter.rows(
            solar: 8.2,
            gridImport: 1.3,
            gridExport: 0.7,
            batteryCharge: 2.4,
            batteryDischarge: 3.6
        )

        // Load = 8.2 + 1.3 + 3.6 - 0.7 - 2.4 = 10.0
        #expect(rows == [
            EnergySummaryRow(title: "Solar", value: "8.20 kWh"),
            EnergySummaryRow(title: "Grid (import/export)", value: "1.30 kWh / 0.70 kWh"),
            EnergySummaryRow(title: "Battery (+/-)", value: "2.40 kWh / 3.60 kWh"),
            EnergySummaryRow(title: "Load", value: "10.00 kWh")
        ])
    }

    @Test
    func missingValuesRenderAsEmDash() {
        let rows = EnergySummaryFormatter.rows(
            solar: nil,
            gridImport: nil,
            gridExport: 0.4,
            batteryCharge: nil,
            batteryDischarge: nil
        )

        #expect(rows[0] == EnergySummaryRow(title: "Solar", value: "—"))
        #expect(rows[1].value == "— / 0.40 kWh")
        #expect(rows[2].value == "— / —")
        #expect(rows[3] == EnergySummaryRow(title: "Load", value: "—"))
    }

    @Test
    func dayEnergyProducesSameRowsAsEquivalentDaySummary() {
        // HistoryView feeds DayEnergy; DayDetailView feeds DaySummary. Both
        // must produce the same rendered rows for the same underlying values
        // so the two screens show the same layout and values.
        let day = DayEnergy(
            date: "2026-04-15",
            epv: 6.5,
            eInput: 2.1,
            eOutput: 0.3,
            eCharge: 1.8,
            eDischarge: 2.7
        )
        let summary = DaySummary(
            epv: 6.5,
            eInput: 2.1,
            eOutput: 0.3,
            eCharge: 1.8,
            eDischarge: 2.7,
            socLow: nil,
            socLowTime: nil
        )

        let historyRows = EnergySummaryFormatter.rows(for: day)
        let dayDetailRows = EnergySummaryFormatter.rows(for: summary)

        #expect(historyRows == dayDetailRows)
    }

    @Test
    func nilSummaryRendersAllEmDashes() {
        let rows = EnergySummaryFormatter.rows(for: nil as DaySummary?)

        #expect(rows == [
            EnergySummaryRow(title: "Solar", value: "—"),
            EnergySummaryRow(title: "Grid (import/export)", value: "— / —"),
            EnergySummaryRow(title: "Battery (+/-)", value: "— / —"),
            EnergySummaryRow(title: "Load", value: "—")
        ])
    }

    @Test
    func loadIsDerivedFromEnergyBalance() {
        // Energy balance: load = solar + import + discharge - export - charge
        let rows = EnergySummaryFormatter.rows(
            solar: 5.0,
            gridImport: 4.0,
            gridExport: 1.0,
            batteryCharge: 2.0,
            batteryDischarge: 3.0
        )

        // 5 + 4 + 3 - 1 - 2 = 9
        #expect(rows.last == EnergySummaryRow(title: "Load", value: "9.00 kWh"))
    }

    @Test
    func householdLoadClampsToZeroWhenBalanceIsNegative() {
        // Reporting/rounding quirks can transiently push the balance
        // negative; clamp to 0 rather than render a misleading negative load.
        let result = HouseholdLoad.kwh(
            solar: 1.0,
            gridImport: 0.0,
            gridExport: 2.0,
            batteryCharge: 0.0,
            batteryDischarge: 0.0
        )

        #expect(result == 0.0)
    }

    @Test
    func householdLoadReturnsNilWhenAnyInputMissing() {
        // Direct contract test for nil propagation — survives any future
        // refactor of EnergySummaryFormatter that bypasses or replaces it.
        #expect(HouseholdLoad.kwh(
            solar: nil, gridImport: 1, gridExport: 0,
            batteryCharge: 0, batteryDischarge: 0
        ) == nil)
        #expect(HouseholdLoad.kwh(
            solar: 1, gridImport: nil, gridExport: 0,
            batteryCharge: 0, batteryDischarge: 0
        ) == nil)
        #expect(HouseholdLoad.kwh(
            solar: 1, gridImport: 0, gridExport: nil,
            batteryCharge: 0, batteryDischarge: 0
        ) == nil)
        #expect(HouseholdLoad.kwh(
            solar: 1, gridImport: 0, gridExport: 0,
            batteryCharge: nil, batteryDischarge: 0
        ) == nil)
        #expect(HouseholdLoad.kwh(
            solar: 1, gridImport: 0, gridExport: 0,
            batteryCharge: 0, batteryDischarge: nil
        ) == nil)
    }
}
