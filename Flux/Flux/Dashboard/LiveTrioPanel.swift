import FluxCore
import SwiftUI

/// Three-column live readout for Solar / House / Grid. The grid column
/// switches accent colour and verb based on the direction of flow; we never
/// show a minus sign — the verb does the work.
struct LiveTrioPanel: View {
    let live: LiveData?
    /// When true the House value reflects an added simulated load, so it is
    /// tinted in the simulation accent and announced as simulated (Req 5.3/5.4).
    var isSimulating = false

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
                    valueColor: isSimulating ? FluxTheme.Palette.simulation : FluxTheme.Palette.primaryText,
                    sub: "using",
                    showsLeftDivider: true,
                    simulated: isSimulating
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
        showsLeftDivider: Bool,
        simulated: Bool = false
    ) -> some View {
        let parts = watts.map(PowerFormatting.split)
        // Drop to a slightly smaller value font once the numeric portion runs
        // past four characters — values ≥ 10 kW render five characters
        // (e.g. "10.00", "11.51") — so the value plus unit stays on one line
        // instead of wrapping in the column.
        let valueFont = (parts?.value.count ?? 0) > 4
            ? FluxTheme.Typography.trioValueCompact
            : FluxTheme.Typography.trioValue
        HStack(spacing: 0) {
            if showsLeftDivider {
                Rectangle()
                    .fill(FluxTheme.Palette.border)
                    .frame(width: FluxTheme.Metrics.hairline)
            }
            VStack(alignment: .leading, spacing: 4) {
                Text(label.uppercased())
                    .appFont(FluxTheme.Typography.trioLabel)
                    .tracking(1)
                    .foregroundStyle(FluxTheme.Palette.tertiaryText)
                HStack(alignment: .firstTextBaseline, spacing: 2) {
                    Text(parts?.value ?? "—")
                        .appFont(valueFont)
                        .foregroundStyle(valueColor)
                        .lineLimit(1)
                    Text(parts?.unit ?? "kW")
                        .appFont(FluxTheme.Typography.trioUnit)
                        .foregroundStyle(FluxTheme.Palette.tertiaryText)
                }
                Text(sub)
                    .appFont(FluxTheme.Typography.trioSub)
                    .foregroundStyle(FluxTheme.Palette.secondaryText)
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            .accessibilityLabel(columnAccessibilityLabel(label: label, parts: parts, sub: sub, simulated: simulated))
        }
    }

    private func columnAccessibilityLabel(
        label: String,
        parts: (value: String, unit: String)?,
        sub: String,
        simulated: Bool
    ) -> String {
        let value = parts.map { "\($0.value) \($0.unit)" } ?? "unavailable"
        let base = "\(label), \(value), \(sub)"
        return simulated ? "\(base), simulated" : base
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
