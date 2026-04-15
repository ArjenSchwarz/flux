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
        .navigationTitle("Dashboard")
        .refreshable {
            await viewModel.refresh()
        }
        .onAppear {
            viewModel.startAutoRefresh()
            Task {
                await viewModel.refresh()
            }
        }
        .onDisappear {
            viewModel.stopAutoRefresh()
        }
        .onChange(of: scenePhase) { _, newPhase in
            switch newPhase {
            case .active:
                viewModel.startAutoRefresh()
                Task {
                    await viewModel.refresh()
                }
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
                Text("Settings view coming soon")
                    .navigationTitle("Settings")
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
            Label("Showing stale data", systemImage: "exclamationmark.triangle.fill")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.orange)

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
}

private actor PreviewDashboardAPIClient: FluxAPIClient {
    func fetchStatus() async throws -> StatusResponse {
        StatusResponse(
            live: LiveData(
                ppv: 2400,
                pload: 750,
                pbat: 400,
                pgrid: -100,
                pgridSustained: false,
                soc: 62.4,
                timestamp: "2026-04-15T10:00:00Z"
            ),
            battery: BatteryInfo(
                capacityKwh: 13.3,
                cutoffPercent: 10,
                estimatedCutoffTime: "2026-04-15T18:30:00Z",
                low24h: Low24h(soc: 38.2, timestamp: "2026-04-15T08:45:00Z")
            ),
            rolling15min: RollingAvg(avgLoad: 243, avgPbat: 320, estimatedCutoffTime: "2026-04-16T03:00:00Z"),
            offpeak: OffpeakData(
                windowStart: "11:00",
                windowEnd: "14:00",
                gridUsageKwh: 6.1,
                solarKwh: 2.3,
                batteryChargeKwh: 5.0,
                batteryDischargeKwh: 4.2,
                gridExportKwh: 1.4,
                batteryDeltaPercent: 42.3
            ),
            todayEnergy: TodayEnergy(epv: 14.3, eInput: 0.25, eOutput: 5.94, eCharge: 5.7, eDischarge: 6.8)
        )
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchDay(date _: String) async throws -> DayDetailResponse {
        throw FluxAPIError.notConfigured
    }
}

#Preview {
    NavigationStack {
        DashboardView(apiClient: PreviewDashboardAPIClient())
    }
}
