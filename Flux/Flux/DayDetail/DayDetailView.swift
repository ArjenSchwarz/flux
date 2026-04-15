import SwiftUI

struct DayDetailView: View {
    @State private var viewModel: DayDetailViewModel
    @State private var showingSettings = false

    init(date: String, apiClient: any FluxAPIClient) {
        _viewModel = State(initialValue: DayDetailViewModel(date: date, apiClient: apiClient))
    }

    init(viewModel: DayDetailViewModel) {
        _viewModel = State(initialValue: viewModel)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                dayNavigationHeader

                if !viewModel.readings.isEmpty {
                    SOCChartView(date: viewModel.date, readings: viewModel.readings, summary: viewModel.summary)

                    if viewModel.hasPowerData {
                        PowerChartView(date: viewModel.date, readings: viewModel.readings)
                        BatteryPowerChartView(date: viewModel.date, readings: viewModel.readings)
                    } else {
                        noPowerDataCard
                    }
                } else if let error = viewModel.error {
                    errorCard(error: error)
                } else if viewModel.isLoading {
                    ProgressView("Loading day data…")
                        .frame(maxWidth: .infinity)
                        .padding()
                } else {
                    emptyStateCard
                }

                summaryCard
            }
            .padding()
        }
        .navigationTitle("Day Detail")
        .task(id: viewModel.date) {
            await viewModel.loadDay()
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

    private var dayNavigationHeader: some View {
        HStack {
            Button {
                viewModel.navigatePrevious()
            } label: {
                Image(systemName: "chevron.left")
            }
            .buttonStyle(.bordered)

            Spacer()

            Text(formattedDate)
                .font(.headline)

            Spacer()

            Button {
                viewModel.navigateNext()
            } label: {
                Image(systemName: "chevron.right")
            }
            .buttonStyle(.bordered)
            .disabled(viewModel.isToday)
        }
    }

    private var formattedDate: String {
        guard let parsedDate = DateFormatting.parseDayDate(viewModel.date) else {
            return viewModel.date
        }
        return DayDetailDateFormatters.headerFormatter.string(from: parsedDate)
    }

    private var summaryCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Summary")
                .font(.headline)

            summaryRow("Solar", viewModel.summary?.epv)
            summaryRow("Grid imported", viewModel.summary?.eInput)
            summaryRow("Grid exported", viewModel.summary?.eOutput)
            summaryRow("Battery charged", viewModel.summary?.eCharge)
            summaryRow("Battery discharged", viewModel.summary?.eDischarge)

            HStack {
                Text("SOC low")
                    .foregroundStyle(.secondary)
                Spacer()
                Text(socLowText)
            }
            .font(.subheadline)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var socLowText: String {
        guard let low = viewModel.summary?.socLow else { return "—" }
        let lowText = String(format: "%.1f", low)
        if let lowTimeString = viewModel.summary?.socLowTime,
           let lowTime = DateFormatting.parseTimestamp(lowTimeString)
        {
            return "\(lowText)% at \(DateFormatting.clockTime(from: lowTime))"
        }
        return "\(lowText)%"
    }

    private func summaryRow(_ title: String, _ value: Double?) -> some View {
        HStack {
            Text(title)
                .foregroundStyle(.secondary)
            Spacer()
            Text(value.map { "\(String(format: "%.2f", $0)) kWh" } ?? "—")
        }
        .font(.subheadline)
    }

    private var noPowerDataCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Power Charts Unavailable")
                .font(.headline)
            Text("This day has fallback data with SOC readings only.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var emptyStateCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("No readings available")
                .font(.headline)
            Text("Try a different day or pull to refresh.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func errorCard(error: FluxAPIError) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Unable to load day data")
                .font(.headline)
            Text(error.message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            HStack {
                Button("Retry") {
                    Task {
                        await viewModel.loadDay()
                    }
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

private enum DayDetailDateFormatters {
    static let headerFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateStyle = .full
        formatter.timeStyle = .none
        return formatter
    }()
}

#if DEBUG
#Preview {
    NavigationStack {
        DayDetailView(date: MockFluxAPIClient.previewDate, apiClient: MockFluxAPIClient.preview)
    }
}
#endif
