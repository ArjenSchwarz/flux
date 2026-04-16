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

                if !viewModel.parsedReadings.isEmpty {
                    if viewModel.hasPowerData {
                        PowerChartView(date: viewModel.date, readings: viewModel.parsedReadings)
                        BatteryPowerChartView(date: viewModel.date, readings: viewModel.parsedReadings)
                    } else {
                        noPowerDataCard
                    }

                    SOCChartView(date: viewModel.date, readings: viewModel.parsedReadings, summary: viewModel.summary)
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
            pairedSummaryRow(
                title: "Grid",
                positive: viewModel.summary?.eInput,
                positiveLabel: "import",
                negative: viewModel.summary?.eOutput,
                negativeLabel: "export"
            )
            pairedSummaryRow(
                title: "Battery",
                positive: viewModel.summary?.eCharge,
                positiveLabel: "+",
                negative: viewModel.summary?.eDischarge,
                negativeLabel: "-"
            )

            HStack {
                Text("24h low")
                    .foregroundStyle(.secondary)
                Spacer()
                Text(lowText)
            }
            .font(.subheadline)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var lowText: String {
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
            Text(formatKwh(value))
        }
        .font(.subheadline)
    }

    private func pairedSummaryRow(
        title: String,
        positive: Double?,
        positiveLabel: String,
        negative: Double?,
        negativeLabel: String
    ) -> some View {
        HStack {
            Text("\(title) (\(positiveLabel)/\(negativeLabel))")
                .foregroundStyle(.secondary)
            Spacer()
            Text("\(formatKwh(positive)) / \(formatKwh(negative))")
        }
        .font(.subheadline)
    }

    private func formatKwh(_ value: Double?) -> String {
        guard let value else { return "—" }
        return String(format: "%.2f kWh", value)
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
