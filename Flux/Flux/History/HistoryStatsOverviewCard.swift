import FluxCore
import SwiftUI

struct HistoryStatsOverviewCard: View {
    let summary: HistoryViewModel.PeriodSummary
    let entries: [HistoryViewModel.SolarEntry]
    let onSelect: (String) -> Void

    enum TileKey: CaseIterable {
        case totalUsage
        case totalSolar
        case exported
        case peakImports
        case avgNight
        case mostUsage
        case mostSolar
        case lowestSoc
    }

    @Environment(\.horizontalSizeClass) private var hSizeClass

    private var columnCount: Int {
        #if os(macOS)
        return 4
        #else
        return hSizeClass == .regular ? 4 : 2
        #endif
    }

    var body: some View {
        HistoryCardChrome(
            title: "Period overview",
            kpi: HistoryStatsFormatters.dateRange(entries: entries),
            subtitle: nil
        ) {
            grid
        }
    }

    private var grid: some View {
        LazyVGrid(
            columns: Array(
                repeating: GridItem(.flexible(), spacing: 12, alignment: .topLeading),
                count: columnCount
            ),
            alignment: .leading,
            spacing: 12
        ) {
            ForEach(TileKey.allCases, id: \.self) { tile in
                tileView(for: tile)
            }
        }
        .animation(.default, value: columnCount)
    }

    @ViewBuilder
    private func tileView(for tile: TileKey) -> some View {
        let value = Self.valueText(for: tile, summary: summary)
        let dateLine = Self.dateLineText(for: tile, summary: summary)
        let label = Self.label(for: tile)
        let a11y = Self.accessibilityLabel(tile: tile, summary: summary)

        if Self.isRecordTile(tile) {
            DayRecordTile(
                label: label,
                value: value,
                dateLine: dateLine,
                accessibilityLabelText: a11y,
                tapAction: Self.tapAction(for: tile, summary: summary, onSelect: onSelect)
            )
        } else {
            StatTile(label: label, value: value, accessibilityLabelText: a11y)
        }
    }
}

// MARK: - Static helpers

extension HistoryStatsOverviewCard {
    static func label(for tile: TileKey) -> String {
        switch tile {
        case .totalUsage: return "Total usage"
        case .totalSolar: return "Total solar"
        case .exported: return "Exported"
        case .peakImports: return "Peak imports"
        case .avgNight: return "Avg night"
        case .mostUsage: return "Most usage"
        case .mostSolar: return "Most solar"
        case .lowestSoc: return "Lowest SoC"
        }
    }

    // swiftlint:disable:next cyclomatic_complexity
    static func valueText(for tile: TileKey, summary: HistoryViewModel.PeriodSummary) -> String {
        switch tile {
        case .totalUsage:
            return summary.dailyUsageDayCount == 0 ? "—" : HistoryFormatters.kwh(summary.dailyUsageTotalKwh)
        case .totalSolar:
            return summary.solarDayCount == 0 ? "—" : HistoryFormatters.kwh(summary.solarTotalKwh)
        case .exported:
            return summary.gridDayCount == 0 ? "—" : HistoryFormatters.kwh(summary.exportTotalKwh)
        case .peakImports:
            return summary.gridDayCount == 0 ? "—" : HistoryFormatters.kwh(summary.peakImportTotalKwh)
        case .avgNight:
            guard let avg = summary.nightAvgKwh else { return "—" }
            return HistoryFormatters.kwh(avg)
        case .mostUsage:
            guard let record = summary.mostUsageDay else { return "—" }
            return HistoryFormatters.kwh(record.kwh)
        case .mostSolar:
            guard let record = summary.mostSolarDay else { return "—" }
            return HistoryFormatters.kwh(record.kwh)
        case .lowestSoc:
            guard let record = summary.lowestSocDay else { return "—" }
            return HistoryStatsFormatters.socPercent(record.soc)
        }
    }

    static func dateLineText(for tile: TileKey, summary: HistoryViewModel.PeriodSummary) -> String? {
        switch tile {
        case .mostUsage:
            return summary.mostUsageDay.map { HistoryStatsFormatters.shortDate(from: $0.date) }
        case .mostSolar:
            return summary.mostSolarDay.map { HistoryStatsFormatters.shortDate(from: $0.date) }
        case .lowestSoc:
            return summary.lowestSocDay.map {
                HistoryStatsFormatters.dateWithTime(from: $0.date, time: $0.socLowTime)
            }
        case .totalUsage, .totalSolar, .exported, .peakImports, .avgNight:
            return nil
        }
    }

