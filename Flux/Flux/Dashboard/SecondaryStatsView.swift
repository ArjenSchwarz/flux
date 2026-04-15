import SwiftUI

struct SecondaryStatsView: View {
    let battery: BatteryInfo?
    let rolling15min: RollingAvg?
    let offpeak: OffpeakData?
    let nowProvider: () -> Date

    init(
        battery: BatteryInfo?,
        rolling15min: RollingAvg?,
        offpeak: OffpeakData?,
        nowProvider: @escaping () -> Date = { .now }
    ) {
        self.battery = battery
        self.rolling15min = rolling15min
        self.offpeak = offpeak
        self.nowProvider = nowProvider
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Secondary Stats")
                .font(.headline)

            statRow(title: "24h low", value: low24hText)
            statRow(title: "Off-peak grid", value: offpeakGridText)
            statRow(title: "Off-peak Δ battery", value: offpeakDeltaText)
            rollingLoadRow
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var low24hText: String {
        guard let low24h = battery?.low24h else { return "—" }
        let timeText = DateFormatting.parseTimestamp(low24h.timestamp).map(DateFormatting.clockTime(from:)) ?? "—"
        return "\(String(format: "%.1f", low24h.soc))% at \(timeText)"
    }

    private var offpeakGridText: String {
        guard let value = offpeak?.gridUsageKwh else { return "—" }
        return "\(String(format: "%.2f", value)) kWh"
    }

    private var offpeakDeltaText: String {
        guard let value = offpeak?.batteryDeltaPercent else { return "—" }
        return "\(String(format: "%+.1f", value))%"
    }

    @ViewBuilder
    private var rollingLoadRow: some View {
        HStack {
            Text("15m avg load")
                .foregroundStyle(.secondary)

            Spacer()

            if let rolling15min {
                Text("\(Int(rolling15min.avgLoad.rounded()))W")
                if let estimatedCutoffTime = rolling15min.estimatedCutoffTime,
                   let cutoffDate = DateFormatting.parseTimestamp(estimatedCutoffTime)
                {
                    let cutoffColor = CutoffTimeColor.forCutoff(
                        cutoffDate,
                        offpeakWindowStart: offpeak?.windowStart ?? "11:00",
                        now: nowProvider()
                    ).color
                    Text("(~\(DateFormatting.clockTime(from: cutoffDate)))")
                        .foregroundStyle(cutoffColor)
                }
            } else {
                Text("—")
            }
        }
        .font(.subheadline)
    }

    private func statRow(title: String, value: String) -> some View {
        HStack {
            Text(title)
                .foregroundStyle(.secondary)
            Spacer()
            Text(value)
        }
        .font(.subheadline)
    }
}

#if DEBUG
#Preview {
    let status = MockFluxAPIClient.statusResponse
    SecondaryStatsView(
        battery: status.battery,
        rolling15min: status.rolling15min,
        offpeak: status.offpeak
    )
    .padding()
}
#endif
