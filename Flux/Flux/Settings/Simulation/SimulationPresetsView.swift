import FluxCore
import SwiftUI

/// Shown from Settings → "Simulation" → "Load presets". Lists existing
/// simulation presets and lets the user add, edit, and delete. Mirrors
/// SoCAlertsView.
@MainActor
struct SimulationPresetsView: View {
    @State private var viewModel: SimulationPresetsViewModel

    init(service: SimulationPresetsService? = nil) {
        let resolved = service ?? SimulationPresetsService.shared
        _viewModel = State(initialValue: SimulationPresetsViewModel(service: resolved))
    }

    var body: some View {
        Group {
            #if os(macOS)
            macOSContent
            #else
            iOSContent
            #endif
        }
        .navigationTitle("Load presets")
        .task {
            await viewModel.refresh()
        }
        .sheet(
            isPresented: Binding(
                get: { viewModel.isEditorPresented },
                set: { viewModel.setEditorPresented($0) }
            )
        ) {
            SimulationPresetEditor(viewModel: viewModel)
        }
    }

    private var iOSContent: some View {
        Form {
            if viewModel.showsErrorBanner {
                Section {
                    errorBanner
                }
            }
            Section {
                if viewModel.presets.isEmpty {
                    emptyStateRow
                } else {
                    ForEach(viewModel.presets) { preset in
                        presetRow(preset)
                    }
                }
            } header: {
                Text("Presets")
            } footer: {
                if !viewModel.addAffordanceEnabled {
                    Text("You've reached the \(SimulationPresetsViewModel.presetCap)-preset limit. " +
                         "Delete a preset to add a new one.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                } else {
                    Text("Presets let you simulate an added load on the Dashboard without changing anything.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            Section {
                Button {
                    viewModel.beginCreate()
                } label: {
                    Label("Add preset", systemImage: "plus")
                }
                .disabled(!viewModel.addAffordanceEnabled)
            }
        }
    }

    #if os(macOS)
    private var macOSContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if viewModel.showsErrorBanner {
                    errorBanner
                }
                LiquidGlassSection(title: "Presets") {
                    VStack(alignment: .leading, spacing: 8) {
                        if viewModel.presets.isEmpty {
                            emptyStateRow
                        } else {
                            ForEach(viewModel.presets) { preset in
                                presetRow(preset)
                                if preset.id != viewModel.presets.last?.id {
                                    Divider()
                                }
                            }
                        }
                        Divider()
                        Button {
                            viewModel.beginCreate()
                        } label: {
                            Label("Add preset", systemImage: "plus")
                        }
                        .disabled(!viewModel.addAffordanceEnabled)
                    }
                    .padding(8)
                }
            }
            .padding()
        }
    }
    #endif

    private var errorBanner: some View {
        HStack {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)
            VStack(alignment: .leading) {
                Text(viewModel.lastErrorMessage ?? "Couldn't reach the backend")
                    .font(.footnote)
                    .foregroundStyle(.red)
            }
            Spacer()
            Button("Dismiss") { viewModel.clearError() }
                .buttonStyle(.borderless)
        }
    }

    private var emptyStateRow: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("No presets yet")
                .font(.body.weight(.semibold))
            Text("Add a preset to simulate a what-if load, like charging the car, from the Dashboard.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func presetRow(_ preset: SimulationPreset) -> some View {
        Button {
            viewModel.beginEdit(preset)
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(preset.label)
                        .font(.body)
                        .foregroundStyle(.primary)
                    Text(PowerFormatting.format(Double(preset.watts)))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
        }
        .buttonStyle(.plain)
        #if os(iOS)
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) {
                Task { try? await viewModel.delete(preset) }
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
        #endif
    }
}
