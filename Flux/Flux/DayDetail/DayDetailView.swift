import FluxCore
import SwiftUI

struct DayDetailView: View {
    @State private var viewModel: DayDetailViewModel
    @State private var showingSettings = false
    @State private var editingNote = false

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

                noteSection

                if !viewModel.parsedReadings.isEmpty {
                    if viewModel.hasPowerData {
                        PowerChartView(date: viewModel.date, readings: viewModel.parsedReadings)
                        BatteryPowerChartView(date: viewModel.date, readings: viewModel.parsedReadings)
                    } else {
                        noPowerDataCard
                    }

                    SOCChartView(date: viewModel.date, readings: viewModel.parsedReadings, summary: viewModel.summary)

                    if viewModel.hasPowerData && !viewModel.peakPeriods.isEmpty {
                        PeakUsageCard(periods: viewModel.peakPeriods)
                    }

                    if viewModel.hasPowerData,
                       let dailyUsage = viewModel.dailyUsage,
                       !dailyUsage.blocks.isEmpty {
                        DailyUsageCard(dailyUsage: dailyUsage)
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
        .sheet(isPresented: $editingNote) {
            NoteEditorSheet(
                viewModel: NoteEditorViewModel(initial: viewModel.note ?? "", parent: viewModel)
            )
        }
    }

    @ViewBuilder
    private var noteSection: some View {
        if isFutureDate {
            EmptyView()
        } else if let note = viewModel.note, !note.isEmpty {
            Button {
                editingNote = true
            } label: {
                NoteRowView(text: note)
            }
            .buttonStyle(.plain)
        } else {
            Button {
                editingNote = true
            } label: {
                Label("Add note", systemImage: "plus")
                    .font(.subheadline)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding()
                    .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            }
            .buttonStyle(.plain)
        }
    }

    private var isFutureDate: Bool {
        viewModel.date > DateFormatting.todayDateString()
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

            ForEach(EnergySummaryFormatter.rows(for: viewModel.summary)) { row in
                summaryRow(row)
            }

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
           let lowTime = DateFormatting.parseTimestamp(lowTimeString) {
            return "\(lowText)% at \(DateFormatting.clockTime(from: lowTime))"
        }
        return "\(lowText)%"
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
