import FluxCore
import SwiftUI

/// Persistent Dashboard indicator shown while a simulation is active (Req 5.1).
/// Placement mirrors the staleness banner (a full-width Dashboard-level banner
/// at the top of content), but the treatment is deliberately distinct from the
/// calm hero off-peak line (Req 5.2): a dedicated `FluxTheme.Palette.simulation`
/// accent, an SF Symbol, the active preset name + signed added-load delta, and
/// a Stop control. The `+delta` is the preset's *added* load (always positive),
/// which is intentionally a different figure from the hero's *net* battery
/// number.
struct SimulationBanner: View {
    let presetName: String
    let deltaWatts: Int
    let onStop: () -> Void

    private var deltaText: String {
        "+\(PowerFormatting.format(Double(deltaWatts)))"
    }

    var body: some View {
        FluxPanel {
            HStack(spacing: 12) {
                Image(systemName: "wand.and.stars")
                    .foregroundStyle(FluxTheme.Palette.simulation)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Simulation · \(presetName) · \(deltaText)")
                        .appFont(.subheadline, weight: .semibold)
                        .foregroundStyle(FluxTheme.Palette.simulation)
                    Text("Showing a what-if. Real values return when you stop.")
                        .appFont(.caption)
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                }
                Spacer()
                Button("Stop", action: onStop)
                    .buttonStyle(.bordered)
                    .tint(FluxTheme.Palette.simulation)
            }
        }
        .overlay(
            RoundedRectangle(cornerRadius: FluxTheme.Metrics.panelCornerRadius, style: .continuous)
                .strokeBorder(FluxTheme.Palette.simulation.opacity(0.5), lineWidth: 1)
        )
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "Simulated. \(presetName), added load \(deltaText). Double tap Stop to return to real values."
        )
    }
}

/// Menu that activates a preset or turns simulation off ([2.1]). Lists each
/// preset (label + watts) plus an Off entry while simulating; with no presets
/// it offers a path to create one in Settings ([2.3]) rather than an empty
/// selection. Extracted from DashboardView so that view stays under the
/// type-body-length limit.
struct DashboardSimulateMenu: View {
    let viewModel: DashboardViewModel
    let presets: [SimulationPreset]
    let onAddPreset: () -> Void

    var body: some View {
        Menu {
            if presets.isEmpty {
                #if os(macOS)
                // onAddPreset is a no-op on macOS (no settings sheet); open the
                // Settings scene so the empty state still offers a path ([2.3]),
                // matching the staleness banner's macOS treatment.
                SettingsLink {
                    Label("Add a preset…", systemImage: "plus")
                }
                #else
                Button {
                    onAddPreset()
                } label: {
                    Label("Add a preset…", systemImage: "plus")
                }
                #endif
            } else {
                ForEach(presets) { preset in
                    presetButton(preset)
                }
                if viewModel.isSimulating {
                    Divider()
                    Button("Off", systemImage: "xmark") {
                        Task { await viewModel.stopSimulation() }
                    }
                }
            }
        } label: {
            Label("Simulate", systemImage: "wand.and.stars")
                .labelStyle(.iconOnly)
                .foregroundStyle(
                    viewModel.isSimulating ? FluxTheme.Palette.simulation : FluxTheme.Palette.secondaryText
                )
        }
        .accessibilityLabel(
            viewModel.isSimulating ? "Simulation active. Change or stop simulation" : "Simulate"
        )
    }

    @ViewBuilder
    private func presetButton(_ preset: SimulationPreset) -> some View {
        let title = "\(preset.label) · \(PowerFormatting.format(Double(preset.watts)))"
        Button {
            Task { await viewModel.activateSimulation(presetID: preset.id) }
        } label: {
            if viewModel.activeSimulationPresetID == preset.id {
                Label(title, systemImage: "checkmark")
            } else {
                Text(title)
            }
        }
    }
}

#if DEBUG
#Preview {
    ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        SimulationBanner(presetName: "Charge car", deltaWatts: 1700, onStop: {})
            .padding()
    }
}
#endif
