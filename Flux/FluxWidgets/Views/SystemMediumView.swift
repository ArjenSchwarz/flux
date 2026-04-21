import FluxCore
import SwiftUI
import WidgetKit

struct SystemMediumView: View {
    let entry: StatusEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 32) {
                VStack(spacing: 14) {
                    SOCRing(entry: entry, diameter: 100, lineWidth: 9)
                    if let timeLabel {
                        Text(timeLabel)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                            .redacted(reason: entry.isPlaceholder ? .placeholder : [])
                    }
                }

                VStack(alignment: .leading, spacing: 10) {
                    PowerTrioColumns(entry: entry, font: .body, spacing: 10, tight: true)
                    batteryStateRow
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.top, 14)

            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(WidgetAccessibility.label(for: entry, family: .systemMedium))
        .widgetURL(WidgetDeepLink.dashboardURL)
        .containerBackground(for: .widget) { Color.clear }
    }

    private var timeLabel: String? {
        guard let timestamp = entry.live?.timestamp,
              let date = DateFormatting.parseTimestamp(timestamp) else {
            return nil
        }
        return DateFormatting.clockTime(from: date)
    }

    private var batteryStateRow: some View {
        PillRow(
            title: batteryStateTitle,
            value: batteryStateValue,
            color: entry.batteryStateColor,
            font: .body,
            redacted: entry.isPlaceholder,
            tight: true
        )
    }

    private var batteryStateTitle: String {
        if entry.staleness == .offline { return "Offline" }
        guard let live = entry.live else { return "Battery" }
        if live.soc >= 100, live.pbat <= 0 { return "Full" }
        if live.pbat > 0 { return "Discharging" }
        if live.pbat < 0 { return "Charging" }
        return "Idle"
    }

    private var batteryStateValue: String {
        guard let live = entry.live, entry.staleness != .offline else { return "—" }
        if live.soc >= 100, live.pbat <= 0 { return "—" }
        if abs(live.pbat) < 1 { return "—" }
        return PowerFormatting.format(live.pbat)
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
