import FluxCore
import SwiftUI

/// Shown from Settings → "Pricing". Lists configured pricing periods sorted
/// by start date ascending and offers add/edit/delete (Requirement 3).
@MainActor
struct PricingPeriodsView: View {
    @State private var viewModel: PricingViewModel

    init(service: PricingService? = nil) {
        let resolved = service ?? PricingService.shared
        _viewModel = State(initialValue: PricingViewModel(service: resolved))
    }

    var body: some View {
        Group {
            #if os(macOS)
            macOSContent
            #else
            iOSContent
            #endif
        }
        .navigationTitle("Pricing")
        .task { await viewModel.refresh() }
        .sheet(
            isPresented: Binding(
                get: { viewModel.isEditorPresented },
                set: { viewModel.setEditorPresented($0) }
            )
        ) {
            PricingEditor(viewModel: viewModel)
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
                if viewModel.periods.isEmpty {
                    emptyStateRow
                } else {
                    ForEach(viewModel.periods) { period in
                        periodRow(period)
                    }
                }
            } header: {
                Text("Periods")
            }
            Section {
                Button {
                    viewModel.beginCreate()
                } label: {
                    Label("Add pricing period", systemImage: "plus")
                }
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
                LiquidGlassSection(title: "Periods") {
                    VStack(alignment: .leading, spacing: 8) {
                        if viewModel.periods.isEmpty {
                            emptyStateRow
                        } else {
                            ForEach(viewModel.periods) { period in
                                periodRow(period)
                                if period.id != viewModel.periods.last?.id {
                                    Divider()
                                }
                            }
                        }
                        Divider()
                        Button {
                            viewModel.beginCreate()
                        } label: {
                            Label("Add pricing period", systemImage: "plus")
                        }
                    }
                    .padding(8)
                }
            }
            .padding()
        }
    }
    #endif

    private var errorBanner: some View {
        HStack(alignment: .top) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)
            VStack(alignment: .leading, spacing: 4) {
                Text(viewModel.lastValidationError?.message
                     ?? viewModel.lastErrorMessage
                     ?? "Couldn't reach the backend")
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
            Text("No pricing yet")
                .font(.body.weight(.semibold))
            Text("Add a pricing period to see daily costs on Day Detail and totals on History.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func periodRow(_ period: PricingPeriod) -> some View {
        Button {
            viewModel.beginEdit(period)
        } label: {
            VStack(alignment: .leading, spacing: 4) {
                Text(PricingPeriodsView.dateRangeText(for: period))
                    .font(.body.weight(.semibold))
                Text(PricingPeriodsView.rateSummary(for: period))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .buttonStyle(.plain)
        #if os(iOS)
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) {
                Task { try? await viewModel.delete(period) }
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
        #endif
    }
}

// MARK: - Static formatters (exposed for unit tests)

extension PricingPeriodsView {
    static func dateRangeText(for period: PricingPeriod) -> String {
        if let end = period.endDate {
            return "\(period.startDate) – \(end)"
        }
        return "from \(period.startDate)"
    }

    static func rateSummary(for period: PricingPeriod) -> String {
        let peak = formatRate(period.peakRate)
        let feedIn = formatRate(period.feedInRate)
        let savings = formatRate(period.offPeakSavingsRate)
        return "Peak \(peak)/kWh · Feed-in \(feedIn)/kWh · Off-peak \(savings)/kWh"
    }

    static func formatRate(_ rate: Double) -> String {
        // 4dp rate display per AC 3.2 / Decision 10. Use a fixed format so
        // the output is stable across locales.
        String(format: "$%.4f", rate)
    }

    static let emptyStateTitle = "No pricing yet"
    static let emptyStateDetail = "Add a pricing period to see daily costs on Day Detail and totals on History."
    static let addButtonLabel = "Add pricing period"
}

#if canImport(UIKit)
import UIKit
#endif
#if canImport(AppKit)
import AppKit
#endif
