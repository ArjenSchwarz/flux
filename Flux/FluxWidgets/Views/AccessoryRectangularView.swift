import FluxCore
import SwiftUI
import WidgetKit

struct AccessoryRectangularView: View {
    let entry: StatusEntry

    var body: some View {
        HStack(alignment: .center, spacing: 8) {
            Image(systemName: glyph)
                .widgetAccentable()

            VStack(alignment: .leading, spacing: 1) {
                Text("\(entry.soc, specifier: "%.0f")%")
                    .font(.headline)
                    .redacted(reason: entry.isPlaceholder ? .placeholder : [])
                Text(entry.statusLine(style: .short))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                if entry.staleness != .fresh, let env = entry.envelope {
                    Text(env.fetchedAt, style: .relative)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .dynamicTypeSize(...(.accessibility3))
        .accessibilityElement(children: .combine)
        .accessibilityLabel(WidgetAccessibility.label(for: entry, family: .accessoryRectangular))
        .widgetURL(WidgetDeepLink.dashboardURL)
        .containerBackground(for: .widget) { Color.clear }
    }

    private var glyph: String {
        if entry.staleness == .offline { return "bolt.slash" }
        if entry.pbat > 0 { return "battery.50percent" }
        if entry.pbat < 0 { return "battery.100percent.bolt" }
        return "battery.75percent"
    }
}

#if DEBUG
#Preview("fresh", as: .accessoryRectangular) {
    FluxAccessoryWidget()
} timeline: {
    WidgetFixtures.entry()
}

#Preview("offline", as: .accessoryRectangular) {
    FluxAccessoryWidget()
} timeline: {
    WidgetFixtures.entry(staleness: .offline, ageMinutes: 240)
}

#Preview("placeholder", as: .accessoryRectangular) {
    FluxAccessoryWidget()
} timeline: {
    WidgetFixtures.placeholderEntry
}
#endif
