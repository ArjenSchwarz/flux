import Charts
import FluxCore
import SwiftUI

/// Combined battery chart: SOC area in the background, charge/discharge
/// power line overlaid on top. The chart uses a single 0…100 scale for the
/// SOC area and remaps the battery power into that range so a second
/// trailing axis can label the power values in ±kW.
struct BatteryCombinedChartView: View {
    let date: String
    let readings: [ParsedReading]
    let summary: DaySummary?

    @State private var selectedDate: Date?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            statusLine

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
                    .foregroundStyle(FluxTheme.Palette.soc.opacity(0.22))
                }

                RuleMark(y: .value("Zero", powerZeroOnSOCScale))
                    .foregroundStyle(FluxTheme.Palette.tertiaryText)
                    .lineStyle(StrokeStyle(lineWidth: 0.5, dash: [3, 3]))

                RuleMark(y: .value("Cutoff", BatteryEnergy.cutoffPercent))
                    .lineStyle(StrokeStyle(lineWidth: 1, dash: [5, 3]))
                    .foregroundStyle(FluxTheme.Palette.grid.opacity(0.6))

                ForEach(readings) { reading in
                    LineMark(
                        x: .value("Time", reading.date),
                        y: .value("Power", scaledPower(reading.point.pbat))
                    )
                    .foregroundStyle(FluxTheme.Palette.battery)
                    .interpolationMethod(.monotone)
                }

                if let socLow = summary?.socLow,
                   let socLowTime = summary?.socLowTime.flatMap(DateFormatting.parseTimestamp) {
                    PointMark(
                        x: .value("Low Time", socLowTime),
                        y: .value("Low SOC", socLow)
                    )
                    .symbolSize(50)
                    .foregroundStyle(FluxTheme.Palette.battery)
                    .annotation(position: .top) {
                        Text("\(SOCFormatting.format(socLow)) at \(DateFormatting.clockTime(from: socLowTime))")
                            .appFont(.caption2)
                            .padding(4)
                            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                    }
                }

                if let selected = selectedReading {
                    RuleMark(x: .value("Selected", selected.date))
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                        .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 2]))
                    PointMark(
                        x: .value("Selected", selected.date),
                        y: .value("SOC", selected.point.soc)
                    )
                    .symbolSize(40)
                    .foregroundStyle(FluxTheme.Palette.soc)
                    PointMark(
                        x: .value("Selected", selected.date),
                        y: .value("Power", scaledPower(selected.point.pbat))
                    )
                    .symbolSize(40)
                    .foregroundStyle(FluxTheme.Palette.battery)
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
            .chartYAxis {
                AxisMarks(position: .leading, values: [0, 25, 50, 75, 100]) { value in
                    AxisGridLine()
                    AxisValueLabel {
                        if let percent = value.as(Double.self) {
                            Text("\(Int(percent))%")
                        }
                    }
                }
                AxisMarks(position: .trailing, values: [0, 25, 50, 75, 100]) { value in
                    AxisValueLabel {
                        if let scaled = value.as(Double.self) {
                            Text(powerLabel(forScaled: scaled))
                        }
                    }
                }
            }
            .chartXSelection(value: $selectedDate)
            .frame(minHeight: 240)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private var statusLine: some View {
        if let selected = selectedReading {
            Text(
                "\(DateFormatting.clockTime(from: selected.date)) · " +
                "SOC \(SOCFormatting.format(selected.point.soc)) · " +
                "\(PowerFormatting.format(selected.point.pbat)) \(direction(for: selected.point.pbat))"
            )
            .appFont(.caption)
            .foregroundStyle(.secondary)
        } else {
            Text("State of charge with charge/discharge overlay")
                .appFont(.caption)
                .foregroundStyle(.secondary)
        }
    }

    private func direction(for pbat: Double) -> String {
        if pbat < -0.01 { return "charging" }
        if pbat > 0.01 { return "discharging" }
        return "idle"
    }

    /// Half-range of the secondary axis in kW. Picks the smallest 0.5 kW
    /// step that fits the day's |pbat| peak so the power line uses the full
    /// vertical extent without going off-chart.
    private var powerDomain: Double {
        let maxAbsW = readings.map { abs($0.point.pbat) }.max() ?? 0
        let kilowatts = max(0.5, maxAbsW / 1000)
        return ceil(kilowatts * 2) / 2
    }

    /// Where the power-zero line lands on the 0-100 SOC scale. Currently
    /// pinned to 50 so charging climbs above and discharging drops below
    /// the centre of the chart.
    private var powerZeroOnSOCScale: Double { 50 }

    /// Maps a battery power reading (W, sign convention: positive = discharging)
    /// onto the SOC's 0-100 scale, with charging shown above the zero rule.
    private func scaledPower(_ pbatW: Double) -> Double {
        let kilowatts = -pbatW / 1000
        let scaled = powerZeroOnSOCScale + (kilowatts / powerDomain) * powerZeroOnSOCScale
        return min(100, max(0, scaled))
    }

    /// Inverse of `scaledPower` for axis tick labels. Formats to one decimal
    /// for kW; falls back to whole watts for very small magnitudes.
    private func powerLabel(forScaled scaled: Double) -> String {
        let kilowatts = (scaled - powerZeroOnSOCScale) / powerZeroOnSOCScale * powerDomain
        if abs(kilowatts) < 0.05 { return "0" }
        if abs(kilowatts) < 1 { return String(format: "%+.0fW", kilowatts * 1000) }
        return String(format: "%+.1f kW", kilowatts)
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
    BatteryCombinedChartView(date: day.date, readings: parsed, summary: day.summary)
        .padding()
}
#endif
