import FluxCore
import SwiftUI

/// V5 "Today" screen. Wraps the existing chart implementations
/// (PowerChartView / BatteryPowerChartView / SOCChartView) in V5 panels and
/// adds the Summary, Off-peak, and "five blocks" panels.
struct DayDetailView: View {
    @State private var viewModel: DayDetailViewModel
    @State private var showingSettings = false
    @State private var editingNote = false

    @AppStorage(UserDefaults.compareEnabledKey, store: UserDefaults.fluxAppGroup)
    private var compareEnabled: Bool = false

    @AppStorage(UserDefaults.comparePeriodKey, store: UserDefaults.fluxAppGroup)
    private var comparePeriodRaw: String = ComparePeriod.yesterday.rawValue

    private var comparePeriod: Binding<ComparePeriod> {
        Binding(
            get: { ComparePeriod.parseOrDefault(comparePeriodRaw) },
            set: { comparePeriodRaw = $0.rawValue }
        )
    }

    private var tabBinding: Binding<FluxTab>?
    private var onSettingsTap: (() -> Void)?
    private var onTabActivate: ((FluxTab) -> Void)?

    init(
        date: String,
        apiClient: any FluxAPIClient,
        tab: Binding<FluxTab>? = nil,
        onSettingsTap: (() -> Void)? = nil,
        onTabActivate: ((FluxTab) -> Void)? = nil
    ) {
        _viewModel = State(initialValue: DayDetailViewModel(date: date, apiClient: apiClient))
        tabBinding = tab
        self.onSettingsTap = onSettingsTap
        self.onTabActivate = onTabActivate
    }

