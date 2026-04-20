import FluxCore
import SwiftUI

enum SOCHeroSize {
    case small
    case medium
    case large
    case circular

    var font: Font {
        switch self {
        case .small: .system(size: 40, weight: .bold, design: .rounded)
        case .medium: .system(size: 48, weight: .bold, design: .rounded)
        case .large: .system(size: 56, weight: .bold, design: .rounded)
        case .circular: .system(size: 18, weight: .semibold, design: .rounded)
        }
    }
}

struct SOCHeroLabel: View {
    let entry: StatusEntry
    let size: SOCHeroSize

    var body: some View {
        let tint: Color = entry.staleness == .offline
            ? .secondary
            : BatteryColor.forSOC(entry.soc).color

        Text("\(entry.soc, specifier: "%.1f")%")
            .font(size.font)
            .foregroundStyle(tint)
            .minimumScaleFactor(0.5)
            .lineLimit(1)
            .redacted(reason: entry.isPlaceholder ? .placeholder : [])
    }
}

#if DEBUG
#Preview("fresh") {
    SOCHeroLabel(entry: WidgetFixtures.entry(), size: .medium)
        .padding()
}

#Preview("offline") {
    SOCHeroLabel(entry: WidgetFixtures.entry(staleness: .offline), size: .medium)
        .padding()
}

#Preview("placeholder") {
    SOCHeroLabel(entry: WidgetFixtures.placeholderEntry, size: .medium)
        .padding()
}
#endif
