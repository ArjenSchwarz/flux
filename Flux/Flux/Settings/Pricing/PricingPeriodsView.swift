import FluxCore
import SwiftUI

/// Shown from Settings → "Pricing". Lists configured pricing plans sorted by
/// start date ascending and offers add/edit/delete. Each row shows the plan's
/// date range and the bands its rates apply over (AC 6.1).
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
                if viewModel.plans.isEmpty {
                    emptyStateRow
                } else {
                    ForEach(viewModel.plans) { plan in
                        planRow(plan)
                    }
                }
            } header: {
                Text("Plans")
            }
            Section {
                Button {
                    viewModel.beginCreate()
                } label: {
                    Label("Add pricing plan", systemImage: "plus")
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
                LiquidGlassSection(title: "Plans") {
                    VStack(alignment: .leading, spacing: 8) {
                        if viewModel.plans.isEmpty {
                            emptyStateRow
                        } else {
                            ForEach(viewModel.plans) { plan in
                                planRow(plan)
                                if plan.id != viewModel.plans.last?.id {
                                    Divider()
                                }
                            }
                        }
                        Divider()
                        Button {
                            viewModel.beginCreate()
                        } label: {
                            Label("Add pricing plan", systemImage: "plus")
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
            Text("Add a pricing plan to see daily costs on Day Detail and totals on History.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private func planRow(_ plan: PricingPlan) -> some View {
        Button {
            viewModel.beginEdit(plan)
        } label: {
            VStack(alignment: .leading, spacing: 4) {
                Text(PricingPeriodsView.dateRangeText(for: plan))
                    .font(.body.weight(.semibold))
                Text(PricingPeriodsView.bandSummary(for: plan))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text(PricingPeriodsView.feedInSummary(for: plan))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .buttonStyle(.plain)
        #if os(iOS)
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) {
                Task {
                    do {
                        try await viewModel.delete(plan)
                    } catch {
                        // PricingService.delete already recorded the error on
                        // service.lastError before re-throwing; the
                        // errorBanner reads from there and surfaces it
                        // automatically. We just need the do/catch so the
                        // throw doesn't go unhandled.
                    }
                }
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
        #endif
    }
}

// MARK: - Static formatters (exposed for unit tests)

extension PricingPeriodsView {
    /// The end date is exclusive (Decision 5): the plan's last priced day is
    /// the day before it. "until" says that; an inclusive-looking dash range
    /// would not.
    static func dateRangeText(for plan: PricingPlan) -> String {
        if let end = plan.endDate {
            return "\(plan.startDate) until \(end)"
        }
        return "from \(plan.startDate)"
    }

    /// The plan's bands as entered: the free window first (it is the one that
    /// changes behaviour elsewhere), then each rated exception, then the
    /// default rate that fills the rest of the day.
    static func bandSummary(for plan: PricingPlan) -> String {
        var parts: [String] = []
        if let free = plan.freeWindow {
            parts.append("Free \(free.start)–\(free.end)")
        }
        for window in plan.windows where !window.free {
            parts.append("\(formatRate(window.rate ?? 0)) \(window.start)–\(window.end)")
        }
        parts.append("\(formatRate(plan.defaultRate)) default")
        return parts.joined(separator: " · ")
    }

    /// Feed-in is a single flat rate per plan, so it sits on its own line
    /// rather than competing with the import bands.
    static func feedInSummary(for plan: PricingPlan) -> String {
        "Feed-in \(formatRate(plan.feedInRate))/kWh"
    }

    static func formatRate(_ rate: Double) -> String {
        // 4dp rate display (daily-costs Decision 10). Use a fixed format so
        // the output is stable across locales.
        String(format: "$%.4f", rate)
    }

    static let emptyStateTitle = "No pricing yet"
    static let emptyStateDetail = "Add a pricing plan to see daily costs on Day Detail and totals on History."
    static let addButtonLabel = "Add pricing plan"
}
