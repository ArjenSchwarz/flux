import SwiftUI

struct BatteryHeroView: View {
    let live: LiveData?
    let battery: BatteryInfo?

    var body: some View {
        let soc = live?.soc ?? 0
        let batteryColor = BatteryColor.forSOC(soc).color

        VStack(spacing: 12) {
            Text("\(soc, specifier: "%.1f")%")
                .font(.system(size: 56, weight: .bold, design: .rounded))
                .foregroundStyle(batteryColor)
                .frame(maxWidth: .infinity, alignment: .center)

            Text(statusLine)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .center)

            ProgressView(value: max(0, min(100, soc)), total: 100)
                .tint(batteryColor)
        }
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var statusLine: String {
        guard let live else { return "No live data" }

        let rate = PowerFormatting.format(live.pbat)

        if live.soc >= 100, live.pbat < 0 {
            return "Full"
        }

        if live.pbat > 0 {
            if let cutoff = battery?.estimatedCutoffTime.flatMap(DateFormatting.parseTimestamp) {
                return "Discharging at \(rate) · cutoff ~\(DateFormatting.clockTime(from: cutoff))"
            }
            return "Discharging at \(rate)"
        }

        if live.pbat < 0 {
            return "Charging at \(rate)"
        }

        return "Idle"
    }

}

#if DEBUG
#Preview {
    let status = MockFluxAPIClient.statusResponse
    BatteryHeroView(
        live: status.live,
        battery: status.battery
    )
    .padding()
}
#endif
