import Charts
import SwiftUI

struct SOCChartView: View {
    let date: String
    let readings: [TimeSeriesPoint]
    let summary: DaySummary?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Battery SOC")
                .font(.headline)

            Chart {
                ForEach(points) { point in
                    AreaMark(
                        x: .value("Time", point.date),
                        y: .value("SOC", point.soc)
                    )
                    .foregroundStyle(.blue.opacity(0.3))
                }

                RuleMark(y: .value("Cutoff", 10))
                    .lineStyle(StrokeStyle(lineWidth: 1, dash: [5, 3]))
                    .foregroundStyle(.red.opacity(0.6))

                if let socLow = summary?.socLow,
                   let socLowTime = summary?.socLowTime.flatMap(DateFormatting.parseTimestamp)
                {
                    PointMark(
                        x: .value("Low Time", socLowTime),
                        y: .value("Low SOC", socLow)
                    )
                    .symbolSize(50)
                    .foregroundStyle(.purple)
                    .annotation(position: .top) {
                        Text("\(socLow, specifier: "%.1f")%")
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
            .frame(minHeight: 220)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var points: [SOCPoint] {
        var parsed: [SOCPoint] = []
        parsed.reserveCapacity(readings.count)

        for reading in readings {
            guard let parsedDate = DateFormatting.parseTimestamp(reading.timestamp) else {
                continue
            }

            parsed.append(SOCPoint(id: reading.id, date: parsedDate, soc: reading.soc))
        }

        return parsed
    }

    private var xDomain: ClosedRange<Date> {
        DayChartDomain.domain(for: date)
    }
}

private struct SOCPoint: Identifiable {
    let id: String
    let date: Date
    let soc: Double
}

#Preview {
    let day = MockFluxAPIClient.dayDetailResponse()
    SOCChartView(date: day.date, readings: day.readings, summary: day.summary)
        .padding()
}
