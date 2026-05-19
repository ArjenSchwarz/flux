import FluxCore
import SwiftUI

/// Sheet for creating or editing a single SoC alert rule. Validation runs on
/// every field edit and again on save; the Save button disables when the
/// draft doesn't validate.
@MainActor
struct SoCAlertEditor: View {
    @Bindable var viewModel: SoCAlertsViewModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Form {
                Section("Threshold") {
                    Stepper(value: $viewModel.draft.thresholdPercent, in: 1 ... 99) {
                        Text("Notify at \(viewModel.draft.thresholdPercent)%")
                    }
                }
                Section("Window") {
                    timePicker(label: "Start", isoString: Binding(
                        get: { viewModel.draft.windowStart },
                        set: { viewModel.draft.windowStart = $0 }
                    ))
                    timePicker(label: "End", isoString: Binding(
                        get: { viewModel.draft.windowEnd },
                        set: { viewModel.draft.windowEnd = $0 }
                    ))
                    Toggle("Enabled", isOn: $viewModel.draft.enabled)
                }
                Section("Label (optional)") {
                    TextField("e.g. Evening cooking", text: labelBinding)
                }
                if let message = validationMessage {
                    Section {
                        Text(message)
                            .font(.footnote)
                            .foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle(viewModel.editorMode == .create ? "New alert" : "Edit alert")
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
                            try? await viewModel.save()
                            dismiss()
                        }
                    }
                    .disabled(!viewModel.canSave)
                }
            }
        }
    }

    private var labelBinding: Binding<String> {
        Binding(
            get: { viewModel.draft.label ?? "" },
            set: { viewModel.draft.label = $0.isEmpty ? nil : $0 }
        )
    }

    private var validationMessage: String? {
        switch viewModel.draft.validate() {
        case .none:
            return nil
        case .thresholdOutOfRange:
            return "Threshold must be between 1 and 99."
        case .invalidWindowStart:
            return "Start must be a valid HH:MM time."
        case .invalidWindowEnd:
            return "End must be a valid HH:MM time."
        case .startEqualsEnd:
            return "Start and end must differ. Use 00:00 for end-of-day."
        case .labelTooLong:
            return "Label is too long. Use 40 characters or fewer."
        }
    }

    private func timePicker(label: String, isoString: Binding<String>) -> some View {
        DatePicker(
            label,
            selection: Binding(
                get: { dateFromHHMM(isoString.wrappedValue) ?? Date() },
                set: { isoString.wrappedValue = hhmmFromDate($0) }
            ),
            displayedComponents: .hourAndMinute
        )
    }

    private func dateFromHHMM(_ s: String) -> Date? {
        let parts = s.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        guard parts.count == 2, let h = Int(parts[0]), let m = Int(parts[1]) else { return nil }
        var components = DateComponents()
        components.hour = h
        components.minute = m
        return Calendar.current.date(from: components)
    }

    private func hhmmFromDate(_ d: Date) -> String {
        let comps = Calendar.current.dateComponents([.hour, .minute], from: d)
        return String(format: "%02d:%02d", comps.hour ?? 0, comps.minute ?? 0)
    }
}
