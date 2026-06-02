import FluxCore
import SwiftUI

/// "The day in five blocks" panel — a horizontal stacked bar above five
/// labelled rows showing how the day's energy split across the TOU windows.
/// Off-peak is the highlighted row.
struct DayInFiveBlocksPanel: View {
    let dailyUsage: DailyUsage
    var compare: ComparisonState = .off

    var body: some View {
        FluxPanel {
            VStack(alignment: .leading, spacing: 0) {
                FluxPanelHeader(label: "The day in five blocks")
                stackedBar.padding(.bottom, 14)
                ForEach(Array(orderedBlocks.enumerated()), id: \.element.id) { index, block in
                    row(block, isLast: index == orderedBlocks.count - 1)
                }
            }
        }
    }

    private var orderedBlocks: [DailyUsageBlock] {
        let order: [DailyUsageBlock.Kind] = [.night, .morningPeak, .offPeak, .afternoonPeak, .evening]
        return order.compactMap { kind in dailyUsage.blocks.first { $0.kind == kind } }
    }

    private var stackedBar: some View {
        let total = max(0.0001, orderedBlocks.map(\.totalKwh).reduce(0, +))
        let gap: CGFloat = 2
        return GeometryReader { geo in
            let gapsTotal = CGFloat(max(0, orderedBlocks.count - 1)) * gap
            let available = max(0, geo.size.width - gapsTotal)
            HStack(spacing: gap) {
                ForEach(orderedBlocks) { block in
                    Rectangle()
                        .fill(color(for: block.kind))
                        .opacity(opacity(for: block.kind))
                        .frame(width: available * (block.totalKwh / total))
                }
            }
        }
        .frame(height: 8)
        .clipShape(RoundedRectangle(cornerRadius: 2, style: .continuous))
    }

    @ViewBuilder
    private func row(_ block: DailyUsageBlock, isLast: Bool) -> some View {
        let isHighlighted = block.kind == .offPeak
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                RoundedRectangle(cornerRadius: 2, style: .continuous)
                    .fill(color(for: block.kind))
                    .opacity(opacity(for: block.kind))
                    .frame(width: 3)
                    .frame(maxHeight: .infinity)
                VStack(alignment: .leading, spacing: 2) {
                    Text(label(for: block.kind))
                        .appFont {
                            FluxTheme.Typography.touName(family: $0).weight(isHighlighted ? .semibold : .regular)
                        }
                        .foregroundStyle(isHighlighted ? FluxTheme.Palette.offpeak : FluxTheme.Palette.primaryText)
                    Text(timeRange(block))
                        .appFont(FluxTheme.Typography.touTime)
                        .foregroundStyle(FluxTheme.Palette.tertiaryText)
                        .lineLimit(1)
                        .minimumScaleFactor(0.85)
                }
                Spacer(minLength: 8)
                if block.kind.isDaylight, let solar = block.solarKwh {
                    VStack(alignment: .trailing, spacing: 2) {
                        Text(EnergyFormatting.format(solar))
                            .appFont(FluxTheme.Typography.touValue)
                            .foregroundStyle(FluxTheme.Palette.amber)
                            .lineLimit(1)
                            .minimumScaleFactor(0.85)
                        ValueSubline(content: solarValueSub(for: block))
                    }
                    .frame(width: 76, alignment: .trailing)
                }
                VStack(alignment: .trailing, spacing: 2) {
                    Text(EnergyFormatting.format(block.totalKwh))
                        .appFont(FluxTheme.Typography.touValue)
                        .foregroundStyle(FluxTheme.Palette.primaryText)
                        .lineLimit(1)
                        .minimumScaleFactor(0.85)
                    ValueSubline(content: totalValueSub(for: block))
                }
                .frame(width: 76, alignment: .trailing)
            }
            .padding(.vertical, FluxTheme.Metrics.statRowVerticalPadding)
            .modifier(RowAccessibilityModifier(label: rowAccessibilityOverride(for: block)))

