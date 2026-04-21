import Charts
import FluxCore
import SwiftUI

struct SOCChartView: View {
    let date: String
    let readings: [ParsedReading]
    let summary: DaySummary?

    @State private var selectedDate: Date?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Battery %")
                .font(.headline)

            if let selected = selectedReading {
                Text("\(DateFormatting.clockTime(from: selected.date)): \(SOCFormatting.format(selected.point.soc))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Chart {
                if let offpeak = DayChartDomain.offpeakRange(for: date) {
                    RectangleMark(
                        xStart: .value("Start", offpeak.start),
                        xEnd: .value("End", offpeak.end)
                    )
                    .foregroundStyle(.yellow.opacity(0.1))
                }

                ForEach(readings) { reading in
                    AreaMark(
                        x: .value("Time", reading.date),
                        y: .value("SOC", reading.point.soc)
                    )
                    .foregroundStyle(.blue.opacity(0.3))
                }

                RuleMark(y: .value("Cutoff", 10))
                    .lineStyle(StrokeStyle(lineWidth: 1, dash: [5, 3]))
                    .foregroundStyle(.red.opacity(0.6))

                if let selected = selectedReading {
                    RuleMark(x: .value("Selected", selected.date))
                        .foregroundStyle(.secondary.opacity(0.5))
                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 2]))

                    PointMark(
                        x: .value("Selected", selected.date),
                        y: .value("SOC", selected.point.soc)
                    )
                    .symbolSize(40)
                    .foregroundStyle(.blue)
                }

                if let socLow = summary?.socLow,
                   let socLowTime = summary?.socLowTime.flatMap(DateFormatting.parseTimestamp) {
                    PointMark(
                        x: .value("Low Time", socLowTime),
                        y: .value("Low SOC", socLow)
                    )
                    .symbolSize(50)
                    .foregroundStyle(.purple)
                    .annotation(position: .top) {
                        Text("\(SOCFormatting.format(socLow)) at \(DateFormatting.clockTime(from: socLowTime))")
                            .font(.caption2)
                            .padding(4)
                            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                    }
                }
            }
            .chartYScale(domain: 0 ... 100)
            .chartXScale(domain: xDomain)
            .chartXAxis {
                AxisMarks(values: .stride(by: .hour, count: 3)) {
                    AxisGridLine()
                    AxisValueLabel(format: .dateTime.hour())
                }
            }
            .chartXSelection(value: $selectedDate)
            .frame(minHeight: 220)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var selectedReading: ParsedReading? {
        guard let selectedDate else { return nil }
        return readings.nearestReading(to: selectedDate)
    }

    private var xDomain: ClosedRange<Date> {
        DayChartDomain.domain(for: date)
    }
}

#if DEBUG
#Preview {
    let day = MockFluxAPIClient.dayDetailResponse()
    let parsed = day.readings.compactMap { reading -> ParsedReading? in
        guard let date = DateFormatting.parseTimestamp(reading.timestamp) else { return nil }
        return ParsedReading(id: reading.id, date: date, point: reading)
    }
    SOCChartView(date: day.date, readings: parsed, summary: day.summary)
        .padding()
}
#endif
