import SwiftUI

struct PeakUsageCard: View {
    let periods: [PeakPeriod]

    private static let energyFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        formatter.maximumFractionDigits = 0
        formatter.usesGroupingSeparator = true
        return formatter
    }()

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Peak Usage")
                .font(.headline)

            ForEach(periods) { period in
                periodRow(period)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func periodRow(_ period: PeakPeriod) -> some View {
        HStack {
            Text(timeRange(period))
                .foregroundStyle(.secondary)
            Spacer()
            Text("\(String(format: "%.1f", period.avgLoadW / 1000)) kW · \(formattedEnergy(period.energyWh)) Wh")
        }
        .font(.subheadline)
    }

    private func timeRange(_ period: PeakPeriod) -> String {
        guard let startDate = DateFormatting.parseTimestamp(period.start),
              let endDate = DateFormatting.parseTimestamp(period.end)
        else {
            return "—"
        }
        return "\(DateFormatting.clockTime24h(from: startDate)) – \(DateFormatting.clockTime24h(from: endDate))"
    }

    private func formattedEnergy(_ wattHours: Double) -> String {
        Self.energyFormatter.string(from: NSNumber(value: wattHours)) ?? "\(Int(wattHours))"
    }
}

#if DEBUG
#Preview {
    PeakUsageCard(periods: [
        PeakPeriod(start: "2026-04-15T07:15:00Z", end: "2026-04-15T07:45:00Z", avgLoadW: 4200.3, energyWh: 2100),
        PeakPeriod(start: "2026-04-15T17:30:00Z", end: "2026-04-15T18:15:00Z", avgLoadW: 3800.1, energyWh: 2850),
        PeakPeriod(start: "2026-04-15T12:00:00Z", end: "2026-04-15T12:20:00Z", avgLoadW: 2900.5, energyWh: 967)
    ])
    .padding()
}
#endif
