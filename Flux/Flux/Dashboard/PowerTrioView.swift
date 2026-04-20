import FluxCore
import SwiftUI

struct PowerTrioView: View {
    let live: LiveData?
    let offpeak: OffpeakData?
    let nowProvider: () -> Date
    let loadAlertThreshold: Double

    init(
        live: LiveData?,
        offpeak: OffpeakData?,
        loadAlertThreshold: Double = UserDefaults.fluxAppGroup.loadAlertThreshold,
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
                title: gridTitle,
                value: live?.pgrid ?? 0,
                color: GridColor.forGrid(
                    pgrid: live?.pgrid ?? 0,
                    pgridSustained: live?.pgridSustained ?? false,
                    offpeakWindowStart: offpeak?.windowStart ?? OffpeakData.defaultWindowStart,
                    offpeakWindowEnd: offpeak?.windowEnd ?? OffpeakData.defaultWindowEnd,
                    now: nowProvider()
                ).color
            )
        }
        .frame(maxWidth: .infinity)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var gridTitle: String {
        let pgrid = live?.pgrid ?? 0
        if pgrid < 0 { return "Grid (export)" }
        if pgrid > 0 { return "Grid (import)" }
        return "Grid"
    }

    private func metricColumn(
        title: String,
        value: Double,
        color: Color
    ) -> some View {
        VStack(spacing: 4) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(PowerFormatting.format(value))
                .font(.headline)
                .foregroundStyle(color)
        }
        .frame(maxWidth: .infinity)
    }
}

#if DEBUG
#Preview {
    let status = MockFluxAPIClient.statusResponse
    PowerTrioView(
        live: status.live,
        offpeak: status.offpeak
    )
    .padding()
}
#endif
