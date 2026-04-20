import FluxCore
import SwiftUI
import WidgetKit

struct SystemMediumView: View {
    let entry: StatusEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    SOCHeroLabel(entry: entry, size: .medium)
                    StatusLineLabel(entry: entry, style: .full)
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                PowerTrioColumns(entry: entry)
                    .frame(maxWidth: .infinity)
            }

            if entry.staleness != .fresh {
                StalenessFootnote(entry: entry)
            }
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(WidgetAccessibility.label(for: entry, family: .systemMedium))
        .widgetURL(WidgetDeepLink.dashboardURL)
        .containerBackground(for: .widget) { Color.clear }
    }
}

#if DEBUG
#Preview("fresh", as: .systemMedium) {
    FluxBatteryWidget()
} timeline: {
    WidgetFixtures.entry()
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
