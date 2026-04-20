import FluxCore
import SwiftUI
import WidgetKit

struct AccessoryCircularView: View {
    let entry: StatusEntry

    @Environment(\.widgetRenderingMode) private var renderingMode

    var body: some View {
        Gauge(value: min(max(entry.soc, 0), 100), in: 0...100) {
            EmptyView()
        } currentValueLabel: {
            Text(Int(entry.soc.rounded()), format: .number)
                .font(.system(size: 16, weight: .semibold, design: .rounded))
                .minimumScaleFactor(0.5)
                .redacted(reason: entry.isPlaceholder ? .placeholder : [])
        }
        .gaugeStyle(.accessoryCircularCapacity)
        .tint(tint)
        .opacity(entry.staleness == .offline ? 0.5 : 1)
        .dynamicTypeSize(...(.accessibility3))
        .accessibilityLabel(WidgetAccessibility.label(for: entry, family: .accessoryCircular))
        .widgetURL(WidgetDeepLink.dashboardURL)
        .containerBackground(for: .widget) { Color.clear }
    }

    private var tint: Color {
        switch renderingMode {
        case .fullColor:
            return BatteryColor.forSOC(entry.soc).color
        default:
            return .primary
        }
    }
}

#if DEBUG
#Preview("fresh", as: .accessoryCircular) {
    FluxAccessoryWidget()
} timeline: {
    WidgetFixtures.entry()
}

#Preview("offline", as: .accessoryCircular) {
    FluxAccessoryWidget()
} timeline: {
    WidgetFixtures.entry(staleness: .offline, ageMinutes: 240)
}

#Preview("placeholder", as: .accessoryCircular) {
    FluxAccessoryWidget()
} timeline: {
    WidgetFixtures.placeholderEntry
}
#endif
