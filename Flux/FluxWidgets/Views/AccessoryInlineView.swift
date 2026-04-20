import FluxCore
import SwiftUI
import WidgetKit

struct AccessoryInlineView: View {
    let entry: StatusEntry

    var body: some View {
        Text(text)
            .dynamicTypeSize(...(.accessibility3))
            .accessibilityLabel(WidgetAccessibility.label(for: entry, family: .accessoryInline))
            .widgetURL(WidgetDeepLink.dashboardURL)
            .containerBackground(for: .widget) { Color.clear }
    }

    private var text: String {
        if entry.staleness == .offline {
            return "Flux: offline"
        }
        let soc = Int(entry.soc.rounded())
        return "Flux: \(soc)% · \(entry.statusWord)"
    }
}

#if DEBUG
#Preview("fresh", as: .accessoryInline) {
    FluxAccessoryWidget()
} timeline: {
    WidgetFixtures.entry()
}

#Preview("offline", as: .accessoryInline) {
    FluxAccessoryWidget()
} timeline: {
    WidgetFixtures.entry(staleness: .offline, ageMinutes: 240)
}

#Preview("placeholder", as: .accessoryInline) {
    FluxAccessoryWidget()
} timeline: {
    WidgetFixtures.placeholderEntry
}
#endif
