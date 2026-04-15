import SwiftUI

struct DayDetailView: View {
    @State private var viewModel: DayDetailViewModel

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
        guard let parsedDate = DayDetailDateFormatters.dayFormatter.date(from: viewModel.date) else {
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
            Text(error.localizedDescription)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Button("Retry") {
                Task {
                    await viewModel.loadDay()
                }
            }
            .buttonStyle(.borderedProminent)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }
}

private enum DayDetailDateFormatters {
    static let dayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.timeZone = DateFormatting.sydneyTimeZone
        return formatter
    }()

    static let headerFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateStyle = .full
        formatter.timeStyle = .none
        return formatter
    }()
}

private actor PreviewDayDetailAPIClient: FluxAPIClient {
    func fetchStatus() async throws -> StatusResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchDay(date: String) async throws -> DayDetailResponse {
        var readings: [TimeSeriesPoint] = []
        readings.reserveCapacity(24)

        for hour in 0 ... 23 {
            let timestamp = "\(date)T\(String(format: "%02d", hour)):00:00Z"
            let solar = max(0, Double(1800 - abs(12 - hour) * 140))
            let load = Double(450 + hour * 12)
            let battery = Double((hour % 6) - 3) * 120
            let grid = Double((hour % 5) * 90)
            let soc = Double(70 - hour)
            readings.append(
                TimeSeriesPoint(
                    timestamp: timestamp,
                    ppv: solar,
                    pload: load,
                    pbat: battery,
                    pgrid: grid,
                    soc: soc
                )
            )
        }

        let summary = DaySummary(
            epv: 13.4,
            eInput: 2.2,
            eOutput: 4.9,
            eCharge: 5.1,
            eDischarge: 6.2,
            socLow: 18.3,
            socLowTime: "\(date)T19:00:00Z"
        )

        return DayDetailResponse(date: date, readings: readings, summary: summary)
    }
}

#Preview {
    NavigationStack {
        DayDetailView(date: "2026-04-15", apiClient: PreviewDayDetailAPIClient())
    }
}
