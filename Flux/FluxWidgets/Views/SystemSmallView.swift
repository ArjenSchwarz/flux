import FluxCore
import SwiftUI
import WidgetKit

struct SystemSmallView: View {
    let entry: StatusEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            SOCHeroLabel(entry: entry, size: .small)
            StatusLineLabel(entry: entry, style: .short)
            Divider().opacity(0.3)
            LoadRow(entry: entry)
            if entry.staleness != .fresh {
                StalenessFootnote(entry: entry)
            }
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(WidgetAccessibility.label(for: entry, family: .systemSmall))
        .widgetURL(WidgetDeepLink.dashboardURL)
        .containerBackground(for: .widget) { Color.clear }
    }
}

#if DEBUG
#Preview("fresh", as: .systemSmall) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry()
}

#Preview("stale", as: .systemSmall) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry(staleness: .stale, ageMinutes: 60)
}

#Preview("offline", as: .systemSmall) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry(staleness: .offline, ageMinutes: 240)
}

#Preview("placeholder", as: .systemSmall) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.placeholderEntry
}
#endif
