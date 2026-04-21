import Charts
import FluxCore
import SwiftUI

struct PowerChartView: View {
    let date: String
    let readings: [ParsedReading]

    @State private var selectedDate: Date?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Power Flows")
                .font(.headline)

            if let selected = selectedReading {
                HStack(spacing: 12) {
                    Text(DateFormatting.clockTime(from: selected.date))
                    Text("Solar: \(PowerFormatting.format(selected.point.ppv))")
                        .foregroundStyle(.green)
                    Text("Load: \(PowerFormatting.format(selected.point.pload))")
                    Text("Grid: \(PowerFormatting.format(selected.point.pgrid))")
                        .foregroundStyle(.red)
                }
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
                        yStart: .value("Power", 0),
                        yEnd: .value("Power", reading.point.ppv)
                    )
                    .foregroundStyle(by: .value("Series", "Solar"))

                    LineMark(
                        x: .value("Time", reading.date),
                        y: .value("Power", reading.point.pload)
                    )
                    .foregroundStyle(by: .value("Series", "Load"))

                    LineMark(
                        x: .value("Time", reading.date),
                        y: .value("Power", reading.point.pgrid)
                    )
                    .foregroundStyle(by: .value("Series", "Grid"))
                }

                if let selected = selectedReading {
                    RuleMark(x: .value("Selected", selected.date))
                        .foregroundStyle(.secondary.opacity(0.5))
                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 2]))

                    PointMark(x: .value("S", selected.date), y: .value("P", selected.point.ppv))
                        .symbolSize(40).foregroundStyle(.green)
                    PointMark(x: .value("S", selected.date), y: .value("P", selected.point.pload))
                        .symbolSize(40).foregroundStyle(.primary)
                    PointMark(x: .value("S", selected.date), y: .value("P", selected.point.pgrid))
                        .symbolSize(40).foregroundStyle(.red)
                }
            }
            .chartForegroundStyleScale([
                "Solar": Color.green.opacity(0.25),
                "Load": Color.primary,
                "Grid": Color.red
            ])
            .chartXScale(domain: xDomain)
            .chartXAxis {
                AxisMarks(values: .stride(by: .hour, count: 3)) {
                    AxisGridLine()
                    AxisValueLabel(format: .dateTime.hour())
                }
            }
            .chartYAxis {
                AxisMarks { value in
                    AxisGridLine()
                    AxisValueLabel {
                        if let watts = value.as(Double.self) {
                            Text(PowerFormatting.formatAxis(watts))
                        }
                    }
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
    PowerChartView(date: day.date, readings: parsed)
        .padding()
}
#endif
