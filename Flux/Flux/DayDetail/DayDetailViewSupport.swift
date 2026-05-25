import FluxCore
import SwiftUI

enum DayDetailPanels {
    static func power(
        date: String,
        readings: [ParsedReading],
        selectedDate: Binding<Date?>
    ) -> some View {
        FluxPanel {
            VStack(alignment: .leading, spacing: 0) {
                FluxPanelHeader(label: "Power", right: "kW")
                PowerChartView(date: date, readings: readings, selectedDate: selectedDate)
                HStack(spacing: 14) {
                    legendChip(color: FluxTheme.Palette.amber, text: "Solar")
                    legendChip(color: FluxTheme.Palette.load, text: "House")
                    legendChip(color: FluxTheme.Palette.grid, text: "Grid")
                }
                .appFontSystem(size: 10)
                .foregroundStyle(FluxTheme.Palette.secondaryText)
                .padding(.top, 6)
            }
        }
    }

    static func battery(
        date: String,
        readings: [ParsedReading],
        summary: DaySummary?,
        selectedDate: Binding<Date?>
    ) -> some View {
        FluxPanel {
            VStack(alignment: .leading, spacing: 0) {
                FluxPanelHeader(label: "Battery", right: "% · ± kW")
                BatteryCombinedChartView(date: date, readings: readings, summary: summary, selectedDate: selectedDate)
            }
        }
    }

    private static func legendChip(color: Color, text: String) -> some View {
        HStack(spacing: 5) {
            RoundedRectangle(cornerRadius: 2, style: .continuous)
                .fill(color)
                .frame(width: 8, height: 8)
            Text(text)
        }
    }
}

struct DayNavigationHeader: View {
    let viewModel: DayDetailViewModel

    var body: some View {
        HStack {
            navButton(symbol: "chevron.left", disabled: false) {
                viewModel.navigatePrevious()
            }
            Spacer()
            Text(formattedDate)
                .appFontSystem(size: 22, weight: .semibold)
                .tracking(-0.4)
                .foregroundStyle(FluxTheme.Palette.primaryText)
            Spacer()
            navButton(symbol: "chevron.right", disabled: viewModel.isToday) {
                viewModel.navigateNext()
            }
        }
        .foregroundStyle(FluxTheme.Palette.primaryText)
    }

    @ViewBuilder
    private func navButton(symbol: String, disabled: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .padding(8)
                .opacity(disabled ? 0.3 : 1)
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .background(Circle().fill(Color.white.opacity(0.05)))
        .overlay(Circle().strokeBorder(FluxTheme.Palette.border, lineWidth: FluxTheme.Metrics.hairline))
    }

    private var formattedDate: String {
        guard let parsedDate = DateFormatting.parseDayDate(viewModel.date) else {
            return viewModel.date
        }
        return DayDetailEyebrow.full.string(from: parsedDate)
    }
}

struct DayDetailNoteSection: View {
    let viewModel: DayDetailViewModel
    @Binding var editingNote: Bool

    var body: some View {
        if isFutureDate {
            EmptyView()
        } else if let note = viewModel.note, !note.isEmpty {
            Button {
                editingNote = true
            } label: {
                FluxPanel {
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: "note.text")
                            .foregroundStyle(FluxTheme.Palette.secondaryText)
                        Text(note)
                            .foregroundStyle(FluxTheme.Palette.primaryText)
                            .multilineTextAlignment(.leading)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .appFontSystem(size: 13)
                }
            }
            .buttonStyle(.plain)
        } else {
            Button {
                editingNote = true
            } label: {
                FluxPanel {
                    Label("Add note", systemImage: "plus")
                        .appFontSystem(size: 13)
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .buttonStyle(.plain)
        }
    }

    private var isFutureDate: Bool {
        viewModel.date > DateFormatting.todayDateString()
    }
}

struct DayDetailMessagePanel: View {
    let title: String
    let detail: String

    var body: some View {
        FluxPanel {
            VStack(alignment: .leading, spacing: 8) {
                Text(title)
                    .appFont(.headline)
                    .foregroundStyle(FluxTheme.Palette.primaryText)
                Text(detail)
                    .appFont(.subheadline)
                    .foregroundStyle(FluxTheme.Palette.secondaryText)
            }
        }
    }
}

struct DayDetailErrorPanel: View {
    let error: FluxAPIError
    @Binding var showingSettings: Bool
    let onRetry: () -> Void

    var body: some View {
        FluxPanel {
            VStack(alignment: .leading, spacing: 8) {
                Text("Unable to load day data")
                    .appFont(.headline)
                Text(error.message)
                    .appFont(.subheadline)
                    .foregroundStyle(FluxTheme.Palette.secondaryText)
                HStack {
                    Button("Retry", action: onRetry)
                        .buttonStyle(.borderedProminent)

                    if error.suggestsSettings {
                        #if os(macOS)
                        SettingsLink {
                            Text("Settings")
                        }
                        .buttonStyle(.bordered)
                        #else
                        Button("Settings") { showingSettings = true }
                            .buttonStyle(.bordered)
                        #endif
                    }
                }
            }
            .foregroundStyle(FluxTheme.Palette.primaryText)
        }
    }
}

#if os(macOS)
/// Trailing toolbar group for the macOS Day Detail window: prev/next
/// chevrons that drive `viewModel.navigatePrevious()` /
/// `viewModel.navigateNext()`. Extracted from `DayDetailView` to keep that
/// file under the SwiftLint file-length cap. The next-day button mirrors
/// `DayNavigationHeader`'s `isToday`-disabled behavior.
struct DayDetailMacToolbar: ToolbarContent {
    let viewModel: DayDetailViewModel

    var body: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            Button {
                viewModel.navigatePrevious()
            } label: {
                Image(systemName: "chevron.left")
            }
            .accessibilityLabel("Previous day")
            .help("Previous day")

            Button {
                viewModel.navigateNext()
            } label: {
                Image(systemName: "chevron.right")
            }
            .accessibilityLabel("Next day")
            .help("Next day")
            .disabled(viewModel.isToday)
        }
    }
}
#endif

enum DayDetailEyebrow {
    static let formatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateFormat = "EEE · MMM d · yyyy"
        return formatter
    }()

    static let summaryDate: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateFormat = "MMM d"
        return formatter
    }()

    static let full: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateStyle = .full
        formatter.timeStyle = .none
        return formatter
    }()
}
