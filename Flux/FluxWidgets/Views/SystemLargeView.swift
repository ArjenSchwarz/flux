import FluxCore
import SwiftUI
import WidgetKit

struct SystemLargeView: View {
    let entry: StatusEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    SOCHeroLabel(entry: entry, size: .large)
                    StatusLineLabel(entry: entry, style: .full)
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                PowerTrioColumns(entry: entry)
                    .frame(maxWidth: .infinity)
            }

            Divider().opacity(0.3)

            statsRow

            Spacer(minLength: 0)

            StalenessFootnote(entry: entry)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(WidgetAccessibility.label(for: entry, family: .systemLarge))
        .widgetURL(WidgetDeepLink.dashboardURL)
        .containerBackground(for: .widget) { Color.clear }
    }

    @ViewBuilder
    private var statsRow: some View {
        VStack(alignment: .leading, spacing: 4) {
            statRow(title: "24h low", value: low24hText)
            statRow(title: "Off-peak grid", value: offpeakGridText)
            statRow(title: "Off-peak Δ battery", value: offpeakDeltaText)
        }
    }

    private var low24hText: String {
        guard let low = entry.battery?.low24h else { return "—" }
        let timeText = DateFormatting.parseTimestamp(low.timestamp).map(DateFormatting.clockTime(from:)) ?? "—"
        return "\(String(format: "%.1f", low.soc))% at \(timeText)"
    }

    private var offpeakGridText: String {
        guard let value = entry.offpeak?.gridUsageKwh else { return "—" }
        return "\(String(format: "%.2f", value)) kWh"
    }

    private var offpeakDeltaText: String {
        guard let value = entry.offpeak?.batteryDeltaPercent else { return "—" }
        return "\(String(format: "%+.1f", value))%"
    }

    private func statRow(title: String, value: String) -> some View {
        HStack {
            Text(title)
                .foregroundStyle(.secondary)
            Spacer()
            Text(value)
                .redacted(reason: entry.isPlaceholder ? .placeholder : [])
        }
        .font(.subheadline)
    }
}

#if DEBUG
#Preview("fresh", as: .systemLarge) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry()
}

#Preview("stale", as: .systemLarge) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry(staleness: .stale, ageMinutes: 60)
}

#Preview("offline", as: .systemLarge) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry(staleness: .offline, ageMinutes: 240)
}

#Preview("placeholder", as: .systemLarge) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.placeholderEntry
}
#endif
