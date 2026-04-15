import SwiftUI
import SwiftData

struct DashboardView: View {
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.modelContext) private var modelContext
    @State private var viewModel: DashboardViewModel
    @State private var showingSettings = false
    private let historyFactory: (ModelContext) -> AnyView

    init(viewModel: DashboardViewModel) {
        _viewModel = State(initialValue: viewModel)
        historyFactory = { _ in AnyView(Text("History unavailable")) }
    }

    init(apiClient: any FluxAPIClient) {
        _viewModel = State(initialValue: DashboardViewModel(apiClient: apiClient))
        historyFactory = { modelContext in
            AnyView(HistoryView(apiClient: apiClient, modelContext: modelContext))
        }
    }

    var body: some View {
        ScrollView {
            if viewModel.status == nil, let error = viewModel.error {
                initialLoadErrorCard(error: error)
                    .padding()
            } else {
                VStack(alignment: .leading, spacing: 16) {
                    if viewModel.error != nil, viewModel.status != nil {
                        stalenessBanner
                    }

                    BatteryHeroView(
                        live: viewModel.status?.live,
                        battery: viewModel.status?.battery
                    )

                    PowerTrioView(
                        live: viewModel.status?.live,
                        offpeak: viewModel.status?.offpeak
                    )

                    SecondaryStatsView(
                        battery: viewModel.status?.battery,
                        rolling15min: viewModel.status?.rolling15min,
                        offpeak: viewModel.status?.offpeak
                    )

                    TodayEnergyView(todayEnergy: viewModel.status?.todayEnergy)

                    NavigationLink("View history") {
                        historyFactory(modelContext)
                    }
                    .font(.headline)
                    .padding(.horizontal)
                    .padding(.bottom, 8)
                }
                .padding()
            }
        }
        .navigationTitle("Dashboard")
        .refreshable {
            await viewModel.refresh()
        }
        .onAppear {
            viewModel.startAutoRefresh()
        }
        .onDisappear {
            viewModel.stopAutoRefresh()
        }
        .onChange(of: scenePhase) { _, newPhase in
            switch newPhase {
            case .active:
                viewModel.startAutoRefresh()
            case .background, .inactive:
                viewModel.stopAutoRefresh()
            @unknown default:
                viewModel.stopAutoRefresh()
            }
        }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button("Settings") {
                    showingSettings = true
                }
            }
        }
        .sheet(isPresented: $showingSettings) {
            NavigationStack {
                SettingsView()
                    .toolbar {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button("Done") {
                                showingSettings = false
                            }
                        }
                    }
            }
        }
    }

    @ViewBuilder
    private var stalenessBanner: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label(stalenessTitle, systemImage: "exclamationmark.triangle.fill")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.orange)

            if let error = viewModel.error {
                Text(error.message)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            if let lastSuccessfulFetch = viewModel.lastSuccessfulFetch {
                Text("Last updated \(lastSuccessfulFetch, style: .relative) ago")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            HStack {
                Button("Retry") {
                    Task { await viewModel.refresh() }
                }
                .buttonStyle(.borderedProminent)

                Button("Settings") {
                    showingSettings = true
                }
                .buttonStyle(.bordered)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    private var stalenessTitle: String {
        if case .some(.unauthorized) = viewModel.error {
            return "Authentication required"
        }
        return "Showing stale data"
    }

    private func initialLoadErrorCard(error: FluxAPIError) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("Unable to load dashboard", systemImage: "wifi.exclamationmark")
                .font(.headline)
            Text(error.message)
                .font(.subheadline)
                .foregroundStyle(.secondary)

            HStack {
                Button("Retry") {
                    Task { await viewModel.refresh() }
                }
                .buttonStyle(.borderedProminent)

                if error.suggestsSettings {
                    Button("Settings") {
                        showingSettings = true
                    }
                    .buttonStyle(.bordered)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
}

#Preview {
    NavigationStack {
        DashboardView(apiClient: MockFluxAPIClient.preview)
    }
}
