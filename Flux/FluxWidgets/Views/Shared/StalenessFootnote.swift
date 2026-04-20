import FluxCore
import SwiftUI

struct StalenessFootnote: View {
    let entry: StatusEntry

    @ViewBuilder
    var body: some View {
        if entry.staleness != .fresh, let env = entry.envelope {
            Text(env.fetchedAt, style: .relative)
                .font(.caption2)
                .foregroundStyle(.secondary)
        } else if entry.staleness == .offline {
            Text("Offline")
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
    }
}

#if DEBUG
#Preview("stale") {
    StalenessFootnote(entry: WidgetFixtures.entry(staleness: .stale, ageMinutes: 60))
        .padding()
}

#Preview("offline") {
    StalenessFootnote(entry: WidgetFixtures.entry(staleness: .offline, ageMinutes: 240))
        .padding()
}

#Preview("fresh (hidden)") {
    StalenessFootnote(entry: WidgetFixtures.entry())
        .padding()
}
#endif
