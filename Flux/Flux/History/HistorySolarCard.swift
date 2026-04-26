import Charts
import SwiftUI

struct HistorySolarCard: View {
    let entries: [HistoryViewModel.SolarEntry]
    let summary: HistoryViewModel.PeriodSummary
    let selectedDate: Date?
    let onSelect: (String) -> Void

    var body: some View {
        HistoryCardChrome(
            title: "Solar",
            kpi: HistoryFormatters.kwh(summary.solarTotalKwh),
            subtitle: subtitle
        ) {
            chart
                .frame(minHeight: 160)
        }
    }

    private var subtitle: String {
        guard let perDay = summary.solarPerDayKwh else {
            return "No completed days yet"
        }
        return "\(HistoryFormatters.kwh(perDay))/day average"
    }

    @ViewBuilder
    private var chart: some View {
        Chart {
            if let selectedDate {
                RuleMark(x: .value("Day", selectedDate))
                    .foregroundStyle(.gray.opacity(0.18))
                    .lineStyle(StrokeStyle(lineWidth: 12))
            }

            ForEach(entries) { entry in
                BarMark(
                    x: .value("Day", entry.date),
                    y: .value("kWh", entry.kwh)
                )
                .foregroundStyle(Color.green)
                .opacity(entry.isToday ? 0.5 : 1.0)
            }

            if let perDay = summary.solarPerDayKwh {
                RuleMark(y: .value("Avg", perDay))
                    .foregroundStyle(.green.opacity(0.5))
                    .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 3]))
            }
        }
        .historySelectionOverlay(
            entries: entries.map { ($0.dayID, $0.date) },
            onSelect: onSelect
        )
    }
}
