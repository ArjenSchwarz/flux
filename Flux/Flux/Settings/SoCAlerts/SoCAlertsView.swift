import FluxCore
import SwiftUI

/// Shown from Settings → "Alerts" → "Battery alerts". Lists existing SoC
/// alert rules and lets the user add, edit, toggle, and delete.
@MainActor
struct SoCAlertsView: View {
    @State private var viewModel: SoCAlertsViewModel

    init(service: SoCAlertsService? = nil) {
        let resolved = service ?? SoCAlertsService.shared
        _viewModel = State(initialValue: SoCAlertsViewModel(service: resolved))
    }

    var body: some View {
        Group {
            #if os(macOS)
            macOSContent
            #else
            iOSContent
            #endif
        }
        .navigationTitle("Battery alerts")
        .task {
            await viewModel.refresh()
            await viewModel.requestAuthorizationAndRegister()
        }
        .sheet(
            isPresented: Binding(
                get: { viewModel.isEditorPresented },
                set: { viewModel.setEditorPresented($0) }
            )
        ) {
            SoCAlertEditor(viewModel: viewModel)
        }
    }

    private var iOSContent: some View {
        Form {
            if viewModel.showsPermissionDeniedBanner {
                Section {
                    permissionDeniedBanner
                }
            }
            if viewModel.showsErrorBanner {
                Section {
                    errorBanner
                }
            }
            Section {
                if viewModel.rules.isEmpty {
                    emptyStateRow
                } else {
                    ForEach(viewModel.rules) { rule in
                        ruleRow(rule)
                    }
                }
            } header: {
                Text("Rules")
            } footer: {
                if !viewModel.addAffordanceEnabled {
                    Text("You've reached the 10-rule limit. Delete a rule to add a new one.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            Section {
                Button {
                    viewModel.beginCreate()
                } label: {
                    Label("Add alert", systemImage: "plus")
                }
                .disabled(!viewModel.addAffordanceEnabled)
            }
        }
    }

    #if os(macOS)
    private var macOSContent: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if viewModel.showsPermissionDeniedBanner {
                    permissionDeniedBanner
                }
                if viewModel.showsErrorBanner {
                    errorBanner
                }
                LiquidGlassSection(title: "Rules") {
                    VStack(alignment: .leading, spacing: 8) {
                        if viewModel.rules.isEmpty {
                            emptyStateRow
                        } else {
                            ForEach(viewModel.rules) { rule in
                                ruleRow(rule)
                                if rule.id != viewModel.rules.last?.id {
                                    Divider()
                                }
                            }
                        }
                        Divider()
                        Button {
                            viewModel.beginCreate()
                        } label: {
                            Label("Add alert", systemImage: "plus")
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

    private var permissionDeniedBanner: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Notifications are off for Flux")
                .font(.body.weight(.semibold))
                .foregroundStyle(.red)
            Text("Battery alerts won't appear until you enable notifications in System Settings.")
                .font(.footnote)
                .foregroundStyle(.secondary)
            Button("Open Settings") {
                openSystemSettings()
            }
            .buttonStyle(.bordered)
        }
    }

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
            Text("No alerts yet")
                .font(.body.weight(.semibold))
            Text("Add a rule to get notified when the battery dips below a threshold.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func ruleRow(_ rule: SoCAlertRule) -> some View {
        Button {
            viewModel.beginEdit(rule)
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(rule.label?.isEmpty == false ? rule.label! : "Alert at \(rule.thresholdPercent)%")
                        .font(.body)
                        .foregroundStyle(rule.enabled ? .primary : .secondary)
                    Text("\(rule.windowStart)–\(rule.windowEnd) • below \(rule.thresholdPercent)%")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Toggle("", isOn: Binding(
                    get: { rule.enabled },
                    set: { _ in
                        Task { try? await viewModel.toggleEnabled(rule) }
                    }
                ))
                .labelsHidden()
            }
        }
        .buttonStyle(.plain)
        #if os(iOS)
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) {
                Task { try? await viewModel.delete(rule) }
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
        #endif
    }

    private func openSystemSettings() {
        #if canImport(UIKit) && !os(macOS)
        if let url = URL(string: UIApplication.openSettingsURLString) {
            UIApplication.shared.open(url)
        }
        #elseif os(macOS)
        if let url = URL(string: "x-apple.systempreferences:com.apple.preference.notifications") {
            NSWorkspace.shared.open(url)
        }
        #endif
    }
}

#if canImport(UIKit)
import UIKit
#endif
#if canImport(AppKit)
import AppKit
#endif