    static func isTappable(tile: TileKey, summary: HistoryViewModel.PeriodSummary) -> Bool {
        switch tile {
        case .mostUsage: return summary.mostUsageDay != nil
        case .mostSolar: return summary.mostSolarDay != nil
        case .lowestSoc: return summary.lowestSocDay != nil
        case .totalUsage, .totalSolar, .exported, .peakImports, .avgNight:
            return false
        }
    }

    static func tapAction(
        for tile: TileKey,
        summary: HistoryViewModel.PeriodSummary,
        onSelect: @escaping (String) -> Void
    ) -> (() -> Void)? {
        let dayID: String?
        switch tile {
        case .mostUsage: dayID = summary.mostUsageDay?.dayID
        case .mostSolar: dayID = summary.mostSolarDay?.dayID
        case .lowestSoc: dayID = summary.lowestSocDay?.dayID
        case .totalUsage, .totalSolar, .exported, .peakImports, .avgNight:
            dayID = nil
        }
        guard let dayID else { return nil }
        return { onSelect(dayID) }
    }

    // swiftlint:disable:next cyclomatic_complexity
    static func accessibilityLabel(
        tile: TileKey,
        summary: HistoryViewModel.PeriodSummary
    ) -> String {
        let label = label(for: tile)
        switch tile {
        case .totalUsage:
            return summary.dailyUsageDayCount == 0
                ? "\(label), no data"
                : "\(label), \(HistoryStatsFormatters.accessibleKwh(summary.dailyUsageTotalKwh))"
        case .totalSolar:
            return summary.solarDayCount == 0
                ? "\(label), no data"
                : "\(label), \(HistoryStatsFormatters.accessibleKwh(summary.solarTotalKwh))"
        case .exported:
            return summary.gridDayCount == 0
                ? "\(label), no data"
                : "\(label), \(HistoryStatsFormatters.accessibleKwh(summary.exportTotalKwh))"
        case .peakImports:
            return summary.gridDayCount == 0
                ? "\(label), no data"
                : "\(label), \(HistoryStatsFormatters.accessibleKwh(summary.peakImportTotalKwh))"
        case .avgNight:
            guard let avg = summary.nightAvgKwh else { return "\(label), no data" }
            return "\(label), \(HistoryStatsFormatters.accessibleKwh(avg))"
        case .mostUsage:
            guard let record = summary.mostUsageDay else { return "\(label), no data" }
            return "\(label), \(HistoryStatsFormatters.accessibleKwh(record.kwh)), \(longDate(record.date))"
        case .mostSolar:
            guard let record = summary.mostSolarDay else { return "\(label), no data" }
            return "\(label), \(HistoryStatsFormatters.accessibleKwh(record.kwh)), \(longDate(record.date))"
        case .lowestSoc:
            guard let record = summary.lowestSocDay else { return "\(label), no data" }
            return "\(label), \(HistoryStatsFormatters.accessibleSocPercent(record.soc)), "
                + longDateWithTime(date: record.date, time: record.socLowTime)
        }
    }

    static let accessibilityHint = "Selects this day in the charts below."

    private static func isRecordTile(_ tile: TileKey) -> Bool {
        switch tile {
        case .mostUsage, .mostSolar, .lowestSoc: return true
        case .totalUsage, .totalSolar, .exported, .peakImports, .avgNight: return false
        }
    }

    private static func longDate(_ date: Date) -> String {
        longMonthFormatter.string(from: date)
    }

    private static func longDateWithTime(date: Date, time: String?) -> String {
        let prefix = longDate(date)
        guard let time, let parsed = DateFormatting.parseTimestamp(time) else { return prefix }
        return "\(prefix) at \(DateFormatting.clockTime24h(from: parsed))"
    }

    private static let longMonthFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.timeZone = DateFormatting.sydneyTimeZone
        formatter.dateFormat = "MMMM d"
        return formatter
    }()
}

// MARK: - Tile views

private struct StatTile: View {
    let label: String
    let value: String
    let accessibilityLabelText: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .appFont { FluxTheme.Typography.statRowLabel(family: $0) }
                .foregroundStyle(FluxTheme.Palette.secondaryText)
            Text(value)
                .appFont { FluxTheme.Typography.statRowValue(family: $0) }
                .foregroundStyle(FluxTheme.Palette.primaryText)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabelText)
    }
}

