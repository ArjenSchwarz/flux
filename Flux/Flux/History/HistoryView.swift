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

                NoteRowView(text: viewModel.selectedDay?.note)

                if viewModel.days.isEmpty, let error = viewModel.error, !viewModel.isLoading {
                    errorState(error)
                } else if viewModel.days.isEmpty, !viewModel.isLoading {
                    emptyState
                } else {
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
            async let history: Void = viewModel.loadHistory(range: selectedRange)
            async let pricing: Void = viewModel.refreshPricing()
            _ = await (history, pricing)
        }
        .onChange(of: selectedRange) { _, newRange in
            Task {
                async let history: Void = viewModel.loadHistory(range: newRange)
                async let pricing: Void = viewModel.refreshPricing()
                _ = await (history, pricing)
            }
        }
        #if os(macOS)
        .macRefreshAction { [viewModel] in
            await viewModel.reload()
        }
        #endif
        .refreshable {
            await viewModel.loadHistory(range: selectedRange)
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

    @ViewBuilder
    private var historyContent: some View {
        let derived = viewModel.derived
        let selectedDate = viewModel.selectedDay.flatMap { DateFormatting.parseDayDate($0.date) }
        VStack(alignment: .leading, spacing: 16) {
            statsOverviewCard(derived: derived)
            if let periodCosts = viewModel.periodCosts {
                HistoryPeriodCostsCard(costs: periodCosts)
            }
            solarCard(derived: derived, selectedDate: selectedDate)
            gridUsageCard(derived: derived, selectedDate: selectedDate)
            dailyUsageCard(derived: derived, selectedDate: selectedDate)
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
        let selectedDate = viewModel.selectedDay.flatMap { DateFormatting.parseDayDate($0.date) }
        VStack(alignment: .leading, spacing: 16) {
            statsOverviewCard(derived: derived)
            if let periodCosts = viewModel.periodCosts {
                HistoryPeriodCostsCard(costs: periodCosts)
            }
            AdaptiveColumnsLayout {
                solarCard(derived: derived, selectedDate: selectedDate)
                gridUsageCard(derived: derived, selectedDate: selectedDate)
                dailyUsageCard(derived: derived, selectedDate: selectedDate)
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
            onSelect: selectDay
        )
    }

    @ViewBuilder
    private func solarCard(derived: HistoryViewModel.DerivedState, selectedDate: Date?) -> some View {
        HistorySolarCard(
            entries: derived.solar,
            summary: derived.summary,
            selectedDate: selectedDate,
            rangeDays: viewModel.resolvedRangeDays,
            chartDomain: viewModel.chartDomain,
            onSelect: selectDay
        )
    }

    @ViewBuilder
    private func gridUsageCard(derived: HistoryViewModel.DerivedState, selectedDate: Date?) -> some View {
        HistoryGridUsageCard(
            entries: derived.grid,
            summary: derived.summary,
            selectedDate: selectedDate,
            rangeDays: viewModel.resolvedRangeDays,
            chartDomain: viewModel.chartDomain,
            onSelect: selectDay
        )
    }

    @ViewBuilder
    private func dailyUsageCard(derived: HistoryViewModel.DerivedState, selectedDate: Date?) -> some View {
        HistoryDailyUsageCard(
            entries: derived.dailyUsage,
            summary: derived.summary,
            selectedDate: selectedDate,
            rangeDays: viewModel.resolvedRangeDays,
            chartDomain: viewModel.chartDomain,
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
                Button("Retry") {
                    Task { await viewModel.loadHistory(range: selectedRange) }
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

#if DEBUG
#Preview("Compact") {
    let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
    // swiftlint:disable:next force_try
    let container = try! ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
    NavigationStack {
        HistoryView(apiClient: MockFluxAPIClient.preview, modelContext: ModelContext(container))
    }
}

#Preview("Regular 770") {
    let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
    // swiftlint:disable:next force_try
    let container = try! ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
    NavigationStack {
        HistoryView(apiClient: MockFluxAPIClient.preview, modelContext: ModelContext(container))
    }
    .frame(width: 770)
    .environment(\.horizontalSizeClass, .regular)
}
#endif
