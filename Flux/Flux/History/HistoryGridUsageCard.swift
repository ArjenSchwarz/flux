import Charts
import SwiftUI

struct HistoryGridUsageCard: View {
    let entries: [HistoryViewModel.GridEntry]
    let summary: HistoryViewModel.PeriodSummary
    let selectedDayID: String?
    let onSelect: (String) -> Void

    var body: some View {
        HistoryCardChrome(
            title: "Grid usage",
            kpi: HistoryFormatters.kwh(summary.peakImportTotalKwh) + " peak",
            subtitle: subtitle
        ) {
            if entries.isEmpty {
                placeholder
            } else {
                chart.frame(minHeight: 180)
            }
        }
    }

    private var subtitle: String {
        let offpeak = HistoryFormatters.kwh(summary.offpeakImportTotalKwh)
        let exported = HistoryFormatters.kwh(summary.exportTotalKwh)
        return "\(offpeak) off-peak · \(exported) exported"
    }

    private var placeholder: some View {
        Text("No off-peak data yet for this range.")
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, 24)
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
                    y: .value("kWh", entry.offpeakImportKwh)
                )
                .foregroundStyle(by: .value("Series", "Off-peak import"))
                .opacity(entry.isToday ? 0.5 : 1.0)

                BarMark(
                    x: .value("Day", entry.date),
                    y: .value("kWh", entry.peakImportKwh)
                )
                .foregroundStyle(by: .value("Series", "Peak import"))
                .opacity(entry.isToday ? 0.5 : 1.0)

                BarMark(
                    x: .value("Day", entry.date),
                    y: .value("kWh", -entry.exportKwh)
                )
                .foregroundStyle(by: .value("Series", "Export"))
                .opacity(entry.isToday ? 0.5 : 1.0)
            }

            RuleMark(y: .value("Zero", 0))
                .foregroundStyle(.secondary.opacity(0.4))
                .lineStyle(StrokeStyle(lineWidth: 0.5))
        }
        .chartForegroundStyleScale([
            "Off-peak import": Color.teal,
            "Peak import": Color.red,
            "Export": Color.blue
        ])
        .historySelectionOverlay(
            entries: entries.map { ($0.dayID, $0.date) },
            onSelect: onSelect
        )
    }
}
