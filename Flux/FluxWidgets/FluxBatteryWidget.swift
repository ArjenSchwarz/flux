import FluxCore
import SwiftUI
import WidgetKit

struct FluxBatteryWidget: Widget {
    let kind: String = WidgetKinds.battery

    var body: some WidgetConfiguration {
        StaticConfiguration(kind: kind, provider: StatusTimelineProvider()) { entry in
            FluxBatteryEntryView(entry: entry)
        }
        .configurationDisplayName("Flux Battery")
        .description("Battery state and household power at a glance.")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge])
    }
}

private struct FluxBatteryEntryView: View {
    let entry: StatusEntry

    @Environment(\.widgetFamily) private var family

    var body: some View {
        switch family {
        case .systemSmall:
            SystemSmallView(entry: entry)
        case .systemMedium:
            SystemMediumView(entry: entry)
        case .systemLarge:
            SystemLargeView(entry: entry)
        @unknown default:
            SystemMediumView(entry: entry)
        }
    }
}