            if !isLast {
                Rectangle()
                    .fill(FluxTheme.Palette.border)
                    .frame(height: FluxTheme.Metrics.hairline)
            }
        }
    }

    private func solarValueSub(for block: DailyUsageBlock) -> SublineContent {
        DayInFiveBlocksPanelCompareMapping.solarValueSub(for: block, compare: compare)
    }

    private func totalValueSub(for block: DailyUsageBlock) -> SublineContent {
        DayInFiveBlocksPanelCompareMapping.totalValueSub(for: block, compare: compare)
    }

    private func rowAccessibilityOverride(for block: DailyUsageBlock) -> String? {
        DayInFiveBlocksPanelCompareMapping.rowAccessibilityOverride(
            for: block,
            rowLabel: label(for: block.kind),
            timeRange: timeRange(block),
            compare: compare
        )
    }

    private func label(for kind: DailyUsageBlock.Kind) -> String {
        switch kind {
        case .night: "Night"
        case .morningPeak: "Morning peak"
        case .offPeak: "Off-peak"
        case .afternoonPeak: "Afternoon peak"
        case .evening: "Evening"
        }
    }

    private func color(for kind: DailyUsageBlock.Kind) -> Color {
        switch kind {
        case .night, .evening: FluxTheme.Palette.night
        case .morningPeak, .afternoonPeak: FluxTheme.Palette.grid
        case .offPeak: FluxTheme.Palette.offpeak
        }
    }

    private func opacity(for kind: DailyUsageBlock.Kind) -> Double {
        kind == .evening ? 0.4 : 1.0
    }

    private func timeRange(_ block: DailyUsageBlock) -> String {
        guard let startDate = DateFormatting.parseTimestamp(block.start),
              let endDate = DateFormatting.parseTimestamp(block.end)
        else {
            return "—"
        }
        return "\(DateFormatting.clockTime24h(from: startDate))–\(DateFormatting.clockTime24h(from: endDate))"
    }
}

/// Per-block compare-state mapping used by `DayInFiveBlocksPanel`.
/// Factored out so the per-block logic is unit-testable without
/// rendering the SwiftUI view.
enum DayInFiveBlocksPanelCompareMapping {
    static func solarValueSub(for block: DailyUsageBlock, compare: ComparisonState) -> SublineContent {
        compare.subline(current: block.solarKwh, comparison: comparisonBlock(for: block, compare: compare)?.solarKwh)
    }

    static func totalValueSub(for block: DailyUsageBlock, compare: ComparisonState) -> SublineContent {
        compare.subline(current: block.totalKwh, comparison: comparisonBlock(for: block, compare: compare)?.totalKwh)
    }

    private static func comparisonBlock(for block: DailyUsageBlock, compare: ComparisonState) -> DailyUsageBlock? {
        guard case .ready(let snapshot, _) = compare else { return nil }
        return snapshot.dailyUsage?.blocks.first { $0.kind == block.kind }
    }

    /// Composed accessibility label exposing both the total and (on
    /// daylight rows) the solar delta. Format mirrors `FluxStatRow`'s
    /// approach so VoiceOver users hear one element per row.
    static func rowAccessibilityOverride(
        for block: DailyUsageBlock,
        rowLabel: String,
        timeRange: String,
        compare: ComparisonState
    ) -> String? {
        switch compare {
        case .off:
            return nil
        case .loading, .unavailable:
            return fallbackLabel(for: block, rowLabel: rowLabel, timeRange: timeRange)
        case .ready(let snapshot, let period):
            return readyLabel(
                for: block,
                rowLabel: rowLabel,
                timeRange: timeRange,
                snapshot: snapshot,
                period: period
            )
        }
    }

