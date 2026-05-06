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
        Group {
            #if os(macOS)
            macOSForm
            #else
            iOSForm
            #endif
        }
        .onAppear { viewModel.loadExisting() }
        .task { await viewModel.loadFontFamilies() }
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

    #if os(iOS)
    private var iOSForm: some View {
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

                Toggle("Widget icons instead of labels", isOn: $viewModel.widgetUsesSymbols)

                Picker("App font", selection: $viewModel.appFontFamily) {
                    Text("System").tag("")
                    ForEach(viewModel.installedFontFamilies, id: \.self) { family in
                        Text(family).tag(family)
                    }
                }

                Picker("Appearance", selection: $viewModel.theme) {
                    ForEach(ThemeChoice.allCases) { choice in
                        Text(choice.displayName).tag(choice)
                    }
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
    }
    #endif

    #if os(macOS)
    private static let labelWidth: CGFloat = 160

    private var macOSForm: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                LiquidGlassSection(title: "Backend") {
                    Grid(alignment: .leadingFirstTextBaseline,
                         horizontalSpacing: 16, verticalSpacing: 14) {
                        FormRow("API URL", labelWidth: Self.labelWidth) {
                            TextField("https://…", text: $viewModel.apiURL)
                                .autocorrectionDisabled()
                        }
                        FormRow("API Token", labelWidth: Self.labelWidth) {
                            SecureField("", text: $viewModel.apiToken)
                                .autocorrectionDisabled()
                        }
                    }
                }

                LiquidGlassSection(title: "Display") {
                    Grid(alignment: .leadingFirstTextBaseline,
                         horizontalSpacing: 16, verticalSpacing: 14) {
                        FormRow("Load alert threshold", labelWidth: Self.labelWidth) {
                            HStack(spacing: 6) {
                                TextField(
                                    "",
                                    value: $viewModel.loadAlertThreshold,
                                    format: .number.precision(.fractionLength(0))
                                )
                                .multilineTextAlignment(.trailing)
                                .frame(width: 100)
                                Text("W")
                                    .foregroundStyle(.secondary)
                                Spacer()
                            }
                        }
                        FormRow("", labelWidth: Self.labelWidth) {
                            Toggle("Widget icons instead of labels", isOn: $viewModel.widgetUsesSymbols)
                        }
                        FormRow("App font", labelWidth: Self.labelWidth) {
                            Picker("", selection: $viewModel.appFontFamily) {
                                Text("System").tag("")
                                ForEach(viewModel.installedFontFamilies, id: \.self) { family in
                                    Text(family).tag(family)
                                }
                            }
                            .labelsHidden()
                        }
                        FormRow("Appearance", labelWidth: Self.labelWidth) {
                            Picker("", selection: $viewModel.theme) {
                                ForEach(ThemeChoice.allCases) { choice in
                                    Text(choice.displayName).tag(choice)
                                }
                            }
                            .labelsHidden()
                        }
                    }
                }

                if let validationError = viewModel.validationError {
                    Text(validationError)
                        .appFont(.callout)
                        .foregroundStyle(.red)
                }

                HStack {
                    Spacer()
                    Button {
                        Task { await viewModel.save() }
                    } label: {
                        if viewModel.isValidating {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Text("Save")
                        }
                    }
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent)
                    .disabled(viewModel.isValidating || hasMissingRequiredFields)
                }

                #if DEBUG
                LiquidGlassSection(title: "Widget diagnostics") {
                    WidgetDiagnosticsView()
                }
                #endif
            }
            .padding(28)
            .frame(maxWidth: 640, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .navigationTitle("Settings")
    }
    #endif
}

#if os(macOS)
private struct FormRow<Content: View>: View {
    let label: String
    let labelWidth: CGFloat
    let content: Content

    init(_ label: String, labelWidth: CGFloat, @ViewBuilder content: () -> Content) {
        self.label = label
        self.labelWidth = labelWidth
        self.content = content()
    }

    var body: some View {
        GridRow {
            Text(label)
                .foregroundStyle(.secondary)
                .frame(width: labelWidth, alignment: .trailing)
            content
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

private struct LiquidGlassSection<Content: View>: View {
    let title: String
    let content: Content

    init(title: String, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title).appFont(.headline)
            content
                .padding(18)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background {
                    RoundedRectangle(cornerRadius: 16, style: .continuous)
                        .fill(.clear)
                        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
                }
        }
    }
}
#endif

#Preview {
    NavigationStack {
        SettingsView()
    }
}
