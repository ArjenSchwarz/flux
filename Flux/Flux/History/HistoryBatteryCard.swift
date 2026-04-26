import Charts
import SwiftUI

struct HistoryBatteryCard: View {
    let entries: [HistoryViewModel.BatteryEntry]
    let summary: HistoryViewModel.PeriodSummary
    let selectedDayID: String?
    let onSelect: (String) -> Void

    var body: some View {
        HistoryCardChrome(
            title: "Battery",
            kpi: HistoryFormatters.kwh(summary.dischargeTotalKwh) + " discharged",
            subtitle: subtitle
        ) {
            chart.frame(minHeight: 160)
        }
    }

    private var subtitle: String {
        let charged = HistoryFormatters.kwh(summary.chargeTotalKwh)
        guard let perDay = summary.dischargePerDayKwh else {
            return "\(charged) charged"
        }
        return "\(charged) charged · \(HistoryFormatters.kwh(perDay))/day discharged"
    }

    @ViewBuilder
    private var chart: some View {
        Chart {
            if let selectedDayID,
               let selected = entries.first(where: { $0.dayID == selectedDayID }) {
                RuleMark(x: .value("Day", selected.date))
                    .foregroundStyle(.gray.opacity(0.18))
                    .lineStyle(StrokeStyle(lineWidth: 12))
            }

            ForEach(entries) { entry in
                BarMark(
                    x: .value("Day", entry.date),
                    y: .value("kWh", entry.chargeKwh)
                )
                .foregroundStyle(by: .value("Series", "Charged"))
                .opacity(entry.isToday ? 0.5 : 1.0)

                BarMark(
                    x: .value("Day", entry.date),
                    y: .value("kWh", -entry.dischargeKwh)
                )
                .foregroundStyle(by: .value("Series", "Discharged"))
                .opacity(entry.isToday ? 0.5 : 1.0)
            }

            RuleMark(y: .value("Zero", 0))
                .foregroundStyle(.secondary.opacity(0.4))
                .lineStyle(StrokeStyle(lineWidth: 0.5))
        }
        .chartForegroundStyleScale([
            "Charged": Color.orange,
            "Discharged": Color.purple
        ])
        .historySelectionOverlay(
            entries: entries.map { ($0.dayID, $0.date) },
            onSelect: onSelect
        )
    }
}
