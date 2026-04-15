import Charts
import SwiftUI

struct BatteryPowerChartView: View {
    let date: String
    let readings: [ParsedReading]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Battery Power")
                .font(.headline)

            Chart {
                ForEach(readings) { reading in
                    LineMark(
                        x: .value("Time", reading.date),
                        y: .value("Battery power", -reading.point.pbat)
                    )
                    .foregroundStyle(.purple)
                }

                RuleMark(y: .value("Zero", 0))
                    .foregroundStyle(.secondary)
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
    BatteryPowerChartView(date: day.date, readings: parsed)
        .padding()
}
#endif
