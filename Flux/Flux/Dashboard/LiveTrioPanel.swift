import FluxCore
import SwiftUI

/// Three-column live readout for Solar / House / Grid. The grid column
/// switches accent colour and verb based on the direction of flow; we never
/// show a minus sign — the verb does the work.
struct LiveTrioPanel: View {
    let live: LiveData?

    var body: some View {
        FluxPanel(padding: 0) {
            HStack(spacing: 0) {
                column(
                    label: "Solar",
                    watts: live?.ppv,
                    valueColor: FluxTheme.Palette.amber,
                    sub: "producing",
                    showsLeftDivider: false
                )
                column(
                    label: "House",
                    watts: live?.pload,
                    valueColor: FluxTheme.Palette.primaryText,
                    sub: "using",
                    showsLeftDivider: true
                )
                column(
                    label: "Grid",
                    watts: live.map { abs($0.pgrid) },
                    valueColor: gridColor,
                    sub: gridSub,
                    showsLeftDivider: true
                )
            }
        }
    }

    private var gridColor: Color {
        guard let pgrid = live?.pgrid else { return FluxTheme.Palette.primaryText }
        if pgrid < 0 { return FluxTheme.Palette.gridExport }
        if pgrid > 0 { return FluxTheme.Palette.grid }
        return FluxTheme.Palette.primaryText
    }

    private var gridSub: String {
        guard let pgrid = live?.pgrid else { return "—" }
        if pgrid < 0 { return "exporting" }
        if pgrid > 0 { return "importing" }
        return "idle"
    }

    @ViewBuilder
    private func column(
        label: String,
        watts: Double?,
        valueColor: Color,
        sub: String,
        showsLeftDivider: Bool
    ) -> some View {
        let parts = watts.map(PowerFormatting.split)
        HStack(spacing: 0) {
            if showsLeftDivider {
                Rectangle()
                    .fill(FluxTheme.Palette.border)
                    .frame(width: FluxTheme.Metrics.hairline)
            }
            VStack(alignment: .leading, spacing: 4) {
                Text(label.uppercased())
                    .font(FluxTheme.Typography.trioLabel)
                    .tracking(1)
                    .foregroundStyle(FluxTheme.Palette.tertiaryText)
                HStack(alignment: .firstTextBaseline, spacing: 2) {
                    Text(parts?.value ?? "—")
                        .font(FluxTheme.Typography.trioValue)
                        .foregroundStyle(valueColor)
                    Text(parts?.unit ?? "kW")
                        .font(FluxTheme.Typography.trioUnit)
                        .foregroundStyle(FluxTheme.Palette.tertiaryText)
                }
                Text(sub)
                    .font(FluxTheme.Typography.trioSub)
                    .foregroundStyle(FluxTheme.Palette.secondaryText)
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

#if DEBUG
#Preview {
    ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        LiveTrioPanel(live: MockFluxAPIClient.statusResponse.live)
            .padding()
    }
}
#endif
