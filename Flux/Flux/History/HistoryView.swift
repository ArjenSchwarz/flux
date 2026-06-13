import FluxCore
import SwiftData
import SwiftUI

// swiftlint:disable type_body_length
struct HistoryView: View {
    /// Range control segments, in the order required by the spec ([6.1]).
    static let rangeOptions: [HistoryRange] = [.days(7), .days(14), .days(30), .weekToDate, .monthToDate]

    @Environment(\.horizontalSizeClass) private var hSizeClass
    @State private var viewModel: HistoryViewModel
    @State private var selectedRange: HistoryRange = .days(7)
    @State private var showingSettings = false

    private let makeDayDetailViewModel: (String) -> DayDetailViewModel
    private var tabBinding: Binding<FluxTab>?
    private var onSettingsTap: (() -> Void)?
    private var onTabActivate: ((FluxTab) -> Void)?

    init(
        apiClient: any FluxAPIClient,
        modelContext: ModelContext,
        tab: Binding<FluxTab>? = nil,
        onSettingsTap: (() -> Void)? = nil,
        onTabActivate: ((FluxTab) -> Void)? = nil
    ) {
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)
        _viewModel = State(initialValue: viewModel)
        makeDayDetailViewModel = { date in
            DayDetailViewModel(date: date, apiClient: apiClient)
        }
        tabBinding = tab
        self.onSettingsTap = onSettingsTap
        self.onTabActivate = onTabActivate
    }

    init(
        viewModel: HistoryViewModel,
        makeDayDetailViewModel: @escaping (String) -> DayDetailViewModel,
        tab: Binding<FluxTab>? = nil,
        onSettingsTap: (() -> Void)? = nil,
        onTabActivate: ((FluxTab) -> Void)? = nil
    ) {
        _viewModel = State(initialValue: viewModel)
        self.makeDayDetailViewModel = makeDayDetailViewModel
        tabBinding = tab
        self.onSettingsTap = onSettingsTap
        self.onTabActivate = onTabActivate
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if let tabBinding {
                    FluxScreenHeader(
                        selection: tabBinding,
                        onSettingsTap: onSettingsTap,
                        onTabActivate: onTabActivate
                    )
                }

                Picker("Range", selection: $selectedRange) {
                    ForEach(HistoryView.rangeOptions, id: \.self) { range in
                        Text(range.pickerLabel).tag(range)
                    }
                }
                .pickerStyle(.segmented)

                // Period navigation is Wk/Mo only (req 1.5); fixed ranges keep
                // their existing chrome untouched.
                if isPeriodNavigable, let period = viewModel.displayedPeriod {
                    HistoryPeriodHeader(
                        range: viewModel.resolvedRange,
                        period: period,
                        isViewingCurrentPeriod: viewModel.isViewingCurrentPeriod,
                        pickerUpperBound: viewModel.sydneyTodayEnd,
                        onPrevious: { Task { await viewModel.navigatePrevious() } },
                        onNext: { Task { await viewModel.navigateNext() } },
                        onJump: { date in Task { await viewModel.jumpTo(date: date) } },
                        onReturnToCurrent: { Task { await viewModel.returnToCurrent() } }
                    )
                }

                if viewModel.showsEmptyPeriodNotice {
                    noDataForPeriodNotice
                } else {
                    NoteRowView(text: viewModel.selectedDay?.note)
                }

                if viewModel.days.isEmpty, let error = viewModel.error, !viewModel.isLoading {
                    errorState(error)
                } else if viewModel.days.isEmpty, !viewModel.isLoading, !viewModel.showsEmptyPeriodNotice {
                    emptyState
                } else {
                    // An empty past period falls through here on purpose: the
                    // cards stay rendered and the HistoryChartDomain scaffold
                    // reserves the full-period axis (req 1.6), with the
                    // compact notice above replacing the note row — never the
                    // replace-everything emptyState.
                    if usesRegularLayout {
                        historyContentRegular
                    } else {
                        historyContent
                    }
                }
            }
            .padding(.horizontal, FluxTheme.Metrics.screenHorizontalPadding)
            .padding(.bottom, FluxTheme.Metrics.screenBottomPadding)
        }
        .scrollContentBackground(.hidden)
        .scrollBounceBehavior(.basedOnSize)
        .fluxScreenBackground()
        #if os(iOS)
        .toolbar(tabBinding == nil ? .visible : .hidden, for: .navigationBar)
        #endif
        .navigationTitle("History")
        .navigationDestination(for: HistoryRoute.self) { route in
            switch route {
            case .dayDetail(let date):
                DayDetailView(
                    viewModel: makeDayDetailViewModel(date),
                    tab: tabBinding,
                    onSettingsTap: onSettingsTap,
                    onTabActivate: onTabActivate
                )
            }
        }
        .task {
            async let history: Void = viewModel.selectRange(selectedRange)
            async let pricing: Void = viewModel.refreshPricing()
            _ = await (history, pricing)
        }
        .onChange(of: selectedRange) { _, newRange in
            Task {
                async let history: Void = viewModel.selectRange(newRange)
                async let pricing: Void = viewModel.refreshPricing()
                _ = await (history, pricing)
            }
        }
        #if os(macOS)
        .macRefreshAction { [viewModel] in
            await viewModel.reload()
        }
        .focusable()
        .onKeyPress(.leftArrow) {
            guard isPeriodNavigable else { return .ignored }
            Task { await viewModel.navigatePrevious() }
            return .handled
        }
        .onKeyPress(.rightArrow) {
            guard isPeriodNavigable, !viewModel.isViewingCurrentPeriod else { return .ignored }
            Task { await viewModel.navigateNext() }
            return .handled
        }
        #endif
        // reload() rather than loadHistory(range: selectedRange): refresh
        // re-fetches the displayed period — including a navigated past one
        // (req 1.8) — and a stale `selectedRange` capture can never diverge
        // from the view model's `lastRequestedRange`.
        .refreshable {
            await viewModel.reload()
        }
        #if !os(macOS)
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

    private var usesRegularLayout: Bool { IPadLayoutGate.isActive(hSizeClass: hSizeClass) }

    /// Period navigation exists only for the calendar-anchored ranges (req 1.5).
    private var isPeriodNavigable: Bool {
        selectedRange == .weekToDate || selectedRange == .monthToDate
    }

    /// Compact no-data notice for a successfully fetched but empty past period
    /// (req 1.6) — replaces the note row, visually distinct from `errorState`.
    private var noDataForPeriodNotice: some View {
        FluxPanel {
            Label("No data for this period", systemImage: "calendar.badge.exclamationmark")
                .appFontSystem(size: 13)
                .foregroundStyle(FluxTheme.Palette.secondaryText)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    @ViewBuilder
    private var historyContent: some View {
        let derived = viewModel.derived
        // Captured once like `derived`: each access re-runs the interval math.
        let chartDomain = viewModel.chartDomain
        let selectedDate = viewModel.selectedDay.flatMap { DateFormatting.parseDayDate($0.date) }
        VStack(alignment: .leading, spacing: 16) {
            statsOverviewCard(derived: derived)
            if let periodCosts = viewModel.periodCosts {
                HistoryPeriodCostsCard(costs: periodCosts)
            }
            solarCard(derived: derived, selectedDate: selectedDate, chartDomain: chartDomain)
            gridUsageCard(derived: derived, selectedDate: selectedDate, chartDomain: chartDomain)
            dailyUsageCard(derived: derived, selectedDate: selectedDate, chartDomain: chartDomain)
            if let selectedDay = viewModel.selectedDay {
                summaryCard(for: selectedDay)
            }
        }
        .opacity(viewModel.isLoading ? 0.5 : 1.0)
        .animation(.easeInOut(duration: 0.15), value: viewModel.isLoading)
    }

    @ViewBuilder
    private var historyContentRegular: some View {
        let derived = viewModel.derived
        let chartDomain = viewModel.chartDomain
        let selectedDate = viewModel.selectedDay.flatMap { DateFormatting.parseDayDate($0.date) }
        VStack(alignment: .leading, spacing: 16) {
            statsOverviewCard(derived: derived)
            if let periodCosts = viewModel.periodCosts {
                HistoryPeriodCostsCard(costs: periodCosts)
            }
            AdaptiveColumnsLayout {
                solarCard(derived: derived, selectedDate: selectedDate, chartDomain: chartDomain)
                gridUsageCard(derived: derived, selectedDate: selectedDate, chartDomain: chartDomain)
                dailyUsageCard(derived: derived, selectedDate: selectedDate, chartDomain: chartDomain)
                if let selectedDay = viewModel.selectedDay {
                    summaryCard(for: selectedDay)
                }
            }
        }
        .opacity(viewModel.isLoading ? 0.5 : 1.0)
        .animation(.easeInOut(duration: 0.15), value: viewModel.isLoading)
    }

    @ViewBuilder
    private func statsOverviewCard(derived: HistoryViewModel.DerivedState) -> some View {
        HistoryStatsOverviewCard(
            summary: derived.summary,
            entries: derived.solar,
            periodDays: viewModel.pastPeriodDayCount,
            onSelect: selectDay
        )
    }

    @ViewBuilder
    private func solarCard(
        derived: HistoryViewModel.DerivedState, selectedDate: Date?, chartDomain: HistoryChartDomain?
    ) -> some View {
        HistorySolarCard(
            entries: derived.solar,
            summary: derived.summary,
            selectedDate: selectedDate,
            periodQuery: viewModel.periodQuery,
            chartDomain: chartDomain,
            onSelect: selectDay
        )
    }

    @ViewBuilder
    private func gridUsageCard(
        derived: HistoryViewModel.DerivedState, selectedDate: Date?, chartDomain: HistoryChartDomain?
    ) -> some View {
        HistoryGridUsageCard(
            entries: derived.grid,
            summary: derived.summary,
            selectedDate: selectedDate,
            periodQuery: viewModel.periodQuery,
            chartDomain: chartDomain,
            onSelect: selectDay
        )
    }

    @ViewBuilder
    private func dailyUsageCard(
        derived: HistoryViewModel.DerivedState, selectedDate: Date?, chartDomain: HistoryChartDomain?
    ) -> some View {
        HistoryDailyUsageCard(
            entries: derived.dailyUsage,
            summary: derived.summary,
            selectedDate: selectedDate,
            periodQuery: viewModel.periodQuery,
            chartDomain: chartDomain,
            onSelect: selectDay
        )
    }

    private func selectDay(_ dayID: String) {
        if let day = viewModel.days.first(where: { $0.date == dayID }) {
            viewModel.selectDay(day)
        }
    }

    private func summaryCard(for day: DayEnergy) -> some View {
        VStack(alignment: .leading, spacing: FluxTheme.Metrics.panelGap) {
            SummaryBlock(
                title: "Power",
                trailing: shortDate(day.date),
                day: day,
                showsBatteryCycle: false
            )

            // Value-based NavigationLink so the enclosing NavigationStack(path:)
            // owns the push and our root-level path-reset on tab activate
            // actually pops back to the History list.
            NavigationLink(value: HistoryRoute.dayDetail(day.date)) {
                FluxPanel {
                    HStack {
                        Text("View day detail")
                            .appFont { FluxTheme.Typography.statRowLabel(family: $0).weight(.semibold) }
                            .foregroundStyle(FluxTheme.Palette.primaryText)
                        Spacer()
                        Image(systemName: "chevron.right")
                            .appFontSystem(size: 13, weight: .semibold)
                            .foregroundStyle(FluxTheme.Palette.tertiaryText)
                    }
                }
            }
            .buttonStyle(.plain)
        }
    }

    private func shortDate(_ date: String) -> String {
        guard let parsed = DateFormatting.parseDayDate(date) else { return date }
        return HistorySummaryDateFormatter.short.string(from: parsed)
    }

    private var emptyState: some View {
        VStack(alignment: .center, spacing: 8) {
            Image(systemName: "chart.bar.xaxis")
                .appFont(.title2)
                .foregroundStyle(.secondary)
            Text("No data available")
                .appFont(.headline)
            Text("History data will appear once the backend has daily totals.")
                .appFont(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(24)
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func errorState(_ error: FluxAPIError) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("Unable to load history", systemImage: "wifi.exclamationmark")
                .appFont(.headline)
            Text(error.message)
                .appFont(.subheadline)
                .foregroundStyle(.secondary)
            HStack {
                // reload() keeps a navigated past period in place on retry
                // (req 1.8) — see the .refreshable note above.
                Button("Retry") {
                    Task { await viewModel.reload() }
                }
                .buttonStyle(.borderedProminent)

                if error.suggestsSettings {
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
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }
}

// swiftlint:enable type_body_length

enum HistoryRoute: Hashable {
    case dayDetail(String)
}

private enum HistorySummaryDateFormatter {
    static let short: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateFormat = "MMM d"
        return formatter
    }()
}
