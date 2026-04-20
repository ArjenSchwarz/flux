import FluxCore
import SwiftUI

struct StatusLineLabel: View {
    let entry: StatusEntry
    let style: StatusLineStyle

    var body: some View {
        Text(entry.statusLine(style: style))
            .font(.subheadline)
            .foregroundStyle(entry.staleness == .offline ? .secondary : .primary)
            .lineLimit(2)
            .minimumScaleFactor(0.6)
            .redacted(reason: entry.isPlaceholder ? .placeholder : [])
    }
}

#if DEBUG
#Preview("full") {
    StatusLineLabel(entry: WidgetFixtures.entry(), style: .full)
        .padding()
}

#Preview("short") {
    StatusLineLabel(entry: WidgetFixtures.entry(), style: .short)
        .padding()
}

#Preview("word") {
    StatusLineLabel(entry: WidgetFixtures.entry(), style: .word)
        .padding()
}

#Preview("offline") {
    StatusLineLabel(entry: WidgetFixtures.entry(staleness: .offline), style: .full)
        .padding()
}
#endif
