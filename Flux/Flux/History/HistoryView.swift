import FluxCore
import SwiftData
import SwiftUI

struct HistoryView: View {
    @State private var viewModel: HistoryViewModel
    @State private var selectedRange: Int = 7
    @State private var showingSettings = false

    private let makeDayDetailViewModel: (String) -> DayDetailViewModel

    init(apiClient: any FluxAPIClient, modelContext: ModelContext) {
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)
        _viewModel = State(initialValue: viewModel)
        makeDayDetailViewModel = { date in
            DayDetailViewModel(date: date, apiClient: apiClient)
        }
    }

    init(
        viewModel: HistoryViewModel,
        makeDayDetailViewModel: @escaping (String) -> DayDetailViewModel
    ) {
        _viewModel = State(initialValue: viewModel)
        self.makeDayDetailViewModel = makeDayDetailViewModel
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Picker("Range", selection: $selectedRange) {
                    Text("7d").tag(7)
                    Text("14d").tag(14)
                    Text("30d").tag(30)
                }
                .pickerStyle(.segmented)

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

                        HistoryBatteryCard(
                            entries: derived.battery,
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
            .padding()
        }
        .navigationTitle("History")
        .task {
            await viewModel.loadHistory(days: selectedRange)
        }
        .onChange(of: selectedRange) { _, newRange in
            Task { await viewModel.loadHistory(days: newRange) }
        }
        .refreshable {
            await viewModel.loadHistory(days: selectedRange)
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

    private func selectDay(_ dayID: String) {
        if let day = viewModel.days.first(where: { $0.date == dayID }) {
            viewModel.selectDay(day)
        }
    }

    private func summaryCard(for day: DayEnergy) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(day.date)
                .font(.headline)

            ForEach(EnergySummaryFormatter.rows(for: day)) { row in
                summaryRow(row)
            }

            NavigationLink("View day detail") {
                DayDetailView(viewModel: makeDayDetailViewModel(day.date))
            }
            .font(.headline)
            .padding(.top, 4)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var emptyState: some View {
        VStack(alignment: .center, spacing: 8) {
            Image(systemName: "chart.bar.xaxis")
                .font(.title2)
                .foregroundStyle(.secondary)
            Text("No data available")
                .font(.headline)
            Text("History data will appear once the backend has daily totals.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(24)
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func summaryRow(_ row: EnergySummaryRow) -> some View {
        HStack {
            Text(row.title)
                .foregroundStyle(.secondary)
            Spacer()
            Text(row.value)
        }
        .font(.subheadline)
    }

    private func errorState(_ error: FluxAPIError) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("Unable to load history", systemImage: "wifi.exclamationmark")
                .font(.headline)
            Text(error.message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            HStack {
                Button("Retry") {
                    Task { await viewModel.loadHistory(days: selectedRange) }
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
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }
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
