import Charts
import SwiftUI

struct PowerChartView: View {
    let date: String
    let readings: [ParsedReading]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Power Flows")
                .font(.headline)

            Chart {
                ForEach(readings) { reading in
                    AreaMark(
                        x: .value("Time", reading.date),
                        yStart: .value("Power", 0),
                        yEnd: .value("Power", reading.point.ppv)
                    )
                    .foregroundStyle(.green.opacity(0.25))

                    LineMark(
                        x: .value("Time", reading.date),
                        y: .value("Load", reading.point.pload)
                    )
                    .foregroundStyle(.primary)

                    if reading.point.pgrid >= 0 {
                        LineMark(
                            x: .value("Time", reading.date),
                            y: .value("Grid import", reading.point.pgrid)
                        )
                        .foregroundStyle(.red)
                    } else {
                        LineMark(
                            x: .value("Time", reading.date),
                            y: .value("Grid export", abs(reading.point.pgrid))
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