    init(
        viewModel: DayDetailViewModel,
        tab: Binding<FluxTab>? = nil,
        onSettingsTap: (() -> Void)? = nil,
        onTabActivate: ((FluxTab) -> Void)? = nil
    ) {
        _viewModel = State(initialValue: viewModel)
        tabBinding = tab
        self.onSettingsTap = onSettingsTap
        self.onTabActivate = onTabActivate
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FluxTheme.Metrics.panelGap) {
                header
                DayNavigationHeader(viewModel: viewModel)
                DayDetailNoteSection(viewModel: viewModel, editingNote: $editingNote)

                CompareControl(
                    enabled: $compareEnabled,
                    period: comparePeriod,
                    unavailable: viewModel.comparisonState.isUnavailable
                )

                if let dailyUsage = viewModel.dailyUsage, !dailyUsage.blocks.isEmpty {
                    DayInFiveBlocksPanel(dailyUsage: dailyUsage,
                                         compare: viewModel.comparisonState)
                }

                contentSection

                SummaryBlock(
                    title: "Power",
                    trailing: trailingSummaryDate,
                    summary: viewModel.summary,
                    // Prefer the canonical server value (DaySummary now carries
                    // it for any date with an off-peak record); fall back to
                    // the readings-derived approximation when the server
                    // hasn't returned a split.
                    offpeakGridImport: viewModel.summary?.offpeakGridImportKwh ?? viewModel.offpeakStats.gridImportKwh,
                    showsBatteryCycle: false,
                    compare: viewModel.comparisonState
                )
                BatteryBlock(
                    batteryCharge: viewModel.summary?.eCharge,
                    batteryDischarge: viewModel.summary?.eDischarge,
                    lowestSOC: viewModel.offpeakStats.lowestSOC,
                    lowestSOCTimestamp: viewModel.offpeakStats.lowestSOCTimestamp
                )
            }
            .padding(.horizontal, FluxTheme.Metrics.screenHorizontalPadding)
            .padding(.bottom, FluxTheme.Metrics.screenBottomPadding)
        }
        .scrollContentBackground(.hidden)
        .fluxScreenBackground()
        #if os(iOS)
        .toolbar(.hidden, for: .navigationBar)
        #endif
        .task(id: viewModel.date) {
            await viewModel.loadDay()
        }
        // All three reactions call the same updateCompare with the same args;
        // the `.onChange(of: viewModel.date)` reaction fires unconditionally
        // on day navigation, and the early-`.off` short-circuit inside
        // `updateCompare` is what makes it safe when Compare is disabled.
        .onChange(of: compareEnabled, initial: true) { _, _ in triggerCompareUpdate() }
        .onChange(of: comparePeriodRaw) { _, _ in triggerCompareUpdate() }
        .onChange(of: viewModel.date) { _, _ in triggerCompareUpdate() }
        #if os(macOS)
        .macRefreshAction { [viewModel] in
            await viewModel.loadDay()
        }
        .focusable()
        .onKeyPress(.leftArrow) {
            viewModel.navigatePrevious()
            return .handled
        }
        .onKeyPress(.rightArrow) {
            guard !viewModel.isToday else { return .ignored }
            viewModel.navigateNext()
            return .handled
        }
        #endif
        #if !os(macOS)
        .sheet(isPresented: $showingSettings) {
            NavigationStack {
                SettingsView()
                    .toolbar {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button("Done") { showingSettings = false }
                        }
                    }
            }
        }
        #endif
        .sheet(isPresented: $editingNote) {
            NoteEditorSheet(
                viewModel: NoteEditorViewModel(initial: viewModel.note ?? "", parent: viewModel)
            )
        }
    }

    @ViewBuilder
    private var header: some View {
        if let tabBinding {
            // The Day Detail view always represents the "Details" tab visually,
            // even when pushed from History. The setter still writes through to
            // the real tab state so cross-tab taps switch correctly.
            let displaySelection = Binding<FluxTab>(
                get: { .today },
                set: { tabBinding.wrappedValue = $0 }
            )
            FluxScreenHeader(
                selection: displaySelection,
                onSettingsTap: onSettingsTap,
                onTabActivate: onTabActivate
            )
        } else {
            // macOS keeps the eyebrow + title since the sidebar handles tabs.
            VStack(alignment: .leading, spacing: 2) {
                Text(eyebrow.uppercased())
                    .appFont(FluxTheme.Typography.eyebrow)
                    .tracking(1.6)
                    .foregroundStyle(FluxTheme.Palette.tertiaryText)
                Text(pageTitle)
                    .appFont(FluxTheme.Typography.pageTitle)
                    .tracking(-0.6)
                    .foregroundStyle(FluxTheme.Palette.primaryText)
            }
            .padding(.top, 6)
        }
    }

    @ViewBuilder
    private var contentSection: some View {
        if !viewModel.parsedReadings.isEmpty {
            if viewModel.hasPowerData {
                DayDetailPanels.power(date: viewModel.date, readings: viewModel.parsedReadings)
                DayDetailPanels.battery(date: viewModel.date,
                                        readings: viewModel.parsedReadings,
                                        summary: viewModel.summary)
            } else {
                DayDetailMessagePanel(title: "Power charts unavailable",
                                      detail: "This day has fallback data with SOC readings only.")
                DayDetailPanels.battery(date: viewModel.date,
                                        readings: viewModel.parsedReadings,
                                        summary: viewModel.summary)
            }
        } else if let error = viewModel.error {
            DayDetailErrorPanel(error: error,
                                showingSettings: $showingSettings,
                                onRetry: { Task { await viewModel.loadDay() } })
        } else if viewModel.isLoading {
            FluxPanel {
                HStack {
                    Spacer()
                    ProgressView("Loading day data…")
                        .tint(FluxTheme.Palette.primaryText)
                    Spacer()
                }
            }
        } else if viewModel.peakPeriods.isEmpty,
                  viewModel.dailyUsage?.blocks.isEmpty ?? true,
                  viewModel.summary == nil {
            DayDetailMessagePanel(title: "No readings available",
                                  detail: "Try a different day or pull to refresh.")
        }
    }

    private var eyebrow: String {
        guard let parsedDate = DateFormatting.parseDayDate(viewModel.date) else {
            return viewModel.date
        }
        return DayDetailEyebrow.formatter.string(from: parsedDate)
    }

    private var pageTitle: String {
        viewModel.isToday ? "Today" : eyebrow
    }

    private var trailingSummaryDate: String {
        guard let parsedDate = DateFormatting.parseDayDate(viewModel.date) else {
            return viewModel.date
        }
        return DayDetailEyebrow.summaryDate.string(from: parsedDate)
    }

    private func triggerCompareUpdate() {
        viewModel.updateCompare(enabled: compareEnabled, period: comparePeriod.wrappedValue)
    }

}

#if DEBUG
#Preview {
    NavigationStack {
        DayDetailView(date: MockFluxAPIClient.previewDate, apiClient: MockFluxAPIClient.preview)
    }
}
#endif