    private static func fallbackLabel(
        for block: DailyUsageBlock,
        rowLabel: String,
        timeRange: String
    ) -> String {
        // Spoken form ("kilowatt-hours") for VoiceOver; the displayed
        // "kWh" string would be read as the letters k-W-h. AC 7.1.
        let total = EnergyFormatting.formatSpoken(block.totalKwh)
        if block.kind.isDaylight, let solar = block.solarKwh {
            let solarText = EnergyFormatting.formatSpoken(solar)
            return "\(rowLabel), \(timeRange): \(total) total, \(solarText) solar"
        }
        return "\(rowLabel), \(timeRange): \(total)"
    }

    private static func readyLabel(
        for block: DailyUsageBlock,
        rowLabel: String,
        timeRange: String,
        snapshot: ComparisonSnapshot,
        period: ComparePeriod
    ) -> String {
        let comparisonBlock = snapshot.dailyUsage?.blocks.first { $0.kind == block.kind }
        // Spoken form ("kilowatt-hours") for VoiceOver; the displayed
        // "kWh" string would be read as the letters k-W-h. AC 7.1.
        let total = EnergyFormatting.formatSpoken(block.totalKwh)
        let totalClause = DeltaFormatter.voiceOverComparisonClause(
            current: block.totalKwh,
            comparison: comparisonBlock?.totalKwh,
            period: period
        )

        if block.kind.isDaylight, let solar = block.solarKwh {
            let solarText = EnergyFormatting.formatSpoken(solar)
            let solarClause = DeltaFormatter.voiceOverComparisonClause(
                current: solar,
                comparison: comparisonBlock?.solarKwh,
                period: period
            )
            return "\(rowLabel), \(timeRange): \(total) total\(totalClause), \(solarText) solar\(solarClause)"
        }
        return "\(rowLabel), \(timeRange): \(total)\(totalClause)"
    }
}

private extension DailyUsageBlock.Kind {
    /// Daylight blocks (morning peak, off-peak, afternoon peak) display solar
    /// alongside the total; non-daylight blocks (night, evening) show only the
    /// total because solar is effectively zero outside daylight hours.
    var isDaylight: Bool {
        switch self {
        case .morningPeak, .offPeak, .afternoonPeak: true
        case .night, .evening: false
        }
    }
}

#if DEBUG
#Preview {
    let blocks = DailyUsage(blocks: [
        DailyUsageBlock(kind: .night, start: "2026-05-03T14:00:00Z", end: "2026-05-03T20:30:00Z",
                        totalKwh: 3.1, averageKwhPerHour: 0.48, percentOfDay: 18,
                        status: .complete, boundarySource: .readings),
        DailyUsageBlock(kind: .morningPeak, start: "2026-05-03T20:30:00Z", end: "2026-05-04T01:00:00Z",
                        totalKwh: 2.1, solarKwh: 0.6, averageKwhPerHour: 0.47, percentOfDay: 12,
                        status: .complete, boundarySource: .estimated),
        DailyUsageBlock(kind: .offPeak, start: "2026-05-04T01:00:00Z", end: "2026-05-04T04:00:00Z",
                        totalKwh: 5.0, solarKwh: 2.4, averageKwhPerHour: 1.67, percentOfDay: 30,
                        status: .complete, boundarySource: .readings),
        DailyUsageBlock(kind: .afternoonPeak, start: "2026-05-04T04:00:00Z", end: "2026-05-04T08:42:00Z",
                        totalKwh: 4.5, solarKwh: 1.8, averageKwhPerHour: 0.96, percentOfDay: 27,
                        status: .complete, boundarySource: .estimated),
        DailyUsageBlock(kind: .evening, start: "2026-05-04T08:42:00Z", end: "2026-05-04T14:00:00Z",
                        totalKwh: 2.2, averageKwhPerHour: 0.41, percentOfDay: 13,
                        status: .complete, boundarySource: .estimated)
    ])
    return ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        DayInFiveBlocksPanel(dailyUsage: blocks)
            .padding()
    }
}
#endif
