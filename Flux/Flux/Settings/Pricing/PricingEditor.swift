import FluxCore
import SwiftUI

/// Sheet for creating or editing a single pricing plan. A plan is entered as a
/// default rate plus the exception windows that deviate from it (Decision 4);
/// the contiguous full-day segmentation is derived, so gaps and partial
/// coverage are unrepresentable. Rate inputs accept up to four decimal places.
/// Validation errors from the backend surface inline against the offending
/// field when possible, otherwise as a banner. The destructive delete sits
/// behind a confirmation dialog.
@MainActor
struct PricingEditor: View {
    @Bindable var viewModel: PricingViewModel
    @Environment(\.dismiss) private var dismiss

    @State private var showingDeleteConfirmation = false

    var body: some View {
        NavigationStack {
            Form {
                datesSection
                ratesSection
                windowsSection
                if let inlineMessage = inlineValidationMessage {
                    Section {
                        Text(inlineMessage)
                            .font(.footnote)
                            .foregroundStyle(.red)
                    }
                }
                remediationSection
                deleteSection
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
                "Delete this pricing plan?",
                isPresented: $showingDeleteConfirmation,
                titleVisibility: .visible
            ) {
                Button("Delete", role: .destructive) {
                    Task {
                        if case .edit(let plan) = viewModel.editorMode {
                            try? await viewModel.delete(plan)
                            dismiss()
                        }
                    }
                }
                Button("Cancel", role: .cancel) {}
            }
        }
    }

    // MARK: - Sections

    private var datesSection: some View {
        Section {
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
                    "Ends",
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
        } header: {
            Text("Dates")
        } footer: {
            if viewModel.draft.endDate != nil {
                // The end date is exclusive (Decision 5) — spelling that out
                // here is what stops someone entering an off-by-one date.
                Text("The plan's last priced day is the day before this date; a successor starting on it takes over.")
            }
        }
    }

    private var ratesSection: some View {
        Section {
            rateField(label: "Default", value: $viewModel.draft.defaultRate)
            rateField(label: "Solar feed-in", value: $viewModel.draft.feedInRate)
            if hasFreeWindow {
                rateField(label: "Savings reference", value: savingsReferenceBinding)
            }
        } header: {
            Text("Rates (AUD per kWh)")
        } footer: {
            Text("The default rate applies whenever no window below covers the time of day.")
        }
    }

    private var windowsSection: some View {
        Section {
            ForEach(Array(viewModel.draft.windows.indices), id: \.self) { index in
                PricingWindowRow(viewModel: viewModel, index: index)
            }
            Button {
                viewModel.addWindow()
            } label: {
                Label("Add window", systemImage: "plus")
            }
        } header: {
            Text("Windows")
        } footer: {
            Text("Times outside every window are charged at the default rate. A plan can have one free window.")
        }
    }

    @ViewBuilder
    private var remediationSection: some View {
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
                    Label("Close existing open-ended plan and create",
                          systemImage: "arrow.triangle.swap")
                }
                .buttonStyle(.borderedProminent)
            } footer: {
                Text(PricingViewModel.remediationFooter(startDate: viewModel.draft.startDate))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private var deleteSection: some View {
        if case .edit = viewModel.editorMode {
            Section {
                Button(role: .destructive) {
                    showingDeleteConfirmation = true
                } label: {
                    Label("Delete pricing plan", systemImage: "trash")
                        .foregroundStyle(.red)
                }
            }
        }
    }

    // MARK: - Derived state

    private var navigationTitle: String {
        if case .edit = viewModel.editorMode { return "Edit pricing" }
        return "New pricing"
    }

    private var hasFreeWindow: Bool {
        viewModel.draft.windows.contains(where: \.free)
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

    /// The savings reference rate is absent on a plan with no free window. The
    /// field only shows when a free window exists, and writing to it always
    /// produces a value.
    private var savingsReferenceBinding: Binding<Double> {
        Binding(
            get: { viewModel.draft.savingsReferenceRate ?? 0 },
            set: { viewModel.draft.savingsReferenceRate = $0 }
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

    // MARK: - Field builders

    private func rateField(label: String, value: Binding<Double>) -> some View {
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
    static func localValidationMessage(for error: PricingPlanDraft.ValidationError) -> String {
        switch error {
        case .invalidStartDate:
            return "Enter a valid start date (YYYY-MM-DD)."
        case .invalidEndDate:
            return "Enter a valid end date (YYYY-MM-DD)."
        case .invertedDates:
            return "The end date must be after the start date."
        case .bandWindowInvalid:
            return "Each window needs a start before its end, between 00:00 and 24:00."
        case .bandOverlap:
            return "Windows must not overlap each other."
        case .multipleFreeBands:
            return "A plan can have at most one free window."
        case .noRatedBand:
            return "A free window covering the whole day leaves nothing to price."
        case .savingsRateMissing:
            return "A plan with a free window needs a savings reference rate."
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

    /// Band boundaries are times of day, so they are edited on an arbitrary
    /// reference date the formatter then discards.
    static let bandTimeReference: Date = {
        var components = DateComponents()
        components.year = 2000
        components.month = 1
        components.day = 1
        return bandCalendar.date(from: components) ?? Date(timeIntervalSince1970: 0)
    }()

    /// Converts a stored "HH:MM" boundary to a pickable time. "24:00" has no
    /// clock representation, so it is held as 23:59 and mapped back by
    /// `formatBandTime` — without that, a plan whose last window ends at
    /// midnight could not be opened in the editor.
    static func parseBandTime(_ value: String) -> Date? {
        guard let minutes = PlanWindow.parseBandTime(value) else { return nil }
        let clamped = min(minutes, PlanWindow.minutesPerDay - 1)
        return bandCalendar.date(byAdding: .minute, value: clamped, to: bandTimeReference)
    }

    static func formatBandTime(_ date: Date) -> String {
        let components = bandCalendar.dateComponents([.hour, .minute], from: date)
        let minutes = (components.hour ?? 0) * 60 + (components.minute ?? 0)
        // 23:59 is the picker's stand-in for end-of-day; see parseBandTime.
        if minutes == PlanWindow.minutesPerDay - 1 {
            return PlanWindow.formatBandTime(PlanWindow.minutesPerDay)
        }
        return PlanWindow.formatBandTime(minutes)
    }

    private static let bandCalendar: Calendar = {
        var calendar = Calendar(identifier: .iso8601)
        calendar.timeZone = DateFormatting.sydneyTimeZone
        return calendar
    }()

    private static let isoDateFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .iso8601)
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.timeZone = DateFormatting.sydneyTimeZone
        return formatter
    }()
}
