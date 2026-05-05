import FluxCore
import SwiftData
import SwiftUI

/// V5 dashboard. All the V4 sub-views (BatteryHeroView / PowerTrioView /
/// SecondaryStatsView / TodayEnergyView / NoteRowView) have been folded into
/// the panels below.
struct DashboardView: View {
    @Environment(\.scenePhase) private var scenePhase
    @State private var viewModel: DashboardViewModel
    @State private var showingSettings = false
    @AppStorage("heroFontIdentifier", store: .fluxAppGroup) private var heroFontIdentifier: String = ""

    private var tabBinding: Binding<FluxTab>?
    private var onSettingsTap: (() -> Void)?
    private var onTabActivate: ((FluxTab) -> Void)?

    init(
        viewModel: DashboardViewModel,
        tab: Binding<FluxTab>? = nil,
        onSettingsTap: (() -> Void)? = nil,
        onTabActivate: ((FluxTab) -> Void)? = nil
    ) {
        _viewModel = State(initialValue: viewModel)
        tabBinding = tab
        self.onSettingsTap = onSettingsTap
        self.onTabActivate = onTabActivate
    }

    init(
        apiClient: any FluxAPIClient,
        tab: Binding<FluxTab>? = nil,
        onSettingsTap: (() -> Void)? = nil,
        onTabActivate: ((FluxTab) -> Void)? = nil
    ) {
        _viewModel = State(initialValue: DashboardViewModel(apiClient: apiClient))
        tabBinding = tab
        self.onSettingsTap = onSettingsTap
        self.onTabActivate = onTabActivate
    }

    var body: some View {
        contentContainer
            .fluxScreenBackground()
        #if os(iOS)
            .toolbar(.hidden, for: .navigationBar)
        #endif
        #if os(macOS)
        .task {
            await viewModel.runAutoRefresh()
        }
        .macRefreshAction { [viewModel] in
            await viewModel.refresh()
        }
        .modifier(AppearsActiveMonitor(viewModel: viewModel))
        #else
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
        #endif
    }

    @ViewBuilder
    private var contentContainer: some View {
        ScrollView {
            dashboardContent
        }
        .scrollContentBackground(.hidden)
        .scrollBounceBehavior(.basedOnSize)
    }

    @ViewBuilder
    private var dashboardContent: some View {
        VStack(alignment: .leading, spacing: FluxTheme.Metrics.panelGap) {
            if let tabBinding {
                FluxScreenHeader(
                    selection: tabBinding,
                    onSettingsTap: onSettingsTap,
                    onTabActivate: onTabActivate
                )
            } else {
                legacyHeader
            }

            if viewModel.error != nil {
                stalenessBanner
            }

            DashboardHeroPanel(
                live: viewModel.status?.live,
                rolling15min: viewModel.status?.rolling15min,
                heroFont: heroFont
            )

            LiveTrioPanel(live: viewModel.status?.live)

            SummaryBlock(
                todayEnergy: viewModel.status?.todayEnergy,
                offpeakGridImport: viewModel.status?.offpeak?.gridUsageKwh,
                showsBatteryCycle: false,
                avgLoadWatts: viewModel.status?.rolling15min?.avgLoad
            )

            // `low24h` tracks the lowest SoC since the last off-peak end
            // (per T-1084) — the existing "lowest since charged" signal
            // the V4 dashboard surfaced.
            BatteryBlock(
                title: nil,
                batteryCharge: viewModel.status?.todayEnergy?.eCharge,
                batteryDischarge: viewModel.status?.todayEnergy?.eDischarge,
                lowestSOC: viewModel.status?.battery?.low24h?.soc,
                lowestSOCTimestamp: (viewModel.status?.battery?.low24h?.timestamp)
                    .flatMap(DateFormatting.parseTimestamp),
                offpeakBatteryDeltaPercent: viewModel.status?.offpeak?.batteryDeltaPercent
            )
        }
        .padding(.horizontal, FluxTheme.Metrics.screenHorizontalPadding)
        .padding(.bottom, FluxTheme.Metrics.screenBottomPadding)
    }

    private var heroFont: HeroFontChoice {
        HeroFontChoice(rawValue: heroFontIdentifier) ?? .default
    }

    private var eyebrow: String {
        let now = Date()
        let time = DateFormatting.clockTime(from: now)
        let date = DashboardEyebrowFormatter.short.string(from: now)
        return "Now · \(time) · \(date)"
    }

    private var trailingTime: String {
        DateFormatting.clockTime(from: Date())
    }

    @ViewBuilder
    private var legacyHeader: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(eyebrow.uppercased())
                .font(FluxTheme.Typography.eyebrow)
                .tracking(1.6)
                .foregroundStyle(FluxTheme.Palette.tertiaryText)
            Text("Battery")
                .font(FluxTheme.Typography.pageTitle)
                .tracking(-0.6)
                .foregroundStyle(FluxTheme.Palette.primaryText)
        }
        .padding(.top, 6)
    }

    @ViewBuilder
    private var stalenessBanner: some View {
        FluxPanel {
            VStack(alignment: .leading, spacing: 8) {
                Label(stalenessTitle, systemImage: "exclamationmark.triangle.fill")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.orange)

                if let error = viewModel.error {
                    Text(error.message)
                        .font(.caption)
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                }

                if let lastSuccessfulFetch = viewModel.lastSuccessfulFetch {
                    Text("Last updated \(lastSuccessfulFetch, style: .relative) ago")
                        .font(.caption)
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                }

                HStack {
                    Button("Retry") {
                        Task { await viewModel.refresh() }
                    }
                    .buttonStyle(.borderedProminent)

                    #if os(macOS)
                    SettingsLink {
                        Text("Settings")
                    }
                    .buttonStyle(.bordered)
                    #else
                    Button("Settings") {
                        showingSettings = true
                    }
                    .buttonStyle(.bordered)
                    #endif
                }
            }
        }
    }

    private var stalenessTitle: String {
        if case .some(.unauthorized) = viewModel.error {
            return "Authentication required"
        }
        if viewModel.status == nil {
            return "Unable to load data"
        }
        return "Showing stale data"
    }
}

private enum DashboardEyebrowFormatter {
    static let short: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateFormat = "MMM d"
        return formatter
    }()
}

#if DEBUG
#Preview {
    DashboardView(apiClient: MockFluxAPIClient.preview)
}
#endif
