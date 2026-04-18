import SwiftUI

struct PeakUsageCard: View {
    let periods: [PeakPeriod]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Peak Usage")
                .font(.headline)

            headerRow

            ForEach(periods) { period in
                periodRow(period)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var headerRow: some View {
        HStack {
            Text("Timespan")
            Spacer()
            Text("Average · Total")
        }
        .font(.caption)
        .foregroundStyle(.secondary)
    }

    private func periodRow(_ period: PeakPeriod) -> some View {
        let avgKW = String(format: "%.1f", period.avgLoadW / 1000)
        let totalKWh = String(format: "%.1f", period.energyWh / 1000)
        return HStack {
            Text(timeRange(period))
                .foregroundStyle(.secondary)
            Spacer()
            Text("\(avgKW) kW · \(totalKWh) kWh")
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
}

#if DEBUG
#Preview {
    PeakUsageCard(periods: [
        PeakPeriod(start: "2026-04-15T17:30:00Z", end: "2026-04-15T18:15:00Z", avgLoadW: 3800.1, energyWh: 2850),
        PeakPeriod(start: "2026-04-15T07:15:00Z", end: "2026-04-15T07:45:00Z", avgLoadW: 4200.3, energyWh: 2100),
        PeakPeriod(start: "2026-04-15T12:00:00Z", end: "2026-04-15T12:20:00Z", avgLoadW: 2900.5, energyWh: 967)
    ])
    .padding()
}
#endif
