import SwiftUI

struct PowerTrioView: View {
    let live: LiveData?
    let offpeak: OffpeakData?
    let nowProvider: () -> Date
    let loadAlertThreshold: Double

    init(
        live: LiveData?,
        offpeak: OffpeakData?,
        loadAlertThreshold: Double = UserDefaults.standard.loadAlertThreshold,
        nowProvider: @escaping () -> Date = { .now }
    ) {
        self.live = live
        self.offpeak = offpeak
        self.loadAlertThreshold = loadAlertThreshold
        self.nowProvider = nowProvider
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            metricColumn(
                title: "Solar",
                value: live?.ppv ?? 0,
                color: (live?.ppv ?? 0) > 0 ? .green : .secondary
            )

            metricColumn(
                title: "Load",
                value: live?.pload ?? 0,
                color: (live?.pload ?? 0) > loadAlertThreshold ? .red : .primary
            )

            metricColumn(
                title: "Grid",
                value: live?.pgrid ?? 0,
                color: GridColor.forGrid(
                    pgrid: live?.pgrid ?? 0,
                    pgridSustained: live?.pgridSustained ?? false,
                    offpeakWindowStart: offpeak?.windowStart ?? "11:00",
                    offpeakWindowEnd: offpeak?.windowEnd ?? "14:00",
                    now: nowProvider()
                ),
                detail: gridDirection
            )
        }
        .frame(maxWidth: .infinity)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var gridDirection: String {
        let pgrid = live?.pgrid ?? 0
        if pgrid < 0 { return "Exporting" }
        if pgrid > 0 { return "Importing" }
        return "Idle"
    }

    private func metricColumn(
        title: String,
        value: Double,
        color: Color,
        detail: String? = nil
    ) -> some View {
        VStack(spacing: 4) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text("\(Int(value.rounded()))W")
                .font(.headline)
                .foregroundStyle(color)
            if let detail {
                Text(detail)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            } else {
                Text(" ")
                    .font(.caption2)
            }
        }
        .frame(maxWidth: .infinity)
    }
}

#Preview {
    PowerTrioView(
        live: LiveData(
            ppv: 1400,
            pload: 3200,
            pbat: -600,
            pgrid: 550,
            pgridSustained: true,
            soc: 64,
            timestamp: "2026-04-15T02:00:00Z"
        ),
        offpeak: OffpeakData(
            windowStart: "11:00",
            windowEnd: "14:00",
            gridUsageKwh: nil,
            solarKwh: nil,
            batteryChargeKwh: nil,
            batteryDischargeKwh: nil,
            gridExportKwh: nil,
            batteryDeltaPercent: nil
        )
    )
    .padding()
}
