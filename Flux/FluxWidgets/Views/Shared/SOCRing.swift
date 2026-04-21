import FluxCore
import SwiftUI

struct SOCRing: View {
    let entry: StatusEntry
    let diameter: CGFloat
    let lineWidth: CGFloat

    var body: some View {
        let tint = entry.effectiveBatteryColor
        let progress = min(max(entry.soc / 100.0, 0), 1)

        ZStack {
            Circle()
                .stroke(tint.opacity(0.2), lineWidth: lineWidth)

            Circle()
                .trim(from: 0, to: progress)
                .stroke(tint, style: StrokeStyle(lineWidth: lineWidth, lineCap: .round))
                .rotationEffect(.degrees(-90))

            Text(SOCFormatting.format(entry.soc))
                .font(.system(size: diameter * 0.28, weight: .bold, design: .rounded))
                .foregroundStyle(tint)
                .minimumScaleFactor(0.5)
                .lineLimit(1)
                .redacted(reason: entry.isPlaceholder ? .placeholder : [])
        }
        .frame(width: diameter, height: diameter)
    }
}

#if DEBUG
#Preview("73%") {
    SOCRing(entry: WidgetFixtures.entry(), diameter: 110, lineWidth: 10)
        .padding()
}

#Preview("full") {
    SOCRing(entry: WidgetFixtures.entry(soc: 100), diameter: 110, lineWidth: 10)
        .padding()
}

#Preview("offline") {
    SOCRing(entry: WidgetFixtures.entry(staleness: .offline), diameter: 110, lineWidth: 10)
        .padding()
}
#endif