private struct DayRecordTile: View {
    let label: String
    let value: String
    let dateLine: String?
    let accessibilityLabelText: String
    let tapAction: (() -> Void)?

    var body: some View {
        if let tapAction {
            Button(action: tapAction) { content }
                .buttonStyle(.plain)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(accessibilityLabelText)
                .accessibilityAddTraits(.isButton)
                .accessibilityHint(HistoryStatsOverviewCard.accessibilityHint)
        } else {
            content
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(accessibilityLabelText)
        }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .appFont { FluxTheme.Typography.statRowLabel(family: $0) }
                .foregroundStyle(FluxTheme.Palette.secondaryText)
            Text(value)
                .appFont { FluxTheme.Typography.statRowValue(family: $0) }
                .foregroundStyle(FluxTheme.Palette.primaryText)
            // Reserve the date-line slot regardless of em-dash so a record-tile
            // row stays at the same height as a stat-tile row.
            Text(dateLine ?? " ")
                .appFont { FluxTheme.Typography.statRowSub(family: $0) }
                .foregroundStyle(FluxTheme.Palette.tertiaryText)
                .opacity(dateLine == nil ? 0 : 1)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .contentShape(Rectangle())
    }
}

#if DEBUG
private func previewSummary(populated: Bool) -> HistoryViewModel.PeriodSummary {
    if !populated { return .empty }
    let date = DateFormatting.parseDayDate("2026-04-13")!
    return HistoryViewModel.PeriodSummary(
        solarTotalKwh: 98.4,
        solarDayCount: 6,
        peakImportTotalKwh: 7.2,
        offpeakImportTotalKwh: 12.4,
        exportTotalKwh: 14.0,
        gridDayCount: 6,
        chargeTotalKwh: 24.5,
        dischargeTotalKwh: 22.0,
        batteryDayCount: 6,
        dailyUsageTotalKwh: 64.5,
        dailyUsageDayCount: 6,
        dailyUsageLargestKind: .evening,
        dailyUsageLargestKindTotalKwh: 18.0,
        nightTotalKwh: 12.0,
        nightBlockDayCount: 6,
        mostUsageDay: HistoryViewModel.DayKwhRecord(dayID: "2026-04-13", date: date, kwh: 18.7),
        mostSolarDay: HistoryViewModel.DayKwhRecord(dayID: "2026-04-13", date: date, kwh: 14.2),
        lowestSocDay: HistoryViewModel.LowestSocRecord(
            dayID: "2026-04-13", date: date, soc: 12.0,
            socLowTime: "2026-04-12T20:14:00Z"
        )
    )
}

private func previewEntries() -> [HistoryViewModel.SolarEntry] {
    let dates = ["2026-04-09", "2026-04-10", "2026-04-13", "2026-04-15"]
    return dates.compactMap { dayID in
        DateFormatting.parseDayDate(dayID).map {
            HistoryViewModel.SolarEntry(date: $0, dayID: dayID, kwh: 10, isToday: false)
        }
    }
}

#Preview("HistoryStatsOverviewCard — iPhone, populated") {
    HistoryStatsOverviewCard(
        summary: previewSummary(populated: true),
        entries: previewEntries(),
        onSelect: { _ in }
    )
    .padding()
    .frame(width: 375)
    .fluxScreenBackground()
}

#Preview("HistoryStatsOverviewCard — Mac, mixed em-dash") {
    let mixed = HistoryViewModel.PeriodSummary(
        solarTotalKwh: 98.4, solarDayCount: 6,
        peakImportTotalKwh: 0, offpeakImportTotalKwh: 0,
        exportTotalKwh: 0, gridDayCount: 0,
        chargeTotalKwh: 0, dischargeTotalKwh: 0, batteryDayCount: 6,
        dailyUsageTotalKwh: 64.5, dailyUsageDayCount: 6,
        dailyUsageLargestKind: .evening, dailyUsageLargestKindTotalKwh: 18.0,
        nightTotalKwh: 0, nightBlockDayCount: 0,
        mostUsageDay: nil,
        mostSolarDay: HistoryViewModel.DayKwhRecord(
            dayID: "2026-04-13",
            date: DateFormatting.parseDayDate("2026-04-13")!,
            kwh: 14.2
        ),
        lowestSocDay: nil
    )
    HistoryStatsOverviewCard(
        summary: mixed,
        entries: previewEntries(),
        onSelect: { _ in }
    )
    .padding()
    .frame(width: 900)
    .fluxScreenBackground()
}
#endif
