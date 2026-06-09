import FluxCore
import SwiftUI

/// Sheet for creating or editing a single simulation preset. Validation runs
/// on every field edit and again on save; the Save button disables when the
/// draft doesn't validate. Mirrors SoCAlertEditor.
@MainActor
struct SimulationPresetEditor: View {
    @Bindable var viewModel: SimulationPresetsViewModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section("Label") {
                    TextField("e.g. Charge car", text: $viewModel.draft.label)
                    #if os(iOS)
                        .textInputAutocapitalization(.sentences)
                    #endif
                }
                Section("Load") {
                    HStack {
                        Text("Added load")
                        Spacer()
                        TextField(
                            "Watts",
                            value: $viewModel.draft.watts,
                            format: .number.precision(.fractionLength(0))
                        )
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                        .multilineTextAlignment(.trailing)
                        .frame(maxWidth: 120)
                        Text("W")
                            .foregroundStyle(.secondary)
                    }
                }
                if let message = validationMessage {
                    Section {
                        Text(message)
                            .font(.footnote)
                            .foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle(viewModel.editorMode == .create ? "New preset" : "Edit preset")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        viewModel.setEditorPresented(false)
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task {
                            // Only dismiss on success; on failure the view
                            // model surfaces lastError as a banner and we keep
                            // the sheet open so the user's edits aren't lost.
                            do {
                                _ = try await viewModel.save()
                                dismiss()
                            } catch {
                                // viewModel.lastError is already set.
                            }
                        }
                    }
                    .disabled(!viewModel.canSave)
                }
            }
        }
    }

    private var validationMessage: String? {
        switch viewModel.draft.validate() {
        case .none:
            return nil
        case .emptyLabel:
            return "Label must not be empty."
        case .labelTooLong:
            return "Label is too long. Use 40 characters or fewer."
        case .wattsOutOfRange:
            return "Added load must be between 1 and 20000 W."
        }
    }
}
