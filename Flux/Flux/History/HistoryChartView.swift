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
                    RectangleMark(x: .value("Date", selectedChartDay.date))
                        .foregroundStyle(.gray.opacity(0.12))
                }

                ForEach(chartEntries) { entry in
                    BarMark(
                        x: .value("Date", entry.date),
                        y: .value("kWh", entry.value)
                    )
                    .foregroundStyle(by: .value("Metric", entry.metric.label))
                    .position(by: .value("Metric", entry.metric.label))
                    .opacity(entry.isToday ? 0.5 : 1.0)
                }
            }
            .chartForegroundStyleScale(metricColors)
            .chartOverlay { proxy in
                GeometryReader { geometry in
                    Rectangle()
                        .fill(.clear)
                        .contentShape(Rectangle())
                        .gesture(
                            DragGesture(minimumDistance: 0)
                                .onChanged { value in
                                    guard let date = dateFromOverlayLocation(value.location.x, proxy: proxy, geometry: geometry),
                                          let nearestDay = nearestDay(to: date)
                                    else {
                                        return
                                    }

                                    onSelectDay(nearestDay.day)
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

    private func dateFromOverlayLocation(
        _ xLocation: CGFloat,
        proxy: ChartProxy,
        geometry: GeometryProxy
    ) -> Date? {
        guard let plotFrameAnchor = proxy.plotFrame else {
            return nil
        }
        let plotFrame = geometry[plotFrameAnchor]
        let relativeX = xLocation - plotFrame.origin.x
        guard relativeX >= 0, relativeX <= proxy.plotSize.width else {
            return nil
        }
        return proxy.value(atX: relativeX)
    }

    private func nearestDay(to date: Date) -> HistoryViewModel.HistoryChartDay? {
        chartDays.min {
            abs($0.date.timeIntervalSince(date)) < abs($1.date.timeIntervalSince(date))
        }
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
