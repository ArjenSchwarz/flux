import FluxCore
import SwiftUI
import WidgetKit

struct FluxAccessoryWidget: Widget {
    let kind: String = "me.nore.ig.flux.widget.accessory"

    var body: some WidgetConfiguration {
        StaticConfiguration(kind: kind, provider: StatusTimelineProvider()) { entry in
            FluxAccessoryEntryView(entry: entry)
        }
        .configurationDisplayName("Flux Accessory")
        .description("Battery state for the lock screen.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}

private struct FluxAccessoryEntryView: View {
    let entry: StatusEntry

    @Environment(\.widgetFamily) private var family

    var body: some View {
        switch family {
        case .accessoryCircular:
            AccessoryCircularView(entry: entry)
        case .accessoryRectangular:
            AccessoryRectangularView(entry: entry)
        case .accessoryInline:
            AccessoryInlineView(entry: entry)
        default:
            AccessoryCircularView(entry: entry)
        }
    }
}
