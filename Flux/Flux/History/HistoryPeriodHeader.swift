import FluxCore
import SwiftUI

/// Period-navigation header for the Wk/Mo History ranges (req 1.1), rendered
/// in content on both platforms (Decision 14) and styled after
/// `DayNavigationHeader`: chevrons flanking a centred period label. Tapping
/// the label opens a graphical date picker capped at Sydney today (req 3.2);
/// a compact "Current" button appears beneath the label — between the
/// chevrons — only when viewing a past period (req 2.1/2.2, Decision 8). The
/// next chevron is visible but disabled at the current period (req 1.3).
struct HistoryPeriodHeader: View {
    let range: HistoryRange
    let period: HistoryPeriod
    let isViewingCurrentPeriod: Bool
    /// Last selectable instant for the jump picker — the end of the Sydney
    /// day containing now.
    let pickerUpperBound: Date
    let onPrevious: () -> Void
    let onNext: () -> Void
    let onJump: (Date) -> Void
    let onReturnToCurrent: () -> Void

    @State private var showingPicker = false
    @State private var pickerDate = Date()

    var body: some View {
        HStack(spacing: 8) {
            navButton(symbol: "chevron.left", disabled: false, accessibilityLabel: "Previous period") {
                onPrevious()
            }
            Spacer()
            VStack(spacing: 4) {
                periodLabel
                if !isViewingCurrentPeriod {
                    currentButton
                }
            }
            Spacer()
            navButton(
                symbol: "chevron.right",
                disabled: isViewingCurrentPeriod,
                accessibilityLabel: "Next period"
            ) {
                onNext()
            }
        }
        .foregroundStyle(FluxTheme.Palette.primaryText)
    }

    private var periodLabel: some View {
        Button {
            pickerDate = period.start
            showingPicker = true
        } label: {
            Text(Self.label(for: range, period: period))
                .appFontSystem(size: 22, weight: .semibold)
                .tracking(-0.4)
                .foregroundStyle(FluxTheme.Palette.primaryText)
        }
        .buttonStyle(.plain)
        .accessibilityHint("Opens a calendar to jump to a past period.")
        // Popover on macOS and regular iOS; the system adapts it to a sheet
        // on compact iOS, matching the design's presentation split.
        .popover(isPresented: $showingPicker) {
            jumpPicker
        }
    }

    private var jumpPicker: some View {
        DatePicker(
            "Jump to period",
            selection: $pickerDate,
            in: ...pickerUpperBound,
            displayedComponents: .date
        )
        .datePickerStyle(.graphical)
        // The picker renders and caps in the environment calendar — without
        // these, "today" and the snapped period could be off by a day on a
        // non-Sydney device.
        .environment(\.calendar, Self.pickerCalendar)
        .environment(\.timeZone, DateFormatting.sydneyTimeZone)
        .labelsHidden()
        .padding()
        .frame(minWidth: 320)
        .onChange(of: pickerDate) { _, newDate in
            showingPicker = false
            onJump(newDate)
        }
    }

    /// Sydney calendar adopting the device's first-weekday setting and locale,
    /// so the picker's week rows start on the same day as the Wk period maths
    /// (`Calendar.current.firstWeekday` everywhere else) and the user's system
    /// preference.
    private static var pickerCalendar: Calendar {
        var calendar = DateFormatting.sydneyCalendar
        calendar.firstWeekday = Calendar.current.firstWeekday
        calendar.locale = Locale.current
        return calendar
    }

    private var currentButton: some View {
        Button("Current") {
            onReturnToCurrent()
        }
        .appFontSystem(size: 13, weight: .semibold)
        .buttonStyle(.bordered)
        .buttonBorderShape(.capsule)
        .accessibilityLabel("Return to current period")
    }

    @ViewBuilder
    private func navButton(
        symbol: String,
        disabled: Bool,
        accessibilityLabel: String,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .padding(8)
                .opacity(disabled ? 0.3 : 1)
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .background(Circle().fill(Color.white.opacity(0.05)))
        .overlay(Circle().strokeBorder(FluxTheme.Palette.border, lineWidth: FluxTheme.Metrics.hairline))
        .accessibilityLabel(accessibilityLabel)
    }

    /// Period label (req 4.1): week → `"Jun 2 – 8"` (cross-month
    /// `"May 29 – Jun 4"`); month → `"May 2026"`. Static so the view checks
    /// can assert the formatting without rendering.
    static func label(for range: HistoryRange, period: HistoryPeriod) -> String {
        switch range {
        case .days:
            // The header is never shown for fixed ranges (req 1.5).
            return ""
        case .weekToDate:
            let calendar = DateFormatting.sydneyCalendar
            let sameMonth = calendar.isDate(period.start, equalTo: period.lastDay, toGranularity: .month)
            let start = DateFormatting.shortMonthDay(from: period.start)
            let end = sameMonth
                ? HistoryPeriodLabelFormatter.dayOnly.string(from: period.lastDay)
                : DateFormatting.shortMonthDay(from: period.lastDay)
            return "\(start) – \(end)"
        case .monthToDate:
            return HistoryPeriodLabelFormatter.monthYear.string(from: period.start)
        }
    }
}

/// Sydney-zoned label formatters, mirroring the `HistorySummaryDateFormatter`
/// precedent. Only formats that exist nowhere else live here — the shared
/// `"MMM d"` form comes from `DateFormatting.shortMonthDay(from:)`.
private enum HistoryPeriodLabelFormatter {
    static let dayOnly: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateFormat = "d"
        return formatter
    }()

    static let monthYear: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateFormat = "MMM yyyy"
        return formatter
    }()
}
