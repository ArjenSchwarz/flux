import Charts
import SwiftUI

struct BatteryPowerChartView: View {
    let date: String
    let readings: [TimeSeriesPoint]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Battery Power")
                .font(.headline)

            Chart {
                ForEach(points) { point in
                    LineMark(
                        x: .value("Time", point.date),
                        y: .value("Battery power", point.power)
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

    private var points: [BatteryPowerPoint] {
        var parsed: [BatteryPowerPoint] = []
        parsed.reserveCapacity(readings.count)

        for reading in readings {
            guard let parsedDate = DateFormatting.parseTimestamp(reading.timestamp) else {
                continue
            }

            parsed.append(BatteryPowerPoint(id: reading.id, date: parsedDate, power: -reading.pbat))
        }

        return parsed
    }

    private var xDomain: ClosedRange<Date> {
        DayChartDomain.domain(for: date)
    }
}

private struct BatteryPowerPoint: Identifiable {
    let id: String
    let date: Date
    let power: Double
}

#Preview {
    let day = MockFluxAPIClient.dayDetailResponse()
    BatteryPowerChartView(date: day.date, readings: day.readings)
        .padding()
}
