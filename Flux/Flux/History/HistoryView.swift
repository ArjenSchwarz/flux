import SwiftData
import SwiftUI

struct HistoryView: View {
    @State private var viewModel: HistoryViewModel
    @State private var selectedRange: Int
    @State private var showingSettings = false

    private let makeDayDetailViewModel: (String) -> DayDetailViewModel

    init(apiClient: any FluxAPIClient, modelContext: ModelContext) {
        let viewModel = HistoryViewModel(apiClient: apiClient, modelContext: modelContext)
        _viewModel = State(initialValue: viewModel)
        _selectedRange = State(initialValue: viewModel.selectedDayRange)
        makeDayDetailViewModel = { date in
            DayDetailViewModel(date: date, apiClient: apiClient)
        }
    }

    init(
        viewModel: HistoryViewModel,
        makeDayDetailViewModel: @escaping (String) -> DayDetailViewModel
    ) {
        _viewModel = State(initialValue: viewModel)
        _selectedRange = State(initialValue: viewModel.selectedDayRange)
        self.makeDayDetailViewModel = makeDayDetailViewModel
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HistoryChartView(
                    selectedRange: $selectedRange,
                    chartDays: viewModel.chartDays,
                    chartEntries: viewModel.chartEntries,
                    selectedDay: viewModel.selectedDay,
                    onSelectDay: viewModel.selectDay
                )
                .padding()
                .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))

                if let selectedDay = viewModel.selectedDay {
                    summaryCard(for: selectedDay)
                } else if let error = viewModel.error, viewModel.days.isEmpty, !viewModel.isLoading {
                    errorState(error)
                } else if !viewModel.isLoading {
                    emptyState
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

    private func summaryCard(for day: DayEnergy) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(day.date)
                .font(.headline)

            metricRow(title: "Solar", value: day.epv)
            metricRow(title: "Grid imported", value: day.eInput)
            metricRow(title: "Grid exported", value: day.eOutput)
            metricRow(title: "Battery charged", value: day.eCharge)
            metricRow(title: "Battery discharged", value: day.eDischarge)

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

    private func metricRow(title: String, value: Double) -> some View {
        HStack {
            Text(title)
                .foregroundStyle(.secondary)
            Spacer()
            Text("\(value, specifier: "%.2f") kWh")
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
