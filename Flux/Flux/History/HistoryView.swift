import SwiftData
import SwiftUI

struct HistoryView: View {
    @State private var viewModel: HistoryViewModel
    @State private var selectedRange: Int

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
}

private actor PreviewHistoryAPIClient: FluxAPIClient {
    func fetchStatus() async throws -> StatusResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        HistoryResponse(days: [
            DayEnergy(date: "2026-04-15", epv: 11.2, eInput: 2.3, eOutput: 3.1, eCharge: 4.2, eDischarge: 5.4),
            DayEnergy(date: "2026-04-14", epv: 9.7, eInput: 1.9, eOutput: 2.8, eCharge: 3.6, eDischarge: 4.9)
        ])
    }

    func fetchDay(date _: String) async throws -> DayDetailResponse {
        throw FluxAPIError.notConfigured
    }
}

#Preview {
    let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
    let container = try! ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
    NavigationStack {
        HistoryView(apiClient: PreviewHistoryAPIClient(), modelContext: ModelContext(container))
    }
}
