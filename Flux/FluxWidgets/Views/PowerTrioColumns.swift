import FluxCore
import SwiftUI

struct PowerTrioColumns: View {
    let entry: StatusEntry

    private static let suite = "group.me.nore.ig.flux"

    private var loadAlertThreshold: Double {
        guard let defaults = UserDefaults(suiteName: Self.suite) else { return 3000 }
        let stored = defaults.double(forKey: "loadAlertThreshold")
        return stored > 0 ? stored : 3000
    }

    private var offpeakStart: String {
        entry.offpeak?.windowStart ?? OffpeakData.defaultWindowStart
    }

    private var offpeakEnd: String {
        entry.offpeak?.windowEnd ?? OffpeakData.defaultWindowEnd
    }

    var body: some View {
        let offline = entry.staleness == .offline
        let solarColor: Color = offline
            ? .secondary
            : (entry.ppv > 0 ? .green : .secondary)
        let loadColor: Color = offline
            ? .secondary
            : (entry.pload > loadAlertThreshold ? .red : .primary)
        let gridColor: Color = offline
            ? .secondary
            : GridColor.forGrid(
                pgrid: entry.pgrid,
                pgridSustained: entry.live?.pgridSustained ?? false,
                offpeakWindowStart: offpeakStart,
                offpeakWindowEnd: offpeakEnd,
                now: entry.date
            ).color

        HStack(alignment: .top, spacing: 12) {
            column(title: "Solar", value: entry.ppv, color: solarColor)
            column(title: "Load", value: entry.pload, color: loadColor)
            column(title: entry.gridTitle, value: entry.pgrid, color: gridColor)
        }
    }

    private func column(title: String, value: Double, color: Color) -> some View {
        VStack(spacing: 2) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(PowerFormatting.format(value))
                .font(.subheadline)
                .foregroundStyle(color)
                .redacted(reason: entry.isPlaceholder ? .placeholder : [])
        }
        .frame(maxWidth: .infinity)
    }
}
