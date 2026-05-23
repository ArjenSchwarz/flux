import FluxCore
import SwiftUI

/// Sheet for creating or editing a single pricing period. Rate inputs accept
/// up to four decimal places (AC 3.4). Validation errors from the backend
/// surface inline against the offending field when possible, otherwise as a
/// banner. The destructive delete sits behind a confirmation dialog (AC 3.7).
@MainActor
struct PricingEditor: View {
    @Bindable var viewModel: PricingViewModel
    @Environment(\.dismiss) private var dismiss

    @State private var showingDeleteConfirmation = false

    var body: some View {
        NavigationStack {
            Form {
                Section("Dates") {
                    DatePicker(
                        "Start",
                        selection: Binding(
                            get: { PricingEditor.parseDate(viewModel.draft.startDate) ?? Date() },
                            set: { viewModel.draft.startDate = PricingEditor.formatDate($0) }
                        ),
                        displayedComponents: .date
                    )
                    Toggle("Open-ended (no end date)", isOn: openEndedBinding)
                    if viewModel.draft.endDate != nil {
                        DatePicker(
                            "End",
                            selection: Binding(
                                get: {
                                    PricingEditor.parseDate(
                                        viewModel.draft.endDate ?? viewModel.draft.startDate
                                    ) ?? Date()
                                },
                                set: { viewModel.draft.endDate = PricingEditor.formatDate($0) }
                            ),
                            displayedComponents: .date
                        )
                    }
                }
                Section("Rates (AUD per kWh)") {
                    rateField(label: "Peak", value: $viewModel.draft.peakRate, fieldKey: .rate)
                    rateField(label: "Solar feed-in", value: $viewModel.draft.feedInRate, fieldKey: .rate)
                    rateField(label: "Off-peak savings", value: $viewModel.draft.offPeakSavingsRate, fieldKey: .rate)
                }
                if let inlineMessage = inlineValidationMessage {
                    Section {
                        Text(inlineMessage)
                            .font(.footnote)
                            .foregroundStyle(.red)
                    }
                }
                if viewModel.overlapRemediationTargetId != nil {
                    Section {
                        Button {
                            Task {
                                do {
                                    try await viewModel.remediateOverlap()
                                    dismiss()
                                } catch {
                                    // surfaces via lastValidationError
                                }
                            }
                        } label: {
                            Label("Close existing open-ended period and create",
                                  systemImage: "arrow.triangle.swap")
                        }
                        .buttonStyle(.borderedProminent)
                    } footer: {
                        Text(
                            "This closes the existing open-ended period at the day before this start date, " +
                            "then creates the new period — all in one transaction."
                        )
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    }
                }
                if case .edit = viewModel.editorMode {
                    Section {
                        Button(role: .destructive) {
                            showingDeleteConfirmation = true
                        } label: {
                            Label("Delete pricing period", systemImage: "trash")
                                .foregroundStyle(.red)
                        }
                    }
                }
            }
            .navigationTitle(navigationTitle)
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
                            do {
                                try await viewModel.save()
                                dismiss()
                            } catch {
                                // viewModel surfaces validation/banner state
                            }
                        }
                    }
                    .disabled(!viewModel.canSave)
                }
            }
            .confirmationDialog(
                "Delete this pricing period?",
                isPresented: $showingDeleteConfirmation,
                titleVisibility: .visible
            ) {
                Button("Delete", role: .destructive) {
                    Task {
                        if case .edit(let period) = viewModel.editorMode {
                            try? await viewModel.delete(period)
                            dismiss()
                        }
                    }
                }
                Button("Cancel", role: .cancel) {}
            }
        }
    }

    private enum FieldKey { case startDate, endDate, rate }

    private var navigationTitle: String {
        if case .edit = viewModel.editorMode { return "Edit pricing" }
        return "New pricing"
    }

    private var openEndedBinding: Binding<Bool> {
        Binding(
            get: { viewModel.draft.endDate == nil },
            set: { isOpen in
                if isOpen {
                    viewModel.draft.endDate = nil
                } else {
                    viewModel.draft.endDate = viewModel.draft.startDate
                }
            }
        )
    }

    private var inlineValidationMessage: String? {
        if let reason = viewModel.lastValidationError {
            switch reason {
            case .overlap:
                // Surfaced through the remediation section, not here.
                return nil
            default:
                return reason.message
            }
        }
        if let err = viewModel.draft.validate() {
            return PricingEditor.localValidationMessage(for: err)
        }
        return nil
    }

    private func rateField(label: String, value: Binding<Double>, fieldKey _: FieldKey) -> some View {
        HStack {
            Text(label)
            Spacer()
            TextField(
                "0.0000",
                value: value,
                format: .number.precision(.fractionLength(4))
            )
            #if os(iOS)
            .keyboardType(.decimalPad)
            #endif
            .multilineTextAlignment(.trailing)
            .frame(maxWidth: 120)
            Text("/ kWh")
                .foregroundStyle(.secondary)
        }
    }
}

// MARK: - Static helpers (exposed for tests)

extension PricingEditor {
    static func localValidationMessage(for error: PricingPeriodDraft.ValidationError) -> String {
        switch error {
        case .invalidStartDate:
            return "Enter a valid start date (YYYY-MM-DD)."
        case .invertedDates:
            return "End date must not be before the start date."
        case .rateOutOfRange:
            return "Each rate must be between $0.00 and $10.00 per kWh."
        case .ratePrecision:
            return "Rates accept up to four decimal places."
        }
    }

    static func parseDate(_ value: String) -> Date? {
        guard !value.isEmpty else { return nil }
        return isoDateFormatter.date(from: value)
    }

    static func formatDate(_ date: Date) -> String {
        isoDateFormatter.string(from: date)
    }

    private static let isoDateFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .iso8601)
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.timeZone = TimeZone(identifier: "Australia/Melbourne") ?? .current
        return formatter
    }()
}
