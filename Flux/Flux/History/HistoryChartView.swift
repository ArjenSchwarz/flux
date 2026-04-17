import Charts
import SwiftUI

struct HistoryChartView: View {
    @Binding var selectedRange: Int

    let chartDays: [HistoryViewModel.HistoryChartDay]
    let chartEntries: [HistoryViewModel.HistoryChartEntry]
    let selectedDay: DayEnergy?
    let onSelectDay: (DayEnergy) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("Range", selection: $selectedRange) {
                Text("7d").tag(7)
                Text("14d").tag(14)
                Text("30d").tag(30)
            }
            .pickerStyle(.segmented)

            Chart {
                if let selectedChartDay = selectedChartDay {
                    RectangleMark(x: .value("Date", selectedChartDay.day.date))
                        .foregroundStyle(.gray.opacity(0.12))
                }

                ForEach(chartEntries) { entry in
                    BarMark(
                        x: .value("Date", entry.dayID),
                        y: .value("kWh", entry.value)
                    )
                    .foregroundStyle(by: .value("Metric", entry.metric.label))
                    .position(by: .value("Metric", entry.metric.label))
                    .opacity(entry.isToday ? 0.5 : 1.0)
                }
            }
            .chartForegroundStyleScale(metricColors)
            .chartXAxis {
                AxisMarks(values: .automatic) { value in
                    AxisGridLine()
                    AxisTick()
                    if let dateString = value.as(String.self) {
                        AxisValueLabel(axisLabel(for: dateString))
                    }
                }
            }
            .chartOverlay { proxy in
                GeometryReader { geometry in
                    Rectangle()
                        .fill(.clear)
                        .contentShape(Rectangle())
                        .gesture(
                            DragGesture(minimumDistance: 0)
                                .onChanged { value in
                                    guard let dayID = dayIDFromOverlayLocation(
                                        value.location.x,
                                        proxy: proxy,
                                        geometry: geometry
                                    ),
                                    let matchingDay = chartDays.first(where: { $0.day.date == dayID })
                                    else {
                                        return
                                    }

                                    onSelectDay(matchingDay.day)
                                }
                        )
                }
            }
            .frame(minHeight: 260)
        }
    }

    private var selectedChartDay: HistoryViewModel.HistoryChartDay? {
        guard let selectedDay else { return nil }
        return chartDays.first { $0.day.date == selectedDay.date }
    }

    private var metricColors: KeyValuePairs<String, Color> {
        [
            HistoryViewModel.HistoryChartMetric.solar.label: .green,
            HistoryViewModel.HistoryChartMetric.gridImported.label: .red,
            HistoryViewModel.HistoryChartMetric.gridExported.label: .blue,
            HistoryViewModel.HistoryChartMetric.charged.label: .orange,
            HistoryViewModel.HistoryChartMetric.discharged.label: .purple
        ]
    }

    // Maps a touch location to the day ID of the bar group under that
    // x coordinate. Uses a discrete (String) axis because the chart groups
    // bars per metric via .position(by:), which requires a categorical
    // x-axis to allocate visible slot widths.
    private func dayIDFromOverlayLocation(
        _ xLocation: CGFloat,
        proxy: ChartProxy,
        geometry: GeometryProxy
    ) -> String? {
        guard let plotFrameAnchor = proxy.plotFrame else {
            return nil
        }
        let plotFrame = geometry[plotFrameAnchor]
        let relativeX = xLocation - plotFrame.origin.x
        guard relativeX >= 0, relativeX <= proxy.plotSize.width else {
            return nil
        }
        return proxy.value(atX: relativeX, as: String.self)
    }

    // Renders a "M/d" label from a YYYY-MM-DD day ID so the x-axis stays
    // readable when the full range is plotted.
    private func axisLabel(for dayID: String) -> String {
        let parts = dayID.split(separator: "-")
        guard parts.count == 3,
              let month = Int(parts[1]),
              let day = Int(parts[2]) else {
            return dayID
        }
        return "\(month)/\(day)"
    }
}

#Preview {
    @Previewable @State var selectedRange = 7
    HistoryChartView(
        selectedRange: $selectedRange,
        chartDays: [],
        chartEntries: [],
        selectedDay: nil,
        onSelectDay: { _ in }
    )
    .padding()
}
