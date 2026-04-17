import Foundation
import Testing
@testable import Flux

@Suite
struct EnergySummaryFormatterTests {
    // Regression test for T-842: History summary card rendered energy data
    // differently from the Day Detail summary card (five separate rows
    // vs. a Solar row plus paired Grid/Battery rows). This test locks in
    // the shared row structure so the two screens stay consistent.

    @Test
    func rowsUsePairedGridAndBatteryLayout() {
        let rows = EnergySummaryFormatter.rows(
            solar: 8.2,
            gridImport: 1.3,
            gridExport: 0.7,
            batteryCharge: 2.4,
            batteryDischarge: 3.6
        )

        #expect(rows == [
            EnergySummaryRow(title: "Solar", value: "8.20 kWh"),
            EnergySummaryRow(title: "Grid (import/export)", value: "1.30 kWh / 0.70 kWh"),
            EnergySummaryRow(title: "Battery (+/-)", value: "2.40 kWh / 3.60 kWh")
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
}
