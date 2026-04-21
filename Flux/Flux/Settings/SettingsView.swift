import SwiftUI

@MainActor
struct SettingsView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var viewModel: SettingsViewModel
    private let onSaved: @MainActor () -> Void

    init(viewModel: SettingsViewModel, onSaved: @escaping @MainActor () -> Void = {}) {
        _viewModel = State(initialValue: viewModel)
        self.onSaved = onSaved
    }

    init(onSaved: @escaping @MainActor () -> Void = {}) {
        _viewModel = State(initialValue: SettingsViewModel())
        self.onSaved = onSaved
    }

    var body: some View {
        Form {
            Section("Backend") {
                TextField("API URL", text: $viewModel.apiURL)
                    .textInputAutocapitalization(.never)
                    .keyboardType(.URL)
                    .autocorrectionDisabled()

                SecureField("API Token", text: $viewModel.apiToken)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }

            Section("Display") {
                HStack {
                    Text("Load alert threshold")
                    Spacer()
                    TextField(
                        "Watts",
                        value: $viewModel.loadAlertThreshold,
                        format: .number.precision(.fractionLength(0))
                    )
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                        .frame(maxWidth: 120)
                }
            }

            Section {
                Button {
                    Task { await viewModel.save() }
                } label: {
                    if viewModel.isValidating {
                        ProgressView()
                            .frame(maxWidth: .infinity)
                    } else {
                        Text("Save")
                            .frame(maxWidth: .infinity)
                    }
                }
                .disabled(viewModel.isValidating || hasMissingRequiredFields)
            }

            if let validationError = viewModel.validationError {
                Section {
                    Text(validationError)
                        .foregroundStyle(.red)
                }
            }

            #if DEBUG
            WidgetDiagnosticsView()
            #endif
        }
        .navigationTitle("Settings")
        .onAppear {
            viewModel.loadExisting()
        }
        .onChange(of: viewModel.shouldDismiss) { _, shouldDismiss in
            if shouldDismiss {
                onSaved()
                dismiss()
            }
        }
    }

    private var hasMissingRequiredFields: Bool {
        viewModel.apiURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
            viewModel.apiToken.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

#Preview {
    NavigationStack {
        SettingsView()
    }
}
