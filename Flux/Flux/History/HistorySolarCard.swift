import Charts
import FluxCore
import SwiftUI

struct HistorySolarCard: View {
    static let chartKind: ChartKind = .historySolar

    let entries: [HistoryViewModel.SolarEntry]
    let summary: HistoryViewModel.PeriodSummary
    let selectedDate: Date?
    /// The rendered window, passed by `HistoryView` from the view model's
    /// resolved query so the enlarged chart fetches the same period —
    /// including a navigated past one (Decision 13).
    let periodQuery: HistoryQuery
    /// Full-period x-axis reservation (Wk/Mo). `nil` (the default, used by the
    /// expanded host which only knows a day count) leaves the chart auto-fitting.
    var chartDomain: HistoryChartDomain?
    let onSelect: (String) -> Void

    var expansionScope: ChartScope { .historyRange(periodQuery) }

    var body: some View {
        HistoryCardChrome(
            title: "Solar",
            kpi: HistoryFormatters.kwh(summary.solarTotalKwh),
            subtitle: subtitle
        ) {
            ExpandableChartContainer(
                kind: Self.chartKind,
                scopeProvider: { expansionScope },
                content: {
                    chart.frame(minHeight: 160)
                }
            )
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
            HistoryChartDomain.scaffold(chartDomain?.slotDates ?? [])

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
        .historyChartXScale(chartDomain)
        .historySelectionOverlay(
            entries: entries.map { ($0.dayID, $0.date) },
            onSelect: onSelect
        )
    }
}
