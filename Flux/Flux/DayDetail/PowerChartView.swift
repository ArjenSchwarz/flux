import Charts
import SwiftUI

struct PowerChartView: View {
    let date: String
    let readings: [TimeSeriesPoint]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Power Flows")
                .font(.headline)

            Chart {
                ForEach(points) { point in
                    AreaMark(
                        x: .value("Time", point.date),
                        yStart: .value("Power", 0),
                        yEnd: .value("Power", point.solar)
                    )
                    .foregroundStyle(.green.opacity(0.25))

                    LineMark(
                        x: .value("Time", point.date),
                        y: .value("Load", point.load)
                    )
                    .foregroundStyle(.primary)

                    if point.grid >= 0 {
                        LineMark(
                            x: .value("Time", point.date),
                            y: .value("Grid import", point.grid)
                        )
                        .foregroundStyle(.red)
                    } else {
                        LineMark(
                            x: .value("Time", point.date),
                            y: .value("Grid export", abs(point.grid))
                        )
                        .foregroundStyle(.blue)
                    }
                }
            }
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

    private var points: [PowerPoint] {
        var parsed: [PowerPoint] = []
        parsed.reserveCapacity(readings.count)

        for reading in readings {
            guard let parsedDate = DateFormatting.parseTimestamp(reading.timestamp) else {
                continue
            }
            parsed.append(
                PowerPoint(
                    id: reading.id,
                    date: parsedDate,
                    solar: reading.ppv,
                    load: reading.pload,
                    grid: reading.pgrid
                )
            )
        }

        return parsed
    }

    private var xDomain: ClosedRange<Date> {
        DayChartDomain.domain(for: date)
    }
}

private struct PowerPoint: Identifiable {
    let id: String
    let date: Date
    let solar: Double
    let load: Double
    let grid: Double
}
