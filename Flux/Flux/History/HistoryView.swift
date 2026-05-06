import FluxCore
import SwiftData
import SwiftUI

struct HistoryView: View {
    @State private var viewModel: HistoryViewModel
    @State private var selectedRange: Int = 7
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
                    Text("7d").tag(7)
                    Text("14d").tag(14)
                    Text("30d").tag(30)
                }
                .pickerStyle(.segmented)

                NoteRowView(text: viewModel.selectedDay?.note)

                if viewModel.days.isEmpty, let error = viewModel.error, !viewModel.isLoading {
                    errorState(error)
                } else if viewModel.days.isEmpty, !viewModel.isLoading {
                    emptyState
                } else {
                    let derived = viewModel.derived
                    let selectedDate = viewModel.selectedDay
                        .flatMap { DateFormatting.parseDayDate($0.date) }

                    VStack(alignment: .leading, spacing: 16) {
                        HistorySolarCard(
                            entries: derived.solar,
                            summary: derived.summary,
                            selectedDate: selectedDate,
                            onSelect: selectDay
                        )

                        HistoryGridUsageCard(
                            entries: derived.grid,
                            summary: derived.summary,
                            selectedDate: selectedDate,
                            onSelect: selectDay
                        )

                        HistoryDailyUsageCard(
                            entries: derived.dailyUsage,
                            summary: derived.summary,
                            selectedDate: selectedDate,
                            onSelect: selectDay
                        )

                        if let selectedDay = viewModel.selectedDay {
                            summaryCard(for: selectedDay)
                        }
                    }
                    .opacity(viewModel.isLoading ? 0.5 : 1.0)
                    .animation(.easeInOut(duration: 0.15), value: viewModel.isLoading)
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
            await viewModel.loadHistory(days: selectedRange)
        }
        .onChange(of: selectedRange) { _, newRange in
            Task { await viewModel.loadHistory(days: newRange) }
        }
        #if os(macOS)
        .macRefreshAction { [viewModel] in
            await viewModel.reload()
        }
        #endif
        .refreshable {
            await viewModel.loadHistory(days: selectedRange)
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
                    Task { await viewModel.loadHistory(days: selectedRange) }
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
#Preview {
    let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
    // swiftlint:disable:next force_try
    let container = try! ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
    NavigationStack {
        HistoryView(apiClient: MockFluxAPIClient.preview, modelContext: ModelContext(container))
    }
}
#endif
