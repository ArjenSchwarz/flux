import Charts
import FluxCore
import SwiftUI

struct HistoryDailyUsageCard: View {
    let entries: [HistoryViewModel.DailyUsageEntry]
    let summary: HistoryViewModel.PeriodSummary
    let selectedDate: Date?
    let onSelect: (String) -> Void

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
                chart.frame(minHeight: 180)
            }
        }
    }

    static func shouldShowPlaceholder(summary: HistoryViewModel.PeriodSummary) -> Bool {
        summary.dailyUsageDayCount == 0
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
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, 24)
    }

    private var chart: some View {
        let kinds = DailyUsageBlock.Kind.chronologicalOrder
        return Chart {
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
        .chartXAxis {
            AxisMarks(values: .stride(by: .day, count: max(1, entries.count / 6)))
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
