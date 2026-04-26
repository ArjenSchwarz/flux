import FluxCore
import SwiftUI
import WidgetKit

struct SystemMediumView: View {
    let entry: StatusEntry

    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 32) {
                VStack(spacing: 14) {
                    SOCRing(entry: entry, diameter: 100, lineWidth: 9)
                    if let timeLabel {
                        Text(timeLabel + emptySuffix)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                            .redacted(reason: entry.isPlaceholder ? .placeholder : [])
                    }
                }

                statsGrid
                    .frame(maxWidth: .infinity, alignment: useSymbols ? .center : .leading)
            }
            .padding(.top, 14)
            .padding(.leading, 16)

            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(WidgetAccessibility.label(for: entry, family: .systemMedium))
        .widgetURL(WidgetDeepLink.dashboardURL)
        .containerBackground(for: .widget) {
            if colorScheme == .dark {
                Image("Earthset")
                    .resizable()
                    .scaledToFill()
            } else {
                Color.clear
            }
        }
    }

    private var useSymbols: Bool {
        UserDefaults.fluxAppGroup.widgetUsesSymbols
    }

    private var timeLabel: String? {
        if let timestamp = entry.live?.timestamp,
           let date = DateFormatting.parseTimestamp(timestamp) {
            return DateFormatting.clockTime(from: date)
        }
        if let fetchedAt = entry.envelope?.fetchedAt {
            return DateFormatting.clockTime(from: fetchedAt)
        }
        return nil
    }

    private var emptySuffix: String {
        guard let emptyDate = emptyAt else { return "" }
        return " (~\(DateFormatting.clockTime(from: emptyDate)))"
    }

    private var statsGrid: some View {
        Grid(alignment: .leading, horizontalSpacing: 10, verticalSpacing: 10) {
            row(
                label: "Solar",
                symbol: "sun.max.fill",
                value: PowerFormatting.format(entry.ppv),
                color: entry.solarColor,
                useSymbols: useSymbols
            )
            row(
                label: "Load",
                symbol: "house.fill",
                value: PowerFormatting.format(entry.pload),
                color: entry.loadColor,
                useSymbols: useSymbols
            )
            row(
                label: entry.gridTitle,
                symbol: gridSymbol,
                value: PowerFormatting.format(entry.pgrid),
                color: entry.gridTintColor,
                useSymbols: useSymbols
            )
            row(
                label: batteryStateTitle,
                symbol: batteryStateSymbol,
                value: batteryStateValue,
                color: entry.batteryStateColor,
                useSymbols: useSymbols
            )
        }
    }

    private var emptyAt: Date? {
        guard entry.staleness != .offline,
              let live = entry.live,
              live.pbat > 0,
              let cutoffString = entry.rolling15min?.estimatedCutoffTime,
              let cutoffDate = DateFormatting.parseTimestamp(cutoffString) else {
            return nil
        }
        return cutoffDate
    }

    @ViewBuilder
    private func row(
        label: String,
        symbol: String,
        value: String,
        color: Color,
        useSymbols: Bool
    ) -> some View {
        GridRow {
            rowLabel(text: label, symbol: symbol, font: .subheadline, symbolColor: color, useSymbols: useSymbols)
            Text(value)
                .font(.body)
                .monospacedDigit()
                .foregroundStyle(color)
                .lineLimit(1)
                .redacted(reason: entry.isPlaceholder ? .placeholder : [])
        }
    }

    @ViewBuilder
    private func rowLabel(
        text: String,
        symbol: String,
        font: Font,
        symbolColor: Color,
        useSymbols: Bool
    ) -> some View {
        if useSymbols {
            Image(systemName: symbol)
                .font(font)
                .foregroundStyle(symbolColor)
                .gridColumnAlignment(.trailing)
                .accessibilityLabel(text)
        } else {
            Text(text)
                .font(font)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .gridColumnAlignment(.trailing)
        }
    }

    private var batteryStateTitle: String {
        if entry.staleness == .offline { return "Offline" }
        guard let live = entry.live else { return "Battery" }
        if live.pbat > 0 { return "Discharging" }
        if live.pbat < 0 { return "Charging" }
        return "Idle"
    }

    private var batteryStateValue: String {
        guard let live = entry.live, entry.staleness != .offline else { return "—" }
        if abs(live.pbat) < 1 { return "—" }
        return PowerFormatting.format(live.pbat)
    }

    private var batteryStateSymbol: String {
        if entry.staleness == .offline { return "bolt.slash" }
        guard let live = entry.live else { return "battery.50percent" }
        if live.pbat < 0 { return "battery.100percent.bolt" }
        return socBatterySymbol(soc: live.soc)
    }

    private func socBatterySymbol(soc: Double) -> String {
        switch soc {
        case ..<13: return "battery.0percent"
        case ..<38: return "battery.25percent"
        case ..<63: return "battery.50percent"
        case ..<88: return "battery.75percent"
        default: return "battery.100percent"
        }
    }

    private var gridSymbol: String {
        if entry.pgrid < 0 { return "arrow.up.circle" }
        if entry.pgrid > 0 { return "arrow.down.circle" }
        return "bolt.horizontal"
    }
}

#if DEBUG
#Preview("fresh", as: .systemMedium) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry()
}

#Preview("full", as: .systemMedium) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry(soc: 100, pbat: -200)
}

#Preview("cutoff-risk", as: .systemMedium) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry(soc: 45, pbat: 3200)
}

#Preview("stale", as: .systemMedium) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry(staleness: .stale, ageMinutes: 60)
}

#Preview("offline", as: .systemMedium) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry(staleness: .offline, ageMinutes: 240)
}

#Preview("placeholder", as: .systemMedium) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.placeholderEntry
}
#endif
