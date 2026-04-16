import Charts
import SwiftUI

struct BatteryPowerChartView: View {
    let date: String
    let readings: [ParsedReading]

    @State private var selectedDate: Date?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Battery Load")
                .font(.headline)

            if let selected = selectedReading {
                let pbat = -selected.point.pbat
                let label = pbat > 0 ? "charging" : pbat < 0 ? "discharging" : "idle"
                let time = DateFormatting.clockTime(from: selected.date)
                let power = PowerFormatting.format(selected.point.pbat)
                Text("\(time): \(power) (\(label))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                Text("Above zero = charging, below = discharging")
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
                    LineMark(
                        x: .value("Time", reading.date),
                        y: .value("Power", -reading.point.pbat)
                    )
                    .foregroundStyle(.purple)
                }

                RuleMark(y: .value("Zero", 0))
                    .foregroundStyle(.secondary)

                if let selected = selectedReading {
                    RuleMark(x: .value("Selected", selected.date))
                        .foregroundStyle(.secondary.opacity(0.5))
                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 2]))

                    PointMark(
                        x: .value("Selected", selected.date),
                        y: .value("Power", -selected.point.pbat)
                    )
                    .symbolSize(40)
                    .foregroundStyle(.purple)
                }
            }
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
    BatteryPowerChartView(date: day.date, readings: parsed)
        .padding()
}
#endif
