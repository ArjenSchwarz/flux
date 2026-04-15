import SwiftUI

struct BatteryHeroView: View {
    let live: LiveData?
    let battery: BatteryInfo?

    var body: some View {
        let soc = live?.soc ?? 0
        let batteryColor = BatteryColor.forSOC(soc)

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

        if live.soc >= 100, live.pbat < 0 {
            return "Full"
        }

        if live.pbat > 0 {
            if let cutoff = battery?.estimatedCutoffTime.flatMap(DateFormatting.parseTimestamp) {
                return "Discharging · cutoff ~\(DateFormatting.clockTime(from: cutoff))"
            }
            return "Discharging"
        }

        if live.pbat < 0 {
            return "Charging"
        }

        return "Idle"
    }
}

#Preview {
    BatteryHeroView(
        live: LiveData(
            ppv: 2500,
            pload: 900,
            pbat: 800,
            pgrid: 300,
            pgridSustained: false,
            soc: 62.4,
            timestamp: "2026-04-15T02:00:00Z"
        ),
        battery: BatteryInfo(
            capacityKwh: 13.3,
            cutoffPercent: 10,
            estimatedCutoffTime: "2026-04-15T18:30:00Z",
            low24h: nil
        )
    )
    .padding()
}
