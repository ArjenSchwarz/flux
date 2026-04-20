import FluxCore
import SwiftUI

struct LoadRow: View {
    let entry: StatusEntry

    var body: some View {
        let over = entry.pload > UserDefaults.fluxAppGroup.loadAlertThreshold
        let color: Color = entry.staleness == .offline
            ? .secondary
            : (over ? .red : .primary)

        HStack(spacing: 6) {
            Text("Load")
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(PowerFormatting.format(entry.pload))
                .font(.subheadline)
                .foregroundStyle(color)
                .redacted(reason: entry.isPlaceholder ? .placeholder : [])
        }
    }
}

#if DEBUG
#Preview("under") {
    LoadRow(entry: WidgetFixtures.entry(pbat: 500))
        .padding()
}

#Preview("over") {
    LoadRow(entry: WidgetFixtures.entry(pbat: 500, pload: 5000))
        .padding()
}

#Preview("placeholder") {
    LoadRow(entry: WidgetFixtures.placeholderEntry)
        .padding()
}
#endif
