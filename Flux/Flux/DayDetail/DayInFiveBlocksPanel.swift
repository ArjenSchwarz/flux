import FluxCore
import SwiftUI

/// "The day in five blocks" panel — a horizontal stacked bar above five
/// labelled rows showing how the day's energy split across the TOU windows.
/// Off-peak is the highlighted row.
struct DayInFiveBlocksPanel: View {
    let dailyUsage: DailyUsage

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
                        .appFont { FluxTheme.Typography.touName(family: $0).weight(isHighlighted ? .semibold : .regular) }
                        .foregroundStyle(isHighlighted ? FluxTheme.Palette.offpeak : FluxTheme.Palette.primaryText)
                    Text(timeRange(block))
                        .appFont(FluxTheme.Typography.touTime)
                        .foregroundStyle(FluxTheme.Palette.tertiaryText)
                        .lineLimit(1)
                        .minimumScaleFactor(0.85)
                }
                Spacer(minLength: 8)
                if isDaylight(block.kind), let solar = block.solarKwh {
                    Text(EnergyFormatting.format(solar))
                        .appFont(FluxTheme.Typography.touValue)
                        .foregroundStyle(FluxTheme.Palette.amber)
                        .lineLimit(1)
                        .minimumScaleFactor(0.85)
                        .frame(width: 76, alignment: .trailing)
                }
                Text(EnergyFormatting.format(block.totalKwh))
                    .appFont(FluxTheme.Typography.touValue)
                    .foregroundStyle(FluxTheme.Palette.primaryText)
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
                    .frame(width: 76, alignment: .trailing)
            }
            .padding(.vertical, FluxTheme.Metrics.statRowVerticalPadding)

            if !isLast {
                Rectangle()
                    .fill(FluxTheme.Palette.border)
                    .frame(height: FluxTheme.Metrics.hairline)
            }
        }
    }

    private func isDaylight(_ kind: DailyUsageBlock.Kind) -> Bool {
        switch kind {
        case .morningPeak, .offPeak, .afternoonPeak: true
        case .night, .evening: false
        }
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
