import Charts
import FluxCore
import SwiftUI

struct HistoryDailyUsageCard: View {
    static let chartKind: ChartKind = .historyDailyUsage

    let entries: [HistoryViewModel.DailyUsageEntry]
    let summary: HistoryViewModel.PeriodSummary
    let selectedDate: Date?
    let rangeDays: Int
    /// Full-period x-axis reservation (Wk/Mo). `nil` (the default, used by the
    /// expanded host which only knows a day count) leaves the chart auto-fitting.
    var chartDomain: HistoryChartDomain?
    let onSelect: (String) -> Void

    var expansionScope: ChartScope { .historyRange(days: rangeDays) }

    static let placeholderCopy = "No load breakdown available for this range."

    var body: some View {
        HistoryCardChrome(
            title: "Daily usage",
            kpi: Self.kpi(for: summary),
            subtitle: Self.subtitle(for: summary)
        ) {
            if Self.shouldShowPlaceholder(summary: summary) {
                placeholder
            } else {
                ExpandableChartContainer(
                    kind: Self.chartKind,
                    scopeProvider: { expansionScope },
                    content: {
                        chart.frame(minHeight: 180)
                    }
                )
            }
        }
    }

    static func shouldShowPlaceholder(summary: HistoryViewModel.PeriodSummary) -> Bool {
        // Display count includes today, so a today-only range still renders
        // today's bar instead of the "no breakdown" placeholder. The KPI and
        // subtitle (per-day averages) stay em-dash until a complete day exists.
        summary.dailyUsageDisplayDayCount == 0
    }

    static func kpi(for summary: HistoryViewModel.PeriodSummary) -> String {
        guard let avg = summary.dailyUsageAvgKwh else { return "—" }
        return HistoryFormatters.kwh(avg)
    }

    static func subtitle(for summary: HistoryViewModel.PeriodSummary) -> String? {
        guard let largest = summary.dailyUsageLargestKind,
              let avg = summary.dailyUsageLargestKindAvgKwh
        else { return nil }
        return "\(largest.displayLabel) largest at \(HistoryFormatters.kwh(avg))/day average"
    }

    static func opacity(for entry: HistoryViewModel.DailyUsageEntry) -> Double {
        entry.isToday ? 0.5 : 1.0
    }

    private var placeholder: some View {
        Text(Self.placeholderCopy)
            .appFont(.subheadline)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, 24)
    }

    private var chart: some View {
        let kinds = DailyUsageBlock.Kind.chronologicalOrder
        // Base the axis stride on the reserved slot count when a domain is set,
        // so a sparse to-date month still gets ~6 evenly-spaced labels.
        let axisDayCount = chartDomain?.slotDates.count ?? entries.count
        return Chart {
            HistoryChartDomain.scaffold(chartDomain?.slotDates ?? [])

            if let selectedDate {
                RuleMark(x: .value("Day", selectedDate))
                    .foregroundStyle(.gray.opacity(0.18))
                    .lineStyle(StrokeStyle(lineWidth: 12))
            }

            ForEach(entries) { entry in
                ForEach(entry.blocks, id: \.kind) { block in
                    BarMark(
                        x: .value("Day", entry.date),
                        y: .value("kWh", block.totalKwh)
                    )
                    .foregroundStyle(by: .value("Series", block.kind.displayLabel))
                    .opacity(Self.opacity(for: entry))
                }
            }
        }
        .chartForegroundStyleScale(
            domain: kinds.map(\.displayLabel),
            range: kinds.map(\.chartColor)
        )
        .historyChartXScale(chartDomain)
        .chartXAxis {
            AxisMarks(values: .stride(by: .day, count: max(1, axisDayCount / 6)))
        }
        .animation(.default, value: entries.count)
        .accessibilityElement(children: .ignore)
        .accessibilityRepresentation {
            List {
                ForEach(entries) { entry in
                    Text(entry.accessibilitySummary)
                }
            }
        }
        .historySelectionOverlay(
            entries: entries.map { ($0.dayID, $0.date) },
            onSelect: onSelect
        )
    }
}
